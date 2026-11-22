package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
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

// A channel must represent a deterministic upstream account, not a key rotation
// across different accounts. One channel can belong to at most one resource.
type SubscriptionPoolChannel struct {
	ChannelID int `json:"channel_id" gorm:"primaryKey;autoIncrement:false"`
	PoolID    int `json:"pool_id" gorm:"index"`
}

// Sidecar tables preserve the legacy subscription response contract.
type SubscriptionPlanAllocation struct {
	PlanID       int    `json:"plan_id" gorm:"primaryKey;autoIncrement:false"`
	Mode         string `json:"mode" gorm:"type:varchar(16)"` // independent/fixed/assign
	PoolID       int    `json:"pool_id"`
	FixedEndTime int64  `json:"fixed_end_time"`
	Rules        string `json:"-" gorm:"type:text"`
	Version      int64  `json:"version"`
}

type SubscriptionAllocation struct {
	FollowPoolEnd  bool   `json:"follow_pool_end"`
	SubscriptionID int    `json:"subscription_id" gorm:"primaryKey;autoIncrement:false"`
	UserID         int    `json:"user_id" gorm:"index"`
	PoolID         int    `json:"pool_id" gorm:"index"`
	Pending        bool   `json:"pending"`
	Rules          string `json:"-" gorm:"type:text"`
	Version        int64  `json:"version"`
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

func MigrateSubscriptionPools() error { return DB.AutoMigrate(SubscriptionPoolModels()...) }

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
