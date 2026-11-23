package model

import (
	"errors"
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
			return saveSubscriptionSyncBinding(tx, binding, actorID)
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
		if err := tx.Where("pool_id = ?", pool.Id).Delete(&SubscriptionPoolChannel{}).Error; err != nil {
			return err
		}
		seen := map[int]bool{}
		for _, id := range input.Channels {
			if seen[id] {
				continue
			}
			seen[id] = true
			var channel Channel
			if err := tx.Select("id", "channel_info").First(&channel, id).Error; err != nil {
				return err
			}
			if channel.ChannelInfo.IsMultiKey {
				return errors.New("Pool channels must identify a single upstream account")
			}
			if err := tx.Create(&SubscriptionPoolChannel{ChannelID: id, PoolID: pool.Id}).Error; err != nil {
				return errors.New("Channel is already assigned to another upstream pool")
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
		} else {
			cfg.PoolID = 0
		}
		return tx.Save(&cfg).Error
	})
}

// Assignment changes preserve both pools' existing windows and all inflight
// reservations. Personal counters are keyed by pool+user, not subscription.
func AssignSubscriptionPool(subID, poolID int, version, endTime int64, actorID int, confirm bool, poolVersion int64) error {
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
		// A lost HTTP response may be retried with the same optimistic version.
		// Only an identical completed assignment is replayed without an audit or
		// any counter mutation; unrelated changes still require a new preview.
		if a.Version == version+1 && a.PoolID == poolID && !a.Pending && a.FollowPoolEnd == followPoolEnd && sub.EndTime == endTime {
			return nil
		}
		if a.Version != version || a.Version != hint.Version {
			return errors.New("Subscription assignment changed; reload before saving")
		}
		a.SubscriptionID = sub.Id
		a.UserID = sub.UserId
		a.PoolID = poolID
		a.Pending = false
		a.FollowPoolEnd = followPoolEnd
		a.Version++
		if err := tx.Save(&a).Error; err != nil {
			return err
		}
		if err := tx.Model(&sub).Updates(map[string]any{"end_time": endTime, "next_reset_time": 0}).Error; err != nil {
			return err
		}
		detail, _ := cycleJSON(map[string]any{"previous_pool_id": hint.PoolID, "end_time": endTime, "usage_transfer": false})
		return tx.Create(&SubscriptionPoolEvent{PoolID: poolID, SubscriptionID: subID, ActorID: actorID, Action: "assign", Detail: detail, CreatedAt: GetDBTimestamp()}).Error
	})
}

type SubscriptionAssignmentPreview struct {
	SubscriptionID int                     `json:"subscription_id"`
	Current        SubscriptionAllocation  `json:"current"`
	TargetPoolID   int                     `json:"target_pool_id"`
	PoolVersion    int64                   `json:"pool_version"`
	EndTime        int64                   `json:"end_time"`
	TargetCycles   []SubscriptionCycleView `json:"target_cycles"`
	UsageTransfer  bool                    `json:"usage_transfer"`
}

func PreviewSubscriptionPoolAssignment(subID, poolID int, endTime int64) (*SubscriptionAssignmentPreview, error) {
	view := &SubscriptionAssignmentPreview{SubscriptionID: subID, TargetPoolID: poolID}
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
		view.PoolVersion = pool.Version
		view.EndTime = endTime
		if endTime == 0 {
			view.EndTime = pool.EndTime
		}
		if view.EndTime <= now || view.EndTime > pool.EndTime {
			return errors.New("Member expiry must not exceed pool expiry")
		}
		var err error
		view.TargetCycles, err = subscriptionCycleViewsTx(tx, allocationOwner(0, poolID), pool.Rules, sub.UserId, false, now)
		return err
	})
	return view, err
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
