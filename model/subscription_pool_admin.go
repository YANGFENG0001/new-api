package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Saves use optimistic versions to prevent one administrator silently replacing
// another's alignment, assignment, or manual calibration.
type SubscriptionPoolInput struct {
	SubscriptionPool
	Cycles      []SubscriptionCycleRule       `json:"cycles"`
	Channels    []int                         `json:"channels"`
	Sync        *SubscriptionSyncPoolInput    `json:"sync,omitempty"`
	SyncBinding *SubscriptionSyncBindingInput `json:"sync_binding,omitempty"`
}

func SaveSubscriptionPool(input SubscriptionPoolInput, actorID int) (*SubscriptionPool, error) {
	if input.Sync != nil {
		if input.SyncBinding != nil {
			return nil, errors.New("Invalid synchronization binding")
		}
		return createSubscriptionSyncPool(input, actorID)
	}
	if input.SyncBinding != nil {
		var saved *SubscriptionPool
		err := DB.Transaction(func(tx *gorm.DB) error {
			// Synchronization workers lock source before pool. Keep the same order.
			var source SubscriptionSyncSource
			if err := lockForUpdate(tx).First(&source, input.SyncBinding.SourceID).Error; err != nil {
				return err
			}
			var err error
			saved, err = saveSubscriptionPool(tx, input, actorID)
			if err != nil {
				return err
			}
			binding := *input.SyncBinding
			binding.PoolID = saved.Id
			if err := saveSubscriptionSyncBinding(tx, binding, actorID); err != nil {
				return err
			}
			return tx.First(saved, saved.Id).Error
		})
		return saved, err
	}
	return saveSubscriptionPool(DB, input, actorID)
}

