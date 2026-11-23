package model

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Cycle durations are elapsed seconds, not calendar periods. Keys are immutable.
type SubscriptionCycleRule struct {
	Key             string `json:"key"`
	Name            string `json:"name"`
	Seconds         int64  `json:"seconds"`
	NextReset       int64  `json:"next_reset"`
	Limit           int64  `json:"limit"`
	Unlimited       bool   `json:"unlimited"`
	Shared          bool   `json:"shared"`
	Enabled         bool   `json:"enabled"`
	UpstreamLimit   int64  `json:"upstream_limit"`
	EnforceUpstream bool   `json:"enforce_upstream"`
}

type SubscriptionPool struct {
	Id             int    `json:"id"`
	Name           string `json:"name" gorm:"type:varchar(128);not null"`
	Note           string `json:"note" gorm:"type:text"`
	Enabled        bool   `json:"enabled"`
	EndTime        int64  `json:"end_time"`
	Version        int64  `json:"version"`
	Rules          string `json:"-" gorm:"type:text"`
	ExternalSource string `json:"external_source" gorm:"type:varchar(64)"`
	ExternalID     string `json:"external_id" gorm:"type:varchar(128)"`
	UpdatedAt      int64  `json:"updated_at"`
}

// Channels grant access to subscription resources, not physical upstream account
// identity. A shared gateway may serve several pools and rotate upstream keys.
type SubscriptionPoolChannel struct {
	ChannelID int `json:"channel_id" gorm:"primaryKey;autoIncrement:false"`
	PoolID    int `json:"pool_id" gorm:"primaryKey;autoIncrement:false;index"`
}

type SubscriptionCycleOverride struct {
	Key       string `json:"key"`
	Limit     int64  `json:"limit"`
	Unlimited bool   `json:"unlimited"`
}

// Sidecar tables preserve the legacy subscription response contract.
type SubscriptionPlanAllocation struct {
	PlanID             int                         `json:"plan_id" gorm:"primaryKey;autoIncrement:false"`
	Mode               string                      `json:"mode" gorm:"type:varchar(16)"` // legacy/independent/fixed/assign
	PoolID             int                         `json:"pool_id"`
	FixedEndTime       int64                       `json:"fixed_end_time"`
	Rules              string                      `json:"-" gorm:"type:text"`
	QuotaMultiplier    float64                     `json:"quota_multiplier"`
	CycleOverridesJSON string                      `json:"-" gorm:"column:cycle_overrides;type:text"`
	CycleOverrides     []SubscriptionCycleOverride `json:"cycle_overrides" gorm:"-"`
	Version            int64                       `json:"version"`
}

type SubscriptionAllocation struct {
	FollowPoolEnd      bool                        `json:"follow_pool_end"`
	SubscriptionID     int                         `json:"subscription_id" gorm:"primaryKey;autoIncrement:false"`
	UserID             int                         `json:"user_id" gorm:"index"`
	PoolID             int                         `json:"pool_id" gorm:"index"`
	Pending            bool                        `json:"pending"`
	Rules              string                      `json:"-" gorm:"type:text"`
	QuotaMultiplier    float64                     `json:"quota_multiplier"`
	CycleOverridesJSON string                      `json:"-" gorm:"column:cycle_overrides;type:text"`
	CycleOverrides     []SubscriptionCycleOverride `json:"cycle_overrides" gorm:"-"`
	Version            int64                       `json:"version"`
}

type SubscriptionCycleState struct {
	Id          int    `json:"id"`
	Owner       string `json:"owner" gorm:"type:varchar(64);uniqueIndex:idx_sub_cycle_owner,priority:1"`
	CycleKey    string `json:"key" gorm:"type:varchar(64);uniqueIndex:idx_sub_cycle_owner,priority:2"`
	Epoch       int64  `json:"epoch"`
	StartTime   int64  `json:"start_time"`
	EndTime     int64  `json:"end_time"`
	Rule        string `json:"-" gorm:"type:text"`
	PendingRule string `json:"-" gorm:"type:text"`
}

