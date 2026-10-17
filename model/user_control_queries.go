package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserControlSource is one policy that contributes to a user's effective
// limits, together with a human-readable source label.
type UserControlSource struct {
	Policy   UserControlPolicy
	Source   string
	SourceId int
	Replace  bool
}

func ListUserControlPolicies() ([]UserControlPolicy, error) {
	var policies []UserControlPolicy
	err := DB.Order("priority DESC, id ASC").Find(&policies).Error
	return policies, err
}

func GetUserControlPolicy(id int) (*UserControlPolicy, error) {
	var policy UserControlPolicy
	if err := DB.First(&policy, id).Error; err != nil {
		return nil, err
	}
	return &policy, nil
}

func CountUserControlPolicyReferences(id int) (lists int64, overrides int64, bindings int64, err error) {
	if err = DB.Model(&UserControlList{}).Where("policy_id = ?", id).Count(&lists).Error; err != nil {
		return
	}
	if err = DB.Model(&UserControlUserOverride{}).Where("policy_id = ?", id).Count(&overrides).Error; err != nil {
		return
	}
	err = DB.Model(&UserControlPlanBinding{}).Where("policy_id = ?", id).Count(&bindings).Error
	return
}

func CreateUserControlPolicy(policy *UserControlPolicy) error {
	now := common.GetTimestamp()
	policy.CreatedAt = now
	policy.UpdatedAt = now
	if policy.Version == 0 {
		policy.Version = 1
	}
	if err := DB.Create(policy).Error; err != nil {
		return err
	}
	BumpUserControlEpoch()
	return nil
}

func UpdateUserControlPolicy(policy *UserControlPolicy) error {
	policy.UpdatedAt = common.GetTimestamp()
	policy.Version++
	if err := DB.Save(policy).Error; err != nil {
		return err
	}
	BumpUserControlEpoch()
	return nil
}

func DeleteUserControlPolicy(id int) error {
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&UserControlList{}).Where("policy_id = ?", id).Update("policy_id", 0).Error; err != nil {
			return err
		}
		if err := tx.Where("policy_id = ?", id).Delete(&UserControlUserOverride{}).Error; err != nil {
			return err
		}
		if err := tx.Where("policy_id = ?", id).Delete(&UserControlPlanBinding{}).Error; err != nil {
			return err
		}
		return tx.Delete(&UserControlPolicy{}, id).Error
	})
	if err == nil {
		BumpUserControlEpoch()
	}
	return err
}

func ListUserControlLists() ([]UserControlList, error) {
	var lists []UserControlList
	err := DB.Order("id ASC").Find(&lists).Error
	return lists, err
}

func GetUserControlList(id int) (*UserControlList, error) {
	var list UserControlList
	if err := DB.First(&list, id).Error; err != nil {
		return nil, err
	}
	return &list, nil
}

func CreateUserControlList(list *UserControlList) error {
	now := common.GetTimestamp()
	list.CreatedAt = now
	list.UpdatedAt = now
	if err := DB.Create(list).Error; err != nil {
		return err
	}
	BumpUserControlEpoch()
	return nil
}

func UpdateUserControlList(list *UserControlList) error {
	list.UpdatedAt = common.GetTimestamp()
	if err := DB.Save(list).Error; err != nil {
		return err
	}
	BumpUserControlEpoch()
	return nil
}

func DeleteUserControlList(id int) error {
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("list_id = ?", id).Delete(&UserControlListMember{}).Error; err != nil {
			return err
		}
		return tx.Delete(&UserControlList{}, id).Error
	})
	if err == nil {
		BumpUserControlEpoch()
	}
	return err
}

