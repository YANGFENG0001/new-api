package model

import (
	"errors"

	"gorm.io/gorm"
)

// No row lock is held during upstream I/O. A fenced, bounded lease coordinates
// manual actions and workers across instances, independently of polling leases.
func ClaimSubscriptionSyncAuthentication(s SubscriptionSyncSource, lease, action string) error {
	now := GetDBTimestamp()
	updates := map[string]any{"auth_lease_token": lease, "auth_lease_until": now + 2*s.TimeoutSeconds + 30}
	query := DB.Model(&SubscriptionSyncSource{}).Where("id = ? AND version = ? AND (credential_revision = ? OR (credential_revision IS NULL AND ? = 0)) AND (auth_lease_until IS NULL OR auth_lease_until <= ?)", s.Id, s.Version, s.CredentialRevision, s.CredentialRevision, now)
	// Neither manual actions nor automatic login may evade upstream 429 backoff.
	query = query.Where("(last_error IS NULL OR last_error <> ? OR next_run IS NULL OR next_run <= ?)", "Synchronization upstream rate limit reached", now).
		Where("(auth_error IS NULL OR auth_error <> ? OR auth_next_attempt IS NULL OR auth_next_attempt <= ?)", "Synchronization upstream rate limit reached", now)
	if action == "refresh" {
		query = query.Where("(auth_next_attempt IS NULL OR auth_next_attempt <= ?) AND (refresh_blocked IS NULL OR refresh_blocked = ?)", now, false)
		// A rotating OAuth refresh token may be single-use. Fence before sending it:
		// a crash/ambiguous response requires manual replacement, never replay.
		if s.RefreshMode == "oauth" {
			updates["refresh_blocked"] = true
		}
	}
	result := query.Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("Authentication changed, is busy or requires manual replacement; reload before retrying")
	}
	return nil
}

func FinishSubscriptionSyncAuthentication(s SubscriptionSyncSource, lease, action string, encrypted *string, expires int64, message string, retry int64, actorID int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var current SubscriptionSyncSource
		if err := lockForUpdate(tx).First(&current, s.Id).Error; err != nil {
			return err
		}
		now := GetDBTimestamp()
		if current.Version != s.Version || current.CredentialRevision != s.CredentialRevision || current.AuthLeaseToken != lease || current.AuthLeaseUntil <= now {
			return errors.New("Synchronization configuration changed; reload before saving")
		}
		updates := map[string]any{"auth_lease_until": 0, "auth_lease_token": "", "auth_error": message}
		if message == "" {
			updates["last_auth_success"] = now
		}
		if action == "refresh" || message != "" {
			updates["auth_next_attempt"] = now + max(int64(60), min(retry, int64(86400)))
		}
		if encrypted != nil {
			updates["secret_payload"] = *encrypted
			updates["credential_revision"] = current.CredentialRevision + 1
			updates["token_expires_at"] = expires
			updates["refresh_blocked"] = current.RefreshBlocked && s.RefreshMode == "oauth" && action != "refresh"
			if action == "refresh" {
				updates["last_token_refresh"] = now
			}
		}
		if err := tx.Model(&current).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Create(&SubscriptionSyncEvent{SourceID: s.Id, ActorID: actorID, Action: "auth_" + action, Message: message, CreatedAt: now}).Error
	})
}