type SubscriptionCycleUsage struct {
	Generation int64 `json:"generation" gorm:"uniqueIndex:idx_sub_cycle_usage,priority:4"`
	Id         int   `json:"id"`
	StateID    int   `json:"state_id" gorm:"uniqueIndex:idx_sub_cycle_usage,priority:1"`
	Epoch      int64 `json:"epoch" gorm:"uniqueIndex:idx_sub_cycle_usage,priority:2"`
	UserID     int   `json:"user_id" gorm:"uniqueIndex:idx_sub_cycle_usage,priority:3"` // 0 = aggregate
	Used       int64 `json:"used"`
	Reserved   int64 `json:"reserved"`
	Adjustment int64 `json:"adjustment"` // calibration is not real local consumption
}

type SubscriptionCycleReservation struct {
	Id             int
	RequestID      string `gorm:"type:varchar(191);uniqueIndex"`
	SubscriptionID int    `gorm:"index"`
	UserID         int
	PoolID         int
	Owner          string `gorm:"type:varchar(64)"`
	UsageIDs       string `gorm:"type:text"`
	Reserved       int64
	Settled        int64
	Status         string `gorm:"type:varchar(16)"` // reserved/settled/refunded
	CreatedAt      int64
}

// Imported observations never overwrite the local ledger. Future providers feed
// this same authenticated, versioned boundary, rather than fetching arbitrary URLs.
type SubscriptionUpstreamSnapshot struct {
	Id          int    `json:"id"`
	Calibrated  bool   `json:"calibrated"`
	PoolID      int    `json:"pool_id" gorm:"uniqueIndex:idx_sub_pool_event,priority:1"`
	EventID     string `json:"event_id" gorm:"type:varchar(128);uniqueIndex:idx_sub_pool_event,priority:2"`
	CycleKey    string `json:"key" gorm:"type:varchar(64);index"`
	Source      string `json:"source" gorm:"type:varchar(64)"`
	WindowStart int64  `json:"window_start"`
	WindowEnd   int64  `json:"window_end"`
	SampledAt   int64  `json:"sampled_at"`
	Unit        string `json:"unit" gorm:"type:varchar(32)"`
	Used        int64  `json:"used"`
	Limit       int64  `json:"limit"`
}

type SubscriptionPoolEvent struct {
	Id             int    `json:"id"`
	PoolID         int    `json:"pool_id" gorm:"index"`
	ActorID        int    `json:"actor_id"`
	Action         string `json:"action" gorm:"type:varchar(32)"`
	SubscriptionID int    `json:"subscription_id"`
	Detail         string `json:"detail" gorm:"type:text"`
	CreatedAt      int64  `json:"created_at"`
}

var subscriptionPoolMigrationMu sync.Mutex

func MigrateSubscriptionPools() error {
	subscriptionPoolMigrationMu.Lock()
	defer subscriptionPoolMigrationMu.Unlock()
	return DB.Connection(func(db *gorm.DB) error {
		switch db.Dialector.Name() {
		case "mysql":
			var acquired int
			if err := db.Raw("SELECT GET_LOCK('new-api.subscription-pools', 30)").Scan(&acquired).Error; err != nil {
				return err
			}
			if acquired != 1 {
				return errors.New("Subscription migration lock unavailable")
			}
			defer db.Exec("SELECT RELEASE_LOCK('new-api.subscription-pools')")
		case "postgres":
			if err := db.Exec("SELECT pg_advisory_lock(hashtext(current_database()), hashtext('new-api.subscription-pools'))").Error; err != nil {
				return err
			}
			defer db.Exec("SELECT pg_advisory_unlock(hashtext(current_database()), hashtext('new-api.subscription-pools'))")
		}
		if err := migrateSubscriptionPoolChannelKey(db); err != nil {
			return err
		}
		return db.AutoMigrate(SubscriptionPoolModels()...)
	})
}

