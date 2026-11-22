package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Connection secrets never enter a management response or a public usage DTO.
type SubscriptionSyncSource struct {
	RefreshMode        string `json:"refresh_mode" gorm:"type:varchar(16)"`
	RefreshURL         string `json:"refresh_url" gorm:"type:text"`
	AutoRefresh        bool   `json:"auto_refresh"`
	RefreshBefore      int64  `json:"refresh_before"`
	CredentialRevision int64  `json:"credential_revision"`
	TokenExpiresAt     int64  `json:"token_expires_at"`
	LastAuthSuccess    int64  `json:"last_auth_success"`
	LastTokenRefresh   int64  `json:"last_token_refresh"`
	AuthError          string `json:"auth_error" gorm:"type:varchar(128)"`
	AuthNextAttempt    int64  `json:"auth_next_attempt"`
	AuthLeaseUntil     int64  `json:"-"`
	AuthLeaseToken     string `json:"-" gorm:"type:varchar(64)"`
	RefreshBlocked     bool   `json:"refresh_blocked"`

	ObservationStatus string `json:"observation_status" gorm:"-"`
	ObservedAt        int64  `json:"observed_at" gorm:"-"`
	Id                int    `json:"id"`
	Name              string `json:"name" gorm:"type:varchar(128)"`
	URL               string `json:"url" gorm:"type:text"`
	Method            string `json:"method" gorm:"type:varchar(8)"`
	Enabled           bool   `json:"enabled"`
	IntervalSeconds   int64  `json:"interval_seconds"`
	StaleSeconds      int64  `json:"stale_seconds"`
	TimeoutSeconds    int64  `json:"timeout_seconds"`
	AllowHTTP         bool   `json:"allow_http"`
	PrivateIPs        string `json:"private_ips" gorm:"type:text"`
	Version           int64  `json:"version"`
	MappingJSON       string `json:"-" gorm:"type:text"`
	SecretPayload     string `json:"-" gorm:"type:text"`
	CatalogJSON       string `json:"-" gorm:"type:text"`
	HasCredentials    bool   `json:"has_credentials" gorm:"-"`
	LastAttempt       int64  `json:"last_attempt"`
	LastFetch         int64  `json:"last_fetch"`
	LastApplied       int64  `json:"last_applied"`
	LastError         string `json:"last_error" gorm:"type:varchar(128)"`
	Failures          int    `json:"failures"`
	NextRun           int64  `json:"next_run" gorm:"index"`
	LeaseUntil        int64  `json:"-"`
	LeaseToken        string `json:"-" gorm:"type:varchar(64)"`
}

