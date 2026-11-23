package model

import (
	"errors"
	"slices"

	"gorm.io/gorm"
)

// Call after locking candidate pools: pool -> channel is the shared lock order
// used by relation administration, admission and retries.
func subscriptionChannelPoolsTx(tx *gorm.DB, channelID int) (map[int]bool, error) {
	if channelID > 0 {
		var channel Channel
		if err := lockForUpdate(tx).Select("id").First(&channel, channelID).Error; err != nil {
			return nil, err
		}
	}
	var mappings []SubscriptionPoolChannel
	if err := tx.Where("channel_id = ?", channelID).Find(&mappings).Error; err != nil {
		return nil, err
	}
	pools := make(map[int]bool, len(mappings))
	for _, mapping := range mappings {
		pools[mapping.PoolID] = true
	}
	return pools, nil
}

func SubscriptionChannelRequiresSubscription(channelID int) (bool, error) {
	if channelID <= 0 {
		return false, nil
	}
	var count int64
	err := DB.Model(&SubscriptionPoolChannel{}).Where("channel_id = ?", channelID).Count(&count).Error
	return count > 0, err
}

// The filter is an intersection with all pre-existing routing constraints.
func SubscriptionBlockedChannels(userID int, walletOnly bool) ([]int, error) {
	var mappings []SubscriptionPoolChannel
	if err := DB.Find(&mappings).Error; err != nil {
		return nil, err
	}
	allowed := []int{}
	if !walletOnly && len(mappings) > 0 {
		err := DB.Table("subscription_allocations AS a").Select("a.pool_id").Joins("JOIN user_subscriptions AS s ON s.id = a.subscription_id").Joins("JOIN subscription_pools AS p ON p.id = a.pool_id").Where("a.user_id = ? AND a.pending = ? AND s.status = ? AND s.end_time > ? AND p.enabled = ? AND p.end_time > ?", userID, false, "active", GetDBTimestamp(), true, GetDBTimestamp()).Scan(&allowed).Error
		if err != nil {
			return nil, err
		}
	}
	channelAccess := map[int]bool{}
	for _, m := range mappings {
		channelAccess[m.ChannelID] = channelAccess[m.ChannelID] || slices.Contains(allowed, m.PoolID)
	}
	blocked := []int{}
	for channelID, allowed := range channelAccess {
		if !allowed {
			blocked = append(blocked, channelID)
		}
	}
	slices.Sort(blocked)
	return blocked, nil
}

// SubscriptionPinnedChannels pins a retry to the resource selected by billing,
// while preserving all model, group, token and administrator constraints.
func SubscriptionPinnedChannels(requestID string) (allowed, blocked []int, pooled bool, err error) {
	var reservation SubscriptionCycleReservation
	if err = DB.Where("request_id = ?", requestID).Find(&reservation).Error; err != nil {
		return
	}
	var mappings []SubscriptionPoolChannel
	if err = DB.Find(&mappings).Error; err != nil {
		return
	}
	poolID := reservation.PoolID
	pooled = poolID > 0
	channelAccess := map[int]bool{}
	for _, m := range mappings {
		channelAccess[m.ChannelID] = channelAccess[m.ChannelID] || (pooled && m.PoolID == poolID)
	}
	for channelID, accessible := range channelAccess {
		if accessible {
			allowed = append(allowed, channelID)
		} else {
			blocked = append(blocked, channelID)
		}
	}
	slices.Sort(allowed)
	slices.Sort(blocked)
	return
}

func ValidateSubscriptionRetry(requestID string, channelID int, wallet bool) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var r SubscriptionCycleReservation
		if !wallet {
			if err := tx.Where("request_id = ?", requestID).Find(&r).Error; err != nil {
				return err
			}
			if r.PoolID > 0 {
				if err := lockSubscriptionResource(tx, 0, r.PoolID); err != nil {
					return err
				}
			}
		}
		pools, err := subscriptionChannelPoolsTx(tx, channelID)
		if err != nil {
			return err
		}
		if wallet && len(pools) > 0 {
			return errors.New("Wallet requests cannot use pool channels")
		}
		if (r.PoolID > 0 && !pools[r.PoolID]) || (r.PoolID == 0 && len(pools) > 0) {
			return errors.New("Subscription retries cannot change upstream pool")
		}
		return nil
	})
}
