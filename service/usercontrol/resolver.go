package usercontrol

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var ErrPolicyRuntime = errors.New("user control policy unavailable")

const cacheTTL = 15 * time.Second

// Effective is the fully merged policy for one user at a point in time.
type Effective struct {
	Limits         Limits
	Sources        []string
	PolicyIDs      []int
	Timezone       string
	ScheduleHits   []string
	HasPolicy      bool
	HasScopedRules bool
	ResolvedAt     time.Time
}

type cacheEntry struct {
	effective *Effective
	epoch     int64
	expires   time.Time
}

var (
	cacheMu sync.RWMutex
	cache   = make(map[int]cacheEntry)
)

// Resolve returns the merged policy for a user. Results are cached for a short
// TTL and invalidated immediately in this process whenever a policy changes.
func Resolve(userID int, role int) (*Effective, error) {
	if userID <= 0 || role == common.RoleRootUser {
		return &Effective{Timezone: DefaultTimezone, ResolvedAt: time.Now()}, nil
	}
	epoch := model.UserControlEpoch()
	now := time.Now()

	cacheMu.RLock()
	entry, ok := cache[userID]
	cacheMu.RUnlock()
	// Schedule boundaries have minute precision. Never reuse a resolution from
	// the previous minute even when the ordinary cache TTL has not expired.
	if ok && entry.epoch == epoch && now.Before(entry.expires) && now.Unix()/60 == entry.effective.ResolvedAt.Unix()/60 {
		return entry.effective, nil
	}

	effective, err := resolveAt(userID, role, now)
	if err != nil {
		return nil, err
	}
	cacheMu.Lock()
	if len(cache) > 200000 {
		cache = make(map[int]cacheEntry)
	}
	cache[userID] = cacheEntry{effective: effective, epoch: epoch, expires: now.Add(cacheTTL)}
	cacheMu.Unlock()
	return effective, nil
}

// ResolveAt resolves a user's policy for an explicit instant. It is used by the
// simulator and effective-policy view so schedule rules can be inspected
// without waiting for wall-clock time. It intentionally bypasses the cache.
func ResolveAt(userID int, role int, at time.Time) (*Effective, error) {
	if at.IsZero() {
		at = time.Now()
	}
	return resolveAt(userID, role, at)
}

func resolveAt(userID int, role int, now time.Time) (*Effective, error) {
	if userID <= 0 || role == common.RoleRootUser {
		return &Effective{Timezone: DefaultTimezone, ResolvedAt: now}, nil
	}
	sources, err := model.UserControlSourcesForUser(userID, now.Unix())
	if err != nil {
		return nil, err
	}

	effective := &Effective{Timezone: DefaultTimezone, ResolvedAt: now}
	var merged Limits
	for _, source := range sources {
		limits, err := ParseLimits(source.Policy.Limits)
		if err != nil {
			return nil, fmt.Errorf("policy %q: %w", source.Policy.Name, err)
		}
		schedule, err := ParseSchedule(source.Policy.Schedule)
		if err != nil {
			return nil, fmt.Errorf("policy %q schedule: %w", source.Policy.Name, err)
		}
		if len(limits.ScopedRules) > 0 {
			effective.HasScopedRules = true
		}
		if schedule != nil {
			if schedule.Default != nil && len(schedule.Default.ScopedRules) > 0 {
				effective.HasScopedRules = true
			}
			for _, rule := range schedule.Rules {
				if len(rule.Limits.ScopedRules) > 0 {
					effective.HasScopedRules = true
				}
			}
		}
		active, hits, timezone, err := schedule.ActiveLimitsForUser(now, limits, userID)
		if err != nil {
			return nil, fmt.Errorf("policy %q schedule: %w", source.Policy.Name, err)
		}
		loc, _ := time.LoadLocation(timezone)
		active, poolHits, selectedID, err := resolvePool(active, now, loc, userID, source.Policy.Id)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrPolicyRuntime, err)
		}
		if selectedID > 0 {
			effective.PolicyIDs = append(effective.PolicyIDs, selectedID)
			effective.HasScopedRules = true
		}
		hits = append(hits, poolHits...)
		if timezone != "" {
			effective.Timezone = timezone
		}
		if source.Replace {
			merged = Limits{}
		}
		for i := range active.ScopedRules {
			active.ScopedRules[i].Namespace = fmt.Sprintf("%s:%d:%d", source.Source, source.SourceId, source.Policy.Id)
			active.ScopedRules[i].PolicyPriority = source.Policy.Priority
			active.ScopedRules[i].Override = source.Source == "override"
		}
		merged = Merge(merged, active)
		effective.HasPolicy = true
		if source.Policy.Id > 0 {
			effective.PolicyIDs = append(effective.PolicyIDs, source.Policy.Id)
		}
		effective.Sources = append(effective.Sources, sourceLabel(source))
		effective.ScheduleHits = append(effective.ScheduleHits, hits...)
	}
	effective.Limits = merged
	return effective, nil
}

func sourceLabel(source model.UserControlSource) string {
	name := source.Policy.Name
	switch source.Source {
	case "list":
		return fmt.Sprintf("list:%s", name)
	case "subscription":
		return fmt.Sprintf("subscription:%s", name)
	case "override":
		return fmt.Sprintf("override:%s", name)
	default:
		return fmt.Sprintf("%s:%s", source.Source, name)
	}
}

// Invalidate drops every cached resolution in this process.
func Invalidate() {
	cacheMu.Lock()
	cache = make(map[int]cacheEntry)
	cacheMu.Unlock()
}

func (e *Effective) Blocked() bool {
	return e != nil && e.Limits.Blocked != nil && *e.Limits.Blocked
}

func (e *Effective) RelayAllowed() bool {
	return e == nil || e.Limits.AllowRelay == nil || *e.Limits.AllowRelay
}

func (e *Effective) PlaygroundAllowed() bool {
	return e == nil || e.Limits.AllowPlayground == nil || *e.Limits.AllowPlayground
}

func (e *Effective) Rate() RateLimits {
	if e == nil || e.Limits.Rate == nil {
		return RateLimits{}
	}
	return *e.Limits.Rate
}

func (e *Effective) Requests() RequestLimits {
	if e == nil || e.Limits.Requests == nil {
		return RequestLimits{}
	}
	return *e.Limits.Requests
}

func (e *Effective) Usage() UsageLimits {
	if e == nil || e.Limits.Usage == nil {
		return UsageLimits{}
	}
	return *e.Limits.Usage
}

func (e *Effective) APIKeys() APIKeyLimits {
	if e == nil || e.Limits.APIKeys == nil {
		return APIKeyLimits{}
	}
	return *e.Limits.APIKeys
}

// Location returns the timezone used for day/month windows.
func (e *Effective) Location() *time.Location {
	name := DefaultTimezone
	if e != nil && e.Timezone != "" {
		name = e.Timezone
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}
