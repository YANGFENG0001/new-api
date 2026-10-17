package userbatch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
)

// Action identifiers shared with service/security_verification.go.
const (
	ActionEnable                 = "enable"
	ActionDisable                = "disable"
	ActionGroup                  = "group"
	ActionAddQuota               = "add_quota"
	ActionSubtractQuota          = "subtract_quota"
	ActionOverrideQuota          = "override_quota"
	ActionDelete                 = "delete"
	ActionPromote                = "promote"
	ActionDemote                 = "demote"
	ActionReset2FA               = "reset_2fa"
	ActionResetPasskey           = "reset_passkey"
	ActionClearBindings          = "clear_bindings"
	ActionInvalidateSubscription = "invalidate_subscriptions"
	ActionDeleteSubscription     = "delete_subscriptions"
	ActionBlock                  = "block"
	ActionUnblock                = "unblock"
)

var (
	ErrInvalidRequest      = errors.New("invalid batch request")
	ErrIdempotencyConflict = errors.New("idempotency key conflict")
)

// cancelledJobs marks jobs whose operator requested cancellation. Workers
// consult it between users so a cancel takes effect within one item instead
// of only updating the database row.
var cancelledJobs sync.Map

func CancelJob(jobID string) {
	cancelledJobs.Store(jobID, struct{}{})
}

func isJobCancelled(jobID string) bool {
	_, ok := cancelledJobs.Load(jobID)
	return ok
}

// Request is the normalized payload accepted by preview and execute.
type Request struct {
	Action         string `json:"action"`
	UserIDs        []int  `json:"user_ids"`
	Group          string `json:"group,omitempty"`
	Quota          int    `json:"quota,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Note           string `json:"note,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type Actor struct {
	UserID int
	Role   int
}

type ItemPreview struct {
	UserID     int            `json:"user_id"`
	Username   string         `json:"username"`
	Role       int            `json:"role"`
	Status     int            `json:"status"`
	Group      string         `json:"group"`
	Quota      int            `json:"quota"`
	Eligible   bool           `json:"eligible"`
	SkipReason string         `json:"skip_reason,omitempty"`
	Warnings   []string       `json:"warnings,omitempty"`
	Before     map[string]any `json:"before"`
	After      map[string]any `json:"after"`
}

type Preview struct {
	Action    string        `json:"action"`
	BatchHash string        `json:"batch_hash"`
	Total     int           `json:"total"`
	Eligible  int           `json:"eligible"`
	Skipped   int           `json:"skipped"`
	Warnings  []string      `json:"warnings,omitempty"`
	Items     []ItemPreview `json:"items"`
}