func ListUserControlListMembers(listID int, offset, limit int) ([]UserControlListMember, int64, error) {
	var members []UserControlListMember
	var total int64
	q := DB.Model(&UserControlListMember{}).Where("list_id = ?", listID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id ASC").Offset(offset).Limit(limit).Find(&members).Error
	return members, total, err
}

func CountUserControlListMembers(listID int) (int64, error) {
	var total int64
	err := DB.Model(&UserControlListMember{}).Where("list_id = ?", listID).Count(&total).Error
	return total, err
}

func AddUserControlListMembers(listID int, userIDs []int, note string, addedBy int) (int, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	now := common.GetTimestamp()
	inserted := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		for _, userID := range userIDs {
			member := UserControlListMember{
				ListId:    listID,
				UserId:    userID,
				Note:      note,
				AddedBy:   addedBy,
				CreatedAt: now,
			}
			res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&member)
			if res.Error != nil {
				return res.Error
			}
			inserted += int(res.RowsAffected)
		}
		return nil
	})
	if err == nil {
		BumpUserControlEpoch()
	}
	return inserted, err
}

func RemoveUserControlListMembers(listID int, userIDs []int) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	res := DB.Where("list_id = ? AND user_id IN ?", listID, userIDs).
		Delete(&UserControlListMember{})
	if res.Error == nil {
		BumpUserControlEpoch()
	}
	return res.RowsAffected, res.Error
}

func GetUserControlOverride(userID int) (*UserControlUserOverride, error) {
	var override UserControlUserOverride
	if err := DB.Where("user_id = ?", userID).First(&override).Error; err != nil {
		return nil, err
	}
	return &override, nil
}

func UpsertUserControlOverride(override *UserControlUserOverride) error {
	now := common.GetTimestamp()
	override.UpdatedAt = now
	var existing UserControlUserOverride
	err := DB.Where("user_id = ?", override.UserId).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		override.CreatedAt = now
		if err := DB.Create(override).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		override.Id = existing.Id
		override.CreatedAt = existing.CreatedAt
		if err := DB.Save(override).Error; err != nil {
			return err
		}
	}
	BumpUserControlEpoch()
	return nil
}

func DeleteUserControlOverride(userID int) error {
	err := DB.Where("user_id = ?", userID).Delete(&UserControlUserOverride{}).Error
	if err == nil {
		BumpUserControlEpoch()
	}
	return err
}

func ListUserControlOverrides() ([]UserControlUserOverride, error) {
	var overrides []UserControlUserOverride
	err := DB.Order("updated_at DESC").Find(&overrides).Error
	return overrides, err
}

func ListUserControlPlanBindings() ([]UserControlPlanBinding, error) {
	var bindings []UserControlPlanBinding
	err := DB.Order("id ASC").Find(&bindings).Error
	return bindings, err
}

func GetUserControlPlanBinding(planID int) (*UserControlPlanBinding, error) {
	var binding UserControlPlanBinding
	if err := DB.Where("plan_id = ?", planID).First(&binding).Error; err != nil {
		return nil, err
	}
	return &binding, nil
}

func UpsertUserControlPlanBinding(binding *UserControlPlanBinding) error {
	now := common.GetTimestamp()
	binding.UpdatedAt = now
	var existing UserControlPlanBinding
	err := DB.Where("plan_id = ?", binding.PlanId).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		binding.CreatedAt = now
		if err := DB.Create(binding).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		binding.Id = existing.Id
		binding.CreatedAt = existing.CreatedAt
		if err := DB.Save(binding).Error; err != nil {
			return err
		}
	}
	BumpUserControlEpoch()
	return nil
}

func DeleteUserControlPlanBinding(planID int) error {
	err := DB.Where("plan_id = ?", planID).Delete(&UserControlPlanBinding{}).Error
	if err == nil {
		BumpUserControlEpoch()
	}
	return err
}