// Accept a transaction so creating a pool and its synchronization binding is atomic.
func saveSubscriptionPool(db *gorm.DB, input SubscriptionPoolInput, actorID int) (*SubscriptionPool, error) {
	input.Cycles = slices.Clone(input.Cycles)
	if input.Id < 0 || strings.TrimSpace(input.Name) == "" || len(input.Name) > 128 || len(input.Note) > 4096 || input.EndTime <= GetDBTimestamp() || input.EndTime > 253402300799 || len(input.Channels) > 1000 || len(input.ExternalSource) > 64 || len(input.ExternalID) > 128 {
		return nil, errors.New("Invalid upstream pool")
	}
	if err := ValidateSubscriptionCycles(input.Cycles, true); err != nil {
		return nil, err
	}
	if len(input.Cycles) == 0 {
		return nil, errors.New("At least one subscription cycle is required")
	}
	pool := input.SubscriptionPool
	err := db.Transaction(func(tx *gorm.DB) error {
		if pool.Id > 0 {
			var previous SubscriptionPool
			if err := lockForUpdate(tx).First(&previous, pool.Id).Error; err != nil {
				return err
			}
			if pool.Version != previous.Version {
				return errors.New("Upstream pool changed; reload before saving")
			}
			var binding SubscriptionSyncBindingInput
			if input.SyncBinding != nil {
				binding = *input.SyncBinding
			} else {
				if err := tx.Where("pool_id = ?", pool.Id).Find(&binding.SubscriptionSyncBinding).Error; err != nil {
					return err
				}
				if binding.CyclesJSON != "" {
					if err := common.UnmarshalJsonStr(binding.CyclesJSON, &binding.Cycles); err != nil {
						return err
					}
				}
			}
			if binding.Enabled {
				if binding.SyncExpiry && pool.EndTime != previous.EndTime {
					return errors.New("Turn off upstream following before changing synchronized times")
				}
				var previousRules []SubscriptionCycleRule
				if err := common.UnmarshalJsonStr(previous.Rules, &previousRules); err != nil {
					return err
				}
				for _, mapping := range binding.Cycles {
					if !mapping.AlignReset {
						continue
					}
					for index, rule := range input.Cycles {
						if rule.Key != mapping.LocalKey {
							continue
						}
						for _, old := range previousRules {
							if old.Key != rule.Key {
								continue
							}
							state, err := subscriptionCycleStateTx(tx, allocationOwner(0, pool.Id), old, GetDBTimestamp())
							if err != nil {
								return err
							}
							if rule.Seconds != old.Seconds {
								return errors.New("Turn off upstream following before changing synchronized times")
							}
							if rule.NextReset != state.EndTime {
								// An editor may remain open across a natural boundary.
								// Accept only elapsed anchors on this exact cadence;
								// never mistake a new future boundary for a cache refresh.
								if rule.NextReset > GetDBTimestamp() || (state.EndTime-rule.NextReset)%old.Seconds != 0 {
									return errors.New("Turn off upstream following before changing synchronized times")
								}
								input.Cycles[index].NextReset = state.EndTime
							}
						}
					}
				}
			}
			pool.Version++
		} else {
			pool.Version = 1
		}
		raw, err := cycleJSON(input.Cycles)
		if err != nil {
			return err
		}
		pool.Rules = raw
		pool.UpdatedAt = GetDBTimestamp()
		if err := tx.Save(&pool).Error; err != nil {
			return err
		}
		if err := tx.Model(&UserSubscription{}).Where("id IN (?)", tx.Model(&SubscriptionAllocation{}).Select("subscription_id").Where("pool_id = ? AND follow_pool_end = ?", pool.Id, true)).Where("status = ? AND end_time > ?", "active", GetDBTimestamp()).Update("end_time", pool.EndTime).Error; err != nil {
			return err
		}
		if err := configureSubscriptionCyclesTx(tx, allocationOwner(0, pool.Id), input.Cycles, GetDBTimestamp()); err != nil {
			return err
		}
		// A channel is the sentinel for its link set. Serialize relation edits
		// with admission, without imposing a one-channel/one-account assumption.
		var previousChannels []int
		if err := tx.Model(&SubscriptionPoolChannel{}).Where("pool_id = ?", pool.Id).Pluck("channel_id", &previousChannels).Error; err != nil {
			return err
		}
		channelIDs := append(previousChannels, input.Channels...)
		slices.Sort(channelIDs)
		channelIDs = slices.Compact(channelIDs)
		var channels []Channel
		if len(channelIDs) > 0 {
			if err := lockForUpdate(tx).Select("id").Where("id IN ?", channelIDs).Order("id").Find(&channels).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("pool_id = ?", pool.Id).Delete(&SubscriptionPoolChannel{}).Error; err != nil {
			return err
		}
		seen := map[int]bool{}
		for _, id := range input.Channels {
			if seen[id] {
				continue
			}
			seen[id] = true
			if !slices.ContainsFunc(channels, func(channel Channel) bool { return channel.Id == id }) {
				return errors.New("Channel does not exist")
			}
			if err := tx.Create(&SubscriptionPoolChannel{ChannelID: id, PoolID: pool.Id}).Error; err != nil {
				return err
			}
		}
		return tx.Create(&SubscriptionPoolEvent{PoolID: pool.Id, ActorID: actorID, Action: "save", CreatedAt: GetDBTimestamp()}).Error
	})
	return &pool, err
}

func SaveSubscriptionPlanAllocation(cfg SubscriptionPlanAllocation, rules []SubscriptionCycleRule) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		return SaveSubscriptionPlanAllocationTx(tx, cfg, rules)
	})
}

// LockSubscriptionPlanTx serializes plan edits with new-member default changes.
func LockSubscriptionPlanTx(tx *gorm.DB, planID int) error {
	return lockForUpdate(tx).Select("id").First(&SubscriptionPlan{}, planID).Error
}

