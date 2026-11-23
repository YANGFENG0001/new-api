package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Public usage DTOs intentionally do not embed pools, channels, external IDs,
// membership lists, or administrator notes.
type SubscriptionCycleView struct {
	PendingRule  *SubscriptionPlanCycleSummary `json:"pending_rule,omitempty"`
	LimitUnknown bool                          `json:"limit_unknown,omitempty"`
	Key          string                        `json:"key"`
	Name         string                        `json:"name"`
	Seconds      int64                         `json:"seconds"`
	Shared       bool                          `json:"shared"`
	Unlimited    bool                          `json:"unlimited"`
	Limit        int64                         `json:"limit"`
	Used         int64                         `json:"used"`
	Reserved     int64                         `json:"reserved"`
	Adjustment   int64                         `json:"adjustment"`
	StartTime    int64                         `json:"start_time"`
	NextReset    int64                         `json:"next_reset"`
	Pending      bool                          `json:"pending"`
}

type SubscriptionUsageView struct {
	SubscriptionID int                     `json:"subscription_id"`
	PlanID         int                     `json:"plan_id"`
	EndTime        int64                   `json:"end_time"`
	Status         string                  `json:"status"`
	Pending        bool                    `json:"pending"`
	Managed        bool                    `json:"managed"`
	Cycles         []SubscriptionCycleView `json:"cycles"`
}

type SubscriptionPoolOverview struct {
	SubscriptionIDs  []int                             `json:"subscription_ids"`
	ID               int                               `json:"id"`
	Name             string                            `json:"name"`
	EndTime          int64                             `json:"end_time"`
	UpdatedAt        int64                             `json:"updated_at"`
	Cycles           []SubscriptionCycleView           `json:"cycles"`
	Observations     []SubscriptionObservationView     `json:"observations"`
	SyncObservations []SubscriptionSyncObservationView `json:"sync_observations"`
}

type SubscriptionObservationView struct {
	Key         string `json:"key"`
	Source      string `json:"source"`
	Unit        string `json:"unit"`
	Used        int64  `json:"used"`
	Limit       int64  `json:"limit"`
	SampledAt   int64  `json:"sampled_at"`
	WindowStart int64  `json:"window_start"`
	WindowEnd   int64  `json:"window_end"`
}

// Purchase/admin catalogs expose policy terms, never pool identity or routing.
type SubscriptionPlanAllocationSummary struct {
	Mode         string                         `json:"mode"`
	Available    bool                           `json:"available"`
	FixedEndTime int64                          `json:"fixed_end_time"`
	Cycles       []SubscriptionPlanCycleSummary `json:"cycles"`
}

type SubscriptionPlanCycleSummary struct {
	Name      string `json:"name"`
	Seconds   int64  `json:"seconds"`
	Limit     int64  `json:"limit"`
	Unlimited bool   `json:"unlimited"`
	Shared    bool   `json:"shared"`
}

