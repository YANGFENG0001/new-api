package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/service/userbatch"

	"github.com/gin-gonic/gin"
)

type userBatchExecuteRequest struct {
	userbatch.Request
	BatchHash string `json:"batch_hash"`
}

type userBatchActionInfo struct {
	Action      string `json:"action"`
	Permission  string `json:"permission"`
	Allowed     bool   `json:"allowed"`
	Destructive bool   `json:"destructive"`
	Quota       bool   `json:"quota"`
}

func batchActor(c *gin.Context) userbatch.Actor {
	return userbatch.Actor{UserID: c.GetInt("id"), Role: c.GetInt("role")}
}

func batchFail(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"success": false, "message": message})
}

func requireBatchRead(c *gin.Context) bool {
	return requireBatchPermission(c, authz.UserBatchRead)
}

func requireBatchPermission(c *gin.Context, permission authz.Permission) bool {
	if authz.Can(c.GetInt("id"), c.GetInt("role"), permission) {
		return true
	}
	batchFail(c, http.StatusForbidden, "Unauthorized, insufficient privileges")
	return false
}

func canAccessBatchJob(c *gin.Context, job *model.UserBatchJob) bool {
	return job.CreatedBy == c.GetInt("id") || c.GetInt("role") >= common.RoleRootUser
}

// GetUserBatchActions reports every action and whether the caller may use it.
func GetUserBatchActions(c *gin.Context) {
	if !requireBatchRead(c) {
		return
	}
	actions := []string{
		userbatch.ActionEnable, userbatch.ActionDisable, userbatch.ActionGroup,
		userbatch.ActionAddQuota, userbatch.ActionSubtractQuota, userbatch.ActionOverrideQuota,
		userbatch.ActionDelete, userbatch.ActionPromote, userbatch.ActionDemote,
		userbatch.ActionReset2FA, userbatch.ActionResetPasskey, userbatch.ActionClearBindings,
		userbatch.ActionInvalidateSubscription, userbatch.ActionDeleteSubscription,
		userbatch.ActionBlock, userbatch.ActionUnblock,
	}
	destructive := map[string]bool{
		userbatch.ActionDelete: true, userbatch.ActionOverrideQuota: true,
		userbatch.ActionDemote: true, userbatch.ActionSubtractQuota: true,
		userbatch.ActionReset2FA: true, userbatch.ActionResetPasskey: true,
		userbatch.ActionClearBindings: true, userbatch.ActionInvalidateSubscription: true,
		userbatch.ActionDeleteSubscription: true, userbatch.ActionBlock: true,
	}
	quotaActions := map[string]bool{
		userbatch.ActionAddQuota: true, userbatch.ActionSubtractQuota: true,
		userbatch.ActionOverrideQuota: true,
	}
	out := make([]userBatchActionInfo, 0, len(actions))
	for _, action := range actions {
		permission, ok := userbatch.PermissionForAction(action)
		out = append(out, userBatchActionInfo{
			Action:      action,
			Permission:  permission.Resource + "." + permission.Action,
			Allowed:     ok && authz.Can(c.GetInt("id"), c.GetInt("role"), permission),
			Destructive: destructive[action],
			Quota:       quotaActions[action],
		})
	}
	common.ApiSuccess(c, gin.H{
		"actions":   out,
		"can_read":  true,
		"max_users": 5000,
	})
}

func PreviewUserBatch(c *gin.Context) {
	if !requireBatchRead(c) {
		return
	}
	var req userbatch.Request
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		batchFail(c, http.StatusBadRequest, "Invalid parameters")
		return
	}
	permission, ok := userbatch.PermissionForAction(req.Action)
	if !ok {
		batchFail(c, http.StatusBadRequest, "Unknown batch action")
		return
	}
	if !requireBatchPermission(c, permission) {
		return
	}
	preview, err := userbatch.PreviewBatch(batchActor(c), req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, userbatch.ErrInvalidRequest) {
			status = http.StatusBadRequest
		}
		batchFail(c, status, err.Error())
		return
	}
	common.ApiSuccess(c, preview)
}

func ExecuteUserBatch(c *gin.Context) {
	if !requireBatchRead(c) {
		return
	}
	var request userBatchExecuteRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		batchFail(c, http.StatusBadRequest, "Invalid parameters")
		return
	}
	permission, ok := userbatch.PermissionForAction(request.Action)
	if !ok {
		batchFail(c, http.StatusBadRequest, "Unknown batch action")
		return
	}
	if !requireBatchPermission(c, permission) {
		return
	}
	actor := batchActor(c)
	preview, err := userbatch.PreviewBatch(actor, request.Request)
	if err != nil {
		batchFail(c, http.StatusBadRequest, err.Error())
		return
	}
	if request.BatchHash != "" && request.BatchHash != preview.BatchHash {
		batchFail(c, http.StatusConflict, "The batch preview is stale. Refresh the preview and confirm again.")
		return
	}
	ids := make([]int, 0, len(preview.Items))
	for _, item := range preview.Items {
		if item.Eligible {
			ids = append(ids, item.UserID)
		}
	}
	if len(ids) == 0 {
		batchFail(c, http.StatusBadRequest, "No user is eligible for this action")
		return
	}
	authorization := requireAdminUserProof(c, service.VerificationScopeAdminUserBatch, service.AdminUserBatchContext{
		Action:    preview.Action,
		UserIDs:   ids,
		BatchHash: preview.BatchHash,
	})
	if authorization == nil {
		return
	}
	result, err := userbatch.ExecuteBatch(actor, request.Request)
	if err != nil {
		if errors.Is(err, userbatch.ErrIdempotencyConflict) {
			batchFail(c, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, userbatch.ErrInvalidRequest) {
			batchFail(c, http.StatusBadRequest, err.Error())
			return
		}
		batchFail(c, http.StatusInternalServerError, err.Error())
		return
	}
	params := model.AuditFields{
		"action":              request.Action,
		"batch_hash":          preview.BatchHash,
		"requested_user_ids":  request.UserIDs,
		"eligible_user_ids":   ids,
		"job_id":              result.Job.Id,
		"succeeded":           result.Job.Succeeded,
		"failed":              result.Job.Failed,
		"skipped":             result.Job.Skipped,
		"verification_method": authorization.Method,
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), "User batch: "+request.Action, c.ClientIP(),
		"user_batch."+request.Action, params, auditOperatorInfo(c), &model.AuditRequestInfo{
			Method: c.Request.Method, Route: c.FullPath(), Status: http.StatusOK, Success: true,
		}, c)
	markAuditLogged(c)
	common.ApiSuccess(c, result)
}