// The plan and its new-member defaults can be saved in the same transaction.
// A legacy row retains its version so switching modes never deletes history.
func SaveSubscriptionPlanAllocationTx(tx *gorm.DB, cfg SubscriptionPlanAllocation, rules []SubscriptionCycleRule) error {
	if cfg.PlanID <= 0 {
		return errors.New("Invalid parameters")
	}
	if cfg.Mode != "legacy" && cfg.Mode != "independent" && cfg.Mode != "fixed" && cfg.Mode != "assign" {
		return errors.New("Invalid subscription allocation mode")
	}
	if cfg.FixedEndTime < 0 || cfg.FixedEndTime > 253402300799 {
		return errors.New("Invalid subscription expiry")
	}
	if err := ValidateSubscriptionCycles(rules, false); err != nil {
		return err
	}
	if cfg.Mode == "independent" && len(rules) == 0 {
		return errors.New("At least one subscription cycle is required")
	}
	if cfg.Mode != "independent" {
		rules = nil
	}
	raw, err := cycleJSON(rules)
	if err != nil {
		return err
	}
	cfg.Rules = raw
	return tx.Transaction(func(tx *gorm.DB) error {
		if err := LockSubscriptionPlanTx(tx, cfg.PlanID); err != nil {
			return err
		}
		var old SubscriptionPlanAllocation
		res := tx.Where("plan_id = ?", cfg.PlanID).Find(&old)
		if res.Error != nil {
			return res.Error
		}
		if cfg.Version != old.Version {
			return errors.New("Subscription allocation changed; reload before saving")
		}
		cfg.Version++
		if cfg.Mode == "fixed" {
			var pool SubscriptionPool
			if err := tx.First(&pool, cfg.PoolID).Error; err != nil {
				return err
			}
			if !pool.Enabled || pool.EndTime <= GetDBTimestamp() {
				return errors.New("Upstream pool is unavailable")
			}
			var poolRules []SubscriptionCycleRule
			if pool.Rules != "" {
				if err := common.UnmarshalJsonStr(pool.Rules, &poolRules); err != nil {
					return err
				}
			}
			multiplier, overrides, err := ValidateSubscriptionQuotaAllocation(cfg.QuotaMultiplier, cfg.CycleOverrides, poolRules)
			if err != nil {
				return err
			}
			encoded, err := encodeSubscriptionCycleOverrides(overrides)
			if err != nil {
				return err
			}
			cfg.QuotaMultiplier = multiplier
			cfg.CycleOverrides = overrides
			cfg.CycleOverridesJSON = encoded
		} else if cfg.Mode == "assign" {
			cfg.PoolID = 0
			multiplier, overrides, err := ValidateSubscriptionQuotaAllocation(cfg.QuotaMultiplier, cfg.CycleOverrides, nil)
			if err != nil {
				return err
			}
			encoded, err := encodeSubscriptionCycleOverrides(overrides)
			if err != nil {
				return err
			}
			cfg.QuotaMultiplier = multiplier
			cfg.CycleOverrides = overrides
			cfg.CycleOverridesJSON = encoded
		} else {
			cfg.PoolID = 0
			cfg.QuotaMultiplier = 0
			cfg.CycleOverrides = nil
			cfg.CycleOverridesJSON = ""
		}
		return tx.Save(&cfg).Error
	})
}

type SubscriptionPoolAssignmentOptions struct {
	QuotaMultiplier *float64
	CycleOverrides  *[]SubscriptionCycleOverride
}

func resolveAssignmentQuotaAllocation(current SubscriptionAllocation, poolRules []SubscriptionCycleRule, opts SubscriptionPoolAssignmentOptions) (float64, []SubscriptionCycleOverride, string, error) {
	multiplier := current.QuotaMultiplier
	if opts.QuotaMultiplier != nil {
		multiplier = *opts.QuotaMultiplier
	}
	var overrides []SubscriptionCycleOverride
	var err error
	if opts.CycleOverrides != nil {
		overrides = *opts.CycleOverrides
		multiplier, overrides, err = ValidateSubscriptionQuotaAllocation(multiplier, overrides, poolRules)
	} else {
		existing, parseErr := ParseSubscriptionCycleOverrides(current.CycleOverridesJSON)
		if parseErr != nil {
			return 0, nil, "", parseErr
		}
		overrides = filterSubscriptionCycleOverridesForPool(existing, poolRules)
		multiplier, overrides, err = ValidateSubscriptionQuotaAllocation(multiplier, overrides, poolRules)
	}
	if err != nil {
		return 0, nil, "", err
	}
	encoded, err := encodeSubscriptionCycleOverrides(overrides)
	if err != nil {
		return 0, nil, "", err
	}
	return multiplier, overrides, encoded, nil
}

// Assignment changes preserve both pools' existing windows and all inflight
// reservations. Personal counters are keyed by pool+user, not subscription.
func AssignSubscriptionPool(subID, poolID int, version, endTime int64, actorID int, confirm bool, poolVersion int64, expectedCarry ...string) error {
	return AssignSubscriptionPoolWithOptions(subID, poolID, version, endTime, actorID, confirm, poolVersion, SubscriptionPoolAssignmentOptions{}, expectedCarry...)
}

