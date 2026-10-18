package usercontrol

import (
	"fmt"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type PoolMember struct {
	PolicyID    int `json:"policy_id"`
	Probability int `json:"probability"`
}

// Probabilities use basis points (10000 = 100%) to avoid floating-point drift.
type PoolTiming struct {
	Probability int `json:"probability"`
	HoldMin     int `json:"hold_min"`
	HoldMax     int `json:"hold_max"`
}
type PoolConfig struct {
	PoolTiming
	Members       []PoolMember          `json:"members"`
	Schedule      *Schedule             `json:"schedule,omitempty"`
	WindowOptions map[string]PoolTiming `json:"window_options,omitempty"`
	Seed          string                `json:"seed,omitempty"`
}
type PoolPayload struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Enabled     bool       `json:"enabled"`
	Priority    int        `json:"priority"`
	Version     int64      `json:"version"`
	Config      PoolConfig `json:"config"`
}
type PoolView struct {
	Pool   model.UserControlPool `json:"pool"`
	Config PoolConfig            `json:"config"`
}

func BehaviorOnly(policy model.UserControlPolicy) bool {
	limits, err := ParseLimits(policy.Limits)
	return err == nil && limits.RandomPool == nil && (policy.Schedule == "" || policy.Schedule == "null")
}

func ValidatePoolConfig(c *PoolConfig) error {
	if c.Probability < 0 || c.Probability > 10000 || !validMinuteRange(c.HoldMin, c.HoldMax) || len(c.Seed) > 128 {
		return fmt.Errorf("%w: invalid pool probability or holding duration", ErrInvalidInput)
	}
	if len(c.Members) == 0 || len(c.Members) > 64 {
		return fmt.Errorf("%w: select 1..64 pool members", ErrInvalidInput)
	}
	total := 0
	seen := map[int]bool{}
	for _, m := range c.Members {
		if m.PolicyID <= 0 || m.Probability < 0 || m.Probability > 10000 || seen[m.PolicyID] {
			return fmt.Errorf("%w: invalid or duplicate pool member", ErrInvalidInput)
		}
		seen[m.PolicyID] = true
		total += m.Probability
		policy, err := model.GetUserControlPolicy(m.PolicyID)
		if err != nil || !BehaviorOnly(*policy) {
			return fmt.Errorf("%w: pool members must be existing behavior-only policies", ErrInvalidInput)
		}
	}
	if total != 10000 {
		return fmt.Errorf("%w: member probabilities must total 100 percent", ErrInvalidInput)
	}
	if c.Schedule != nil {
		if err := NormalizeSchedule(c.Schedule); err != nil {
			return err
		}
		if c.Schedule.Default != nil {
			return fmt.Errorf("%w: pools cannot contain fallback behavior", ErrInvalidInput)
		}
		for _, r := range c.Schedule.Rules {
			if !r.Limits.Empty() || r.Random != nil {
				return fmt.Errorf("%w: pool windows contain timing only", ErrInvalidInput)
			}
		}
	}
	for id, opt := range c.WindowOptions {
		found := false
		if c.Schedule != nil {
			for _, r := range c.Schedule.Rules {
				if r.ID == id {
					found = true
				}
			}
		}
		if !found || opt.Probability < 0 || opt.Probability > 10000 || !validMinuteRange(opt.HoldMin, opt.HoldMax) {
			return fmt.Errorf("%w: invalid pool window options", ErrInvalidInput)
		}
	}
	if c.Seed == "" {
		c.Seed = common.GetUUID()
	}
	return nil
}

func BuildPool(payload PoolPayload, existing *model.UserControlPool, actor int) (*model.UserControlPool, error) {
	name, err := validateName(payload.Name)
	if err != nil {
		return nil, err
	}
	// Clients cannot rotate the random seed by accidentally omitting it.
	if existing != nil {
		var old PoolConfig
		if err := common.UnmarshalJsonStr(existing.Config, &old); err != nil {
			return nil, err
		}
		payload.Config.Seed = old.Seed
	}
	if err := ValidatePoolConfig(&payload.Config); err != nil {
		return nil, err
	}
	raw, err := common.Marshal(payload.Config)
	if err != nil {
		return nil, err
	}
	row := &model.UserControlPool{CreatedBy: actor}
	if existing != nil {
		*row = *existing
		if payload.Version != existing.Version {
			return nil, fmt.Errorf("%w: pool changed, reload before saving", ErrInvalidInput)
		}
	}
	row.Name = name
	row.Description = trimDescription(payload.Description)
	row.Enabled = payload.Enabled
	row.Priority = payload.Priority
	row.Config = string(raw)
	row.UpdatedBy = actor
	return row, nil
}