// AutoMigrate does not replace an existing single-column primary key. Upgrade
// only the known legacy shape, retaining the same authoritative relation table.
func migrateSubscriptionPoolChannelKey(db *gorm.DB) error {
	const table = "subscription_pool_channels"
	if !db.Migrator().HasTable(table) {
		return nil
	}
	columns, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return err
	}
	keys := []string{}
	for _, column := range columns {
		if primary, ok := column.PrimaryKey(); ok && primary {
			keys = append(keys, column.Name())
		}
	}
	if len(keys) == 2 && ((keys[0] == "channel_id" && keys[1] == "pool_id") || (keys[0] == "pool_id" && keys[1] == "channel_id")) {
		return nil
	}
	if len(keys) != 1 || keys[0] != "channel_id" {
		return errors.New("Unsupported subscription channel primary key")
	}
	switch db.Dialector.Name() {
	case "mysql":
		// A single ALTER is atomic for this key change, including on MySQL 5.7.
		return db.Exec("ALTER TABLE subscription_pool_channels DROP PRIMARY KEY, ADD PRIMARY KEY (channel_id, pool_id)").Error
	case "postgres":
		return db.Transaction(func(tx *gorm.DB) error {
			var name string
			if err := tx.Raw("SELECT conname FROM pg_constraint WHERE conrelid = to_regclass(?) AND contype = 'p'", table).Scan(&name).Error; err != nil {
				return err
			}
			if name == "" {
				return errors.New("Subscription channel primary key is missing")
			}
			if err := tx.Exec("ALTER TABLE subscription_pool_channels DROP CONSTRAINT ?", clause.Column{Name: name}).Error; err != nil {
				return err
			}
			return tx.Exec("ALTER TABLE subscription_pool_channels ADD PRIMARY KEY (channel_id, pool_id)").Error
		})
	case "sqlite":
		return db.Transaction(func(tx *gorm.DB) error {
			// Obtain SQLite's writer reservation before inspecting/copying rows.
			if err := tx.Exec("UPDATE subscription_pool_channels SET pool_id = pool_id WHERE 1 = 0").Error; err != nil {
				return err
			}
			const replacement = "subscription_pool_channels_upgrade"
			if tx.Migrator().HasTable(replacement) {
				return errors.New("Subscription channel migration requires reconciliation")
			}
			if err := tx.Exec("CREATE TABLE subscription_pool_channels_upgrade (channel_id integer NOT NULL, pool_id integer NOT NULL, PRIMARY KEY (channel_id, pool_id))").Error; err != nil {
				return err
			}
			if err := tx.Exec("INSERT INTO subscription_pool_channels_upgrade (channel_id, pool_id) SELECT channel_id, pool_id FROM subscription_pool_channels").Error; err != nil {
				return err
			}
			var original, copied int64
			if err := tx.Table(table).Count(&original).Error; err != nil {
				return err
			}
			if err := tx.Table(replacement).Count(&copied).Error; err != nil {
				return err
			}
			if copied != original {
				return errors.New("Subscription channel migration row count mismatch")
			}
			if err := tx.Migrator().DropTable(table); err != nil {
				return err
			}
			return tx.Migrator().RenameTable(replacement, table)
		})
	default:
		return errors.New("Unsupported subscription channel database")
	}
}

func SubscriptionPoolModels() []any {
	return []any{&SubscriptionPool{}, &SubscriptionPoolChannel{}, &SubscriptionPlanAllocation{}, &SubscriptionAllocation{}, &SubscriptionCycleState{}, &SubscriptionCycleUsage{}, &SubscriptionCycleReservation{}, &SubscriptionUpstreamSnapshot{}, &SubscriptionPoolEvent{}, &SubscriptionSyncSource{}, &SubscriptionSyncBinding{}, &SubscriptionSyncObservation{}, &SubscriptionSyncEvent{}}
}

