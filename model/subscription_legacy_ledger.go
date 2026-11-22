package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Legacy request provenance is kept after an administrator assigns the instance
// to a pool. Such inflight work settles its original legacy counter, never the
// target pool. A managed record cannot fall back here if its cycle ledger is lost.
func modifyLegacySubscriptionRequest(requestID string, subscriptionID int, delta int64, operation string) (bool, error) {
	if strings.TrimSpace(requestID) == "" {
		return false, nil
	}
	var hint SubscriptionPreConsumeRecord
	result := DB.Where("request_id = ?", requestID).Find(&hint)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	if (operation != "refund" && (subscriptionID <= 0 || hint.UserSubscriptionId != subscriptionID)) || hint.Status == "managed" {
		return true, errors.New("Invalid subscription reservation")
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Match admission's subscription-before-request lock order. No current pool
		// counter is modified, so taking a new resource lock here would be incorrect.
		var sub UserSubscription
		if err := lockForUpdate(tx).First(&sub, hint.UserSubscriptionId).Error; err != nil {
			return err
		}
		var record SubscriptionPreConsumeRecord
		if err := lockForUpdate(tx).First(&record, hint.Id).Error; err != nil {
			return err
		}
		if record.Status == "managed" || record.UserSubscriptionId != sub.Id || record.UserId != sub.UserId {
			return errors.New("Invalid subscription reservation")
		}
		if operation == "refund" {
			if record.Status == "refunded" || record.Status == "settled" {
				return nil
			}
			delta = -record.PreConsumed
			record.Status = "refunded"
		} else {
			if record.Status == "refunded" {
				return errors.New("Subscription reservation was refunded")
			}
			if operation == "settle" && record.Status == "settled" {
				return nil
			}
			if operation == "reserve" {
				if record.Status != "consumed" || delta < -record.PreConsumed || delta > common.MaxWalletQuota-record.PreConsumed {
					return errors.New("Invalid subscription reservation amount")
				}
				if delta > 0 && sub.AmountTotal > 0 && sub.AmountUsed > sub.AmountTotal-delta {
					return errors.New("subscription quota insufficient: cycle limit reached")
				}
				record.PreConsumed += delta
			} else if operation == "settle" || operation == "adjust" {
				record.Status = "settled"
			} else {
				return errors.New("Invalid subscription reservation operation")
			}
		}
		if delta < -common.MaxWalletQuota || delta > common.MaxWalletQuota || sub.AmountUsed > common.MaxWalletQuota-max(delta, 0) {
			return errors.New("Invalid subscription settlement amount")
		}
		sub.AmountUsed = max(sub.AmountUsed+delta, 0)
		if err := tx.Save(&sub).Error; err != nil {
			return err
		}
		return tx.Save(&record).Error
	})
	return true, err
}
