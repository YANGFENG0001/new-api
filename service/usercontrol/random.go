package usercontrol

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// RandomWindow alternates continuous idle and active intervals. A persisted
// seed makes decisions stable across requests, restarts and gateway replicas.
type RandomWindow struct {
	Enabled   bool   `json:"enabled"`
	ActiveMin int    `json:"active_min"`
	ActiveMax int    `json:"active_max"`
	IdleMin   int    `json:"idle_min"`
	IdleMax   int    `json:"idle_max"`
	Seed      string `json:"seed,omitempty"`
}

type RandomPool struct {
	PolicyIDs []int  `json:"policy_ids"`
	HoldMin   int    `json:"hold_min"`
	HoldMax   int    `json:"hold_max"`
	Seed      string `json:"seed,omitempty"`
}

func randomMinutes(seed string, index, low, high int) int {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", seed, index)))
	return low + int(binary.BigEndian.Uint64(hash[:8])%uint64(high-low+1))
}

func validMinuteRange(low, high int) bool { return low >= 1 && high >= low && high <= 1440 }

func (r *RandomWindow) Active(now, start time.Time, userID int, ruleID string) bool {
	if !validMinuteRange(r.ActiveMin, r.ActiveMax) || !validMinuteRange(r.IdleMin, r.IdleMax) {
		return false
	}
	seed := fmt.Sprintf("%s:%d:%s:%d", r.Seed, userID, ruleID, start.Unix())
	elapsed := int(now.Sub(start) / time.Minute)
	// A window is at most 25h on DST days; every interval is at least 1m.
	for i, offset := 0, 0; offset <= elapsed && i < 3000; i++ {
		active := i%2 == 1
		low, high := r.IdleMin, r.IdleMax
		if active {
			low, high = r.ActiveMin, r.ActiveMax
		}
		offset += randomMinutes(seed, i, low, high)
		if elapsed < offset {
			return active
		}
	}
	return false
}

func normalizeRandomPool(p *RandomPool) error {
	if p == nil {
		return nil
	}
	if len(p.PolicyIDs) == 0 || len(p.PolicyIDs) > 64 || !validMinuteRange(p.HoldMin, p.HoldMax) || len(p.Seed) > 128 {
		return fmt.Errorf("%w: invalid random strategy pool", ErrInvalidInput)
	}
	p.PolicyIDs = normalizePositiveInts(p.PolicyIDs)
	for _, id := range p.PolicyIDs {
		if id <= 0 {
			return fmt.Errorf("%w: invalid pool policy ID", ErrInvalidInput)
		}
	}
	if p.Seed == "" {
		p.Seed = common.GetUUID()
	}
	return nil
}

// Pools are ordinary named/bindable policy records with a random_pool payload.
// Members must be complete non-pool policies; nesting is deliberately rejected.
func ValidatePoolReferences(l Limits, schedule *Schedule, selfID int) error {
	if schedule != nil {
		if schedule.Default != nil && schedule.Default.RandomPool != nil {
			return fmt.Errorf("%w: pools belong in base limits", ErrInvalidInput)
		}
		for _, rule := range schedule.Rules {
			if rule.Limits.RandomPool != nil {
				return fmt.Errorf("%w: pools belong in base limits", ErrInvalidInput)
			}
		}
	}
	if l.RandomPool == nil {
		return nil
	}
	for _, id := range l.RandomPool.PolicyIDs {
		if id == selfID {
			return fmt.Errorf("%w: pool cannot reference itself", ErrInvalidInput)
		}
		policy, err := model.GetUserControlPolicy(id)
		if err != nil {
			return fmt.Errorf("%w: pool member does not exist", ErrInvalidInput)
		}
		limits, err := ParseLimits(policy.Limits)
		if err != nil || limits.RandomPool != nil {
			return fmt.Errorf("%w: nested pools are not supported", ErrInvalidInput)
		}
	}
	if selfID > 0 {
		used, err := PoolReferences(selfID)
		if err != nil {
			return err
		}
		if used {
			return fmt.Errorf("%w: a pool member cannot become a pool", ErrInvalidInput)
		}
	}
	return nil
}

func PoolReferences(id int) (bool, error) {
	if used, err := ApplicationReferences("policy", id); err != nil || used {
		return used, err
	}
	policies, err := model.ListUserControlPolicies()
	if err != nil {
		return false, err
	}
	for _, p := range policies {
		l, err := ParseLimits(p.Limits)
		if err != nil {
			return false, err
		}
		if l.RandomPool != nil && containsInt(l.RandomPool.PolicyIDs, id) {
			return true, nil
		}
	}
	return false, nil
}

// resolvePool adds one complete selected policy, keeping the wrapper baseline.
// Selection is fixed for a random continuous holding interval, not per request.
func resolvePool(active Limits, now time.Time, loc *time.Location, userID int, poolID int) (Limits, []string, int, error) {
	p := active.RandomPool
	if p == nil {
		return active, nil, 0, nil
	}
	if !validMinuteRange(p.HoldMin, p.HoldMax) {
		return Limits{}, nil, 0, fmt.Errorf("invalid pool interval")
	}
	var members []model.UserControlPolicy
	for _, id := range p.PolicyIDs {
		member, err := model.GetUserControlPolicy(id)
		if err != nil {
			return Limits{}, nil, 0, err
		}
		if member.Enabled {
			members = append(members, *member)
		}
	}
	if len(members) == 0 {
		return Limits{}, nil, 0, fmt.Errorf("pool has no enabled members")
	}
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	seed := fmt.Sprintf("%s:%d:%d:%d", p.Seed, userID, poolID, start.Unix())
	elapsed, offset, segment := int(now.Sub(start)/time.Minute), 0, 0
	for ; segment < 3000; segment++ {
		offset += randomMinutes(seed, segment, p.HoldMin, p.HoldMax)
		if elapsed < offset {
			break
		}
	}
	member := members[randomMinutes(seed+":member", segment, 0, len(members)-1)]
	limits, err := ParseLimits(member.Limits)
	if err != nil || limits.RandomPool != nil {
		return Limits{}, nil, 0, fmt.Errorf("invalid nested pool")
	}
	schedule, err := ParseSchedule(member.Schedule)
	if err != nil {
		return Limits{}, nil, 0, err
	}
	limits, hits, _, err := schedule.ActiveLimitsForUser(now, limits, userID)
	if err != nil || limits.RandomPool != nil {
		return Limits{}, nil, 0, fmt.Errorf("invalid pool member schedule")
	}
	active.RandomPool = nil
	for i := range limits.ScopedRules {
		limits.ScopedRules[i].ID = fmt.Sprintf("pool-%d-%s", member.Id, limits.ScopedRules[i].ID)
	}
	hits = append(hits, fmt.Sprintf("pool:%d:policy:%d:segment:%d", poolID, member.Id, segment))
	return Merge(active, limits), hits, member.Id, nil
}