func AssignSubscriptionPoolWithOptions(subID, poolID int, version, endTime int64, actorID int, confirm bool, poolVersion int64, opts SubscriptionPoolAssignmentOptions, expectedCarry ...string) error {
	if subID <= 0 || poolID <= 0 || version < 0 || !confirm {
		return errors.New("Confirm the subscription pool assignment impact")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var hint SubscriptionAllocation
		res := tx.Where("subscription_id = ?", subID).Find(&hint)
		if res.Error != nil {
			return res.Error
		}
		poolIDs := []int{poolID}
		if hint.PoolID > 0 {
			poolIDs = append(poolIDs, hint.PoolID)
		}
		slices.Sort(poolIDs)
		poolIDs = slices.Compact(poolIDs)
		var pools []SubscriptionPool
		if err := lockForUpdate(tx).Where("id IN ?", poolIDs).Order("id").Find(&pools).Error; err != nil {
			return err
		}
		var pool SubscriptionPool
		for _, p := range pools {
			if p.Id == poolID {
				pool = p
			}
		}
		if pool.Id == 0 || !pool.Enabled || pool.EndTime <= GetDBTimestamp() {
			return errors.New("Upstream pool is unavailable")
		}
		if pool.Version != poolVersion {
			return errors.New("Upstream pool changed; reload before saving")
		}
		if hint.SubscriptionID > 0 && hint.PoolID == 0 {
			if err := lockSubscriptionResource(tx, subID, 0); err != nil {
				return err
			}
		}
		var sub UserSubscription
		if err := lockForUpdate(tx).First(&sub, subID).Error; err != nil {
			return err
		}
		if sub.Status != "active" || sub.EndTime <= GetDBTimestamp() {
			return errors.New("Subscription is not active")
		}
		var a SubscriptionAllocation
		result := lockForUpdate(tx).Where("subscription_id = ?", subID).Find(&a)
		if result.Error != nil {
			return result.Error
		}
		followPoolEnd := endTime == 0
		if endTime == 0 {
			endTime = pool.EndTime
		}
		if endTime <= GetDBTimestamp() || endTime > pool.EndTime {
			return errors.New("Member expiry must not exceed pool expiry")
		}
		var poolRules []SubscriptionCycleRule
		if pool.Rules != "" {
			if err := common.UnmarshalJsonStr(pool.Rules, &poolRules); err != nil {
				return err
			}
		}
		multiplier, overrides, encodedOverrides, err := resolveAssignmentQuotaAllocation(a, poolRules, opts)
		if err != nil {
			return err
		}
		// A lost HTTP response may be retried with the same optimistic version.
		// Only an identical completed assignment is replayed without an audit or
		// any counter mutation; unrelated changes still require a new preview.
		if a.Version == version+1 && a.PoolID == poolID && !a.Pending && a.FollowPoolEnd == followPoolEnd && sub.EndTime == endTime && NormalizeSubscriptionQuotaMultiplier(a.QuotaMultiplier) == multiplier && a.CycleOverridesJSON == encodedOverrides {
			return nil
		}
		if a.Version != version || a.Version != hint.Version {
			return errors.New("Subscription assignment changed; reload before saving")
		}
		carry, err := legacySubscriptionCarryTx(tx, &sub, &a, pool, false)
		if err != nil {
			return err
		}
		if len(expectedCarry) > 0 && expectedCarry[0] != "" && expectedCarry[0] != carry.Digest {
			return errors.New("Legacy usage changed; preview the assignment again")
		}
		if carry.Amount > 0 {
			if _, err := legacySubscriptionCarryTx(tx, &sub, &a, pool, true); err != nil {
				return err
			}
			detail, err := cycleJSON(carry)
			if err != nil {
				return err
			}
			if err := tx.Create(&SubscriptionPoolEvent{PoolID: poolID, SubscriptionID: subID, ActorID: actorID, Action: "legacy_carry", Detail: detail, CreatedAt: GetDBTimestamp()}).Error; err != nil {
				return err
			}
		}
		a.SubscriptionID = sub.Id
		a.UserID = sub.UserId
		a.PoolID = poolID
		a.Pending = false
		a.FollowPoolEnd = followPoolEnd
		a.QuotaMultiplier = multiplier
		a.CycleOverrides = overrides
		a.CycleOverridesJSON = encodedOverrides
		a.Version++
		if err := tx.Save(&a).Error; err != nil {
			return err
		}
		if err := tx.Model(&sub).Updates(map[string]any{"end_time": endTime, "next_reset_time": 0}).Error; err != nil {
			return err
		}
		detail, _ := cycleJSON(map[string]any{"previous_pool_id": hint.PoolID, "end_time": endTime, "usage_transfer": carry.Amount > 0, "quota_multiplier": multiplier, "cycle_overrides": overrides})
		return tx.Create(&SubscriptionPoolEvent{PoolID: poolID, SubscriptionID: subID, ActorID: actorID, Action: "assign", Detail: detail, CreatedAt: GetDBTimestamp()}).Error
	})
}

