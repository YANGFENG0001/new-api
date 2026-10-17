package model

import (
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
)

// ---------------------------------------------------------------------------
// User control policies
// ---------------------------------------------------------------------------

// UserControlPolicy is a reusable bundle of limits plus an optional schedule.
// Limits and Schedule are JSON documents parsed by service/usercontrol.
type UserControlPolicy struct {
	Id          int    `json:"id" gorm:"primaryKey"`
	Name        string `json:"name" gorm:"type:varchar(64);uniqueIndex;not null"`
	Description string `json:"description" gorm:"type:varchar(255)"`
	Enabled     bool   `json:"enabled" gorm:"default:true;index"`
	Priority    int    `json:"priority" gorm:"default:0"`
	Limits      string `json:"limits" gorm:"type:text"`
	Schedule    string `json:"schedule" gorm:"type:text"`
	Version     int64  `json:"version" gorm:"bigint;default:1"`
	CreatedBy   int    `json:"created_by"`
	UpdatedBy   int    `json:"updated_by"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint"`
}

func (UserControlPolicy) TableName() string { return "user_control_policies" }

// UserControlList is a named group of users that shares one policy.
type UserControlList struct {
	Id          int    `json:"id" gorm:"primaryKey"`
	Name        string `json:"name" gorm:"type:varchar(64);uniqueIndex;not null"`
	Description string `json:"description" gorm:"type:varchar(255)"`
	PolicyId    int    `json:"policy_id" gorm:"index;default:0"`
	Enabled     bool   `json:"enabled" gorm:"default:true;index"`
	CreatedBy   int    `json:"created_by"`
	UpdatedBy   int    `json:"updated_by"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint"`
}

func (UserControlList) TableName() string { return "user_control_lists" }

type UserControlListMember struct {
	Id        int    `json:"id" gorm:"primaryKey"`
	ListId    int    `json:"list_id" gorm:"index;uniqueIndex:idx_uc_list_user,priority:1"`
	UserId    int    `json:"user_id" gorm:"index;uniqueIndex:idx_uc_list_user,priority:2"`
	Note      string `json:"note" gorm:"type:varchar(255)"`
	AddedBy   int    `json:"added_by"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
}

func (UserControlListMember) TableName() string { return "user_control_list_members" }

// UserControlUserOverride attaches limits directly to one user. Mode merge
// tightens inherited limits; mode replace swaps the inherited bundle and is
// only available to root (or a holder of user_control.override_relax).
type UserControlUserOverride struct {
	Id        int    `json:"id" gorm:"primaryKey"`
	UserId    int    `json:"user_id" gorm:"uniqueIndex;not null"`
	PolicyId  int    `json:"policy_id" gorm:"default:0"`
	Limits    string `json:"limits" gorm:"type:text"`
	Schedule  string `json:"schedule" gorm:"type:text"`
	Mode      string `json:"mode" gorm:"type:varchar(16);default:'merge'"`
	ExpiresAt int64  `json:"expires_at" gorm:"bigint;default:0;index"`
	Reason    string `json:"reason" gorm:"type:varchar(255)"`
	CreatedBy int    `json:"created_by"`
	UpdatedBy int    `json:"updated_by"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint"`
}

func (UserControlUserOverride) TableName() string { return "user_control_user_overrides" }

// UserControlPlanBinding applies a policy to every active subscriber of a plan.
type UserControlPlanBinding struct {
	Id        int   `json:"id" gorm:"primaryKey"`
	PlanId    int   `json:"plan_id" gorm:"uniqueIndex;not null"`
	PolicyId  int   `json:"policy_id" gorm:"index;not null"`
	CreatedBy int   `json:"created_by"`
	CreatedAt int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

func (UserControlPlanBinding) TableName() string { return "user_control_plan_bindings" }

// ---------------------------------------------------------------------------
// Batch jobs
// ---------------------------------------------------------------------------

type UserBatchJob struct {
	Id             string `json:"id" gorm:"primaryKey;type:varchar(64)"`
	Action         string `json:"action" gorm:"type:varchar(32);index"`
	Params         string `json:"params" gorm:"type:text"`
	Total          int    `json:"total"`
	Succeeded      int    `json:"succeeded"`
	Failed         int    `json:"failed"`
	Skipped        int    `json:"skipped"`
	Status         string `json:"status" gorm:"type:varchar(16);index"`
	CreatedBy      int    `json:"created_by" gorm:"index"`
	OperatorRole   int    `json:"operator_role"`
	IdempotencyKey string `json:"idempotency_key" gorm:"type:varchar(64);uniqueIndex:idx_batch_idem"`
	BatchHash      string `json:"batch_hash" gorm:"type:varchar(64)"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint;index"`
	StartedAt      int64  `json:"started_at" gorm:"bigint"`
	FinishedAt     int64  `json:"finished_at" gorm:"bigint"`
}

func (UserBatchJob) TableName() string { return "user_batch_jobs" }

type UserBatchJobItem struct {
	Id           int    `json:"id" gorm:"primaryKey"`
	JobId        string `json:"job_id" gorm:"type:varchar(64);index;index:idx_batch_item_status,priority:1"`
	UserId       int    `json:"user_id" gorm:"index"`
	Username     string `json:"username" gorm:"type:varchar(64)"`
	Status       string `json:"status" gorm:"type:varchar(16);index:idx_batch_item_status,priority:2"`
	ErrorCode    string `json:"error_code" gorm:"type:varchar(64)"`
	ErrorMessage string `json:"error_message" gorm:"type:varchar(512)"`
	Before       string `json:"before" gorm:"type:text"`
	After        string `json:"after" gorm:"type:text"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint"`
}

func (UserBatchJobItem) TableName() string { return "user_batch_job_items" }

// UserQuotaLedger is the immutable audit trail for batch quota changes.
type UserQuotaLedger struct {
	Id         int    `json:"id" gorm:"primaryKey"`
	JobId      string `json:"job_id" gorm:"type:varchar(64);index"`
	UserId     int    `json:"user_id" gorm:"index"`
	Username   string `json:"username" gorm:"type:varchar(64)"`
	Mode       string `json:"mode" gorm:"type:varchar(16)"`
	Requested  int    `json:"requested"`
	Before     int    `json:"before"`
	After      int    `json:"after"`
	OperatorId int    `json:"operator_id" gorm:"index"`
	Reason     string `json:"reason" gorm:"type:varchar(255)"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint"`
}

func (UserQuotaLedger) TableName() string { return "user_quota_ledger" }

// ---------------------------------------------------------------------------
// Cache epoch
// ---------------------------------------------------------------------------

var userControlEpoch atomic.Int64

func UserControlEpoch() int64 { return userControlEpoch.Load() }

// BumpUserControlEpoch invalidates every in-process resolver cache. The cache
// also has a short TTL so other instances converge without a pub/sub channel.
func BumpUserControlEpoch() int64 { return userControlEpoch.Add(1) }

func NewUserControlID(prefix string) string {
	return prefix + "-" + common.GetUUID()
}
