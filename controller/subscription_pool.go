package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/service/usercontrol"
	"github.com/gin-gonic/gin"
)

func requireSubscriptionPoolPermission(c *gin.Context, action string) bool {
	if authz.Can(c.GetInt("id"), c.GetInt("role"), authz.SubscriptionPoolPermission(action)) {
		return true
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": "Unauthorized, insufficient privileges"})
	return false
}

func GetSubscriptionPoolCapabilities(c *gin.Context) {
	result := map[string]bool{}
	for _, action := range []string{"read", "manage", "assign", "calibrate", "shared_reset", "personal_reset", "batch_migrate", "audit_read", "sync_read", "sync_run", "sync_credentials", "sync_binding", "sync_apply"} {
		result[action] = authz.Can(c.GetInt("id"), c.GetInt("role"), authz.SubscriptionPoolPermission(action))
	}
	common.ApiSuccess(c, result)
}

func GetSubscriptionSelfUsage(c *gin.Context) {
	rows, err := model.GetSubscriptionUsage(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"server_time": model.GetDBTimestamp(), "subscriptions": rows})
}

// TokenAuthReadOnly validates the key, account status and IP restrictions before
// this handler. Quota visibility is rechecked from the database on every export.
func GetTokenSubscriptionUsage(c *gin.Context) {
	token, err := model.GetTokenById(c.GetInt("token_id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if token.UserId != c.GetInt("id") || !token.AllowUserQuotaQuery {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": "Unauthorized, insufficient privileges"})
		return
	}
	GetSubscriptionSelfUsage(c)
}

