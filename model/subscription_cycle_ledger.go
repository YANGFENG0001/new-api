package model

import (
	"errors"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

func subscriptionCycleStateTx(tx *gorm.DB, owner string, r SubscriptionCycleRule, now int64) (*SubscriptionCycleState, error) {
	var s SubscriptionCycleState
	err := lockForUpdate(tx).Where("owner = ? AND cycle_key = ?", owner, r.Key).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// The initial start is derived from the anchor even if the service was offline.
		end := r.NextReset
		if end <= now {
			end += (1 + (now-end)/r.Seconds) * r.Seconds
		}
		raw, e := cycleJSON(r)
		if e != nil {
			return nil, e
		}
		s = SubscriptionCycleState{Owner: owner, CycleKey: r.Key, Epoch: 1, StartTime: end - r.Seconds, EndTime: end, Rule: raw}
		return &s, tx.Create(&s).Error
	}
	if err != nil {
		return nil, err
	}
	if s.EndTime <= now {
		var active SubscriptionCycleRule
		if err := common.UnmarshalJsonStr(s.Rule, &active); err != nil {
			return nil, err
		}
		start := s.EndTime
		if s.PendingRule != "" {
			s.Rule = s.PendingRule
			s.PendingRule = ""
			if err := common.UnmarshalJsonStr(s.Rule, &active); err != nil {
				return nil, err
			}
		}
		jumps := (now - start) / active.Seconds
		s.Epoch += jumps + 1
		s.StartTime = start + jumps*active.Seconds
		s.EndTime = s.StartTime + active.Seconds
		if err := tx.Save(&s).Error; err != nil {
			return nil, err
		}
	}
	return &s, nil
}

