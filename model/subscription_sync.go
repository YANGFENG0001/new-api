package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
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

	FreshCycles           int      `json:"fresh_cycles" gorm:"-"`
	ReportedCycles        int      `json:"reported_cycles" gorm:"-"`
	OldestObservedAt      int64    `json:"oldest_observed_at" gorm:"-"`
	StaleAccounts         []string `json:"stale_accounts" gorm:"-"`
	ObservationStatus     string   `json:"observation_status" gorm:"-"`
	ObservedAt            int64    `json:"observed_at" gorm:"-"`
	EffectiveStaleSeconds int64    `json:"effective_stale_seconds" gorm:"-"`
	FetchStatus           string   `json:"fetch_status" gorm:"-"`
	Id                    int      `json:"id"`
	Name                  string   `json:"name" gorm:"type:varchar(128)"`
	URL                   string   `json:"url" gorm:"type:text"`
	Method                string   `json:"method" gorm:"type:varchar(8)"`
	Enabled               bool     `json:"enabled"`
	IntervalSeconds       int64    `json:"interval_seconds"`
	StaleSeconds          int64    `json:"stale_seconds"`
	TimeoutSeconds        int64    `json:"timeout_seconds"`
	AllowHTTP             bool     `json:"allow_http"`
	PrivateIPs            string   `json:"private_ips" gorm:"type:text"`
	Version               int64    `json:"version"`
	MappingJSON           string   `json:"-" gorm:"type:text"`
	SecretPayload         string   `json:"-" gorm:"type:text"`
	CatalogJSON           string   `json:"-" gorm:"type:text"`
	HasCredentials        bool     `json:"has_credentials" gorm:"-"`
	LastAttempt           int64    `json:"last_attempt"`
	LastFetch             int64    `json:"last_fetch"`
	LastApplied           int64    `json:"last_applied"`
	LastError             string   `json:"last_error" gorm:"type:varchar(128)"`
	Failures              int      `json:"failures"`
	NextRun               int64    `json:"next_run" gorm:"index"`
	LeaseUntil            int64    `json:"-"`
	LeaseToken            string   `json:"-" gorm:"type:varchar(64)"`
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