type SubscriptionAssignmentPreview struct {
	Carry           SubscriptionLegacyCarry     `json:"legacy_carry"`
	SubscriptionID  int                         `json:"subscription_id"`
	Current         SubscriptionAllocation      `json:"current"`
	TargetPoolID    int                         `json:"target_pool_id"`
	PoolVersion     int64                       `json:"pool_version"`
	EndTime         int64                       `json:"end_time"`
	QuotaMultiplier float64                     `json:"quota_multiplier"`
	CycleOverrides  []SubscriptionCycleOverride `json:"cycle_overrides"`
	TargetCycles    []SubscriptionCycleView     `json:"target_cycles"`
	UsageTransfer   bool                        `json:"usage_transfer"`
}

func PreviewSubscriptionPoolAssignment(subID, poolID int, endTime int64) (*SubscriptionAssignmentPreview, error) {
	return PreviewSubscriptionPoolAssignmentWithOptions(subID, poolID, endTime, SubscriptionPoolAssignmentOptions{})
}

func PreviewSubscriptionPoolAssignmentWithOptions(subID, poolID int, endTime int64, opts SubscriptionPoolAssignmentOptions) (*SubscriptionAssignmentPreview, error) {
	view := &SubscriptionAssignmentPreview{SubscriptionID: subID, TargetPoolID: poolID, CycleOverrides: []SubscriptionCycleOverride{}}
	if subID <= 0 || poolID <= 0 {
		return nil, errors.New("Invalid parameters")
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var hint SubscriptionAllocation
		if err := tx.Where("subscription_id = ?", subID).Find(&hint).Error; err != nil {
			return err
		}
		poolIDs := []int{poolID}
		if hint.PoolID > 0 {
			poolIDs = append(poolIDs, hint.PoolID)
		}
		slices.Sort(poolIDs)
		for _, id := range slices.Compact(poolIDs) {
			if err := lockSubscriptionResource(tx, 0, id); err != nil {
				return err
			}
		}
		if hint.SubscriptionID > 0 && hint.PoolID == 0 {
			if err := lockSubscriptionResource(tx, subID, 0); err != nil {
				return err
			}
		}
		var pool SubscriptionPool
		if err := lockForUpdate(tx).First(&pool, poolID).Error; err != nil {
			return err
		}
		var sub UserSubscription
		if err := lockForUpdate(tx).First(&sub, subID).Error; err != nil {
			return err
		}
		now := GetDBTimestamp()
		if !pool.Enabled || pool.EndTime <= now {
			return errors.New("Upstream pool is unavailable")
		}
		if sub.Status != "active" || sub.EndTime <= now {
			return errors.New("Subscription is not active")
		}
		if err := lockForUpdate(tx).Where("subscription_id = ?", subID).Find(&view.Current).Error; err != nil {
			return err
		}
		if view.Current.Version != hint.Version || view.Current.PoolID != hint.PoolID {
			return errors.New("Subscription assignment changed; retry the request")
		}
		if err := HydrateSubscriptionAllocation(&view.Current); err != nil {
			return err
		}
		view.PoolVersion = pool.Version
		view.EndTime = endTime
		if endTime == 0 {
			view.EndTime = pool.EndTime
		}
		if view.EndTime <= now || view.EndTime > pool.EndTime {
			return errors.New("Member expiry must not exceed pool expiry")
		}
		var poolRules []SubscriptionCycleRule
		if pool.Rules != "" {
			if err := common.UnmarshalJsonStr(pool.Rules, &poolRules); err != nil {
				return err
			}
		}
		multiplier, overrides, _, err := resolveAssignmentQuotaAllocation(view.Current, poolRules, opts)
		if err != nil {
			return err
		}
		view.QuotaMultiplier = multiplier
		view.CycleOverrides = overrides
		view.TargetCycles, err = subscriptionCycleViewsWithAllocationTx(tx, allocationOwner(0, poolID), pool.Rules, sub.UserId, false, now, multiplier, cycleOverridesMap(overrides))
		if err != nil {
			return err
		}
		view.Carry, err = legacySubscriptionCarryTx(tx, &sub, &view.Current, pool, false)
		view.UsageTransfer = view.Carry.Amount > 0
		return err
	})
	return view, err
}

