package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

func ListSubscriptionSyncSources(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "sync_read") {
		return
	}
	var sources []model.SubscriptionSyncSource
	if err := model.DB.Order("id desc").Limit(200).Find(&sources).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	views := []model.SubscriptionSyncSourceInput{}
	for _, s := range sources {
		view := model.SubscriptionSyncSourceInput{SubscriptionSyncSource: s}
		view.HasCredentials = s.SecretPayload != ""
		view.EffectiveStaleSeconds = model.SubscriptionSyncStaleSeconds(s)
		view.FetchStatus = "Awaiting observation"
		if s.LastError != "" {
			view.FetchStatus = s.LastError
		} else if s.LastFetch > 0 {
			view.FetchStatus = "Synchronization is on schedule"
			if model.GetDBTimestamp()-s.LastFetch > view.EffectiveStaleSeconds {
				view.FetchStatus = "Synchronization is delayed"
			}
		}
		if view.RefreshMode == "" {
			view.RefreshMode = "manual"
		}
		if view.RefreshBefore == 0 {
			view.RefreshBefore = 86400
		}
		if err := common.UnmarshalJsonStr(s.MappingJSON, &view.Mapping); err != nil {
			common.ApiError(c, err)
			return
		}
		accounts := []model.SubscriptionSyncAccount{}
		if s.CatalogJSON != "" {
			if err := common.UnmarshalJsonStr(s.CatalogJSON, &accounts); err != nil {
				common.ApiError(c, err)
				return
			}
		}
		accounts = model.ReportedSubscriptionSyncAccounts(accounts)
		total, fresh := 0, 0
		view.StaleAccounts = []string{}
		now := model.GetDBTimestamp()
		for _, account := range accounts {
			accountStale := false
			for _, cycle := range account.Cycles {
				if cycle.Utilization == nil {
					continue
				}
				total++
				view.ObservedAt = max(view.ObservedAt, cycle.SampledAt)
				if view.OldestObservedAt == 0 || cycle.SampledAt < view.OldestObservedAt {
					view.OldestObservedAt = cycle.SampledAt
				}
				if cycle.Error == "" && cycle.SampledAt > 0 && cycle.SampledAt <= now+60 && now-cycle.SampledAt <= view.EffectiveStaleSeconds && cycle.WindowEnd > now {
					fresh++
				} else {
					accountStale = true
				}
			}
			if accountStale {
				name := account.Name
				if name == "" {
					name = account.ID
				}
				view.StaleAccounts = append(view.StaleAccounts, name)
			}
		}
		view.FreshCycles = fresh
		view.ReportedCycles = total
		switch {
		case total == 0:
			view.ObservationStatus = "No upstream observation"
		case fresh == 0:
			view.ObservationStatus = "Upstream observation is stale"
		case fresh < total:
			view.ObservationStatus = "Some upstream observations are stale"
		default:
			view.ObservationStatus = "Upstream observations are current"
		}
		views = append(views, view)
	}
	common.ApiSuccess(c, gin.H{"sources": views, "template": model.PanelSubscriptionSyncMapping(), "privileged": c.GetInt("role") >= common.RoleRootUser})
}

func SaveSubscriptionSyncSource(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "sync_credentials") {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2*1024*1024)
	var input model.SubscriptionSyncSourceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	input.Id, _ = strconv.Atoi(c.Param("id"))
	if c.Param("id") != "" && input.Id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	context, err := common.Marshal(service.SubscriptionSyncCredentialContext{SourceID: input.Id, Action: "save", Version: input.Version, Revision: input.CredentialRevision})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeSubscriptionSyncCredentials, Context: context}) == nil {
		return
	}
	input.MayApply = authz.Can(c.GetInt("id"), c.GetInt("role"), authz.SubscriptionPoolPermission("sync_apply"))
	source, err := service.SaveSubscriptionSyncSource(input, c.GetInt("id"), c.GetInt("role") >= common.RoleRootUser)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, source)
}