type ItemResult struct {
	UserID       int            `json:"user_id"`
	Username     string         `json:"username"`
	Status       string         `json:"status"`
	ErrorCode    string         `json:"error_code,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	Before       map[string]any `json:"before,omitempty"`
	After        map[string]any `json:"after,omitempty"`
}

type ExecuteResult struct {
	Job   *model.UserBatchJob      `json:"job"`
	Items []model.UserBatchJobItem `json:"items"`
}

// PermissionForAction maps a batch action to the permission an operator needs.
// Read access is always required separately.
func PermissionForAction(action string) (authz.Permission, bool) {
	switch action {
	case ActionEnable, ActionDisable, ActionGroup, ActionBlock, ActionUnblock:
		return authz.UserBatchExecute, true
	case ActionAddQuota, ActionSubtractQuota, ActionOverrideQuota:
		return authz.UserBatchQuota, true
	case ActionDelete:
		return authz.UserBatchDelete, true
	case ActionPromote, ActionDemote:
		return authz.UserBatchRole, true
	case ActionReset2FA, ActionResetPasskey, ActionClearBindings:
		return authz.UserBatchSecurity, true
	case ActionInvalidateSubscription, ActionDeleteSubscription:
		return authz.UserBatchSubscription, true
	default:
		return authz.Permission{}, false
	}
}

func normalizeRequest(req *Request) error {
	req.Action = strings.TrimSpace(strings.ToLower(req.Action))
	req.Group = strings.TrimSpace(req.Group)
	req.Reason = strings.TrimSpace(req.Reason)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if len(req.Reason) > 200 {
		req.Reason = req.Reason[:200]
	}
	if _, ok := PermissionForAction(req.Action); !ok {
		return fmt.Errorf("%w: unknown action", ErrInvalidRequest)
	}
	seen := make(map[int]struct{}, len(req.UserIDs))
	ids := make([]int, 0, len(req.UserIDs))
	for _, id := range req.UserIDs {
		if id <= 0 {
			return fmt.Errorf("%w: invalid user id", ErrInvalidRequest)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 || len(ids) > 5000 {
		return fmt.Errorf("%w: user_ids must contain 1..5000 ids", ErrInvalidRequest)
	}
	sort.Ints(ids)
	req.UserIDs = ids
	switch req.Action {
	case ActionGroup:
		if req.Group == "" || len(req.Group) > 64 {
			return fmt.Errorf("%w: group is required", ErrInvalidRequest)
		}
	case ActionAddQuota, ActionSubtractQuota:
		if req.Quota <= 0 {
			return fmt.Errorf("%w: quota must be positive", ErrInvalidRequest)
		}
	case ActionOverrideQuota:
		if req.Quota < 0 {
			return fmt.Errorf("%w: quota cannot be negative", ErrInvalidRequest)
		}
	}
	return nil
}

func BatchHash(req Request) string {
	payload := map[string]any{
		"action":   req.Action,
		"user_ids": req.UserIDs,
		"group":    req.Group,
		"quota":    req.Quota,
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

func canOperate(actor Actor, target *model.User) error {
	if target == nil || target.Id <= 0 {
		return errors.New("user does not exist")
	}
	if actor.UserID == target.Id {
		return errors.New("cannot operate on yourself")
	}
	if target.Role >= common.RoleRootUser {
		return errors.New("cannot operate on the super administrator")
	}
	if actor.Role < common.RoleRootUser && actor.Role <= target.Role {
		return errors.New("cannot operate on users with the same or higher role")
	}
	return nil
}

func previewItem(actor Actor, action string, user *model.User, req Request) ItemPreview {
	item := ItemPreview{
		UserID:   user.Id,
		Username: user.Username,
		Role:     user.Role,
		Status:   user.Status,
		Group:    user.Group,
		Quota:    user.Quota,
		Before: map[string]any{
			"status": user.Status,
			"role":   user.Role,
			"group":  user.Group,
			"quota":  user.Quota,
		},
	}
	if err := canOperate(actor, user); err != nil {
		item.SkipReason = err.Error()
		return item
	}
	item.Eligible = true
	after := map[string]any{
		"status": user.Status,
		"role":   user.Role,
		"group":  user.Group,
		"quota":  user.Quota,
	}
	switch action {
	case ActionEnable, ActionUnblock:
		if user.Status == common.UserStatusEnabled {
			item.Eligible = false
			item.SkipReason = "user is already enabled"
		} else {
			after["status"] = common.UserStatusEnabled
		}
	case ActionDisable, ActionBlock:
		if user.Status == common.UserStatusDisabled {
			item.Eligible = false
			item.SkipReason = "user is already disabled"
		} else {
			after["status"] = common.UserStatusDisabled
		}
	case ActionGroup:
		if user.Group == req.Group {
			item.Eligible = false
			item.SkipReason = "user already belongs to this group"
		} else {
			after["group"] = req.Group
		}
	case ActionAddQuota:
		after["quota"] = user.Quota + req.Quota
	case ActionSubtractQuota:
		after["quota"] = user.Quota - req.Quota
		if user.Quota-req.Quota < 0 {
			item.Warnings = append(item.Warnings, "balance will become negative")
		}
	case ActionOverrideQuota:
		if user.Quota == req.Quota {
			item.Eligible = false
			item.SkipReason = "quota already equals the requested value"
		} else {
			after["quota"] = req.Quota
		}
	case ActionPromote:
		if user.Role >= common.RoleAdminUser {
			item.Eligible = false
			item.SkipReason = "user is already an administrator"
		} else {
			after["role"] = common.RoleAdminUser
		}
	case ActionDemote:
		if user.Role == common.RoleCommonUser {
			item.Eligible = false
			item.SkipReason = "user is already a common user"
		} else {
			after["role"] = common.RoleCommonUser
		}
	}
	item.After = after
	return item
}

func PreviewBatch(actor Actor, req Request) (*Preview, error) {
	if err := normalizeRequest(&req); err != nil {
		return nil, err
	}
	users, err := model.GetUsersByIds(req.UserIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[int]*model.User, len(users))
	for i := range users {
		byID[users[i].Id] = &users[i]
	}
	preview := &Preview{Action: req.Action, BatchHash: BatchHash(req), Items: make([]ItemPreview, 0, len(req.UserIDs))}
	for _, id := range req.UserIDs {
		user, ok := byID[id]
		if !ok {
			preview.Items = append(preview.Items, ItemPreview{UserID: id, SkipReason: "user does not exist"})
			continue
		}
		item := previewItem(actor, req.Action, user, req)
		if item.Eligible {
			preview.Eligible++
		} else {
			preview.Skipped++
		}
		preview.Items = append(preview.Items, item)
	}
	preview.Total = len(preview.Items)
	if preview.Eligible == 0 {
		preview.Warnings = append(preview.Warnings, "No user is eligible for this action.")
	}
	switch req.Action {
	case ActionDelete, ActionOverrideQuota, ActionDemote:
		preview.Warnings = append(preview.Warnings, "This is a destructive change; the preview must be confirmed.")
	}
	return preview, nil
}

func ExecuteBatch(actor Actor, req Request) (*ExecuteResult, error) {
	if err := normalizeRequest(&req); err != nil {
		return nil, err
	}
	preview, err := PreviewBatch(actor, req)
	if err != nil {
		return nil, err
	}
	hash := preview.BatchHash
	idempotencyKey := req.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = "batch-" + hash + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	params, _ := json.Marshal(req)
	now := common.GetTimestamp()
	job := &model.UserBatchJob{
		Id:             model.NewUserControlID("job"),
		Action:         req.Action,
		Params:         string(params),
		Total:          preview.Total,
		Skipped:        preview.Skipped,
		Status:         "pending",
		CreatedBy:      actor.UserID,
		OperatorRole:   actor.Role,
		IdempotencyKey: idempotencyKey,
		BatchHash:      hash,
		CreatedAt:      now,
	}
	created, fresh, err := model.CreateUserBatchJob(job)
	if err != nil {
		return nil, err
	}
	if !fresh {
		if created.CreatedBy != actor.UserID || created.Action != req.Action || created.BatchHash != hash {
			return nil, fmt.Errorf("%w: this key was already used for a different batch request", ErrIdempotencyConflict)
		}
		items, _ := model.ListUserBatchJobItems(created.Id)
		return &ExecuteResult{Job: created, Items: items}, nil
	}
	job.Status = "running"
	job.StartedAt = common.GetTimestamp()
	_ = model.UpdateUserBatchJob(job)

	items := make([]model.UserBatchJobItem, 0, len(preview.Items))
	eligibleIndexes := make([]int, 0, len(preview.Items))
	for _, item := range preview.Items {
		beforeJSON, _ := json.Marshal(item.Before)
		row := model.UserBatchJobItem{
			JobId:     job.Id,
			UserId:    item.UserID,
			Username:  item.Username,
			Status:    "skipped",
			ErrorCode: "skipped",
			Before:    string(beforeJSON),
			CreatedAt: common.GetTimestamp(),
		}
		if item.Eligible {
			row.Status = "pending"
			row.ErrorCode = ""
			eligibleIndexes = append(eligibleIndexes, len(items))
		} else {
			row.ErrorMessage = item.SkipReason
		}
		items = append(items, row)
	}
	if err := model.CreateUserBatchItems(items); err != nil {
		job.Status = "failed"
		job.FinishedAt = common.GetTimestamp()
		_ = model.UpdateUserBatchJob(job)
		return nil, err
	}
	// Reload rows to obtain ids used by the worker updates.
	items, _ = model.ListUserBatchJobItems(job.Id)

	if len(eligibleIndexes) > 200 {
		go runJob(actor, req, hash, job.Id, eligibleIndexes)
		items, _ = model.ListUserBatchJobItems(job.Id)
		return &ExecuteResult{Job: job, Items: items}, nil
	}
	runItems(actor, req, hash, job.Id, items, eligibleIndexes)
	job, _ = model.GetUserBatchJob(job.Id)
	items, _ = model.ListUserBatchJobItems(job.Id)
	return &ExecuteResult{Job: job, Items: items}, nil
}

func runJob(actor Actor, req Request, hash, jobID string, indexes []int) {
	items, err := model.ListUserBatchJobItems(jobID)
	if err != nil {
		return
	}
	runItems(actor, req, hash, jobID, items, indexes)
}

func runItems(actor Actor, req Request, hash, jobID string, items []model.UserBatchJobItem, indexes []int) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, index := range indexes {
		if index < 0 || index >= len(items) {
			continue
		}
		item := &items[index]
		wg.Add(1)
		sem <- struct{}{}
		go func(item *model.UserBatchJobItem) {
			defer wg.Done()
			defer func() { <-sem }()
			if isJobCancelled(jobID) {
				return
			}
			if item.Status != "pending" && item.Status != "failed" {
				return
			}
			user, err := model.GetUserById(item.UserId, false)
			if err != nil {
				mu.Lock()
				item.Status = "failed"
				item.ErrorCode = "user_not_found"
				item.ErrorMessage = "user does not exist"
				mu.Unlock()
				_ = model.UpdateUserBatchJobItem(item)
				return
			}
			before := map[string]any{"status": user.Status, "role": user.Role, "group": user.Group, "quota": user.Quota}
			after, code, message := applyAction(actor, req, hash, jobID, user)
			beforeJSON, _ := json.Marshal(before)
			afterJSON, _ := json.Marshal(after)
			mu.Lock()
			item.Before = string(beforeJSON)
			item.After = string(afterJSON)
			if code == "" {
				item.Status = "succeeded"
				item.ErrorCode = ""
				item.ErrorMessage = ""
			} else {
				item.Status = "failed"
				item.ErrorCode = code
				item.ErrorMessage = message
			}
			mu.Unlock()
			_ = model.UpdateUserBatchJobItem(item)
		}(item)
	}
	wg.Wait()

	cancelled := isJobCancelled(jobID)
	if cancelled {
		// Persist a final state for items that never started so the job
		// history does not keep rows stuck in pending forever.
		_, _ = model.CancelUserBatchPendingItems(jobID)
	}

	succeeded, failed := 0, 0
	for i := range items {
		switch items[i].Status {
		case "succeeded":
			succeeded++
		case "failed":
			failed++
		}
	}
	job, err := model.GetUserBatchJob(jobID)
	if err != nil {
		return
	}
	job.Succeeded = succeeded
	job.Failed = failed
	if cancelled {
		job.Status = "cancelled"
		job.FinishedAt = common.GetTimestamp()
		_ = model.UpdateUserBatchJob(job)
		return
	}
	job.Status = "completed"
	if failed > 0 && succeeded > 0 {
		job.Status = "partial"
	} else if failed > 0 {
		job.Status = "failed"
	}
	job.FinishedAt = common.GetTimestamp()
	_ = model.UpdateUserBatchJob(job)
	model.BumpUserControlEpoch()
}

func applyAction(actor Actor, req Request, hash, jobID string, user *model.User) (map[string]any, string, string) {
	if err := canOperate(actor, user); err != nil {
		return nil, "forbidden", err.Error()
	}
	after := map[string]any{"status": user.Status, "role": user.Role, "group": user.Group, "quota": user.Quota}
	action := req.Action

	switch action {
	case ActionEnable, ActionUnblock:
		if user.Status == common.UserStatusEnabled {
			return after, "already_enabled", "user is already enabled"
		}
		user.Status = common.UserStatusEnabled
	case ActionDisable, ActionBlock:
		if user.Status == common.UserStatusDisabled {
			return after, "already_disabled", "user is already disabled"
		}
		user.Status = common.UserStatusDisabled
	case ActionGroup:
		if user.Group == req.Group {
			return after, "already_in_group", "user already belongs to this group"
		}
		user.Group = req.Group
	case ActionAddQuota, ActionSubtractQuota, ActionOverrideQuota:
		mode := "add"
		if action == ActionSubtractQuota {
			mode = "subtract"
		} else if action == ActionOverrideQuota {
			mode = "override"
		}
		ledger := &model.UserQuotaLedger{
			JobId:      jobID,
			UserId:     user.Id,
			Username:   user.Username,
			Mode:       mode,
			Requested:  req.Quota,
			OperatorId: actor.UserID,
			Reason:     req.Reason,
		}
		adjustment, err := model.AdjustUserQuotaWithLedger(user.Id, actor.Role, mode, req.Quota, ledger)
		if err != nil {
			switch {
			case errors.Is(err, model.ErrUserQuotaPermission):
				return after, "forbidden", err.Error()
			case errors.Is(err, model.ErrInvalidUserQuotaAdjustment):
				return after, "invalid_quota", err.Error()
			case errors.Is(err, model.ErrWalletQuotaLimitExceeded):
				return after, "quota_limit", err.Error()
			default:
				return after, "database_error", err.Error()
			}
		}
		after["quota"] = adjustment.After
		return after, "", ""
	case ActionDelete:
		if _, err := user.Delete(); err != nil {
			return after, "delete_failed", err.Error()
		}
		if err := model.InvalidateUserTokensCache(user.Id); err != nil {
			common.SysLog(common.LogText("failed to invalidate tokens cache for user %d: %s", user.Id, err.Error()))
		}
		after["deleted"] = true
		return after, "", ""
	case ActionPromote:
		if user.Role >= common.RoleAdminUser {
			return after, "already_admin", "user is already an administrator"
		}
		user.Role = common.RoleAdminUser
	case ActionDemote:
		if user.Role == common.RoleCommonUser {
			return after, "already_common", "user is already a common user"
		}
		user.Role = common.RoleCommonUser
		if err := authz.ClearUserAuthorization(user.Id); err != nil {
			return after, "authorization_cleanup_failed", err.Error()
		}
	case ActionReset2FA:
		if err := model.DisableTwoFAWithAuthVersion(user.Id); err != nil {
			if errors.Is(err, model.ErrTwoFANotEnabled) {
				return after, "2fa_not_enabled", "two-factor authentication is not enabled"
			}
			return after, "2fa_reset_failed", err.Error()
		}
		after["twofa"] = false
		return after, "", ""
	case ActionResetPasskey:
		if err := model.DeletePasskeyByUserIDWithAuthVersion(user.Id); err != nil {
			if errors.Is(err, model.ErrPasskeyNotFound) {
				return after, "passkey_not_found", "no passkey is registered"
			}
			return after, "passkey_reset_failed", err.Error()
		}
		after["passkey"] = false
		return after, "", ""
	case ActionClearBindings:
		cleared := 0
		for _, binding := range []string{"github", "discord", "oidc", "wechat", "telegram", "linuxdo"} {
			if err := user.ClearBinding(binding); err != nil {
				return after, "binding_clear_failed", err.Error()
			}
			cleared++
		}
		after["cleared_bindings"] = cleared
		return after, "", ""
	case ActionInvalidateSubscription, ActionDeleteSubscription:
		subs, err := model.GetAllActiveUserSubscriptions(user.Id)
		if err != nil {
			return after, "subscription_lookup_failed", err.Error()
		}
		if len(subs) == 0 {
			return after, "no_active_subscription", "user has no active subscription"
		}
		processed := 0
		for _, sub := range subs {
			var err error
			if action == ActionInvalidateSubscription {
				_, err = model.AdminInvalidateUserSubscription(sub.Subscription.Id)
			} else {
				_, err = model.AdminDeleteUserSubscription(sub.Subscription.Id)
			}
			if err != nil {
				return after, "subscription_change_failed", err.Error()
			}
			processed++
		}
		after["subscriptions"] = processed
		return after, "", ""
	default:
		return after, "unknown_action", "unknown batch action"
	}

	if err := user.Update(false); err != nil {
		return after, "update_failed", err.Error()
	}
	if err := model.InvalidateUserTokensCache(user.Id); err != nil {
		common.SysLog(common.LogText("failed to invalidate tokens cache for user %d: %s", user.Id, err.Error()))
	}
	after["status"] = user.Status
	after["role"] = user.Role
	after["group"] = user.Group
	return after, "", ""
}

// RetryFailed re-runs only the failed items of a job.
func RetryFailed(actor Actor, jobID string) (*ExecuteResult, error) {
	job, err := model.GetUserBatchJob(jobID)
	if err != nil {
		return nil, err
	}
	if job.Status == "pending" || job.Status == "running" {
		return nil, fmt.Errorf("%w: job is still running", ErrInvalidRequest)
	}
	var req Request
	if err := json.Unmarshal([]byte(job.Params), &req); err != nil {
		return nil, fmt.Errorf("%w: stored params are invalid", ErrInvalidRequest)
	}
	if err := normalizeRequest(&req); err != nil {
		return nil, err
	}
	items, err := model.ResetFailedUserBatchItems(jobID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return &ExecuteResult{Job: job, Items: items}, nil
	}
	indexes := make([]int, len(items))
	for i := range items {
		items[i].Status = "pending"
		items[i].ErrorCode = ""
		items[i].ErrorMessage = ""
		indexes[i] = i
	}
	cancelledJobs.Delete(jobID)
	job.Status = "running"
	job.FinishedAt = 0
	_ = model.UpdateUserBatchJob(job)
	runItems(actor, req, job.BatchHash, jobID, items, indexes)
	job, _ = model.GetUserBatchJob(jobID)
	items, _ = model.ListUserBatchJobItems(jobID)
	return &ExecuteResult{Job: job, Items: items}, nil
}