func GetSubscriptionPlanAllocationSummaries(ids []int) (map[int]SubscriptionPlanAllocationSummary, error) {
	result := make(map[int]SubscriptionPlanAllocationSummary, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	for _, id := range ids {
		result[id] = SubscriptionPlanAllocationSummary{Mode: "legacy", Available: true, Cycles: []SubscriptionPlanCycleSummary{}}
	}
	var configs []SubscriptionPlanAllocation
	if err := DB.Where("plan_id IN ?", ids).Find(&configs).Error; err != nil {
		return nil, err
	}
	poolIDs := []int{}
	for _, cfg := range configs {
		if cfg.Mode == "fixed" {
			poolIDs = append(poolIDs, cfg.PoolID)
		}
	}
	var pools []SubscriptionPool
	if len(poolIDs) > 0 {
		if err := DB.Where("id IN ?", poolIDs).Find(&pools).Error; err != nil {
			return nil, err
		}
	}
	byID := make(map[int]SubscriptionPool, len(pools))
	owners := make([]string, 0, len(pools))
	for _, pool := range pools {
		byID[pool.Id] = pool
		owners = append(owners, allocationOwner(0, pool.Id))
	}
	var states []SubscriptionCycleState
	if len(owners) > 0 {
		if err := DB.Where("owner IN ?", owners).Find(&states).Error; err != nil {
			return nil, err
		}
	}
	byOwner := map[string]map[string]SubscriptionCycleState{}
	for _, state := range states {
		if byOwner[state.Owner] == nil {
			byOwner[state.Owner] = map[string]SubscriptionCycleState{}
		}
		byOwner[state.Owner][state.CycleKey] = state
	}
	for _, cfg := range configs {
		if cfg.Mode == "legacy" {
			continue
		}
		summary := SubscriptionPlanAllocationSummary{Mode: cfg.Mode, Available: true, FixedEndTime: cfg.FixedEndTime, Cycles: []SubscriptionPlanCycleSummary{}}
		raw := cfg.Rules
		if cfg.Mode == "fixed" {
			pool, found := byID[cfg.PoolID]
			summary.Available = found && pool.Enabled && pool.EndTime > GetDBTimestamp()
			raw = pool.Rules
			if cfg.FixedEndTime == 0 {
				summary.FixedEndTime = pool.EndTime
			} else if found {
				summary.FixedEndTime = min(cfg.FixedEndTime, pool.EndTime)
			}
		}
		if summary.FixedEndTime > 0 && summary.FixedEndTime <= GetDBTimestamp() {
			summary.Available = false
		}
		var rules []SubscriptionCycleRule
		if raw != "" {
			if err := common.UnmarshalJsonStr(raw, &rules); err != nil {
				return nil, err
			}
		}
		for _, rule := range rules {
			if cfg.Mode == "fixed" {
				if state, found := byOwner[allocationOwner(0, cfg.PoolID)][rule.Key]; found {
					// Catalog reads do not advance windows or create counters. Until
					// the boundary, new members also receive the active pool rule;
					// legacy quota-only pending rules are already effective.
					active := state.Rule
					if state.PendingRule != "" {
						if state.EndTime <= GetDBTimestamp() {
							active = state.PendingRule
						} else {
							var currentRule, pendingRule SubscriptionCycleRule
							if common.UnmarshalJsonStr(state.Rule, &currentRule) == nil &&
								common.UnmarshalJsonStr(state.PendingRule, &pendingRule) == nil &&
								subscriptionCycleQuotaOnlyChange(currentRule, pendingRule) {
								active = state.PendingRule
							}
						}
					}
					if err := common.UnmarshalJsonStr(active, &rule); err != nil {
						return nil, err
					}
				}
			}
			if rule.Enabled {
				summary.Cycles = append(summary.Cycles, SubscriptionPlanCycleSummary{Name: rule.Name, Seconds: rule.Seconds, Limit: rule.Limit, Unlimited: rule.Unlimited, Shared: rule.Shared})
			}
		}
		result[cfg.PlanID] = summary
	}
	return result, nil
}

func subscriptionCycleViewsTx(tx *gorm.DB, owner string, raw string, userID int, aggregateOnly bool, now int64) ([]SubscriptionCycleView, error) {
	var rules []SubscriptionCycleRule
	if raw != "" {
		if err := common.UnmarshalJsonStr(raw, &rules); err != nil {
			return nil, err
		}
	}
	views := []SubscriptionCycleView{}
	for _, r := range rules {
		state, err := subscriptionCycleStateTx(tx, owner, r, now)
		if err != nil {
			return nil, err
		}
		if err := common.UnmarshalJsonStr(state.Rule, &r); err != nil {
			return nil, err
		}
		if !r.Enabled {
			continue
		}
		effectiveUser := userID
		if r.Shared || aggregateOnly {
			effectiveUser = 0
		}
		usage, err := cycleUsageTx(tx, state, effectiveUser)
		if err != nil {
			return nil, err
		}
		limit := r.Limit
		unlimited := r.Unlimited
		if aggregateOnly && !r.Shared {
			limit = r.UpstreamLimit
			unlimited = limit == 0
		}
		var pending *SubscriptionPlanCycleSummary
		if state.PendingRule != "" {
			var next SubscriptionCycleRule
			if err := common.UnmarshalJsonStr(state.PendingRule, &next); err != nil {
				return nil, err
			}
			limit, unlimited := next.Limit, next.Unlimited
			if aggregateOnly && !next.Shared {
				limit, unlimited = next.UpstreamLimit, next.UpstreamLimit == 0
			}
			pending = &SubscriptionPlanCycleSummary{Name: SubscriptionCycleName(next.Name, next.Seconds), Seconds: next.Seconds, Limit: limit, Unlimited: unlimited, Shared: next.Shared}
		}
		views = append(views, SubscriptionCycleView{PendingRule: pending, Key: r.Key, Name: r.Name, LimitUnknown: aggregateOnly && !r.Shared && r.UpstreamLimit == 0, Seconds: r.Seconds, Shared: r.Shared, Unlimited: unlimited, Limit: limit, Used: usage.Used, Reserved: usage.Reserved, Adjustment: usage.Adjustment, StartTime: state.StartTime, NextReset: state.EndTime, Pending: state.PendingRule != ""})
	}
	return views, nil
}

func GetSubscriptionUsage(userID int) ([]SubscriptionUsageView, error) {
	var subs []UserSubscription
	if err := DB.Where("user_id = ?", userID).Order("id desc").Find(&subs).Error; err != nil {
		return nil, err
	}
	result := []SubscriptionUsageView{}
	for _, sub := range subs {
		view := SubscriptionUsageView{SubscriptionID: sub.Id, PlanID: sub.PlanId, EndTime: sub.EndTime, Status: sub.Status, Cycles: []SubscriptionCycleView{}}
		if sub.Status == "active" && sub.EndTime <= GetDBTimestamp() {
			view.Status = "expired"
		}
		var a SubscriptionAllocation
		if err := DB.Where("subscription_id = ?", sub.Id).Find(&a).Error; err != nil {
			return nil, err
		}
		if a.SubscriptionID == 0 {
			view.Cycles = append(view.Cycles, SubscriptionCycleView{Key: "legacy", Name: "Subscription quota", Unlimited: sub.AmountTotal == 0, Limit: sub.AmountTotal, Used: sub.AmountUsed, StartTime: sub.LastResetTime, NextReset: sub.NextResetTime})
		} else {
			view.Managed = true
			view.Pending = a.Pending
			if view.Status != "active" || a.Pending {
				result = append(result, view)
				continue
			}
			err := DB.Transaction(func(tx *gorm.DB) error {
				if err := lockSubscriptionResource(tx, sub.Id, a.PoolID); err != nil {
					return err
				}
				var current SubscriptionAllocation
				if err := lockForUpdate(tx).Where("subscription_id = ?", sub.Id).First(&current).Error; err != nil {
					return err
				}
				if current.Version != a.Version || current.PoolID != a.PoolID {
					return errors.New("Subscription assignment changed; retry the request")
				}
				raw := a.Rules
				if a.PoolID > 0 {
					var pool SubscriptionPool
					if err := tx.First(&pool, a.PoolID).Error; err != nil {
						return err
					}
					raw = pool.Rules
					view.EndTime = min(view.EndTime, pool.EndTime)
					if view.EndTime <= GetDBTimestamp() {
						view.Status = "expired"
						return nil
					}
					if !pool.Enabled {
						view.Pending = true
					}
				}
				var err error
				view.Cycles, err = subscriptionCycleViewsTx(tx, allocationOwner(sub.Id, a.PoolID), raw, sub.UserId, false, GetDBTimestamp())
				return err
			})
			if err != nil {
				return nil, err
			}
		}
		result = append(result, view)
	}
	return result, nil
}

// GetSubscriptionPoolConfiguration exposes the configured (possibly pending)
// rules with their real current reset heads, never a stale anchor from creation.
func GetSubscriptionPoolConfiguration(poolID int) (SubscriptionPoolInput, error) {
	var view SubscriptionPoolInput
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&view.SubscriptionPool, poolID).Error; err != nil {
			return err
		}
		view.Cycles = []SubscriptionCycleRule{}
		view.Channels = []int{}
		if err := common.UnmarshalJsonStr(view.Rules, &view.Cycles); err != nil {
			return err
		}
		for i, r := range view.Cycles {
			s, err := subscriptionCycleStateTx(tx, allocationOwner(0, poolID), r, GetDBTimestamp())
			if err != nil {
				return err
			}
			view.Cycles[i].NextReset = s.EndTime
		}
		return tx.Model(&SubscriptionPoolChannel{}).Where("pool_id = ?", poolID).Pluck("channel_id", &view.Channels).Error
	})
	return view, err
}