func RunSubscriptionSync(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "sync_run") {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var input struct {
		Preview *bool `json:"preview"`
	}
	if c.ShouldBindJSON(&input) != nil || input.Preview == nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	// Fetch tests may expose stable account IDs and therefore need sync_read too.
	if !requireSubscriptionPoolPermission(c, "sync_read") {
		return
	}
	result, err := service.RunSubscriptionSync(c.Request.Context(), id, c.GetInt("id"), *input.Preview)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func GetSubscriptionSyncSourceState(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "sync_read") {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var source model.SubscriptionSyncSource
	if err := model.DB.First(&source, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	accounts := []model.SubscriptionSyncAccount{}
	if source.CatalogJSON != "" {
		if err := common.UnmarshalJsonStr(source.CatalogJSON, &accounts); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	events := []model.SubscriptionSyncEvent{}
	if err := model.DB.Where("source_id = ?", id).Order("id desc").Limit(200).Find(&events).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	for i := range events {
		events[i].Changes = []model.SubscriptionSyncChange{}
		if events[i].Detail != "" {
			if err := common.UnmarshalJsonStr(events[i].Detail, &events[i].Changes); err != nil {
				common.ApiError(c, err)
				return
			}
		}
	}
	common.ApiSuccess(c, gin.H{"accounts": model.ReportedSubscriptionSyncAccounts(accounts), "events": events})
}

func GetSubscriptionSyncBinding(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "sync_read") {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	binding, err := model.GetSubscriptionSyncBinding(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	observations, err := model.GetSubscriptionSyncViews(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"binding": binding, "observations": observations})
}

func SaveSubscriptionSyncBinding(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "sync_binding") {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128*1024)
	var input model.SubscriptionSyncBindingInput
	if c.ShouldBindJSON(&input) != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	input.PoolID, _ = strconv.Atoi(c.Param("id"))
	// Turning on time/expiry mutation or quota enforcement is separately delegated.
	old, err := model.GetSubscriptionSyncBinding(input.PoolID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	sensitive := input.SyncExpiry || old.SyncExpiry
	for _, item := range input.Cycles {
		sensitive = sensitive || item.AlignReset || item.Enforce || item.QuotaPerFullWindow > 0
	}
	for _, item := range old.Cycles {
		sensitive = sensitive || item.AlignReset || item.Enforce || item.QuotaPerFullWindow > 0
	}
	if sensitive && !requireSubscriptionPoolPermission(c, "sync_apply") {
		return
	}
	if err := model.SaveSubscriptionSyncBinding(input, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func ConfirmSubscriptionSyncReset(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "sync_apply") {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	var input struct {
		Key     string `json:"key"`
		Digest  string `json:"digest"`
		Version int64  `json:"version"`
		Confirm bool   `json:"confirm"`
	}
	if c.ShouldBindJSON(&input) != nil || !input.Confirm || id <= 0 || len(input.Digest) != 64 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid parameters"})
		return
	}
	if err := model.ConfirmSubscriptionSyncReset(id, input.Key, input.Digest, input.Version, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func UnbindSubscriptionSyncPool(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "sync_binding") {
		return
	}
	poolID, _ := strconv.Atoi(c.Param("id"))
	var input struct {
		Version int64 `json:"version"`
		Confirm bool  `json:"confirm"`
	}
	if c.ShouldBindJSON(&input) != nil || !input.Confirm || poolID <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	old, err := model.GetSubscriptionSyncBinding(poolID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	sensitive := old.SyncExpiry
	for _, cycle := range old.Cycles {
		sensitive = sensitive || cycle.AlignReset || cycle.Enforce
	}
	if sensitive && !requireSubscriptionPoolPermission(c, "sync_apply") {
		return
	}
	if err := model.UnbindSubscriptionSyncPool(poolID, input.Version, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func ManageSubscriptionSyncAuthentication(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "sync_credentials") || !requireSubscriptionPoolPermission(c, "sync_read") {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32768)
	var input struct {
		Action   string `json:"action"`
		Token    string `json:"token"`
		Version  int64  `json:"version"`
		Revision int64  `json:"credential_revision"`
		Confirm  bool   `json:"confirm"`
	}
	if id <= 0 || c.ShouldBindJSON(&input) != nil || !input.Confirm {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	context, err := common.Marshal(service.SubscriptionSyncCredentialContext{SourceID: id, Action: input.Action, Version: input.Version, Revision: input.Revision})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeSubscriptionSyncCredentials, Context: context}) == nil {
		return
	}
	source, err := service.ManageSubscriptionSyncAuthentication(c.Request.Context(), id, c.GetInt("id"), input.Action, input.Token, input.Version, input.Revision)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, source)
}

func DeleteSubscriptionSyncSource(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "sync_credentials") {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	var input struct {
		Version  int64 `json:"version"`
		Revision int64 `json:"credential_revision"`
		Confirm  bool  `json:"confirm"`
	}
	if id <= 0 || c.ShouldBindJSON(&input) != nil || !input.Confirm {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	context, err := common.Marshal(service.SubscriptionSyncCredentialContext{SourceID: id, Action: "delete", Version: input.Version, Revision: input.Revision})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeSubscriptionSyncCredentials, Context: context}) == nil {
		return
	}
	if err := model.DeleteSubscriptionSyncSource(id, input.Version, input.Revision, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