// Access-token route policy deliberately makes this a browser-session-only
// endpoint; wallet:read exports never include the aggregate upstream overview.
func GetSubscriptionSelfPools(c *gin.Context) {
	rows, err := model.GetSubscriptionPoolOverview(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func ListSubscriptionPools(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "read") {
		return
	}
	var pools []model.SubscriptionPool
	query := model.DB.Order("id desc")
	if search := c.Query("search"); search != "" {
		query = query.Where("name LIKE ?", "%"+search+"%")
	}
	if err := query.Limit(1000).Find(&pools).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	result := []model.SubscriptionPoolInput{}
	for _, pool := range pools {
		view, err := model.GetSubscriptionPoolConfiguration(pool.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		result = append(result, view)
	}
	common.ApiSuccess(c, result)
}

func SaveSubscriptionPool(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "manage") || !requirePaymentCompliance(c) {
		return
	}
	var input model.SubscriptionPoolInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if c.Request.Method == http.MethodPut && id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	input.Id = id
	if input.Sync != nil && (!requireSubscriptionPoolPermission(c, "sync_read") || !requireSubscriptionPoolPermission(c, "sync_binding") || !requireSubscriptionPoolPermission(c, "sync_apply")) {
		return
	}
	if input.SyncBinding != nil && (!requireSubscriptionPoolPermission(c, "sync_binding") || !requireSubscriptionPoolPermission(c, "sync_apply")) {
		return
	}
	pool, err := model.SaveSubscriptionPool(input, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	view, err := model.GetSubscriptionPoolConfiguration(pool.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func SubscriptionPoolCatalog(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "read") {
		return
	}
	var channels []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := model.DB.Model(&model.Channel{}).Select("id", "name").Order("id").Find(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, channels)
}

func GetSubscriptionPoolDetail(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "read") {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var pool model.SubscriptionPool
	if err := model.DB.First(&pool, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	// Admin own resource overview; membership is deliberately not joined into
	// user-facing DTOs or returned by the external quota endpoint.
	var members []struct {
		SubscriptionID     int                               `json:"subscription_id"`
		UserID             int                               `json:"user_id"`
		Username           string                            `json:"username"`
		EndTime            int64                             `json:"end_time"`
		Status             string                            `json:"status"`
		Version            int64                             `json:"version"`
		QuotaMultiplier    float64                           `json:"quota_multiplier"`
		CycleOverridesJSON string                            `json:"-" gorm:"column:cycle_overrides"`
		CycleOverrides     []model.SubscriptionCycleOverride `json:"cycle_overrides" gorm:"-"`
	}
	if err := model.DB.Table("subscription_allocations AS a").Select("a.subscription_id, a.user_id, a.version, a.quota_multiplier, a.cycle_overrides, u.username, s.end_time, s.status").Joins("JOIN user_subscriptions AS s ON s.id = a.subscription_id").Joins("LEFT JOIN users AS u ON u.id = a.user_id").Where("a.pool_id = ?", id).Order("a.subscription_id desc").Limit(1000).Scan(&members).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	for i := range members {
		members[i].QuotaMultiplier = model.NormalizeSubscriptionQuotaMultiplier(members[i].QuotaMultiplier)
		overrides, err := model.ParseSubscriptionCycleOverrides(members[i].CycleOverridesJSON)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		members[i].CycleOverrides = overrides
	}
	events := []model.SubscriptionPoolEvent{}
	syncEvents := []model.SubscriptionSyncEvent{}
	if authz.Can(c.GetInt("id"), c.GetInt("role"), authz.SubscriptionPoolPermission("audit_read")) {
		if err := model.DB.Where("pool_id = ?", id).Order("id desc").Limit(200).Find(&events).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		if authz.Can(c.GetInt("id"), c.GetInt("role"), authz.SubscriptionPoolPermission("sync_read")) {
			if err := model.DB.Where("pool_id = ?", id).Order("id desc").Limit(200).Find(&syncEvents).Error; err != nil {
				common.ApiError(c, err)
				return
			}
			for i := range syncEvents {
				if syncEvents[i].Detail != "" {
					if err := common.UnmarshalJsonStr(syncEvents[i].Detail, &syncEvents[i].Changes); err != nil {
						common.ApiError(c, err)
						return
					}
				}
			}
		}
	}
	cycles, err := model.GetSubscriptionPoolStatistics(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"members": members, "events": events, "sync_events": syncEvents, "cycles": cycles})
}

type subscriptionPlanAllocationInput struct {
	model.SubscriptionPlanAllocation
	Cycles []model.SubscriptionCycleRule `json:"cycles"`
}

func GetSubscriptionPlanAllocation(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "read") {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	cfg := model.SubscriptionPlanAllocation{}
	if err := model.DB.Where("plan_id = ?", id).Find(&cfg).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.HydrateSubscriptionPlanAllocation(&cfg); err != nil {
		common.ApiError(c, err)
		return
	}
	cycles := []model.SubscriptionCycleRule{}
	if cfg.Rules != "" {
		if err := common.UnmarshalJsonStr(cfg.Rules, &cycles); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	// Unused cycles were persisted as JSON null for pooled/legacy plans.
	// The editor contract always requires an array, including after decoding.
	if cycles == nil {
		cycles = []model.SubscriptionCycleRule{}
	}
	common.ApiSuccess(c, subscriptionPlanAllocationInput{SubscriptionPlanAllocation: cfg, Cycles: cycles})
}
func PutSubscriptionPlanAllocation(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "manage") || !requirePaymentCompliance(c) {
		return
	}
	var input subscriptionPlanAllocationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	input.PlanID, _ = strconv.Atoi(c.Param("id"))
	if err := model.SaveSubscriptionPlanAllocation(input.SubscriptionPlanAllocation, input.Cycles); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func GetUserSubscriptionAllocations(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "read") {
		return
	}
	userID, _ := strconv.Atoi(c.Param("id"))
	if userID <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var rows []model.SubscriptionAllocation
	if err := model.DB.Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	for i := range rows {
		if err := model.HydrateSubscriptionAllocation(&rows[i]); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	usage, err := model.GetSubscriptionUsage(userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"allocations": rows, "subscriptions": usage})
}

type subscriptionPoolAssignment struct {
	UsageDigest     string                             `json:"usage_digest"`
	SubscriptionID  int                                `json:"subscription_id"`
	PoolID          int                                `json:"pool_id"`
	Version         int64                              `json:"version"`
	EndTime         int64                              `json:"end_time"`
	Confirm         bool                               `json:"confirm"`
	PoolVersion     int64                              `json:"pool_version"`
	QuotaMultiplier *float64                           `json:"quota_multiplier,omitempty"`
	CycleOverrides  *[]model.SubscriptionCycleOverride `json:"cycle_overrides,omitempty"`
}

func subscriptionAssignmentTarget(c *gin.Context, id int) error {
	if id <= 0 {
		return errors.New("Invalid parameters")
	}
	var sub model.UserSubscription
	if err := model.DB.First(&sub, id).Error; err != nil {
		return err
	}
	if c.GetInt("role") >= common.RoleRootUser {
		return nil
	}
	return usercontrol.CheckControlTarget(c.GetInt("id"), c.GetInt("role"), sub.UserId)
}
func AssignUserSubscriptionPool(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "assign") {
		return
	}
	var input subscriptionPoolAssignment
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err := subscriptionAssignmentTarget(c, input.SubscriptionID); err != nil {
		common.ApiError(c, err)
		return
	}
	opts := model.SubscriptionPoolAssignmentOptions{
		QuotaMultiplier: input.QuotaMultiplier,
		CycleOverrides:  input.CycleOverrides,
	}
	if !input.Confirm {
		preview, err := model.PreviewSubscriptionPoolAssignmentWithOptions(input.SubscriptionID, input.PoolID, input.EndTime, opts)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiSuccess(c, preview)
		return
	}
	if err := model.AssignSubscriptionPoolWithOptions(input.SubscriptionID, input.PoolID, input.Version, input.EndTime, c.GetInt("id"), true, input.PoolVersion, opts, input.UsageDigest); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
func BatchAssignSubscriptionPool(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "batch_migrate") || !requireSubscriptionPoolPermission(c, "assign") {
		return
	}
	var input struct {
		Items []subscriptionPoolAssignment `json:"items"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || len(input.Items) == 0 || len(input.Items) > 200 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	results := []gin.H{}
	seen := map[int]bool{}
	for _, item := range input.Items {
		if seen[item.SubscriptionID] {
			continue
		}
		seen[item.SubscriptionID] = true
		err := subscriptionAssignmentTarget(c, item.SubscriptionID)
		var preview *model.SubscriptionAssignmentPreview
		if err == nil {
			opts := model.SubscriptionPoolAssignmentOptions{
				QuotaMultiplier: item.QuotaMultiplier,
				CycleOverrides:  item.CycleOverrides,
			}
			if item.Confirm {
				err = model.AssignSubscriptionPoolWithOptions(item.SubscriptionID, item.PoolID, item.Version, item.EndTime, c.GetInt("id"), true, item.PoolVersion, opts, item.UsageDigest)
			} else {
				preview, err = model.PreviewSubscriptionPoolAssignmentWithOptions(item.SubscriptionID, item.PoolID, item.EndTime, opts)
			}
		}
		message := ""
		if err != nil {
			message = err.Error()
		}
		results = append(results, gin.H{"subscription_id": item.SubscriptionID, "success": err == nil, "message": message, "preview": preview})
	}
	common.ApiSuccess(c, results)
}
func IngestSubscriptionPoolSnapshot(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "calibrate") {
		return
	}
	var input struct {
		model.SubscriptionUpstreamSnapshot
		Calibrate bool `json:"calibrate"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	input.Id = 0
	input.PoolID, _ = strconv.Atoi(c.Param("id"))
	if err := model.IngestSubscriptionSnapshot(input.SubscriptionUpstreamSnapshot, c.GetInt("id"), input.Calibrate); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
func ResetSubscriptionPoolCycle(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "shared_reset") {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var input struct {
		Key     string `json:"key"`
		Confirm bool   `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || !input.Confirm {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err := model.ResetSubscriptionCycle(0, id, 0, input.Key, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
func ResetIndependentSubscriptionCycle(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "personal_reset") {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if err := subscriptionAssignmentTarget(c, id); err != nil {
		common.ApiError(c, err)
		return
	}
	var allocation model.SubscriptionAllocation
	err := model.DB.Where("subscription_id = ?", id).First(&allocation).Error
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if allocation.Pending {
		common.ApiErrorT(c, "Subscription assignment changed; retry the request")
		return
	}
	var input struct {
		Key     string `json:"key"`
		Confirm bool   `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || !input.Confirm {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err := model.ResetSubscriptionCycle(id, allocation.PoolID, allocation.UserID, input.Key, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func DeleteSubscriptionPool(c *gin.Context) {
	if !requireSubscriptionPoolPermission(c, "manage") {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	var input struct {
		Version int64 `json:"version"`
		Confirm bool  `json:"confirm"`
	}
	if id <= 0 || c.ShouldBindJSON(&input) != nil || !input.Confirm {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	binding, err := model.GetSubscriptionSyncBinding(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if binding.SourceID > 0 && !requireSubscriptionPoolPermission(c, "sync_binding") {
		return
	}
	if err := model.DeleteSubscriptionPool(id, input.Version, binding.Version, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