func ValidateSubscriptionCycles(rules []SubscriptionCycleRule, pooled bool) error {
	if len(rules) > 32 {
		return errors.New("Too many subscription cycles")
	}
	keys := map[string]bool{}
	for _, r := range rules {
		if len(r.Key) == 0 || len(r.Key) > 64 || strings.IndexFunc(r.Key, func(c rune) bool {
			return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-')
		}) >= 0 || keys[r.Key] {
			return errors.New("Invalid or duplicate cycle key")
		}
		keys[r.Key] = true
		if strings.TrimSpace(r.Name) == "" || len(r.Name) > 128 || r.Seconds <= 0 || r.Seconds > 315360000 || r.NextReset <= 0 || r.NextReset > 253402300799 || r.Limit < 0 || r.Limit > common.MaxWalletQuota || r.UpstreamLimit < 0 || r.UpstreamLimit > common.MaxWalletQuota || (!r.Unlimited && r.Limit == 0) || (!pooled && r.Shared) || (r.Shared && r.EnforceUpstream) || (r.EnforceUpstream && r.UpstreamLimit == 0) {
			return errors.New("Invalid subscription cycle")
		}
	}
	return nil
}

func cycleJSON(v any) (string, error) { b, e := common.Marshal(v); return string(b), e }

func validSubscriptionCycleKey(key string) bool {
	return len(key) > 0 && len(key) <= 64 && strings.IndexFunc(key, func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-')
	}) < 0
}

func NormalizeSubscriptionQuotaMultiplier(multiplier float64) float64 {
	if multiplier == 0 {
		return 1
	}
	return multiplier
}

func ParseSubscriptionCycleOverrides(raw string) ([]SubscriptionCycleOverride, error) {
	if strings.TrimSpace(raw) == "" {
		return []SubscriptionCycleOverride{}, nil
	}
	var overrides []SubscriptionCycleOverride
	if err := common.UnmarshalJsonStr(raw, &overrides); err != nil {
		return nil, err
	}
	if overrides == nil {
		return []SubscriptionCycleOverride{}, nil
	}
	return overrides, nil
}

func encodeSubscriptionCycleOverrides(overrides []SubscriptionCycleOverride) (string, error) {
	if len(overrides) == 0 {
		return "", nil
	}
	return cycleJSON(overrides)
}

func HydrateSubscriptionPlanAllocation(cfg *SubscriptionPlanAllocation) error {
	if cfg == nil {
		return nil
	}
	overrides, err := ParseSubscriptionCycleOverrides(cfg.CycleOverridesJSON)
	if err != nil {
		return err
	}
	cfg.CycleOverrides = overrides
	if cfg.Mode == "fixed" || cfg.Mode == "assign" {
		cfg.QuotaMultiplier = NormalizeSubscriptionQuotaMultiplier(cfg.QuotaMultiplier)
	}
	return nil
}

func HydrateSubscriptionAllocation(a *SubscriptionAllocation) error {
	if a == nil {
		return nil
	}
	overrides, err := ParseSubscriptionCycleOverrides(a.CycleOverridesJSON)
	if err != nil {
		return err
	}
	a.CycleOverrides = overrides
	if a.PoolID > 0 || a.Pending {
		a.QuotaMultiplier = NormalizeSubscriptionQuotaMultiplier(a.QuotaMultiplier)
	}
	return nil
}