type SubscriptionSyncRequest struct {
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`

	AuthType   string            `json:"auth_type"`
	AuthHeader string            `json:"auth_header"`
	Username   string            `json:"username"`
	Password   string            `json:"password"`
	Token      string            `json:"token"`
	Headers    map[string]string `json:"headers"`
	Query      map[string]string `json:"query"`
	Body       string            `json:"body"`
}

type SubscriptionSyncCycleMapping struct {
	Key             string `json:"key"`
	Name            string `json:"name"`
	Seconds         int64  `json:"seconds"`
	UtilizationPath string `json:"utilization_path"`
	Scale           string `json:"scale"` // ratio or percent; never a currency conversion
	ResetPath       string `json:"reset_path"`
	SampledAtPath   string `json:"sampled_at_path"`
}

type SubscriptionSyncMapping struct {
	SuccessPath   string                         `json:"success_path"`
	AccountsPath  string                         `json:"accounts_path"`
	IDPath        string                         `json:"id_path"`
	NamePath      string                         `json:"name_path"`
	SampledAtPath string                         `json:"sampled_at_path"`
	SourcePath    string                         `json:"source_path"`
	ExpiryPath    string                         `json:"expiry_path"`
	TimeFormat    string                         `json:"time_format"`
	Cycles        []SubscriptionSyncCycleMapping `json:"cycles"`
}

type SubscriptionSyncSourceInput struct {
	MayApply bool `json:"-"`
	SubscriptionSyncSource
	Mapping SubscriptionSyncMapping  `json:"mapping"`
	Request *SubscriptionSyncRequest `json:"request,omitempty"`
}

func PanelSubscriptionSyncMapping() SubscriptionSyncMapping {
	return SubscriptionSyncMapping{SuccessPath: "ok", AccountsPath: "data.accounts", IDPath: "account_id", NamePath: "vm_id", SampledAtPath: "last_probe", SourcePath: "probe_source", TimeFormat: "rfc3339", Cycles: []SubscriptionSyncCycleMapping{
		{Key: "5h", Name: "5h", Seconds: 18000, UtilizationPath: "utilization_5h", Scale: "ratio", ResetPath: "reset_5h"},
		{Key: "7d", Name: "7d", Seconds: 604800, UtilizationPath: "utilization_7d", Scale: "ratio", ResetPath: "reset_7d"},
		{Key: "sonnet", Name: "Sonnet", Seconds: 604800, UtilizationPath: "utilization_7d_sonnet", Scale: "ratio", ResetPath: "reset_7d_sonnet"},
		{Key: "oi", Name: "OI", Seconds: 604800, UtilizationPath: "utilization_7d_oi", Scale: "ratio", ResetPath: "reset_7d_oi"},
		{Key: "fable", Name: "Fable", Seconds: 604800, UtilizationPath: "fable.utilization", Scale: "ratio", ResetPath: "fable.reset", SampledAtPath: "fable.probed_at"},
	}}
}

type SubscriptionSyncCycle struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Seconds     int64    `json:"seconds"`
	Utilization *float64 `json:"utilization"`
	WindowStart int64    `json:"window_start"`
	WindowEnd   int64    `json:"window_end"`
	SampledAt   int64    `json:"sampled_at"`
	Source      string   `json:"source"`
	Error       string   `json:"error"`
}

type SubscriptionSyncAccount struct {
	ID        string                  `json:"id"`
	Name      string                  `json:"name"`
	Expiry    int64                   `json:"expiry"`
	SampledAt int64                   `json:"sampled_at"`
	Cycles    []SubscriptionSyncCycle `json:"cycles"`
}

type SubscriptionSyncCycleBinding struct {
	LocalKey        string  `json:"local_key"`
	UpstreamKey     string  `json:"upstream_key"`
	AlignReset      bool    `json:"align_reset"`
	Enforce         bool    `json:"enforce"`
	Threshold       float64 `json:"threshold"`
	MaxShiftSeconds int64   `json:"max_shift_seconds"`
}

type SubscriptionSyncBinding struct {
	LastError    string  `json:"last_error" gorm:"type:varchar(128)"`
	PoolID       int     `json:"pool_id" gorm:"primaryKey;autoIncrement:false"`
	SourceID     int     `json:"source_id" gorm:"index"`
	AccountID    string  `json:"account_id" gorm:"type:varchar(128)"`
	ResourceKey  *string `json:"-" gorm:"type:varchar(64);uniqueIndex"`
	Enabled      bool    `json:"enabled"`
	SyncExpiry   bool    `json:"sync_expiry"`
	StalePolicy  string  `json:"stale_policy"` // stop or local, with bounded grace
	GraceSeconds int64   `json:"grace_seconds"`
	Version      int64   `json:"version"`
	CyclesJSON   string  `json:"-" gorm:"type:text"`
}

type SubscriptionSyncBindingInput struct {
	SubscriptionSyncBinding
	Cycles []SubscriptionSyncCycleBinding `json:"cycles"`
}

// Latest normalized observations are separate from the immutable billing ledger.
// Rebinding/configuration versions invalidate old enforcement without erasing history.
type SubscriptionSyncObservation struct {
	LastError      string `json:"last_error" gorm:"type:varchar(128)"`
	Id             int    `json:"id"`
	PoolID         int    `json:"pool_id" gorm:"uniqueIndex:idx_sub_sync_observation,priority:1"`
	LocalKey       string `json:"local_key" gorm:"type:varchar(64);uniqueIndex:idx_sub_sync_observation,priority:2"`
	SourceVersion  int64  `json:"-"`
	BindingVersion int64  `json:"-"`
	Digest         string `json:"digest" gorm:"type:varchar(64)"`
	Payload        string `json:"-" gorm:"type:text"`
	SampledAt      int64  `json:"sampled_at"`
	FetchedAt      int64  `json:"fetched_at"`
	Pending        string `json:"pending" gorm:"type:varchar(128)"`
}

type SubscriptionSyncEvent struct {
	Detail    string                   `json:"-" gorm:"type:text"`
	Changes   []SubscriptionSyncChange `json:"changes" gorm:"-"`
	Id        int                      `json:"id"`
	SourceID  int                      `json:"source_id" gorm:"index"`
	PoolID    int                      `json:"pool_id" gorm:"index"`
	ActorID   int                      `json:"actor_id"`
	Action    string                   `json:"action" gorm:"type:varchar(32)"`
	Message   string                   `json:"message" gorm:"type:varchar(128)"`
	CreatedAt int64                    `json:"created_at"`
}

func GetSubscriptionSyncBinding(poolID int) (SubscriptionSyncBindingInput, error) {
	view := SubscriptionSyncBindingInput{Cycles: []SubscriptionSyncCycleBinding{}}
	err := DB.Where("pool_id = ?", poolID).Find(&view.SubscriptionSyncBinding).Error
	if err == nil && view.CyclesJSON != "" {
		err = common.UnmarshalJsonStr(view.CyclesJSON, &view.Cycles)
	}
	if view.StalePolicy == "" {
		view.StalePolicy = "stop"
	}
	view.PoolID = poolID
	return view, err
}

func SaveSubscriptionSyncBinding(input SubscriptionSyncBindingInput, actorID int) error {
	if input.PoolID <= 0 || input.SourceID <= 0 || strings.TrimSpace(input.AccountID) == "" || len(input.AccountID) > 128 || len(input.Cycles) > 32 || (input.StalePolicy != "stop" && input.StalePolicy != "local") || input.GraceSeconds < 0 || input.GraceSeconds > 86400 {
		return errors.New("Invalid synchronization binding")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var source SubscriptionSyncSource
		if err := lockForUpdate(tx).First(&source, input.SourceID).Error; err != nil {
			return err
		}
		var pool SubscriptionPool
		if err := lockForUpdate(tx).First(&pool, input.PoolID).Error; err != nil {
			return err
		}
		var prior SubscriptionSyncBinding
		if err := tx.Where("pool_id = ?", input.PoolID).Find(&prior).Error; err != nil {
			return err
		}
		if prior.Version != input.Version {
			return errors.New("Synchronization configuration changed; reload before saving")
		}
		var rules []SubscriptionCycleRule
		if err := common.UnmarshalJsonStr(pool.Rules, &rules); err != nil {
			return err
		}
		var mapping SubscriptionSyncMapping
		if err := common.UnmarshalJsonStr(source.MappingJSON, &mapping); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, c := range input.Cycles {
			var local *SubscriptionCycleRule
			for i := range rules {
				if rules[i].Key == c.LocalKey {
					local = &rules[i]
					break
				}
			}
			var remote *SubscriptionSyncCycleMapping
			for i := range mapping.Cycles {
				if mapping.Cycles[i].Key == c.UpstreamKey {
					remote = &mapping.Cycles[i]
					break
				}
			}
			if seen[c.LocalKey] || local == nil || remote == nil || c.Threshold <= 0 || c.Threshold > 1 || math.IsNaN(c.Threshold) || math.IsInf(c.Threshold, 0) || c.MaxShiftSeconds < 0 || c.MaxShiftSeconds > remote.Seconds || (c.AlignReset && local.Seconds != remote.Seconds) {
				return errors.New("Invalid synchronization cycle mapping")
			}
			seen[c.LocalKey] = true
		}
		raw, err := cycleJSON(input.Cycles)
		if err != nil {
			return err
		}
		b := input.SubscriptionSyncBinding
		b.LastError = ""
		b.Version = prior.Version + 1
		b.CyclesJSON = raw
		resourceKey := common.GenerateHMACWithKey([]byte("subscription-resource-binding-v1"), source.URL+"\x00"+input.AccountID)
		b.ResourceKey = &resourceKey
		if err := tx.Save(&b).Error; err != nil {
			return errors.New("Upstream resource is already bound or unavailable")
		}
		return tx.Create(&SubscriptionSyncEvent{SourceID: source.Id, PoolID: pool.Id, ActorID: actorID, Action: "binding_save", CreatedAt: GetDBTimestamp()}).Error
	})
}

func SaveSubscriptionSyncSource(input SubscriptionSyncSourceInput, encrypted *string, actorID int, privileged bool) (*SubscriptionSyncSource, error) {
	var saved SubscriptionSyncSource
	err := DB.Transaction(func(tx *gorm.DB) error {
		var old SubscriptionSyncSource
		if input.Id > 0 {
			if err := lockForUpdate(tx).First(&old, input.Id).Error; err != nil {
				return err
			}
		}
		if old.RefreshMode == "" {
			old.RefreshMode = "manual"
		}
		if old.Version != input.Version || (encrypted != nil && old.CredentialRevision != input.CredentialRevision) {
			return errors.New("Synchronization configuration changed; reload before saving")
		}
		if !privileged && (old.AllowHTTP != input.AllowHTTP || old.PrivateIPs != input.PrivateIPs) {
			return errors.New("Only super administrators can authorize private or HTTP synchronization")
		}
		// A delegated editor cannot redirect credentials to another destination.
		if !privileged && old.Id > 0 && (old.URL != input.URL || old.Method != input.Method || old.RefreshURL != input.RefreshURL || old.RefreshMode != input.RefreshMode) {
			return errors.New("Only super administrators can change synchronization destinations")
		}
		if !privileged && !input.MayApply && old.Id > 0 {
			var bindings []SubscriptionSyncBinding
			if err := tx.Where("source_id = ?", old.Id).Find(&bindings).Error; err != nil {
				return err
			}
			for _, b := range bindings {
				var cycles []SubscriptionSyncCycleBinding
				if err := common.UnmarshalJsonStr(b.CyclesJSON, &cycles); err != nil {
					return err
				}
				if b.SyncExpiry {
					return errors.New("Applying synchronization controls requires separate permission")
				}
				for _, cycle := range cycles {
					if cycle.AlignReset || cycle.Enforce {
						return errors.New("Applying synchronization controls requires separate permission")
					}
				}
			}
		}
		if old.URL != input.URL && old.SecretPayload != "" && encrypted == nil {
			return errors.New("Replace saved request settings when changing the destination")
		}
		if old.Id == 0 {
			var count int64
			if err := tx.Model(&SubscriptionSyncSource{}).Count(&count).Error; err != nil {
				return err
			}
			if count >= 200 {
				return errors.New("Too many synchronization sources")
			}
		}
		mapping, err := cycleJSON(input.Mapping)
		if err != nil {
			return err
		}
		saved = input.SubscriptionSyncSource
		saved.SecretPayload = old.SecretPayload
		if encrypted != nil {
			saved.SecretPayload = *encrypted
		}
		// Never trust incoming runtime authentication state. Credential changes use
		// their own revision; automatic rotation never changes source.Version.
		saved.CredentialRevision = old.CredentialRevision
		saved.TokenExpiresAt = old.TokenExpiresAt
		saved.LastAuthSuccess = old.LastAuthSuccess
		saved.LastTokenRefresh = old.LastTokenRefresh
		saved.AuthError = old.AuthError
		saved.AuthNextAttempt = old.AuthNextAttempt
		saved.AuthLeaseUntil = old.AuthLeaseUntil
		saved.AuthLeaseToken = old.AuthLeaseToken
		saved.RefreshBlocked = old.RefreshBlocked
		if old.RefreshMode != saved.RefreshMode || old.RefreshURL != saved.RefreshURL {
			if encrypted == nil {
				return errors.New("Replace saved request settings when changing authentication mode")
			}
		}
		if encrypted != nil {
			saved.CredentialRevision++
			saved.TokenExpiresAt = 0
			saved.LastAuthSuccess = 0
			saved.LastTokenRefresh = 0
			saved.AuthError = ""
			saved.AuthNextAttempt = 0
			saved.AuthLeaseUntil = 0
			saved.AuthLeaseToken = ""
			saved.RefreshBlocked = false
		}
		saved.MappingJSON = mapping
		saved.Version = old.Version + 1
		saved.LastAttempt = old.LastAttempt
		saved.LastFetch = old.LastFetch
		saved.LastApplied = old.LastApplied
		saved.CatalogJSON = "[]"
		saved.LastError = ""
		saved.Failures = 0
		saved.NextRun = GetDBTimestamp()
		saved.LeaseUntil = 0
		saved.LeaseToken = ""
		if err := tx.Save(&saved).Error; err != nil {
			return err
		}
		if old.URL != saved.URL {
			var bindings []SubscriptionSyncBinding
			if err := tx.Where("source_id = ?", saved.Id).Find(&bindings).Error; err != nil {
				return err
			}
			for _, b := range bindings {
				key := common.GenerateHMACWithKey([]byte("subscription-resource-binding-v1"), saved.URL+"\x00"+b.AccountID)
				if err := tx.Model(&b).Update("resource_key", key).Error; err != nil {
					return errors.New("Upstream resource is already bound or unavailable")
				}
			}
		}
		return tx.Create(&SubscriptionSyncEvent{SourceID: saved.Id, ActorID: actorID, Action: "source_save", CreatedAt: GetDBTimestamp()}).Error
	})
	saved.HasCredentials = saved.SecretPayload != ""
	return &saved, err
}

func ClaimSubscriptionSyncSource(id int, token string, force bool) (*SubscriptionSyncSource, error) {
	var source SubscriptionSyncSource
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&source, id).Error; err != nil {
			return err
		}
		now := GetDBTimestamp()
		if source.LeaseUntil > now {
			return errors.New("Synchronization is already running")
		}
		if !force && (!source.Enabled || source.NextRun > now) {
			return errors.New("Synchronization is not due")
		}
		source.LeaseToken = token
		// This lease covers the bounded network call and processing. Every application
		// checks it again, so an expired worker cannot write after another takes over.
		source.LeaseUntil = now + 4*source.TimeoutSeconds + 120
		return tx.Model(&source).Updates(map[string]any{"lease_token": token, "lease_until": source.LeaseUntil, "last_attempt": now}).Error
	})
	return &source, err
}

func FinishSubscriptionSyncSource(source SubscriptionSyncSource, token string, accounts []SubscriptionSyncAccount, message string, applied bool, actorID int, retryAfter ...int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var current SubscriptionSyncSource
		if err := lockForUpdate(tx).First(&current, source.Id).Error; err != nil {
			return err
		}
		if current.LeaseToken != token || current.Version != source.Version || current.LeaseUntil < GetDBTimestamp() {
			return errors.New("Synchronization configuration changed; retry the request")
		}
		now := GetDBTimestamp()
		updates := map[string]any{"lease_token": "", "lease_until": 0, "last_error": message}
		action := "sync"
		if message != "" {
			failures := min(current.Failures+1, 10)
			delay := max(source.IntervalSeconds, min(source.IntervalSeconds*(int64(1)<<min(failures, 6)), int64(3600)))
			if len(retryAfter) > 0 {
				delay = max(delay, min(max(retryAfter[0], 0), int64(86400)))
			}
			if message == "Synchronization authentication failed" {
				delay = max(delay, int64(600))
			}
			updates["failures"] = failures
			updates["next_run"] = now + delay
			action = "sync_failed"
		} else {
			raw, err := cycleJSON(accounts)
			if err != nil {
				return err
			}
			updates["catalog_json"] = raw
			updates["last_fetch"] = now
			updates["failures"] = 0
			updates["next_run"] = now + source.IntervalSeconds
			if applied {
				updates["last_applied"] = now
			}
		}
		if err := tx.Model(&current).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Create(&SubscriptionSyncEvent{SourceID: source.Id, ActorID: actorID, Action: action, Message: message, CreatedAt: now}).Error; err != nil {
			return err
		}
		// Bounded per-source diagnostic history, including per-pool application events.
		var boundary SubscriptionSyncEvent
		result := tx.Where("source_id = ?", source.Id).Order("id desc").Offset(999).Limit(1).Find(&boundary)
		if result.Error != nil {
			return result.Error
		}
		if boundary.Id > 0 {
			return tx.Where("source_id = ? AND id < ?", source.Id, boundary.Id).Delete(&SubscriptionSyncEvent{}).Error
		}
		return nil
	})
}

type SubscriptionSyncChange struct {
	PreviousUtilization *float64 `json:"previous_utilization"`
	Utilization         *float64 `json:"utilization"`
	PoolID              int      `json:"pool_id"`
	Key                 string   `json:"key"`
	Before              int64    `json:"before"`
	After               int64    `json:"after"`
	Message             string   `json:"message"`
}

// The caller has fetched outside the transaction. Import and alignment share the
// pool's admission lock, but never replace Used/Reserved or reservation provenance.
func ApplySubscriptionSyncAccount(source SubscriptionSyncSource, token string, hint SubscriptionSyncBindingInput, account SubscriptionSyncAccount, actorID int, preview bool) ([]SubscriptionSyncChange, error) {
	changes := []SubscriptionSyncChange{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var currentSource SubscriptionSyncSource
		if err := lockForUpdate(tx).First(&currentSource, source.Id).Error; err != nil {
			return err
		}
		if currentSource.Version != source.Version || (!preview && (currentSource.LeaseToken != token || currentSource.LeaseUntil < GetDBTimestamp())) {
			return errors.New("Synchronization configuration changed; retry the request")
		}
		var pool SubscriptionPool
		if err := lockForUpdate(tx).First(&pool, hint.PoolID).Error; err != nil {
			return err
		}
		var b SubscriptionSyncBinding
		if err := tx.Where("pool_id = ?", hint.PoolID).First(&b).Error; err != nil {
			return err
		}
		if b.Version != hint.Version || b.SourceID != source.Id || b.AccountID != account.ID || !b.Enabled {
			return errors.New("Synchronization binding changed; retry the request")
		}
		var mappings []SubscriptionSyncCycleBinding
		if err := common.UnmarshalJsonStr(b.CyclesJSON, &mappings); err != nil {
			return err
		}
		var rules []SubscriptionCycleRule
		if err := common.UnmarshalJsonStr(pool.Rules, &rules); err != nil {
			return err
		}
		now := GetDBTimestamp()
		poolChanged := false
		for _, m := range mappings {
			change := SubscriptionSyncChange{PoolID: pool.Id, Key: m.LocalKey}
			var cycle *SubscriptionSyncCycle
			for i := range account.Cycles {
				if account.Cycles[i].Key == m.UpstreamKey {
					cycle = &account.Cycles[i]
					break
				}
			}
			if cycle == nil || cycle.Error != "" {
				if !preview {
					if err := tx.Model(&SubscriptionSyncObservation{}).Where("pool_id = ? AND local_key = ? AND source_version = ? AND binding_version = ?", pool.Id, m.LocalKey, source.Version, b.Version).Update("last_error", "Missing or inconsistent upstream cycle data").Error; err != nil {
						return err
					}
				}
				change.Message = "Missing or inconsistent upstream cycle data"
				changes = append(changes, change)
				continue
			}
			raw, err := cycleJSON(cycle)
			if err != nil {
				return err
			}
			hash := sha256.Sum256([]byte(raw))
			digest := hex.EncodeToString(hash[:])
			var previous SubscriptionSyncObservation
			if err := tx.Where("pool_id = ? AND local_key = ?", pool.Id, m.LocalKey).Find(&previous).Error; err != nil {
				return err
			}
			compatible := previous.BindingVersion == b.Version && previous.SourceVersion == source.Version
			change.Utilization = cycle.Utilization
			if compatible && previous.Payload != "" {
				var priorCycle SubscriptionSyncCycle
				if err := common.UnmarshalJsonStr(previous.Payload, &priorCycle); err != nil {
					return err
				}
				change.PreviousUtilization = priorCycle.Utilization
			}
			if compatible && previous.SampledAt >= cycle.SampledAt {
				if !preview && previous.SampledAt == cycle.SampledAt && previous.Digest == digest && previous.LastError != "" {
					if err := tx.Model(&previous).Update("last_error", "").Error; err != nil {
						return err
					}
				}
				change.Message = "Upstream observation is unchanged or older"
				if previous.SampledAt == cycle.SampledAt && previous.Digest != digest {
					change.Message = "Conflicting upstream observation timestamp"
				}
				changes = append(changes, change)
				continue
			}
			pending := ""
			stale := now-cycle.SampledAt > source.StaleSeconds || cycle.WindowEnd <= now
			if stale {
				pending = "Upstream observation is stale"
			}
			if m.AlignReset && !stale {
				for i := range rules {
					if rules[i].Key == m.LocalKey {
						var state SubscriptionCycleState
						if err := tx.Where("owner = ? AND cycle_key = ?", allocationOwner(0, pool.Id), m.LocalKey).First(&state).Error; err != nil {
							return err
						}
						// Preview does not advance a ledger window or create usage rows.
						end := state.EndTime
						if end <= now {
							end += (1 + (now-end)/rules[i].Seconds) * rules[i].Seconds
						}
						change.Before = end
						change.After = cycle.WindowEnd
						difference := end - cycle.WindowEnd
						if difference < 0 {
							difference = -difference
						}
						if cycle.Seconds != rules[i].Seconds || difference > m.MaxShiftSeconds {
							pending = "Upstream reset shift requires confirmation"
						} else if !preview && end != cycle.WindowEnd {
							active, err := subscriptionCycleStateTx(tx, state.Owner, rules[i], now)
							if err != nil {
								return err
							}
							var effective SubscriptionCycleRule
							if err := common.UnmarshalJsonStr(active.Rule, &effective); err != nil {
								return err
							}
							if effective.Seconds != cycle.Seconds {
								pending = "Upstream reset shift requires confirmation"
								break
							}
							active.StartTime = cycle.WindowStart
							active.EndTime = cycle.WindowEnd
							effective.NextReset = cycle.WindowEnd
							active.Rule, err = cycleJSON(effective)
							if err != nil {
								return err
							}
							if active.PendingRule != "" {
								var next SubscriptionCycleRule
								if err := common.UnmarshalJsonStr(active.PendingRule, &next); err != nil {
									return err
								}
								next.NextReset = cycle.WindowEnd
								active.PendingRule, err = cycleJSON(next)
								if err != nil {
									return err
								}
							}
							if err := tx.Save(active).Error; err != nil {
								return err
							}
							rules[i].NextReset = cycle.WindowEnd
							poolChanged = true
						}
						break
					}
				}
			}
			if pending != "" {
				change.Message = pending
			} else {
				change.Message = "Upstream observation accepted"
			}
			changes = append(changes, change)
			if preview {
				continue
			}
			observation := SubscriptionSyncObservation{Id: previous.Id, PoolID: pool.Id, LocalKey: m.LocalKey, SourceVersion: source.Version, BindingVersion: b.Version, Digest: digest, Payload: raw, SampledAt: cycle.SampledAt, FetchedAt: now, Pending: pending}
			if err := tx.Save(&observation).Error; err != nil {
				return err
			}
		}
		// Expiry requires a real, fresh account observation; request time is not evidence.
		if b.SyncExpiry && account.Expiry > 0 && account.SampledAt > 0 && account.SampledAt <= now+60 && now-account.SampledAt <= source.StaleSeconds && account.Expiry != pool.EndTime {
			changes = append(changes, SubscriptionSyncChange{PoolID: pool.Id, Key: "expiry", Before: pool.EndTime, After: account.Expiry, Message: "Upstream expiry aligned"})
			if !preview {
				if err := tx.Model(&UserSubscription{}).Where("id IN (?)", tx.Model(&SubscriptionAllocation{}).Select("subscription_id").Where("pool_id = ? AND follow_pool_end = ?", pool.Id, true)).Where("status = ? AND end_time > ?", "active", now).Update("end_time", account.Expiry).Error; err != nil {
					return err
				}
				pool.EndTime = account.Expiry
				poolChanged = true
			}
		}
		if poolChanged {
			raw, err := cycleJSON(rules)
			if err != nil {
				return err
			}
			if err := tx.Model(&pool).Updates(map[string]any{"rules": raw, "end_time": pool.EndTime, "version": pool.Version + 1, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		if !preview && b.LastError != "" {
			if err := tx.Model(&b).Update("last_error", "").Error; err != nil {
				return err
			}
		}
		if !preview && len(changes) > 0 {
			detail, err := cycleJSON(changes)
			if err != nil {
				return err
			}
			return tx.Create(&SubscriptionSyncEvent{SourceID: source.Id, PoolID: pool.Id, ActorID: actorID, Action: "pool_sync", Message: "Upstream synchronization evaluated", Detail: detail, CreatedAt: now}).Error
		}
		return nil
	})
	return changes, err
}

type SubscriptionSyncObservationView struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Utilization *float64 `json:"utilization"`
	SampledAt   int64    `json:"sampled_at"`
	FetchedAt   int64    `json:"fetched_at"`
	WindowEnd   int64    `json:"window_end"`
	Stale       bool     `json:"stale"`
	Pending     string   `json:"pending"`
	Digest      string   `json:"digest,omitempty"`
}

func subscriptionSyncViewsTx(tx *gorm.DB, poolID int, now int64) ([]SubscriptionSyncObservationView, error) {
	views := []SubscriptionSyncObservationView{}
	var b SubscriptionSyncBinding
	if err := tx.Where("pool_id = ?", poolID).Find(&b).Error; err != nil {
		return nil, err
	}
	if !b.Enabled || b.SourceID == 0 {
		return views, nil
	}
	var source SubscriptionSyncSource
	if err := tx.First(&source, b.SourceID).Error; err != nil {
		return nil, err
	}
	var records []SubscriptionSyncObservation
	if err := tx.Where("pool_id = ?", poolID).Find(&records).Error; err != nil {
		return nil, err
	}
	for _, r := range records {
		if r.BindingVersion != b.Version || r.SourceVersion != source.Version {
			continue
		}
		var c SubscriptionSyncCycle
		if err := common.UnmarshalJsonStr(r.Payload, &c); err != nil {
			return nil, err
		}
		if r.Pending == "" {
			r.Pending = r.LastError
		}
		if b.LastError != "" {
			r.Pending = b.LastError
		}
		views = append(views, SubscriptionSyncObservationView{Key: r.LocalKey, Name: c.Name, Utilization: c.Utilization, SampledAt: r.SampledAt, FetchedAt: r.FetchedAt, WindowEnd: c.WindowEnd, Stale: b.LastError != "" || r.LastError != "" || now-r.SampledAt > source.StaleSeconds || c.WindowEnd <= now, Pending: r.Pending, Digest: r.Digest})
	}
	return views, nil
}

func GetSubscriptionSyncViews(poolID int) ([]SubscriptionSyncObservationView, error) {
	return subscriptionSyncViewsTx(DB, poolID, GetDBTimestamp())
}

func enforceSubscriptionSyncTx(tx *gorm.DB, poolID int, now int64) error {
	var b SubscriptionSyncBinding
	if err := tx.Where("pool_id = ?", poolID).Find(&b).Error; err != nil {
		return err
	}
	if !b.Enabled || b.SourceID == 0 {
		return nil
	}
	var mappings []SubscriptionSyncCycleBinding
	if err := common.UnmarshalJsonStr(b.CyclesJSON, &mappings); err != nil {
		return err
	}
	var source SubscriptionSyncSource
	if err := tx.First(&source, b.SourceID).Error; err != nil {
		return err
	}
	for _, m := range mappings {
		if !m.Enforce {
			continue
		}
		var state SubscriptionCycleState
		if err := tx.Where("owner = ? AND cycle_key = ?", allocationOwner(0, poolID), m.LocalKey).First(&state).Error; err != nil {
			return err
		}
		var rule SubscriptionCycleRule
		if err := common.UnmarshalJsonStr(state.Rule, &rule); err != nil {
			return err
		}
		effective, err := subscriptionCycleStateTx(tx, state.Owner, rule, now)
		if err != nil {
			return err
		}
		if err := common.UnmarshalJsonStr(effective.Rule, &rule); err != nil {
			return err
		}
		if !rule.Enabled {
			continue
		}
		var r SubscriptionSyncObservation
		if err := tx.Where("pool_id = ? AND local_key = ?", poolID, m.LocalKey).Find(&r).Error; err != nil {
			return err
		}
		var c SubscriptionSyncCycle
		valid := r.Id > 0 && r.SourceVersion == source.Version && r.BindingVersion == b.Version && common.UnmarshalJsonStr(r.Payload, &c) == nil && c.Error == "" && c.Utilization != nil && !math.IsNaN(*c.Utilization) && !math.IsInf(*c.Utilization, 0)
		stale := b.LastError != "" || r.LastError != "" || !valid || now-r.SampledAt > source.StaleSeconds || c.WindowEnd <= now || r.Pending != ""
		if stale {
			// Grace never turns missing data into a zero balance or overrides a known
			// exhausted observation. It only allows local admission for a bounded age.
			if valid && *c.Utilization >= m.Threshold {
				return errors.New("subscription quota insufficient: upstream utilization limit reached")
			}
			if b.LastError != "" || r.LastError != "" || !valid || b.StalePolicy != "local" || b.GraceSeconds == 0 || now-r.SampledAt > source.StaleSeconds+b.GraceSeconds || r.Pending == "Upstream reset shift requires confirmation" {
				return errors.New("subscription quota insufficient: upstream observation is unavailable or stale")
			}
			continue
		}
		if *c.Utilization >= m.Threshold {
			return errors.New("subscription quota insufficient: upstream utilization limit reached")
		}
	}
	return nil
}

func ConfirmSubscriptionSyncReset(poolID int, key, digest string, poolVersion int64, actorID int) error {
	hint, err := GetSubscriptionSyncBinding(poolID)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var source SubscriptionSyncSource
		if err := lockForUpdate(tx).First(&source, hint.SourceID).Error; err != nil {
			return err
		}
		var pool SubscriptionPool
		if err := lockForUpdate(tx).First(&pool, poolID).Error; err != nil {
			return err
		}
		if pool.Version != poolVersion {
			return errors.New("Upstream pool changed; reload before saving")
		}
		var b SubscriptionSyncBinding
		if err := tx.First(&b, "pool_id = ?", poolID).Error; err != nil {
			return err
		}
		if b.Version != hint.Version || !b.Enabled || b.SourceID != source.Id {
			return errors.New("Synchronization binding changed; retry the request")
		}
		var mappings []SubscriptionSyncCycleBinding
		if err := common.UnmarshalJsonStr(b.CyclesJSON, &mappings); err != nil {
			return err
		}
		allowed := false
		for _, m := range mappings {
			if m.LocalKey == key && m.AlignReset {
				allowed = true
			}
		}
		if !allowed {
			return errors.New("Invalid synchronization cycle mapping")
		}
		var observation SubscriptionSyncObservation
		if err := tx.Where("pool_id = ? AND local_key = ?", poolID, key).First(&observation).Error; err != nil {
			return err
		}
		if observation.Digest != digest || observation.BindingVersion != b.Version || observation.SourceVersion != source.Version {
			return errors.New("Synchronization observation changed; preview again")
		}
		var cycle SubscriptionSyncCycle
		if err := common.UnmarshalJsonStr(observation.Payload, &cycle); err != nil {
			return err
		}
		now := GetDBTimestamp()
		if cycle.Error != "" || now-observation.SampledAt > source.StaleSeconds || cycle.WindowEnd <= now || cycle.WindowStart > now {
			return errors.New("Upstream observation is stale")
		}
		var rules []SubscriptionCycleRule
		if err := common.UnmarshalJsonStr(pool.Rules, &rules); err != nil {
			return err
		}
		for i := range rules {
			if rules[i].Key == key {
				if cycle.Seconds != rules[i].Seconds {
					return errors.New("Invalid synchronization cycle mapping")
				}
				state, err := subscriptionCycleStateTx(tx, allocationOwner(0, poolID), rules[i], now)
				if err != nil {
					return err
				}
				var active SubscriptionCycleRule
				if err := common.UnmarshalJsonStr(state.Rule, &active); err != nil {
					return err
				}
				if active.Seconds != cycle.Seconds {
					return errors.New("Invalid synchronization cycle mapping")
				}
				state.StartTime = cycle.WindowStart
				state.EndTime = cycle.WindowEnd
				active.NextReset = cycle.WindowEnd
				state.Rule, err = cycleJSON(active)
				if err != nil {
					return err
				}
				if state.PendingRule != "" {
					var pending SubscriptionCycleRule
					if err := common.UnmarshalJsonStr(state.PendingRule, &pending); err != nil {
						return err
					}
					pending.NextReset = cycle.WindowEnd
					state.PendingRule, err = cycleJSON(pending)
					if err != nil {
						return err
					}
				}
				if err := tx.Save(state).Error; err != nil {
					return err
				}
				rules[i].NextReset = cycle.WindowEnd
				raw, err := cycleJSON(rules)
				if err != nil {
					return err
				}
				if err := tx.Model(&pool).Updates(map[string]any{"rules": raw, "version": pool.Version + 1, "updated_at": now}).Error; err != nil {
					return err
				}
				if err := tx.Model(&observation).Update("pending", "").Error; err != nil {
					return err
				}
				return tx.Create(&SubscriptionSyncEvent{SourceID: source.Id, PoolID: poolID, ActorID: actorID, Action: "reset_confirm", Message: "Upstream reset alignment confirmed", CreatedAt: now}).Error
			}
		}
		return errors.New("Invalid synchronization cycle mapping")
	})
}

func MarkSubscriptionSyncAccountMissing(source SubscriptionSyncSource, token string, binding SubscriptionSyncBinding, actorID int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var current SubscriptionSyncSource
		if err := lockForUpdate(tx).First(&current, source.Id).Error; err != nil {
			return err
		}
		if current.Version != source.Version || current.LeaseToken != token || current.LeaseUntil < GetDBTimestamp() {
			return errors.New("Synchronization configuration changed; retry the request")
		}
		var pool SubscriptionPool
		if err := lockForUpdate(tx).First(&pool, binding.PoolID).Error; err != nil {
			return err
		}
		result := tx.Model(&SubscriptionSyncBinding{}).Where("pool_id = ? AND version = ? AND source_id = ?", binding.PoolID, binding.Version, source.Id).Update("last_error", "Bound upstream account was not found")
		if result.Error != nil {
			return result.Error
		}
		return tx.Create(&SubscriptionSyncEvent{SourceID: source.Id, PoolID: binding.PoolID, ActorID: actorID, Action: "binding_error", Message: "Bound upstream account was not found", CreatedAt: GetDBTimestamp()}).Error
	})
}

func UnbindSubscriptionSyncPool(poolID int, version int64, actorID int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var pool SubscriptionPool
		if err := lockForUpdate(tx).First(&pool, poolID).Error; err != nil {
			return err
		}
		var b SubscriptionSyncBinding
		if err := tx.First(&b, "pool_id = ?", poolID).Error; err != nil {
			return err
		}
		if b.Version != version {
			return errors.New("Synchronization binding changed; retry the request")
		}
		sourceID := b.SourceID
		// Keep a monotonic tombstone version: deleting/recreating version 1 would
		// accidentally validate observations or workers from a previous binding.
		if err := tx.Model(&b).Updates(map[string]any{"enabled": false, "source_id": 0, "account_id": "", "resource_key": nil, "cycles_json": "[]", "sync_expiry": false, "last_error": "", "version": b.Version + 1}).Error; err != nil {
			return err
		}
		return tx.Create(&SubscriptionSyncEvent{SourceID: sourceID, PoolID: poolID, ActorID: actorID, Action: "unbind", Message: "Synchronization binding removed", CreatedAt: GetDBTimestamp()}).Error
	})
}