// UserControlSourcesForUser returns every policy that currently applies to a
// user. The caller merges them into one effective bundle.
func UserControlSourcesForUser(userID int, now int64) ([]UserControlSource, error) {
	sources := make([]UserControlSource, 0, 4)

	var listPolicies []UserControlPolicy
	err := DB.Table("user_control_policies AS p").
		Select("p.*").
		Joins("JOIN user_control_lists AS l ON l.policy_id = p.id").
		Joins("JOIN user_control_list_members AS m ON m.list_id = l.id").
		Where("m.user_id = ? AND l.enabled = ? AND p.enabled = ?", userID, true, true).
		Order("p.priority DESC, p.id ASC").
		Find(&listPolicies).Error
	if err != nil {
		return nil, err
	}
	for _, policy := range listPolicies {
		sources = append(sources, UserControlSource{Policy: policy, Source: "list", SourceId: policy.Id})
	}

	var planPolicies []UserControlPolicy
	err = DB.Table("user_control_policies AS p").
		Select("p.*").
		Joins("JOIN user_control_plan_bindings AS b ON b.policy_id = p.id").
		Joins("JOIN user_subscriptions AS s ON s.plan_id = b.plan_id").
		Where("s.user_id = ? AND s.status = ? AND s.start_time <= ? AND s.end_time > ? AND p.enabled = ?",
			userID, "active", now, now, true).
		Order("p.priority DESC, p.id ASC").
		Find(&planPolicies).Error
	if err != nil {
		return nil, err
	}
	for _, policy := range planPolicies {
		sources = append(sources, UserControlSource{Policy: policy, Source: "subscription", SourceId: policy.Id})
	}

	override, err := GetUserControlOverride(userID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if override != nil && (override.ExpiresAt == 0 || override.ExpiresAt > now) {
		// "replace" may consist of a referenced policy, inline limits, or
		// both. The reset must happen exactly once, on the first override
		// source, otherwise a second source would wipe the first one.
		replacePending := override.Mode == "replace"
		var policy UserControlPolicy
		if override.PolicyId > 0 {
			if err := DB.First(&policy, override.PolicyId).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, err
				}
			} else if policy.Enabled {
				sources = append(sources, UserControlSource{
					Policy: policy, Source: "override", SourceId: override.Id, Replace: replacePending,
				})
				replacePending = false
			}
		}
		// The override's own limits are represented by a synthetic, non-DB
		// policy entry so the resolver can merge them in one pass.
		synthetic := UserControlPolicy{
			Id:       0,
			Name:     "user override",
			Enabled:  true,
			Limits:   override.Limits,
			Schedule: override.Schedule,
			Version:  override.UpdatedAt,
		}
		if synthetic.Limits != "" || synthetic.Schedule != "" {
			sources = append(sources, UserControlSource{
				Policy: synthetic, Source: "override", SourceId: override.Id, Replace: replacePending,
			})
		}
	}

	return sources, nil
}

// ---------------------------------------------------------------------------
// Batch helpers
// ---------------------------------------------------------------------------

