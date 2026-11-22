package model

import (
	"errors"
	"slices"
)

func SubscriptionChannelPool(channelID int) (int, error) {
	if channelID <= 0 {
		return 0, nil
	}
	var mapping SubscriptionPoolChannel
	result := DB.Where("channel_id = ?", channelID).Find(&mapping)
	return mapping.PoolID, result.Error
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
	blocked := []int{}
	for _, m := range mappings {
		if !slices.Contains(allowed, m.PoolID) {
			blocked = append(blocked, m.ChannelID)
		}
	}
	return blocked, nil
}

// SubscriptionPinnedChannels pins a retry to the resource selected by billing,
// while preserving all model, group, token and administrator constraints.
func SubscriptionPinnedChannels(channelID int) (allowed, blocked []int, pooled bool, err error) {
	var mappings []SubscriptionPoolChannel
	if err = DB.Find(&mappings).Error; err != nil {
		return
	}
	poolID := 0
	for _, m := range mappings {
		if m.ChannelID == channelID {
			poolID = m.PoolID
			break
		}
	}
	pooled = poolID > 0
	for _, m := range mappings {
		if pooled && m.PoolID == poolID {
			allowed = append(allowed, m.ChannelID)
		} else {
			blocked = append(blocked, m.ChannelID)
		}
	}
	return
}

func ValidateSubscriptionRetry(requestID string, channelID int, wallet bool) error {
	poolID, err := SubscriptionChannelPool(channelID)
	if err != nil {
		return err
	}
	if wallet {
		if poolID > 0 {
			return errors.New("Wallet requests cannot use pool channels")
		}
		return nil
	}
	var r SubscriptionCycleReservation
	if err := DB.Where("request_id = ?", requestID).Find(&r).Error; err != nil {
		return err
	}
	if r.PoolID != poolID {
		return errors.New("Subscription retries cannot change upstream pool")
	}
	if poolID > 0 {
		var channel Channel
		if err := DB.Select("id", "channel_info").First(&channel, channelID).Error; err != nil {
			return err
		}
		if channel.ChannelInfo.IsMultiKey {
			return errors.New("Pool channels must identify a single upstream account")
		}
	}
	return nil
}
