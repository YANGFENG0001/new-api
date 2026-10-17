package model

import (
	"github.com/QuantumNous/new-api/common"
)

// CountActiveUserTokens counts API keys that can still authenticate: enabled
// status and either never expire or not yet expired.
func CountActiveUserTokens(userID int) (int64, error) {
	var total int64
	err := DB.Model(&Token{}).
		Where("user_id = ? AND status = ? AND (expired_time = ? OR expired_time > ?)",
			userID, common.TokenStatusEnabled, -1, common.GetTimestamp()).
		Count(&total).Error
	return total, err
}