func cycleUsageTx(tx *gorm.DB, s *SubscriptionCycleState, userID int) (*SubscriptionCycleUsage, error) {
	row := SubscriptionCycleUsage{StateID: s.Id, Epoch: s.Epoch, UserID: userID}
	if err := lockForUpdate(tx).Where("state_id = ? AND epoch = ? AND user_id = ?", s.Id, s.Epoch, userID).Order("generation desc").FirstOrCreate(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func configureSubscriptionCyclesTx(tx *gorm.DB, owner string, rules []SubscriptionCycleRule, now int64) error {
	var states []SubscriptionCycleState
	if err := lockForUpdate(tx).Where("owner = ?", owner).Find(&states).Error; err != nil {
		return err
	}
	// Removing a key would discard its enforcement while inflight reservations
	// exist. Disable it explicitly at the next boundary instead.
	for _, s := range states {
		if !slices.ContainsFunc(rules, func(r SubscriptionCycleRule) bool { return r.Key == s.CycleKey }) {
			return errors.New("Disable existing cycles instead of deleting their keys")
		}
	}
	for _, r := range rules {
		s, e := subscriptionCycleStateTx(tx, owner, r, now)
		if e != nil {
			return e
		}
		var old SubscriptionCycleRule
		if e = common.UnmarshalJsonStr(s.Rule, &old); e != nil {
			return e
		}
		// Only moving the next reset is immediate; duration/scope/limits start at
		// the boundary. Usage stays attached to the original immutable epoch.
		if old.NextReset != r.NextReset && s.EndTime != r.NextReset {
			if r.NextReset <= now {
				return errors.New("Next reset must be in the future")
			}
			s.EndTime = r.NextReset
		}
		// The anchor is transport metadata, not a policy change. Editing a
		// name or simply saving after a natural reset must not schedule a new
		// window or make unchanged rules appear pending forever.
		old.NextReset = s.EndTime
		r.NextReset = s.EndTime
		old.Name = r.Name
		s.Rule, e = cycleJSON(old)
		if e != nil {
			return e
		}
		s.PendingRule = ""
		if old != r {
			s.PendingRule, e = cycleJSON(r)
			if e != nil {
				return e
			}
		}
		if e = tx.Save(s).Error; e != nil {
			return e
		}
	}
	return nil
}

func initializeSubscriptionAllocationTx(tx *gorm.DB, sub *UserSubscription) error {
	var cfg SubscriptionPlanAllocation
	err := tx.Where("plan_id = ?", sub.PlanId).First(&cfg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if cfg.Mode == "legacy" {
		return nil
	}
	if cfg.FixedEndTime > 0 {
		if cfg.FixedEndTime <= sub.StartTime {
			return errors.New("Subscription expiry is in the past")
		}
		sub.EndTime = cfg.FixedEndTime
	}
	a := SubscriptionAllocation{SubscriptionID: sub.Id, UserID: sub.UserId, Rules: cfg.Rules, Version: 1}
	if cfg.Mode == "assign" {
		a.Pending = true
	}
	if cfg.Mode == "fixed" {
		var p SubscriptionPool
		if err := lockForUpdate(tx).First(&p, cfg.PoolID).Error; err != nil {
			return err
		}
		if !p.Enabled || p.EndTime <= sub.StartTime {
			return errors.New("Upstream pool is unavailable")
		}
		a.PoolID = p.Id
		sub.EndTime = p.EndTime
		a.FollowPoolEnd = cfg.FixedEndTime == 0
		if cfg.FixedEndTime > 0 {
			sub.EndTime = min(cfg.FixedEndTime, p.EndTime)
		}
	}
	if err := tx.Create(&a).Error; err != nil {
		return err
	}
	if a.PoolID == 0 && !a.Pending {
		var rules []SubscriptionCycleRule
		if cfg.Rules != "" {
			if err := common.UnmarshalJsonStr(cfg.Rules, &rules); err != nil {
				return err
			}
		}
		if err := configureSubscriptionCyclesTx(tx, allocationOwner(sub.Id, 0), rules, sub.StartTime); err != nil {
			return err
		}
	}
	return tx.Model(sub).Updates(map[string]any{"end_time": sub.EndTime, "next_reset_time": 0}).Error
}

// Called inside legacy pre-consume's transaction. A nil allocation means the
// original single-cycle behavior remains in force.
func reserveSubscriptionCyclesTx(tx *gorm.DB, requestID string, sub *UserSubscription, channelID int, amount, now int64) (bool, error) {
	var a SubscriptionAllocation
	err := tx.Where("subscription_id = ?", sub.Id).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if a.Pending {
		return true, errors.New("subscription quota insufficient: pool assignment pending")
	}
	if err = lockSubscriptionResource(tx, sub.Id, a.PoolID); err != nil {
		return true, err
	}
	var activeSub UserSubscription
	if err = lockForUpdate(tx).First(&activeSub, sub.Id).Error; err != nil {
		return true, err
	}
	if activeSub.Status != "active" || activeSub.EndTime <= now {
		return true, errors.New("subscription quota insufficient: subscription expired")
	}
	// Re-read after acquiring the lock: an assignment may have changed meanwhile.
	var current SubscriptionAllocation
	if err = lockForUpdate(tx).Where("subscription_id = ?", sub.Id).First(&current).Error; err != nil {
		return true, err
	}
	if current.Version != a.Version {
		return true, errors.New("Subscription assignment changed; retry the request")
	}
	raw := a.Rules
	if a.PoolID > 0 {
		var p SubscriptionPool
		if err = lockForUpdate(tx).First(&p, a.PoolID).Error; err != nil {
			return true, err
		}
		if !p.Enabled || p.EndTime <= now {
			return true, errors.New("subscription quota insufficient: upstream pool expired")
		}
		var count int64
		if err = tx.Model(&SubscriptionPoolChannel{}).Where("pool_id = ? AND channel_id = ?", a.PoolID, channelID).Count(&count).Error; err != nil {
			return true, err
		}
		if count == 0 {
			return true, errors.New("subscription quota insufficient: incompatible pool channel")
		}
		var channel Channel
		if err := tx.Select("id", "channel_info").First(&channel, channelID).Error; err != nil {
			return true, err
		}
		if channel.ChannelInfo.IsMultiKey {
			return true, errors.New("Pool channels must identify a single upstream account")
		}
		raw = p.Rules
	}
	var rules []SubscriptionCycleRule
	if raw != "" {
		if err = common.UnmarshalJsonStr(raw, &rules); err != nil {
			return true, err
		}
	}
	var existing SubscriptionCycleReservation
	result := lockForUpdate(tx).Where("request_id = ?", requestID).Find(&existing)
	if result.Error != nil {
		return true, result.Error
	}
	if result.RowsAffected > 0 {
		if existing.UserID != sub.UserId || existing.SubscriptionID != sub.Id || existing.Status == "refunded" {
			return true, errors.New("Invalid subscription reservation")
		}
		return true, nil
	}
	if a.PoolID > 0 {
		if err := enforceSubscriptionSyncTx(tx, a.PoolID, now, amount); err != nil {
			return true, err
		}
	}
	owner := allocationOwner(sub.Id, a.PoolID)
	ids := []int{}
	rows := []*SubscriptionCycleUsage{}
	for _, r := range rules {
		state, e := subscriptionCycleStateTx(tx, owner, r, now)
		if e != nil {
			return true, e
		}
		if e = common.UnmarshalJsonStr(state.Rule, &r); e != nil {
			return true, e
		}
		if !r.Enabled {
			continue
		}
		aggregate, e := cycleUsageTx(tx, state, 0)
		if e != nil {
			return true, e
		}
		personal, e := cycleUsageTx(tx, state, sub.UserId)
		if e != nil {
			return true, e
		}
		effective := personal
		if r.Shared {
			effective = aggregate
		}
		if !r.Unlimited && effective.Used+effective.Reserved+effective.Adjustment > r.Limit-amount {
			return true, errors.New("subscription quota insufficient: cycle limit reached")
		}
		if r.EnforceUpstream && aggregate.Used+aggregate.Reserved+aggregate.Adjustment > r.UpstreamLimit-amount {
			return true, errors.New("subscription quota insufficient: upstream limit reached")
		}
		for _, row := range []*SubscriptionCycleUsage{aggregate, personal} {
			if row.Used+row.Reserved > common.MaxWalletQuota-amount {
				return true, errors.New("Subscription counter overflow")
			}
			ids = append(ids, row.Id)
			rows = append(rows, row)
		}
	}
	if len(ids) == 0 {
		return true, errors.New("subscription quota insufficient: no enabled cycles")
	}
	data, e := cycleJSON(ids)
	if e != nil {
		return true, e
	}
	rec := SubscriptionCycleReservation{RequestID: requestID, SubscriptionID: sub.Id, UserID: sub.UserId, PoolID: a.PoolID, Owner: owner, UsageIDs: data, Reserved: amount, Status: "reserved", CreatedAt: now}
	if e = tx.Create(&rec).Error; e != nil {
		return true, e
	}
	for _, row := range rows {
		if e = tx.Model(row).Update("reserved", row.Reserved+amount).Error; e != nil {
			return true, e
		}
	}
	return true, nil
}

// ModifySubscriptionCycleReservation distinguishes admission reserves from true
// final usage. The original epoch IDs are retained across resets and moves.
func ModifySubscriptionCycleReservation(requestID string, subscriptionID int, delta int64, operation string) (bool, error) {
	var hint SubscriptionCycleReservation
	result := DB.Where("request_id = ?", requestID).Find(&hint)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	if operation != "refund" && (subscriptionID <= 0 || hint.SubscriptionID != subscriptionID) {
		return true, errors.New("Invalid subscription reservation")
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockSubscriptionResource(tx, hint.SubscriptionID, hint.PoolID); err != nil {
			return err
		}
		var rec SubscriptionCycleReservation
		if err := lockForUpdate(tx).First(&rec, hint.Id).Error; err != nil {
			return err
		}
		var ids []int
		if err := common.UnmarshalJsonStr(rec.UsageIDs, &ids); err != nil {
			return err
		}
		var rows []SubscriptionCycleUsage
		if err := lockForUpdate(tx).Where("id IN ?", ids).Order("id").Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != len(ids) {
			return errors.New("Subscription reservation ledger is incomplete")
		}
		switch operation {
		case "reserve":
			if rec.Status != "reserved" {
				return errors.New("Subscription reservation is already finalized")
			}
			if delta < -rec.Reserved || delta > common.MaxWalletQuota-rec.Reserved {
				return errors.New("Invalid subscription reservation amount")
			}
			for _, row := range rows {
				if delta > 0 {
					var state SubscriptionCycleState
					if err := tx.First(&state, row.StateID).Error; err != nil {
						return err
					}
					var r SubscriptionCycleRule
					if err := common.UnmarshalJsonStr(state.Rule, &r); err != nil {
						return err
					}
					// Growing a reservation across a boundary cannot consume the new window
					// invisibly. The original allocation remains refundable and is rejected.
					if state.Epoch != row.Epoch || state.EndTime <= GetDBTimestamp() {
						return errors.New("Subscription reservation window expired")
					}
					effective := (r.Shared && row.UserID == 0) || (!r.Shared && row.UserID == rec.UserID)
					if effective && !r.Unlimited && row.Used+row.Reserved+row.Adjustment > r.Limit-delta {
						return errors.New("subscription quota insufficient: cycle limit reached")
					}
					if row.UserID == 0 && r.EnforceUpstream && row.Used+row.Reserved+row.Adjustment > r.UpstreamLimit-delta {
						return errors.New("subscription quota insufficient: upstream limit reached")
					}
				}
				if row.Reserved+delta < 0 || row.Used+row.Reserved > common.MaxWalletQuota-max(delta, 0) {
					return errors.New("Invalid subscription counter")
				}
				if err := tx.Model(&row).Update("reserved", row.Reserved+delta).Error; err != nil {
					return err
				}
			}
			rec.Reserved += delta
		case "settle", "adjust":
			if rec.Status == "refunded" {
				return errors.New("Subscription reservation was refunded")
			}
			if operation == "settle" && rec.Status == "settled" {
				return nil
			}
			if delta < -common.MaxWalletQuota || delta > common.MaxWalletQuota {
				return errors.New("Invalid subscription settlement amount")
			}
			actual := rec.Settled + delta
			if rec.Status == "reserved" {
				actual = rec.Reserved + delta
			}
			if actual < 0 || actual > common.MaxWalletQuota {
				return errors.New("Invalid subscription settlement amount")
			}
			usedDelta := actual - rec.Settled
			for _, row := range rows {
				reserved := row.Reserved
				if rec.Status == "reserved" {
					reserved -= rec.Reserved
				}
				if reserved < 0 || row.Used > common.MaxWalletQuota-max(usedDelta, 0) || row.Used+usedDelta < 0 {
					return errors.New("Invalid subscription counter")
				}
				// Real excess is recorded, never clipped to an admission limit.
				if err := tx.Model(&row).Updates(map[string]any{"reserved": reserved, "used": row.Used + usedDelta}).Error; err != nil {
					return err
				}
			}
			rec.Status = "settled"
			rec.Settled = actual
		case "refund":
			if rec.Status == "refunded" || rec.Status == "settled" {
				return nil
			}
			for _, row := range rows {
				if row.Reserved < rec.Reserved {
					return errors.New("Invalid subscription refund")
				}
				if err := tx.Model(&row).Update("reserved", row.Reserved-rec.Reserved).Error; err != nil {
					return err
				}
			}
			rec.Status = "refunded"
		default:
			return errors.New("Invalid subscription reservation operation")
		}
		return tx.Save(&rec).Error
	})
	return true, err
}