func poolWindow(config PoolConfig, now time.Time, anchor time.Time) (PoolTiming, time.Time, string, bool, error) {
	if config.Schedule == nil {
		return config.PoolTiming, anchor, "all", true, nil
	}
	loc, err := time.LoadLocation(config.Schedule.Timezone)
	if err != nil {
		return PoolTiming{}, anchor, "", false, err
	}
	rules := append([]ScheduleRule(nil), config.Schedule.Rules...)
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		return rules[i].ID < rules[j].ID
	})
	for _, rule := range rules {
		check := rule
		check.Limits = Limits{Blocked: boolPtr(true)}
		s := &Schedule{Timezone: config.Schedule.Timezone, Rules: []ScheduleRule{check}}
		_, hits, _, err := s.ActiveLimits(now, Limits{})
		if err != nil {
			return PoolTiming{}, anchor, "", false, err
		}
		if len(hits) == 0 {
			continue
		}
		local := now.In(loc)
		start, _ := parseClock(rule.Start)
		end, _ := parseClock(rule.End)
		if start == end {
			start = 0
			end = 0
		}
		if start > end && local.Hour()*60+local.Minute() < end {
			local = local.AddDate(0, 0, -1)
		}
		anchor = time.Date(local.Year(), local.Month(), local.Day(), start/60, start%60, 0, 0, loc)
		opt := config.PoolTiming
		if override, ok := config.WindowOptions[rule.ID]; ok {
			opt = override
		}
		return opt, anchor, rule.ID, true, nil
	}
	return config.PoolTiming, anchor, "", false, nil
}

// The repeatable duration cycle permits exact O(256) seeking even years after
// creation. Decision draws use the full segment index, not the cycle position.
// No midnight reset, process-local RNG, or per-request resampling is involved.
func poolSegment(seed string, elapsed int64, low, high int) int64 {
	if elapsed < 0 {
		elapsed = 0
	}
	var lengths [256]int64
	var total int64
	for i := range lengths {
		lengths[i] = int64(randomMinutes(seed+":duration", i, low, high))
		total += lengths[i]
	}
	segment := elapsed / total * 256
	remaining := elapsed % total
	for _, length := range lengths {
		if remaining < length {
			return segment
		}
		remaining -= length
		segment++
	}
	return segment
}

func resolveScheduledPool(pool model.UserControlPool, userID int, now time.Time) (model.UserControlPolicy, Limits, []string, string, error) {
	var config PoolConfig
	if err := common.UnmarshalJsonStr(pool.Config, &config); err != nil {
		return model.UserControlPolicy{}, Limits{}, nil, "", err
	}
	timezone := DefaultTimezone
	if config.Schedule != nil {
		timezone = config.Schedule.Timezone
	}
	opt, anchor, window, matched, err := poolWindow(config, now, time.Unix(pool.CreatedAt, 0).Truncate(time.Minute))
	prefix := fmt.Sprintf("pool:%d:version:%d", pool.Id, pool.Version)
	if err != nil {
		return model.UserControlPolicy{}, Limits{}, nil, timezone, err
	}
	if !matched {
		return model.UserControlPolicy{}, Limits{}, []string{prefix + ":outside_window"}, timezone, nil
	}
	if !validMinuteRange(opt.HoldMin, opt.HoldMax) || opt.Probability < 0 || opt.Probability > 10000 {
		return model.UserControlPolicy{}, Limits{}, nil, timezone, ErrInvalidInput
	}
	seed := fmt.Sprintf("%s:%d:%d:%d:%s:%d", config.Seed, pool.Id, pool.Version, userID, window, anchor.Unix())
	segment := poolSegment(seed, int64(now.Sub(anchor)/time.Minute), opt.HoldMin, opt.HoldMax)
	drawSeed := fmt.Sprintf("%s:%d", seed, segment)
	hit := randomMinutes(drawSeed+":hit", 0, 0, 9999)
	draw := randomMinutes(drawSeed+":member", 0, 0, 9999)
	decision := fmt.Sprintf("%s:window:%s:segment:%d:probability:%d:draw:%d", prefix, window, segment, opt.Probability, hit)
	if hit >= opt.Probability {
		return model.UserControlPolicy{}, Limits{}, []string{decision + ":miss"}, timezone, nil
	}
	cumulative := 0
	for _, member := range config.Members {
		cumulative += member.Probability
		if draw >= cumulative {
			continue
		}
		policy, err := model.GetUserControlPolicy(member.PolicyID)
		if err != nil {
			return model.UserControlPolicy{}, Limits{}, nil, timezone, err
		}
		decision += fmt.Sprintf(":member_draw:%d:policy:%d", draw, policy.Id)
		if !policy.Enabled {
			return model.UserControlPolicy{}, Limits{}, []string{decision + ":disabled"}, timezone, nil
		}
		if !BehaviorOnly(*policy) {
			return model.UserControlPolicy{}, Limits{}, nil, timezone, ErrPolicyRuntime
		}
		limits, err := ParseLimits(policy.Limits)
		return *policy, limits, []string{decision + ":selected"}, timezone, err
	}
	return model.UserControlPolicy{}, Limits{}, nil, timezone, ErrPolicyRuntime
}

func ApplicationReferences(kind string, id int) (bool, error) {
	apps, err := model.ListUserControlApplications()
	if err != nil {
		return false, err
	}
	for _, app := range apps {
		if app.UseLegacy {
			continue
		}
		var targets []model.UserControlTarget
		if err := common.UnmarshalJsonStr(app.Targets, &targets); err != nil {
			return false, err
		}
		for _, t := range targets {
			if t.Kind == kind && t.Id == id {
				return true, nil
			}
		}
	}
	if kind == "policy" {
		pools, err := model.ListUserControlPools()
		if err != nil {
			return false, err
		}
		for _, pool := range pools {
			var cfg PoolConfig
			if err := common.UnmarshalJsonStr(pool.Config, &cfg); err != nil {
				return false, err
			}
			for _, m := range cfg.Members {
				if m.PolicyID == id {
					return true, nil
				}
			}
		}
	}
	return false, nil
}