// A legacy total has no per-request window history. Transfer only settled use
// into exactly matching windows, never copy a weekly total into a short window.
// Other transitions retain their existing ledgers without transferring balances.
type SubscriptionLegacyCarry struct {
	Amount           int64    `json:"amount"`
	ExcludedReserved int64    `json:"excluded_reserved"`
	WindowStart      int64    `json:"window_start"`
	WindowEnd        int64    `json:"window_end"`
	Keys             []string `json:"keys"`
	Digest           string   `json:"digest"`
}

func legacySubscriptionCarryTx(tx *gorm.DB, sub *UserSubscription, allocation *SubscriptionAllocation, pool SubscriptionPool, apply bool) (SubscriptionLegacyCarry, error) {
	carry := SubscriptionLegacyCarry{Keys: []string{}}
	if allocation.SubscriptionID > 0 || sub.AmountUsed == 0 {
		return carry, nil
	}
	if sub.AmountUsed < 0 || sub.AmountUsed > common.MaxWalletQuota {
		return carry, errors.New("Invalid subscription counter")
	}
	var open []SubscriptionPreConsumeRecord
	if err := lockForUpdate(tx).Where("user_subscription_id = ? AND status = ?", sub.Id, "consumed").Order("id").Find(&open).Error; err != nil {
		return carry, err
	}
	for _, rec := range open {
		if rec.PreConsumed < 0 || rec.PreConsumed > common.MaxWalletQuota-carry.ExcludedReserved {
			return carry, errors.New("Invalid subscription counter")
		}
		carry.ExcludedReserved += rec.PreConsumed
	}
	if carry.ExcludedReserved > sub.AmountUsed {
		return carry, errors.New("Legacy usage requires reconciliation before migration")
	}
	carry.Amount = sub.AmountUsed - carry.ExcludedReserved
	if carry.Amount == 0 {
		return carry, nil
	}
	now := GetDBTimestamp()
	carry.WindowStart, carry.WindowEnd = sub.LastResetTime, sub.NextResetTime
	if sub.LastResetTime <= 0 || sub.NextResetTime <= now || sub.LastResetTime < now-7*86400 {
		return carry, errors.New("Legacy usage window does not match; reconcile usage before migration")
	}
	var rules []SubscriptionCycleRule
	if err := common.UnmarshalJsonStr(pool.Rules, &rules); err != nil {
		return carry, err
	}
	for _, rule := range rules {
		state, err := subscriptionCycleStateTx(tx, allocationOwner(0, pool.Id), rule, now)
		if err != nil {
			return carry, err
		}
		if err := common.UnmarshalJsonStr(state.Rule, &rule); err != nil {
			return carry, err
		}
		if !rule.Enabled || rule.Seconds != carry.WindowEnd-carry.WindowStart || state.StartTime != carry.WindowStart || state.EndTime != carry.WindowEnd {
			continue
		}
		carry.Keys = append(carry.Keys, rule.Key)
		if !apply {
			continue
		}
		for _, userID := range []int{0, sub.UserId} {
			usage, err := cycleUsageTx(tx, state, userID)
			if err != nil {
				return carry, err
			}
			if usage.Used+usage.Reserved > common.MaxWalletQuota-carry.Amount {
				return carry, errors.New("Subscription counter overflow")
			}
			// Reclassify an existing calibration, not a new upstream request.
			overlap := min(carry.Amount, max(int64(0), usage.Adjustment))
			if err := tx.Model(usage).Updates(map[string]any{"used": usage.Used + carry.Amount, "adjustment": usage.Adjustment - overlap}).Error; err != nil {
				return carry, err
			}
			if userID == 0 {
				var observation SubscriptionSyncObservation
				if err := tx.Where("pool_id = ? AND local_key = ?", pool.Id, rule.Key).Find(&observation).Error; err != nil {
					return carry, err
				}
				if observation.Payload != "" {
					var payload subscriptionSyncPayload
					if err := common.UnmarshalJsonStr(observation.Payload, &payload); err != nil {
						return carry, err
					}
					if payload.Baseline != nil && payload.Baseline.UsageID == usage.Id && payload.WindowEnd == state.EndTime {
						if payload.Baseline.Used > common.MaxWalletQuota-carry.Amount {
							return carry, errors.New("Subscription counter overflow")
						}
						payload.Baseline.Used += carry.Amount
						raw, err := cycleJSON(payload)
						if err != nil {
							return carry, err
						}
						if err := tx.Model(&observation).Update("payload", raw).Error; err != nil {
							return carry, err
						}
					}
				}
			}
		}
	}
	if len(carry.Keys) == 0 {
		return carry, errors.New("Legacy usage window does not match; reconcile usage before migration")
	}
	raw, err := cycleJSON(carry)
	if err != nil {
		return carry, err
	}
	carry.Digest = fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
	return carry, nil
}