func ListUserBatchJobs(c *gin.Context) {
	if !requireBatchRead(c) {
		return
	}
	pageInfo := common.GetPageQuery(c)
	all := c.Query("all") == "true" && c.GetInt("role") >= common.RoleRootUser
	jobs, total, err := model.ListUserBatchJobs(c.GetInt("id"), all, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(jobs)
	common.ApiSuccess(c, pageInfo)
}

func GetUserBatchJob(c *gin.Context) {
	if !requireBatchRead(c) {
		return
	}
	job, err := model.GetUserBatchJob(c.Param("id"))
	if err != nil {
		batchFail(c, http.StatusNotFound, "Batch job does not exist")
		return
	}
	if !canAccessBatchJob(c, job) {
		batchFail(c, http.StatusForbidden, "Unauthorized, insufficient privileges")
		return
	}
	items, err := model.ListUserBatchJobItems(job.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"job": job, "items": items})
}

func ListUserBatchJobItems(c *gin.Context) {
	if !requireBatchRead(c) {
		return
	}
	job, err := model.GetUserBatchJob(c.Param("id"))
	if err != nil {
		batchFail(c, http.StatusNotFound, "Batch job does not exist")
		return
	}
	if !canAccessBatchJob(c, job) {
		batchFail(c, http.StatusForbidden, "Unauthorized, insufficient privileges")
		return
	}
	pageInfo := common.GetPageQuery(c)
	items, total, err := model.ListUserBatchJobItemsPage(job.Id, c.Query("status"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func RetryUserBatchJob(c *gin.Context) {
	if !requireBatchRead(c) {
		return
	}
	job, err := model.GetUserBatchJob(c.Param("id"))
	if err != nil {
		batchFail(c, http.StatusNotFound, "Batch job does not exist")
		return
	}
	if !canAccessBatchJob(c, job) {
		batchFail(c, http.StatusForbidden, "Unauthorized, insufficient privileges")
		return
	}
	permission, ok := userbatch.PermissionForAction(job.Action)
	if !ok {
		batchFail(c, http.StatusBadRequest, "Unknown batch action")
		return
	}
	if !requireBatchPermission(c, permission) {
		return
	}
	failedItems, err := model.ResetFailedUserBatchItems(job.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(failedItems) == 0 {
		// Nothing to authorize or retry; return the current state quietly.
		items, _ := model.ListUserBatchJobItems(job.Id)
		common.ApiSuccess(c, gin.H{"job": job, "items": items, "retried": 0})
		return
	}
	ids := make([]int, 0, len(failedItems))
	for _, item := range failedItems {
		ids = append(ids, item.UserId)
	}
	authorization := requireAdminUserProof(c, service.VerificationScopeAdminUserBatch, service.AdminUserBatchContext{
		Action:    job.Action,
		UserIDs:   ids,
		BatchHash: job.BatchHash,
	})
	if authorization == nil {
		return
	}
	result, err := userbatch.RetryFailed(batchActor(c), job.Id)
	if err != nil {
		if errors.Is(err, userbatch.ErrInvalidRequest) {
			batchFail(c, http.StatusConflict, err.Error())
			return
		}
		batchFail(c, http.StatusInternalServerError, err.Error())
		return
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), "Retry failed batch items: "+job.Action, c.ClientIP(),
		"user_batch.retry", model.AuditFields{"job_id": job.Id, "action": job.Action, "retried": len(ids), "verification_method": authorization.Method}, auditOperatorInfo(c), nil, c)
	markAuditLogged(c)
	common.ApiSuccess(c, result)
}

func CancelUserBatchJob(c *gin.Context) {
	if !requireBatchRead(c) {
		return
	}
	job, err := model.GetUserBatchJob(c.Param("id"))
	if err != nil {
		batchFail(c, http.StatusNotFound, "Batch job does not exist")
		return
	}
	if !canAccessBatchJob(c, job) {
		batchFail(c, http.StatusForbidden, "Unauthorized, insufficient privileges")
		return
	}
	if job.Status != "pending" && job.Status != "running" {
		batchFail(c, http.StatusConflict, "Only pending or running jobs can be cancelled")
		return
	}
	userbatch.CancelJob(job.Id)
	if _, err := model.CancelUserBatchPendingItems(job.Id); err != nil {
		common.SysLog(common.LogText("failed to mark cancelled batch items for job %s: %s", job.Id, err.Error()))
	}
	job.Status = "cancelled"
	job.FinishedAt = common.GetTimestamp()
	if err := model.UpdateUserBatchJob(job); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), "Cancel batch job: "+job.Action, c.ClientIP(),
		"user_batch.cancel", model.AuditFields{"job_id": job.Id, "action": job.Action}, auditOperatorInfo(c), nil, c)
	markAuditLogged(c)
	common.ApiSuccess(c, job)
}