func GetSubscriptionPoolOverview(userID int) ([]SubscriptionPoolOverview, error) {
	var ids []int
	err := DB.Table("subscription_allocations AS a").Distinct("a.pool_id").Joins("JOIN user_subscriptions AS s ON s.id = a.subscription_id").Where("a.user_id = ? AND a.pool_id > 0 AND a.pending = ? AND s.status = ? AND s.end_time > ?", userID, false, "active", GetDBTimestamp()).Scan(&ids).Error
	if err != nil {
		return nil, err
	}
	result := []SubscriptionPoolOverview{}
	for _, id := range ids {
		err := DB.Transaction(func(tx *gorm.DB) error {
			var pool SubscriptionPool
			if err := lockForUpdate(tx).First(&pool, id).Error; err != nil {
				return err
			}
			// Recheck membership under the assignment's resource lock.
			var subscriptionIDs []int
			if err := tx.Table("subscription_allocations AS a").Joins("JOIN user_subscriptions AS s ON s.id = a.subscription_id").Where("a.user_id = ? AND a.pool_id = ? AND a.pending = ? AND s.status = ? AND s.end_time > ?", userID, id, false, "active", GetDBTimestamp()).Order("a.subscription_id").Pluck("a.subscription_id", &subscriptionIDs).Error; err != nil {
				return err
			}
			if len(subscriptionIDs) == 0 || !pool.Enabled || pool.EndTime <= GetDBTimestamp() {
				return nil
			}
			cycles, err := subscriptionCycleViewsTx(tx, allocationOwner(0, id), pool.Rules, 0, true, GetDBTimestamp())
			if err != nil {
				return err
			}
			snapshots := []SubscriptionUpstreamSnapshot{}
			if err := tx.Where("pool_id = ?", id).Order("sampled_at desc, id desc").Limit(64).Find(&snapshots).Error; err != nil {
				return err
			}
			observations := []SubscriptionObservationView{}
			seen := map[string]bool{}
			for _, s := range snapshots {
				if seen[s.CycleKey] {
					continue
				}
				seen[s.CycleKey] = true
				observations = append(observations, SubscriptionObservationView{Key: s.CycleKey, Source: s.Source, Unit: s.Unit, Used: s.Used, Limit: s.Limit, SampledAt: s.SampledAt, WindowStart: s.WindowStart, WindowEnd: s.WindowEnd})
			}
			syncViews, err := subscriptionSyncViewsTx(tx, id, GetDBTimestamp())
			if err != nil {
				return err
			}
			for i := range syncViews {
				syncViews[i].Digest = ""
			}
			result = append(result, SubscriptionPoolOverview{SubscriptionIDs: subscriptionIDs, SyncObservations: syncViews, ID: id, Name: pool.Name, EndTime: pool.EndTime, UpdatedAt: pool.UpdatedAt, Cycles: cycles, Observations: observations})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func GetSubscriptionPoolStatistics(poolID int) ([]SubscriptionCycleView, error) {
	var result []SubscriptionCycleView
	err := DB.Transaction(func(tx *gorm.DB) error {
		var p SubscriptionPool
		if err := lockForUpdate(tx).First(&p, poolID).Error; err != nil {
			return err
		}
		var err error
		result, err = subscriptionCycleViewsTx(tx, allocationOwner(0, poolID), p.Rules, 0, true, GetDBTimestamp())
		return err
	})
	return result, err
}