// Snapshot ingestion is idempotent; stale or incompatible observations fail
// closed. Unit "quota" is the only enforceable local unit. Other units remain
// observations and are displayed without fabricated conversion percentages.
func IngestSubscriptionSnapshot(s SubscriptionUpstreamSnapshot, actorID int, calibrate bool) error {
	s.Calibrated = calibrate
	if s.EventID == "" || len(s.EventID) > 128 || s.Source == "" || len(s.Source) > 64 || s.Unit == "" || len(s.Unit) > 32 || s.Used < 0 || s.Used > common.MaxWalletQuota || s.Limit < 0 || s.Limit > common.MaxWalletQuota || s.SampledAt <= 0 || s.SampledAt > GetDBTimestamp()+60 || s.WindowEnd <= s.WindowStart {
		return errors.New("Invalid upstream snapshot")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var pool SubscriptionPool
		if err := lockForUpdate(tx).First(&pool, s.PoolID).Error; err != nil {
			return err
		}
		var prior SubscriptionUpstreamSnapshot
		result := tx.Where("pool_id = ? AND event_id = ?", s.PoolID, s.EventID).Find(&prior)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			s.Id = prior.Id
			if s == prior {
				return nil
			}
			return errors.New("Upstream snapshot event conflicts with existing data")
		}
		var latest SubscriptionUpstreamSnapshot
		result = tx.Where("pool_id = ? AND cycle_key = ?", s.PoolID, s.CycleKey).Order("sampled_at desc, id desc").Limit(1).Find(&latest)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 && latest.SampledAt >= s.SampledAt {
			return errors.New("Upstream snapshot is stale")
		}
		var state SubscriptionCycleState
		if err := tx.Where("owner = ? AND cycle_key = ?", allocationOwner(0, s.PoolID), s.CycleKey).First(&state).Error; err != nil {
			return err
		}
		var r SubscriptionCycleRule
		if err := common.UnmarshalJsonStr(state.Rule, &r); err != nil {
			return err
		}
		current, err := subscriptionCycleStateTx(tx, state.Owner, r, GetDBTimestamp())
		if err != nil {
			return err
		}
		if current.StartTime != s.WindowStart || current.EndTime != s.WindowEnd || s.SampledAt < current.StartTime || s.SampledAt >= current.EndTime {
			return errors.New("Upstream snapshot window does not match")
		}
		if calibrate {
			if s.Unit != "quota" {
				return errors.New("Only matching quota units can be calibrated")
			}
			row, err := cycleUsageTx(tx, current, 0)
			if err != nil {
				return err
			}
			if s.Used < row.Used {
				return errors.New("Calibration cannot hide recorded local consumption")
			}
			if err := tx.Model(row).Update("adjustment", s.Used-row.Used).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&s).Error; err != nil {
			return err
		}
		action := "snapshot"
		if calibrate {
			action = "calibrate"
		}
		return tx.Create(&SubscriptionPoolEvent{PoolID: s.PoolID, ActorID: actorID, Action: action, Detail: s.EventID, CreatedAt: GetDBTimestamp()}).Error
	})
}