func GetUserBatchJob(id string) (*UserBatchJob, error) {
	var job UserBatchJob
	if err := DB.First(&job, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func GetUserBatchJobByIdempotencyKey(key string) (*UserBatchJob, error) {
	var job UserBatchJob
	if err := DB.Where("idempotency_key = ?", key).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

// CreateUserBatchJob inserts a job, returning the existing row when the
// idempotency key was already used.
func CreateUserBatchJob(job *UserBatchJob) (*UserBatchJob, bool, error) {
	err := DB.Create(job).Error
	if err == nil {
		return job, true, nil
	}
	existing, lookupErr := GetUserBatchJobByIdempotencyKey(job.IdempotencyKey)
	if lookupErr == nil {
		return existing, false, nil
	}
	return nil, false, err
}

func UpdateUserBatchJob(job *UserBatchJob) error {
	return DB.Save(job).Error
}

func ListUserBatchJobs(createdBy int, all bool, offset, limit int) ([]UserBatchJob, int64, error) {
	var jobs []UserBatchJob
	var total int64
	q := DB.Model(&UserBatchJob{})
	if !all {
		q = q.Where("created_by = ?", createdBy)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC").Offset(offset).Limit(limit).Find(&jobs).Error
	return jobs, total, err
}

func ListUserBatchJobItems(jobID string) ([]UserBatchJobItem, error) {
	var items []UserBatchJobItem
	err := DB.Where("job_id = ?", jobID).Order("id ASC").Find(&items).Error
	return items, err
}

func ListUserBatchJobItemsPage(jobID, status string, offset, limit int) ([]UserBatchJobItem, int64, error) {
	var items []UserBatchJobItem
	var total int64
	q := DB.Model(&UserBatchJobItem{}).Where("job_id = ?", jobID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id ASC").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, err
}

func CreateUserBatchItems(items []UserBatchJobItem) error {
	if len(items) == 0 {
		return nil
	}
	return DB.CreateInBatches(items, 200).Error
}

func UpdateUserBatchJobItem(item *UserBatchJobItem) error {
	return DB.Save(item).Error
}

// CancelUserBatchPendingItems marks every item that has not reached a final
// state as cancelled. It is called when an operator cancels a job so history
// does not retain rows stuck in pending/running forever, including after a
// process restart.
func CancelUserBatchPendingItems(jobID string) (int64, error) {
	res := DB.Model(&UserBatchJobItem{}).
		Where("job_id = ? AND status IN ?", jobID, []string{"pending", "running"}).
		Updates(map[string]any{
			"status":        "cancelled",
			"error_code":    "cancelled",
			"error_message": "the batch job was cancelled",
		})
	return res.RowsAffected, res.Error
}

func ResetFailedUserBatchItems(jobID string) ([]UserBatchJobItem, error) {
	var items []UserBatchJobItem
	if err := DB.Where("job_id = ? AND status = ?", jobID, "failed").Order("id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func WriteUserQuotaLedger(entry *UserQuotaLedger) error {
	if entry.CreatedAt == 0 {
		entry.CreatedAt = common.GetTimestamp()
	}
	return DB.Create(entry).Error
}

// GetUsersByIds loads users for previews and batch execution. Deleted users
// are included so a delete job can report them accurately.
func GetUsersByIds(ids []int) ([]User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var users []User
	err := DB.Unscoped().Where("id IN ?", ids).Order("id ASC").Find(&users).Error
	return users, err
}

// CountUserControlReferencesSummary reports whether a policy is in use.
func CountUserControlReferencesSummary(id int) (int64, error) {
	lists, overrides, bindings, err := CountUserControlPolicyReferences(id)
	return lists + overrides + bindings, err
}

// RecoverStaleUserBatchJobs marks jobs abandoned by a process restart as
// failed instead of leaving them "running" forever. In-flight items are
// failed with a stable error code so operators can retry them explicitly.
func RecoverStaleUserBatchJobs(staleBefore int64) (int64, error) {
	now := common.GetTimestamp()
	var jobs []UserBatchJob
	if err := DB.Where("status IN ? AND created_at < ?", []string{"pending", "running"}, staleBefore).
		Find(&jobs).Error; err != nil {
		return 0, err
	}
	if len(jobs) == 0 {
		return 0, nil
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		for i := range jobs {
			job := &jobs[i]
			if err := tx.Model(&UserBatchJobItem{}).
				Where("job_id = ? AND status IN ?", job.Id, []string{"pending", "running"}).
				Updates(map[string]any{
					"status":        "failed",
					"error_code":    "job_interrupted",
					"error_message": "the batch job was interrupted by a service restart; retry to continue",
				}).Error; err != nil {
				return err
			}
			var succeeded, failed int64
			if err := tx.Model(&UserBatchJobItem{}).
				Where("job_id = ? AND status = ?", job.Id, "succeeded").
				Count(&succeeded).Error; err != nil {
				return err
			}
			if err := tx.Model(&UserBatchJobItem{}).
				Where("job_id = ? AND status = ?", job.Id, "failed").
				Count(&failed).Error; err != nil {
				return err
			}
			if err := tx.Model(&UserBatchJob{}).Where("id = ?", job.Id).
				Updates(map[string]any{
					"status":      "failed",
					"finished_at": now,
					"succeeded":   int(succeeded),
					"failed":      int(failed),
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int64(len(jobs)), nil
}