func ValidateSubscriptionQuotaAllocation(multiplier float64, overrides []SubscriptionCycleOverride, poolRules []SubscriptionCycleRule) (float64, []SubscriptionCycleOverride, error) {
	normalized := NormalizeSubscriptionQuotaMultiplier(multiplier)
	if math.IsNaN(normalized) || math.IsInf(normalized, 0) || normalized < 0.01 || normalized > 100 {
		return 0, nil, errors.New("Invalid quota multiplier")
	}
	if len(overrides) > 32 {
		return 0, nil, errors.New("Too many subscription cycles")
	}
	var poolByKey map[string]SubscriptionCycleRule
	if poolRules != nil {
		poolByKey = make(map[string]SubscriptionCycleRule, len(poolRules))
		for _, r := range poolRules {
			poolByKey[r.Key] = r
		}
	}
	seen := make(map[string]bool, len(overrides))
	cleaned := make([]SubscriptionCycleOverride, 0, len(overrides))
	for _, ov := range overrides {
		if !validSubscriptionCycleKey(ov.Key) || seen[ov.Key] {
			return 0, nil, errors.New("Invalid or duplicate cycle key")
		}
		seen[ov.Key] = true
		if ov.Limit < 0 || ov.Limit > common.MaxWalletQuota || (!ov.Unlimited && ov.Limit == 0) {
			return 0, nil, errors.New("Invalid subscription cycle override")
		}
		if ov.Unlimited {
			ov.Limit = 0
		}
		if poolByKey != nil {
			poolRule, ok := poolByKey[ov.Key]
			if !ok {
				return 0, nil, errors.New("Invalid subscription cycle override")
			}
			if poolRule.Shared {
				return 0, nil, errors.New("Shared cycles cannot have per-plan quota overrides")
			}
		}
		cleaned = append(cleaned, ov)
	}
	return normalized, cleaned, nil
}

func filterSubscriptionCycleOverridesForPool(overrides []SubscriptionCycleOverride, poolRules []SubscriptionCycleRule) []SubscriptionCycleOverride {
	if len(overrides) == 0 || len(poolRules) == 0 {
		return []SubscriptionCycleOverride{}
	}
	poolByKey := make(map[string]SubscriptionCycleRule, len(poolRules))
	for _, r := range poolRules {
		poolByKey[r.Key] = r
	}
	filtered := make([]SubscriptionCycleOverride, 0, len(overrides))
	for _, ov := range overrides {
		rule, ok := poolByKey[ov.Key]
		if !ok || rule.Shared {
			continue
		}
		filtered = append(filtered, ov)
	}
	return filtered
}

func cycleOverridesMap(overrides []SubscriptionCycleOverride) map[string]SubscriptionCycleOverride {
	if len(overrides) == 0 {
		return nil
	}
	m := make(map[string]SubscriptionCycleOverride, len(overrides))
	for _, ov := range overrides {
		m[ov.Key] = ov
	}
	return m
}

func scaleSubscriptionQuota(limit int64, multiplier float64) int64 {
	if limit <= 0 || multiplier <= 0 || multiplier == 1 {
		return limit
	}
	scaled := math.Round(float64(limit) * multiplier)
	if scaled < 1 {
		return 1
	}
	if scaled >= float64(common.MaxWalletQuota) {
		return common.MaxWalletQuota
	}
	return int64(scaled)
}

func applySubscriptionCycleAllocation(rule SubscriptionCycleRule, multiplier float64, overrides map[string]SubscriptionCycleOverride) SubscriptionCycleRule {
	if rule.Shared {
		return rule
	}
	if ov, ok := overrides[rule.Key]; ok {
		rule.Unlimited = ov.Unlimited
		if ov.Unlimited {
			rule.Limit = 0
		} else {
			rule.Limit = ov.Limit
		}
		return rule
	}
	if !rule.Unlimited {
		rule.Limit = scaleSubscriptionQuota(rule.Limit, NormalizeSubscriptionQuotaMultiplier(multiplier))
	}
	return rule
}

func allocationOwner(subID, poolID int) string {
	if poolID > 0 {
		return fmt.Sprintf("pool:%d", poolID)
	}
	return fmt.Sprintf("subscription:%d", subID)
}

// Every mutation of a resource's counters, windows, assignments, or snapshots
// holds this resource lock first. This serializes members on MySQL/PostgreSQL;
// SQLite callers use the existing immediate-write transaction configuration.
func lockSubscriptionResource(tx *gorm.DB, subID, poolID int) error {
	if poolID > 0 {
		var p SubscriptionPool
		return lockForUpdate(tx).First(&p, poolID).Error
	}
	var a SubscriptionAllocation
	return lockForUpdate(tx).Where("subscription_id = ?", subID).First(&a).Error
}