func ResetSubscriptionCycle(subID, poolID, userID int, key string, actorID int) error {
	if subID < 0 || poolID < 0 || userID < 0 || (subID == 0 && poolID == 0) || key == "" {
		return errors.New("Invalid parameters")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockSubscriptionResource(tx, subID, poolID); err != nil {
			return err
		}
		if subID > 0 {
			// The browser may have read the allocation before a concurrent move.
			// Validate membership under the same resource/subscription locks used by
			// admission, so a stale reset cannot erase counters in the former pool.
			var sub UserSubscription
			if err := lockForUpdate(tx).First(&sub, subID).Error; err != nil {
				return err
			}
			var allocation SubscriptionAllocation
			if err := lockForUpdate(tx).Where("subscription_id = ?", subID).First(&allocation).Error; err != nil {
				return err
			}
			if allocation.PoolID != poolID || allocation.UserID != userID || sub.UserId != userID || allocation.Pending {
				return errors.New("Subscription assignment changed; retry the request")
			}
			if sub.Status != "active" || sub.EndTime <= GetDBTimestamp() {
				return errors.New("Subscription is not active")
			}
		}
		var state SubscriptionCycleState
		if err := tx.Where("owner = ? AND cycle_key = ?", allocationOwner(subID, poolID), key).First(&state).Error; err != nil {
			return err
		}
		var rule SubscriptionCycleRule
		if err := common.UnmarshalJsonStr(state.Rule, &rule); err != nil {
			return err
		}
		current, err := subscriptionCycleStateTx(tx, state.Owner, rule, GetDBTimestamp())
		if err != nil {
			return err
		}
		state = *current
		if poolID > 0 && userID > 0 {
			if err := common.UnmarshalJsonStr(state.Rule, &rule); err != nil {
				return err
			}
			if rule.Shared {
				return errors.New("Shared cycles can only be reset from the pool")
			}
			previous, err := cycleUsageTx(tx, &state, userID)
			if err != nil {
				return err
			}
			next := SubscriptionCycleUsage{StateID: state.Id, Epoch: state.Epoch, UserID: userID, Generation: previous.Generation + 1}
			if err := tx.Create(&next).Error; err != nil {
				return err
			}
			return tx.Create(&SubscriptionPoolEvent{PoolID: poolID, SubscriptionID: subID, ActorID: actorID, Action: "personal_reset", Detail: key, CreatedAt: GetDBTimestamp()}).Error
		}
		state.Epoch++
		state.StartTime = GetDBTimestamp()
		if err := tx.Save(&state).Error; err != nil {
			return err
		}
		return tx.Create(&SubscriptionPoolEvent{PoolID: poolID, SubscriptionID: subID, ActorID: actorID, Action: "reset", Detail: key, CreatedAt: GetDBTimestamp()}).Error
	})
}

// Delete only unused resources. Immutable accounting/audit rows are deliberately
// retained; there is no cascading deletion of consumption or refund provenance.
func DeleteSubscriptionPool(id int, version, bindingVersion int64, actorID int) error {
	hint, err := GetSubscriptionSyncBinding(id)
	if err != nil {
		return err
	}
	// Pin the binding inspected by the controller's permission check. A new
	// binding must not be implicitly detached using only pool-manage permission.
	if hint.Version != bindingVersion {
		return errors.New("Synchronization binding changed; retry the request")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if hint.SourceID > 0 {
			var source SubscriptionSyncSource
			if err := lockForUpdate(tx).First(&source, hint.SourceID).Error; err != nil {
				return err
			}
		}
		var pool SubscriptionPool
		if err := lockForUpdate(tx).First(&pool, id).Error; err != nil {
			return err
		}
		if pool.Version != version {
			return errors.New("Upstream pool changed; reload before saving")
		}
		var binding SubscriptionSyncBinding
		if err := tx.Where("pool_id = ?", id).Find(&binding).Error; err != nil {
			return err
		}
		if binding.Version != hint.Version || binding.SourceID != hint.SourceID {
			return errors.New("Synchronization binding changed; retry the request")
		}
		var count int64
		if err := tx.Model(&SubscriptionPlanAllocation{}).Where("pool_id = ? AND mode = ?", id, "fixed").Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("Change the subscription plans linked to this pool before deleting it")
		}
		if err := tx.Table("subscription_allocations AS a").Joins("JOIN user_subscriptions AS s ON s.id = a.subscription_id").Where("a.pool_id = ? AND s.status = ? AND s.end_time > ?", id, "active", GetDBTimestamp()).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("Move or cancel active members before deleting this pool")
		}
		if err := tx.Model(&SubscriptionCycleReservation{}).Where("pool_id = ? AND status = ?", id, "reserved").Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("Wait for in-flight requests to finish before deleting this pool")
		}
		var channelIDs []int
		if err := tx.Model(&SubscriptionPoolChannel{}).Where("pool_id = ?", id).Pluck("channel_id", &channelIDs).Error; err != nil {
			return err
		}
		if len(channelIDs) > 0 {
			var channels []Channel
			if err := lockForUpdate(tx).Select("id").Where("id IN ?", channelIDs).Order("id").Find(&channels).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("pool_id = ?", id).Delete(&SubscriptionPoolChannel{}).Error; err != nil {
			return err
		}
		if binding.PoolID > 0 {
			if err := tx.Model(&binding).Updates(map[string]any{"enabled": false, "source_id": 0, "account_id": "", "resource_key": nil, "version": binding.Version + 1}).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&SubscriptionPoolEvent{PoolID: id, ActorID: actorID, Action: "delete", CreatedAt: GetDBTimestamp()}).Error; err != nil {
			return err
		}
		return tx.Delete(&pool).Error
	})
}