// Protocol keys (e.g. utilization_5h) are stable identifiers, not durations.
var subscriptionDurationName = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?[smhd]$`)

func SubscriptionCycleName(name string, seconds int64) string {
	if !subscriptionDurationName.MatchString(name) || seconds <= 0 {
		return name
	}
	for _, unit := range []struct {
		seconds int64
		suffix  string
	}{{86400, "d"}, {3600, "h"}, {60, "m"}, {1, "s"}} {
		if seconds%unit.seconds == 0 {
			return fmt.Sprintf("%d%s", seconds/unit.seconds, unit.suffix)
		}
	}
	return name
}

// Filter old cached catalogs too; a reported zero is a real observation.
func ReportedSubscriptionSyncAccounts(accounts []SubscriptionSyncAccount) []SubscriptionSyncAccount {
	result := make([]SubscriptionSyncAccount, 0, len(accounts))
	for _, account := range accounts {
		cycles := make([]SubscriptionSyncCycle, 0, len(account.Cycles))
		for _, cycle := range account.Cycles {
			if cycle.Utilization == nil {
				continue
			}
			cycle.Name = SubscriptionCycleName(cycle.Name, cycle.Seconds)
			cycles = append(cycles, cycle)
		}
		if len(cycles) == 0 {
			continue
		}
		account.Cycles = cycles
		result = append(result, account)
	}
	return result
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
	// Explicit administrator conversion, never inferred from an upstream percentage.
	QuotaPerFullWindow int64   `json:"quota_per_full_window,omitempty"`
	LocalKey           string  `json:"local_key"`
	UpstreamKey        string  `json:"upstream_key"`
	AlignReset         bool    `json:"align_reset"`
	Enforce            bool    `json:"enforce"`
	Threshold          float64 `json:"threshold"`
	MaxShiftSeconds    int64   `json:"max_shift_seconds"`
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

// Allow two complete polling intervals plus transport and scheduler jitter.
// A cached upstream response does not refresh its actual observation timestamp.
func SubscriptionSyncStaleSeconds(source SubscriptionSyncSource) int64 {
	return max(source.StaleSeconds, 2*source.IntervalSeconds+source.TimeoutSeconds+10)
}

func SaveSubscriptionSyncBinding(input SubscriptionSyncBindingInput, actorID int) error {
	return saveSubscriptionSyncBinding(DB, input, actorID)
}

func saveSubscriptionSyncBinding(db *gorm.DB, input SubscriptionSyncBindingInput, actorID int) error {
	if input.PoolID <= 0 || input.SourceID <= 0 || strings.TrimSpace(input.AccountID) == "" || len(input.AccountID) > 128 || len(input.Cycles) > 32 || (input.StalePolicy != "stop" && input.StalePolicy != "local") || input.GraceSeconds < 0 || input.GraceSeconds > 86400 {
		return errors.New("Invalid synchronization binding")
	}
	return db.Transaction(func(tx *gorm.DB) error {
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
			if c.QuotaPerFullWindow < 0 || c.QuotaPerFullWindow > common.MaxWalletQuota || seen[c.LocalKey] || local == nil || remote == nil || c.Threshold <= 0 || c.Threshold > 1 || math.IsNaN(c.Threshold) || math.IsInf(c.Threshold, 0) || c.MaxShiftSeconds < 0 || c.MaxShiftSeconds > remote.Seconds {
				return errors.New("Invalid synchronization cycle mapping")
			}
			seen[c.LocalKey] = true
		}
		raw, err := cycleJSON(input.Cycles)
		if err != nil {
			return err
		}
		if prior.PoolID != 0 && prior.SourceID == input.SourceID && prior.AccountID == input.AccountID && prior.Enabled == input.Enabled && prior.SyncExpiry == input.SyncExpiry && prior.StalePolicy == input.StalePolicy && prior.GraceSeconds == input.GraceSeconds && prior.CyclesJSON == raw {
			return nil // Unrelated saves do not invalidate or reapply observations.
		}
		b := input.SubscriptionSyncBinding
		b.LastError = ""
		b.Version = prior.Version + 1
		b.CyclesJSON = raw
		resourceKey := common.GenerateHMACWithKey([]byte("subscription-resource-binding-v1"), source.URL+"\x00"+input.AccountID)
		b.ResourceKey = &resourceKey
		// Save may fall back to an upsert. MySQL then updates a different pool on
		// a unique resource-key collision instead of rejecting duplicate ownership.
		var saved *gorm.DB
		if prior.PoolID == 0 {
			saved = tx.Create(&b)
		} else {
			saved = tx.Model(&prior).Select("*").Updates(&b)
		}
		if saved.Error != nil {
			return errors.New("Upstream resource is already bound or unavailable")
		}
		if err := tx.Create(&SubscriptionSyncEvent{SourceID: source.Id, PoolID: pool.Id, ActorID: actorID, Action: "binding_save", CreatedAt: GetDBTimestamp()}).Error; err != nil {
			return err
		}
		return applySubscriptionSyncCatalogTx(tx, source, b, actorID)
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
		var err error
		changes, err = applySubscriptionSyncAccountTx(tx, currentSource, hint, account, actorID, preview, false)
		return err
	})
	return changes, err
}

// Called under the same source-then-pool lock order as scheduled synchronization.
// Selecting a binding is explicit consent to adopt its current observed times.
func applySubscriptionSyncCatalogTx(tx *gorm.DB, source SubscriptionSyncSource, binding SubscriptionSyncBinding, actorID int) error {
	if !binding.Enabled || source.CatalogJSON == "" {
		return nil
	}
	var accounts []SubscriptionSyncAccount
	if err := common.UnmarshalJsonStr(source.CatalogJSON, &accounts); err != nil {
		return err
	}
	for _, account := range accounts {
		if account.ID != binding.AccountID {
			continue
		}
		_, err := applySubscriptionSyncAccountTx(tx, source, SubscriptionSyncBindingInput{SubscriptionSyncBinding: binding}, account, actorID, false, true)
		return err
	}
	return nil
}

func applySubscriptionSyncAccountTx(tx *gorm.DB, source SubscriptionSyncSource, hint SubscriptionSyncBindingInput, account SubscriptionSyncAccount, actorID int, preview, adopt bool) ([]SubscriptionSyncChange, error) {
	changes := []SubscriptionSyncChange{}
	err := func() error {
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
				if !preview && previous.SampledAt == cycle.SampledAt && previous.Digest == digest {
					if err := tx.Model(&previous).Updates(map[string]any{"last_error": "", "fetched_at": now}).Error; err != nil {
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
			stale := now-cycle.SampledAt > SubscriptionSyncStaleSeconds(source) || cycle.WindowEnd <= now
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
						if !adopt && (cycle.Seconds != rules[i].Seconds || difference > m.MaxShiftSeconds) {
							pending = "Upstream reset shift requires confirmation"
						} else if !preview && (end != cycle.WindowEnd || rules[i].Seconds != cycle.Seconds) {
							active, err := subscriptionCycleStateTx(tx, state.Owner, rules[i], now)
							if err != nil {
								return err
							}
							var effective SubscriptionCycleRule
							if err := common.UnmarshalJsonStr(active.Rule, &effective); err != nil {
								return err
							}
							if !adopt && effective.Seconds != cycle.Seconds {
								pending = "Upstream reset shift requires confirmation"
								break
							}
							active.StartTime = cycle.WindowStart
							active.EndTime = cycle.WindowEnd
							effective.NextReset = cycle.WindowEnd
							effective.Seconds = cycle.Seconds
							effective.Name = SubscriptionCycleName(effective.Name, cycle.Seconds)
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
								next.Seconds = cycle.Seconds
								next.Name = SubscriptionCycleName(next.Name, cycle.Seconds)
								active.PendingRule, err = cycleJSON(next)
								if err != nil {
									return err
								}
							}
							if err := tx.Save(active).Error; err != nil {
								return err
							}
							rules[i].NextReset = cycle.WindowEnd
							rules[i].Seconds = cycle.Seconds
							rules[i].Name = SubscriptionCycleName(rules[i].Name, cycle.Seconds)
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
			payload, err := subscriptionSyncPayloadTx(tx, pool.Id, m, *cycle)
			if err != nil {
				return err
			}
			observation := SubscriptionSyncObservation{Id: previous.Id, PoolID: pool.Id, LocalKey: m.LocalKey, SourceVersion: source.Version, BindingVersion: b.Version, Digest: digest, Payload: payload, SampledAt: cycle.SampledAt, FetchedAt: now, Pending: pending}
			if err := tx.Save(&observation).Error; err != nil {
				return err
			}
		}
		// Expiry requires a real, fresh account observation; request time is not evidence.
		if b.SyncExpiry && account.Expiry > 0 && account.SampledAt > 0 && account.SampledAt <= now+60 && now-account.SampledAt <= SubscriptionSyncStaleSeconds(source) && account.Expiry != pool.EndTime {
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
	}()
	return changes, err
}

type SubscriptionSyncObservationView struct {
	Seconds              int64    `json:"seconds"`
	EstimatedUtilization *float64 `json:"estimated_utilization"`
	LocalDelta           int64    `json:"local_delta"`
	Key                  string   `json:"key"`
	Name                 string   `json:"name"`
	Utilization          *float64 `json:"utilization"`
	SampledAt            int64    `json:"sampled_at"`
	FetchedAt            int64    `json:"fetched_at"`
	WindowEnd            int64    `json:"window_end"`
	Stale                bool     `json:"stale"`
	Pending              string   `json:"pending"`
	Digest               string   `json:"digest,omitempty"`
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
	// Display only windows present in the most recent successful response. Keep
	// immutable observations for admission checks, audit and in-flight settlement.
	reported := map[string]bool{}
	if source.LastFetch > 0 && source.CatalogJSON != "" {
		var accounts []SubscriptionSyncAccount
		if err := common.UnmarshalJsonStr(source.CatalogJSON, &accounts); err != nil {
			return nil, err
		}
		for _, account := range ReportedSubscriptionSyncAccounts(accounts) {
			if account.ID == b.AccountID {
				for _, cycle := range account.Cycles {
					reported[cycle.Key] = true
				}
			}
		}
	}
	var mappings []SubscriptionSyncCycleBinding
	if err := common.UnmarshalJsonStr(b.CyclesJSON, &mappings); err != nil {
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
		if c.Utilization == nil {
			continue
		}
		if source.LastFetch > 0 && source.CatalogJSON != "" {
			visible := false
			for _, mapping := range mappings {
				if mapping.LocalKey == r.LocalKey && reported[mapping.UpstreamKey] {
					visible = true
					break
				}
			}
			if !visible {
				continue
			}
		}
		// Age is derived below from the current effective threshold. A persisted
		// pre-upgrade age warning must not keep a valid observation stale forever.
		if r.Pending == "Upstream observation is stale" {
			r.Pending = ""
		}
		if r.Pending == "" {
			r.Pending = r.LastError
		}
		if b.LastError != "" {
			r.Pending = b.LastError
		}
		estimate, delta, _, err := subscriptionSyncEstimateTx(tx, poolID, r, now)
		if err != nil {
			return nil, err
		}
		views = append(views, SubscriptionSyncObservationView{EstimatedUtilization: estimate, LocalDelta: delta, Key: r.LocalKey, Name: SubscriptionCycleName(c.Name, c.Seconds), Seconds: c.Seconds, Utilization: c.Utilization, SampledAt: r.SampledAt, FetchedAt: r.FetchedAt, WindowEnd: c.WindowEnd, Stale: b.LastError != "" || r.LastError != "" || now-r.SampledAt > SubscriptionSyncStaleSeconds(source) || c.WindowEnd <= now, Pending: r.Pending, Digest: r.Digest})
	}
	return views, nil
}

func GetSubscriptionSyncViews(poolID int) ([]SubscriptionSyncObservationView, error) {
	return subscriptionSyncViewsTx(DB, poolID, GetDBTimestamp())
}

func enforceSubscriptionSyncTx(tx *gorm.DB, poolID int, now int64, amount int64) error {
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
		// Recompute age with the current threshold; reset confirmations remain
		// blocking and are never cleared by a freshness-policy upgrade.
		if r.Pending == "Upstream observation is stale" {
			r.Pending = ""
		}
		effectiveRatio := c.Utilization
		if valid && m.QuotaPerFullWindow > 0 {
			estimate, _, reservedRatio, err := subscriptionSyncEstimateTx(tx, poolID, r, now)
			if err != nil {
				return err
			}
			if estimate != nil {
				effectiveRatio = estimate
				if *estimate+reservedRatio+float64(amount)/float64(m.QuotaPerFullWindow) > m.Threshold {
					return errors.New("subscription quota insufficient: upstream utilization limit reached")
				}
			}
		}
		stale := b.LastError != "" || r.LastError != "" || !valid || now-r.SampledAt > SubscriptionSyncStaleSeconds(source) || c.WindowEnd <= now || r.Pending != ""
		if stale {
			// Grace never turns missing data into a zero balance or overrides a known
			// exhausted observation. It only allows local admission for a bounded age.
			if valid && *effectiveRatio >= m.Threshold {
				return errors.New("subscription quota insufficient: upstream utilization limit reached")
			}
			if b.LastError != "" || r.LastError != "" || !valid || b.StalePolicy != "local" || b.GraceSeconds == 0 || now-r.SampledAt > SubscriptionSyncStaleSeconds(source)+b.GraceSeconds || r.Pending == "Upstream reset shift requires confirmation" {
				return errors.New("subscription quota insufficient: upstream observation is unavailable or stale")
			}
			continue
		}
		if *effectiveRatio >= m.Threshold {
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
		var selectedMapping SubscriptionSyncCycleBinding
		for _, m := range mappings {
			if m.LocalKey == key && m.AlignReset {
				allowed = true
				selectedMapping = m
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
		if cycle.Error != "" || now-observation.SampledAt > SubscriptionSyncStaleSeconds(source) || cycle.WindowEnd <= now || cycle.WindowStart > now {
			return errors.New("Upstream observation is stale")
		}
		var rules []SubscriptionCycleRule
		if err := common.UnmarshalJsonStr(pool.Rules, &rules); err != nil {
			return err
		}
		for i := range rules {
			if rules[i].Key == key {
				state, err := subscriptionCycleStateTx(tx, allocationOwner(0, poolID), rules[i], now)
				if err != nil {
					return err
				}
				var active SubscriptionCycleRule
				if err := common.UnmarshalJsonStr(state.Rule, &active); err != nil {
					return err
				}
				state.StartTime = cycle.WindowStart
				state.EndTime = cycle.WindowEnd
				active.NextReset = cycle.WindowEnd
				active.Seconds = cycle.Seconds
				active.Name = SubscriptionCycleName(active.Name, cycle.Seconds)
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
					pending.Seconds = cycle.Seconds
					pending.Name = SubscriptionCycleName(pending.Name, cycle.Seconds)
					state.PendingRule, err = cycleJSON(pending)
					if err != nil {
						return err
					}
				}
				if err := tx.Save(state).Error; err != nil {
					return err
				}
				rules[i].NextReset = cycle.WindowEnd
				rules[i].Seconds = cycle.Seconds
				rules[i].Name = SubscriptionCycleName(rules[i].Name, cycle.Seconds)
				raw, err := cycleJSON(rules)
				if err != nil {
					return err
				}
				if err := tx.Model(&pool).Updates(map[string]any{"rules": raw, "version": pool.Version + 1, "updated_at": now}).Error; err != nil {
					return err
				}
				payload, err := subscriptionSyncPayloadTx(tx, poolID, selectedMapping, cycle)
				if err != nil {
					return err
				}
				if err := tx.Model(&observation).Updates(map[string]any{"pending": "", "payload": payload}).Error; err != nil {
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

// Only available when creating a resource. Existing pools retain their immutable
// cycle keys and are edited through the separately versioned binding endpoint.
type SubscriptionSyncPoolInput struct {
	SourceID      int    `json:"source_id"`
	SourceVersion int64  `json:"source_version"`
	AccountID     string `json:"account_id"`
	SyncExpiry    bool   `json:"sync_expiry"`
}

func createSubscriptionSyncPool(input SubscriptionPoolInput, actorID int) (*SubscriptionPool, error) {
	if input.Id != 0 || input.Sync.SourceID <= 0 || input.Sync.AccountID == "" {
		return nil, errors.New("Invalid synchronization binding")
	}
	var pool *SubscriptionPool
	err := DB.Transaction(func(tx *gorm.DB) error {
		var source SubscriptionSyncSource
		if err := lockForUpdate(tx).First(&source, input.Sync.SourceID).Error; err != nil {
			return err
		}
		if source.Version != input.Sync.SourceVersion {
			return errors.New("Synchronization configuration changed; reload before saving")
		}
		var accounts []SubscriptionSyncAccount
		if source.CatalogJSON == "" || common.UnmarshalJsonStr(source.CatalogJSON, &accounts) != nil {
			return errors.New("No upstream observation")
		}
		var account *SubscriptionSyncAccount
		for i := range accounts {
			if accounts[i].ID == input.Sync.AccountID {
				account = &accounts[i]
				break
			}
		}
		if account == nil {
			return errors.New("Bound upstream account was not found")
		}
		now := GetDBTimestamp()
		usable := map[string]SubscriptionSyncCycle{}
		for _, cycle := range account.Cycles {
			if cycle.Error == "" && cycle.Utilization != nil && cycle.SampledAt > 0 && cycle.SampledAt <= now+60 && cycle.WindowStart > 0 && cycle.WindowEnd > now && cycle.Seconds > 0 {
				usable[cycle.Key] = cycle
			}
		}
		if len(input.Cycles) == 0 {
			for _, cycle := range account.Cycles {
				if _, ok := usable[cycle.Key]; !ok {
					continue
				}
				name := cycle.Name
				if name == "" {
					name = cycle.Key
				}
				input.Cycles = append(input.Cycles, SubscriptionCycleRule{Key: cycle.Key, Name: name, Seconds: cycle.Seconds, NextReset: cycle.WindowEnd, Unlimited: true, Enabled: true})
			}
		}
		if len(input.Cycles) == 0 {
			return errors.New("Missing or inconsistent upstream cycle data")
		}
		mappings := []SubscriptionSyncCycleBinding{}
		for _, rule := range input.Cycles {
			cycle, ok := usable[rule.Key]
			if !ok {
				continue
			} // extra manually configured local cycles are not bound implicitly
			mappings = append(mappings, SubscriptionSyncCycleBinding{LocalKey: rule.Key, UpstreamKey: cycle.Key, AlignReset: rule.Seconds == cycle.Seconds && rule.NextReset == cycle.WindowEnd, Enforce: true, Threshold: 1, MaxShiftSeconds: min(300, cycle.Seconds)})
		}
		if len(mappings) == 0 {
			return errors.New("Invalid synchronization cycle mapping")
		}
		if input.EndTime == 0 {
			input.EndTime = 253402300799 // no upstream expiry reported; never invent a 30-day subscription
			if account.Expiry > 0 {
				input.EndTime = account.Expiry
			}
		}
		var err error
		pool, err = saveSubscriptionPool(tx, input, actorID)
		if err != nil {
			return err
		}
		binding := SubscriptionSyncBindingInput{SubscriptionSyncBinding: SubscriptionSyncBinding{PoolID: pool.Id, SourceID: source.Id, AccountID: account.ID, Enabled: true, SyncExpiry: input.Sync.SyncExpiry, StalePolicy: "stop"}, Cycles: mappings}
		if err := saveSubscriptionSyncBinding(tx, binding, actorID); err != nil {
			return err
		}
		return nil
	})
	return pool, err
}

// This server-only baseline is stored inside the existing observation payload.
// It is not an upstream usage fact and never modifies billing Used/Reserved.
type subscriptionSyncBaseline struct {
	UsageID  int   `json:"usage_id"`
	Used     int64 `json:"used"`
	Capacity int64 `json:"capacity"`
}
type subscriptionSyncPayload struct {
	SubscriptionSyncCycle
	Baseline *subscriptionSyncBaseline `json:"local_baseline,omitempty"`
}

func subscriptionSyncPayloadTx(tx *gorm.DB, poolID int, mapping SubscriptionSyncCycleBinding, cycle SubscriptionSyncCycle) (string, error) {
	payload := subscriptionSyncPayload{SubscriptionSyncCycle: cycle}
	if mapping.QuotaPerFullWindow > 0 {
		var state SubscriptionCycleState
		if err := tx.Where("owner = ? AND cycle_key = ?", allocationOwner(0, poolID), mapping.LocalKey).First(&state).Error; err != nil {
			return "", err
		}
		// A changed reset boundary must be aligned/confirmed before using local deltas.
		if state.EndTime == cycle.WindowEnd && state.StartTime < cycle.WindowEnd {
			usage, err := cycleUsageTx(tx, &state, 0)
			if err != nil {
				return "", err
			}
			payload.Baseline = &subscriptionSyncBaseline{UsageID: usage.Id, Used: usage.Used, Capacity: mapping.QuotaPerFullWindow}
		}
	}
	return cycleJSON(payload)
}

func subscriptionSyncEstimateTx(tx *gorm.DB, poolID int, observation SubscriptionSyncObservation, now int64) (*float64, int64, float64, error) {
	var payload subscriptionSyncPayload
	if observation.Payload == "" {
		return nil, 0, 0, nil
	}
	if err := common.UnmarshalJsonStr(observation.Payload, &payload); err != nil {
		return nil, 0, 0, err
	}
	b := payload.Baseline
	if b == nil || b.Capacity <= 0 || b.Capacity > common.MaxWalletQuota || payload.Utilization == nil || payload.WindowEnd <= now {
		return nil, 0, 0, nil
	}
	var state SubscriptionCycleState
	if err := tx.Where("owner = ? AND cycle_key = ?", allocationOwner(0, poolID), observation.LocalKey).First(&state).Error; err != nil {
		return nil, 0, 0, err
	}
	if state.EndTime != payload.WindowEnd {
		return nil, 0, 0, nil
	}
	var usage SubscriptionCycleUsage
	result := tx.Where("state_id = ? AND epoch = ? AND user_id = ?", state.Id, state.Epoch, 0).Order("generation desc").Limit(1).Find(&usage)
	if result.Error != nil {
		return nil, 0, 0, result.Error
	}
	if usage.Id != b.UsageID {
		return nil, 0, 0, nil
	} // reset/generation invalidates the baseline
	delta := max(int64(0), usage.Used-b.Used)
	ratio := *payload.Utilization + float64(delta)/float64(b.Capacity)
	return &ratio, delta, float64(usage.Reserved) / float64(b.Capacity), nil
}

func DeleteSubscriptionSyncSource(id int, version, revision int64, actorID int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var source SubscriptionSyncSource
		if err := lockForUpdate(tx).First(&source, id).Error; err != nil {
			return err
		}
		if source.Version != version || source.CredentialRevision != revision {
			return errors.New("Synchronization configuration changed; reload before saving")
		}
		var count int64
		if err := tx.Model(&SubscriptionSyncBinding{}).Where("source_id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("Unbind the pools linked to this source before deleting it")
		}
		if err := tx.Create(&SubscriptionSyncEvent{SourceID: id, ActorID: actorID, Action: "delete", CreatedAt: GetDBTimestamp()}).Error; err != nil {
			return err
		}
		// Removing the row also invalidates every in-flight worker/auth lease.
		return tx.Delete(&source).Error
	})
}
