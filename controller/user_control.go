package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/service/usercontrol"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type userControlMemberView struct {
	model.UserControlListMember
	Username string `json:"username"`
}

type userControlOverrideView struct {
	model.UserControlUserOverride
	Username string                `json:"username"`
	LimitsV  usercontrol.Limits    `json:"limits_view"`
	Schedule *usercontrol.Schedule `json:"schedule_view,omitempty"`
}

type userControlPlanBindingView struct {
	model.UserControlPlanBinding
	PlanTitle string `json:"plan_title"`
}

func userControlFail(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"success": false, "message": message})
}

// userControlSkippedTarget reports one target that the operator is not
// allowed to control (self, root, equal/higher role, or missing user).
type userControlSkippedTarget struct {
	UserID int    `json:"user_id"`
	Reason string `json:"reason"`
}

// filterControllableTargets keeps only users the operator may manage. Adding a
// protected account to a controlled list would let an admin apply limits to a
// peer or to root, so the same target rules used by overrides are enforced.
func filterControllableTargets(c *gin.Context, userIDs []int) ([]int, []userControlSkippedTarget) {
	eligible := make([]int, 0, len(userIDs))
	skipped := make([]userControlSkippedTarget, 0)
	seen := make(map[int]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			skipped = append(skipped, userControlSkippedTarget{UserID: userID, Reason: "invalid user id"})
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		if err := usercontrol.CheckControlTarget(c.GetInt("id"), c.GetInt("role"), userID); err != nil {
			skipped = append(skipped, userControlSkippedTarget{UserID: userID, Reason: err.Error()})
			continue
		}
		eligible = append(eligible, userID)
	}
	return eligible, skipped
}

// filterRemovableTargets applies the same role boundary as adding members but
// still allows cleaning up membership rows for users that no longer exist.
func filterRemovableTargets(c *gin.Context, userIDs []int) ([]int, []userControlSkippedTarget) {
	eligible := make([]int, 0, len(userIDs))
	skipped := make([]userControlSkippedTarget, 0)
	seen := make(map[int]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			skipped = append(skipped, userControlSkippedTarget{UserID: userID, Reason: "invalid user id"})
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		err := usercontrol.CheckControlTarget(c.GetInt("id"), c.GetInt("role"), userID)
		if err != nil && !errors.Is(err, usercontrol.ErrTargetNotFound) {
			skipped = append(skipped, userControlSkippedTarget{UserID: userID, Reason: err.Error()})
			continue
		}
		eligible = append(eligible, userID)
	}
	return eligible, skipped
}

func requireUserControlPermission(c *gin.Context, permission authz.Permission) bool {
	if authz.Can(c.GetInt("id"), c.GetInt("role"), permission) {
		return true
	}
	userControlFail(c, http.StatusForbidden, "Unauthorized, insufficient privileges")
	return false
}

func userControlWriteError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, usercontrol.ErrNotFound), errors.Is(err, usercontrol.ErrTargetNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		userControlFail(c, http.StatusNotFound, err.Error())
	case errors.Is(err, usercontrol.ErrPolicyInUse):
		userControlFail(c, http.StatusConflict, err.Error())
	case errors.Is(err, usercontrol.ErrInvalidInput), errors.Is(err, usercontrol.ErrInvalidOverride),
		errors.Is(err, usercontrol.ErrPolicyDisabled), errors.Is(err, usercontrol.ErrSelfTarget),
		errors.Is(err, usercontrol.ErrRoleTarget), errors.Is(err, usercontrol.ErrRootTarget):
		userControlFail(c, http.StatusBadRequest, err.Error())
	default:
		userControlFail(c, http.StatusInternalServerError, err.Error())
	}
}

// gormNotFound is assigned once so the controller does not need to import gorm.
var gormNotFound = errors.New("record not found")

func auditUserControl(c *gin.Context, action string, params model.AuditFields) {
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), "User control: "+action, c.ClientIP(),
		"user_control."+action, params, auditOperatorInfo(c), &model.AuditRequestInfo{
			Method: c.Request.Method, Route: c.FullPath(), Status: http.StatusOK, Success: true,
		}, c)
	markAuditLogged(c)
}

// ---------------------------------------------------------------------------
// Capabilities
// ---------------------------------------------------------------------------

func GetUserControlCapabilities(c *gin.Context) {
	can := func(permission authz.Permission) bool {
		return authz.Can(c.GetInt("id"), c.GetInt("role"), permission)
	}
	if !can(authz.UserControlRead) {
		userControlFail(c, http.StatusForbidden, "Unauthorized, insufficient privileges")
		return
	}
	common.ApiSuccess(c, gin.H{
		"read":             true,
		"list_write":       can(authz.UserControlListWrite),
		"policy_write":     can(authz.UserControlPolicyWrite),
		"override_write":   can(authz.UserControlOverrideWrite),
		"override_relax":   can(authz.UserControlOverrideRelax),
		"apply":            can(authz.UserControlApply),
		"audit_read":       can(authz.UserControlAuditRead),
		"default_timezone": usercontrol.DefaultTimezone,
		"root":             c.GetInt("role") >= common.RoleRootUser,
	})
}

// ---------------------------------------------------------------------------
// Policies
// ---------------------------------------------------------------------------

func ListUserControlPolicies(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlRead) {
		return
	}
	policies, err := model.ListUserControlPolicies()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]usercontrol.PolicyView, 0, len(policies))
	for i := range policies {
		view, err := usercontrol.ParsePolicyView(&policies[i])
		if err != nil {
			common.ApiError(c, err)
			return
		}
		views = append(views, view)
	}
	common.ApiSuccess(c, gin.H{"items": views})
}

func GetUserControlPolicy(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlRead) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	policy, err := model.GetUserControlPolicy(id)
	if err != nil {
		userControlFail(c, http.StatusNotFound, "Policy does not exist")
		return
	}
	view, err := usercontrol.ParsePolicyView(policy)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func CreateUserControlPolicy(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlPolicyWrite) {
		return
	}
	var payload usercontrol.PolicyPayload
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil {
		userControlFail(c, http.StatusBadRequest, "Invalid parameters")
		return
	}
	policy, err := usercontrol.BuildPolicy(payload, c.GetInt("id"))
	if err != nil {
		userControlWriteError(c, err)
		return
	}
	if err := model.CreateUserControlPolicy(policy); err != nil {
		userControlWriteError(c, err)
		return
	}
	usercontrol.Invalidate()
	auditUserControl(c, "policy.create", model.AuditFields{"policy_id": policy.Id, "name": policy.Name, "priority": policy.Priority})
	view, _ := usercontrol.ParsePolicyView(policy)
	common.ApiSuccess(c, view)
}

func UpdateUserControlPolicy(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlPolicyWrite) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	policy, err := model.GetUserControlPolicy(id)
	if err != nil {
		userControlFail(c, http.StatusNotFound, "Policy does not exist")
		return
	}
	var payload usercontrol.PolicyPayload
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil {
		userControlFail(c, http.StatusBadRequest, "Invalid parameters")
		return
	}
	if err := usercontrol.ApplyPolicyPayload(policy, payload, c.GetInt("id")); err != nil {
		userControlWriteError(c, err)
		return
	}
	if err := model.UpdateUserControlPolicy(policy); err != nil {
		userControlWriteError(c, err)
		return
	}
	usercontrol.Invalidate()
	auditUserControl(c, "policy.update", model.AuditFields{"policy_id": policy.Id, "name": policy.Name, "version": policy.Version})
	view, _ := usercontrol.ParsePolicyView(policy)
	common.ApiSuccess(c, view)
}

func DeleteUserControlPolicy(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlPolicyWrite) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	references, err := model.CountUserControlReferencesSummary(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	poolReference, err := usercontrol.PoolReferences(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if references > 0 || poolReference {
		userControlFail(c, http.StatusConflict, usercontrol.ErrPolicyInUse.Error())
		return
	}
	if err := model.DeleteUserControlPolicy(id); err != nil {
		userControlWriteError(c, err)
		return
	}
	usercontrol.Invalidate()
	auditUserControl(c, "policy.delete", model.AuditFields{"policy_id": id})
	common.ApiSuccess(c, gin.H{"id": id})
}

// ---------------------------------------------------------------------------
// Lists and members
// ---------------------------------------------------------------------------

func ListUserControlLists(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlRead) {
		return
	}
	lists, err := model.ListUserControlLists()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	type listView struct {
		model.UserControlList
		Members int64 `json:"members"`
	}
	views := make([]listView, 0, len(lists))
	for _, list := range lists {
		count, _ := model.CountUserControlListMembers(list.Id)
		views = append(views, listView{UserControlList: list, Members: count})
	}
	common.ApiSuccess(c, gin.H{"items": views})
}

func CreateUserControlList(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlListWrite) {
		return
	}
	var payload usercontrol.ListPayload
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil {
		userControlFail(c, http.StatusBadRequest, "Invalid parameters")
		return
	}
	list, err := usercontrol.BuildList(payload, c.GetInt("id"))
	if err != nil {
		userControlWriteError(c, err)
		return
	}
	if err := model.CreateUserControlList(list); err != nil {
		userControlWriteError(c, err)
		return
	}
	usercontrol.Invalidate()
	auditUserControl(c, "list.create", model.AuditFields{"list_id": list.Id, "name": list.Name, "policy_id": list.PolicyId})
	common.ApiSuccess(c, list)
}

func UpdateUserControlList(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlListWrite) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	list, err := model.GetUserControlList(id)
	if err != nil {
		userControlFail(c, http.StatusNotFound, "List does not exist")
		return
	}
	var payload usercontrol.ListPayload
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil {
		userControlFail(c, http.StatusBadRequest, "Invalid parameters")
		return
	}
	if err := usercontrol.ApplyListPayload(list, payload, c.GetInt("id")); err != nil {
		userControlWriteError(c, err)
		return
	}
	if err := model.UpdateUserControlList(list); err != nil {
		userControlWriteError(c, err)
		return
	}
	usercontrol.Invalidate()
	auditUserControl(c, "list.update", model.AuditFields{"list_id": list.Id, "name": list.Name, "policy_id": list.PolicyId, "enabled": list.Enabled})
	common.ApiSuccess(c, list)
}

func DeleteUserControlList(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlListWrite) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if err := model.DeleteUserControlList(id); err != nil {
		userControlWriteError(c, err)
		return
	}
	usercontrol.Invalidate()
	auditUserControl(c, "list.delete", model.AuditFields{"list_id": id})
	common.ApiSuccess(c, gin.H{"id": id})
}

func ListUserControlListMembers(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlRead) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	page := common.GetPageQuery(c)
	members, total, err := model.ListUserControlListMembers(id, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ids := make([]int, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.UserId)
	}
	users, _ := model.GetUsersByIds(ids)
	names := make(map[int]string, len(users))
	for i := range users {
		names[users[i].Id] = users[i].Username
	}
	views := make([]userControlMemberView, 0, len(members))
	for _, member := range members {
		views = append(views, userControlMemberView{UserControlListMember: member, Username: names[member.UserId]})
	}
	page.SetTotal(int(total))
	page.SetItems(views)
	common.ApiSuccess(c, page)
}

type userControlMembersPayload struct {
	UserIDs []int  `json:"user_ids"`
	Note    string `json:"note"`
}

func AddUserControlListMembers(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlListWrite) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if _, err := model.GetUserControlList(id); err != nil {
		userControlFail(c, http.StatusNotFound, "List does not exist")
		return
	}
	var payload userControlMembersPayload
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil {
		userControlFail(c, http.StatusBadRequest, "Invalid parameters")
		return
	}
	if len(payload.UserIDs) == 0 || len(payload.UserIDs) > 5000 {
		userControlFail(c, http.StatusBadRequest, "user_ids must contain 1..5000 ids")
		return
	}
	eligible, skipped := filterControllableTargets(c, payload.UserIDs)
	inserted := 0
	if len(eligible) > 0 {
		var err error
		inserted, err = model.AddUserControlListMembers(id, eligible, strings.TrimSpace(payload.Note), c.GetInt("id"))
		if err != nil {
			userControlWriteError(c, err)
			return
		}
		usercontrol.Invalidate()
	}
	auditUserControl(c, "list.members.add", model.AuditFields{
		"list_id": id, "user_ids": eligible, "inserted": inserted,
		"skipped": len(skipped), "requested": len(payload.UserIDs),
	})
	common.ApiSuccess(c, gin.H{
		"list_id": id, "inserted": inserted, "requested": len(payload.UserIDs),
		"skipped": skipped,
	})
}

func RemoveUserControlListMembers(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlListWrite) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	var payload userControlMembersPayload
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil {
		userControlFail(c, http.StatusBadRequest, "Invalid parameters")
		return
	}
	eligible, skipped := filterRemovableTargets(c, payload.UserIDs)
	removed := int64(0)
	if len(eligible) > 0 {
		var err error
		removed, err = model.RemoveUserControlListMembers(id, eligible)
		if err != nil {
			userControlWriteError(c, err)
			return
		}
		usercontrol.Invalidate()
	}
	auditUserControl(c, "list.members.remove", model.AuditFields{
		"list_id": id, "user_ids": eligible, "removed": removed, "skipped": len(skipped),
	})
	common.ApiSuccess(c, gin.H{"list_id": id, "removed": removed, "skipped": skipped})
}

// ---------------------------------------------------------------------------
// User overrides
// ---------------------------------------------------------------------------

func ListUserControlOverrides(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlRead) {
		return
	}
	overrides, err := model.ListUserControlOverrides()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ids := make([]int, 0, len(overrides))
	for _, override := range overrides {
		ids = append(ids, override.UserId)
	}
	users, _ := model.GetUsersByIds(ids)
	names := make(map[int]string, len(users))
	for i := range users {
		names[users[i].Id] = users[i].Username
	}
	views := make([]userControlOverrideView, 0, len(overrides))
	for i := range overrides {
		override := &overrides[i]
		limits, _ := usercontrol.ParseLimits(override.Limits)
		schedule, _ := usercontrol.ParseSchedule(override.Schedule)
		views = append(views, userControlOverrideView{
			UserControlUserOverride: *override,
			Username:                names[override.UserId],
			LimitsV:                 limits,
			Schedule:                schedule,
		})
	}
	common.ApiSuccess(c, gin.H{"items": views})
}

func GetUserControlOverride(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlRead) {
		return
	}
	userID, _ := strconv.Atoi(c.Param("user_id"))
	override, err := model.GetUserControlOverride(userID)
	if err != nil {
		userControlFail(c, http.StatusNotFound, usercontrol.ErrOverrideNotExists.Error())
		return
	}
	limits, _ := usercontrol.ParseLimits(override.Limits)
	schedule, _ := usercontrol.ParseSchedule(override.Schedule)
	user, _ := model.GetUserById(userID, false)
	username := ""
	if user != nil {
		username = user.Username
	}
	common.ApiSuccess(c, userControlOverrideView{
		UserControlUserOverride: *override,
		Username:                username,
		LimitsV:                 limits,
		Schedule:                schedule,
	})
}

func PutUserControlOverride(c *gin.Context) {
	userID, _ := strconv.Atoi(c.Param("user_id"))
	var payload usercontrol.OverridePayload
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil {
		userControlFail(c, http.StatusBadRequest, "Invalid parameters")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(payload.Mode))
	if mode == "" {
		mode = "merge"
	}
	if mode == "replace" {
		if !requireUserControlPermission(c, authz.UserControlOverrideRelax) {
			return
		}
	}
	if !requireUserControlPermission(c, authz.UserControlOverrideWrite) {
		return
	}
	if err := usercontrol.CheckControlTarget(c.GetInt("id"), c.GetInt("role"), userID); err != nil {
		userControlWriteError(c, err)
		return
	}
	override, err := usercontrol.BuildOverride(userID, payload, c.GetInt("id"))
	if err != nil {
		userControlWriteError(c, err)
		return
	}
	if err := model.UpsertUserControlOverride(override); err != nil {
		userControlWriteError(c, err)
		return
	}
	usercontrol.Invalidate()
	auditUserControl(c, "override.set", model.AuditFields{
		"user_id": userID, "policy_id": override.PolicyId, "mode": override.Mode,
		"expires_at": override.ExpiresAt, "reason": override.Reason,
	})
	limits, _ := usercontrol.ParseLimits(override.Limits)
	schedule, _ := usercontrol.ParseSchedule(override.Schedule)
	common.ApiSuccess(c, userControlOverrideView{
		UserControlUserOverride: *override,
		LimitsV:                 limits,
		Schedule:                schedule,
	})
}

func DeleteUserControlOverride(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlOverrideWrite) {
		return
	}
	userID, _ := strconv.Atoi(c.Param("user_id"))
	if err := usercontrol.CheckControlTarget(c.GetInt("id"), c.GetInt("role"), userID); err != nil {
		userControlWriteError(c, err)
		return
	}
	if err := model.DeleteUserControlOverride(userID); err != nil {
		userControlWriteError(c, err)
		return
	}
	usercontrol.Invalidate()
	auditUserControl(c, "override.delete", model.AuditFields{"user_id": userID})
	common.ApiSuccess(c, gin.H{"user_id": userID})
}

// ---------------------------------------------------------------------------
// Subscription plan bindings
// ---------------------------------------------------------------------------

func ListUserControlPlanBindings(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlRead) {
		return
	}
	bindings, err := model.ListUserControlPlanBindings()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]userControlPlanBindingView, 0, len(bindings))
	for _, binding := range bindings {
		title := ""
		if plan, err := model.GetSubscriptionPlanById(binding.PlanId); err == nil {
			title = plan.Title
		}
		views = append(views, userControlPlanBindingView{UserControlPlanBinding: binding, PlanTitle: title})
	}
	common.ApiSuccess(c, gin.H{"items": views})
}

type userControlPlanBindingPayload struct {
	PolicyId int `json:"policy_id"`
}

func PutUserControlPlanBinding(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlPolicyWrite) {
		return
	}
	planID, _ := strconv.Atoi(c.Param("plan_id"))
	if _, err := model.GetSubscriptionPlanById(planID); err != nil {
		userControlFail(c, http.StatusNotFound, "Subscription plan does not exist")
		return
	}
	var payload userControlPlanBindingPayload
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil {
		userControlFail(c, http.StatusBadRequest, "Invalid parameters")
		return
	}
	policy, err := model.GetUserControlPolicy(payload.PolicyId)
	if err != nil {
		userControlFail(c, http.StatusBadRequest, "Policy does not exist")
		return
	}
	if !policy.Enabled {
		userControlWriteError(c, usercontrol.ErrPolicyDisabled)
		return
	}
	binding := &model.UserControlPlanBinding{
		PlanId: planID, PolicyId: payload.PolicyId,
		CreatedBy: c.GetInt("id"), CreatedAt: common.GetTimestamp(), UpdatedAt: common.GetTimestamp(),
	}
	if err := model.UpsertUserControlPlanBinding(binding); err != nil {
		userControlWriteError(c, err)
		return
	}
	usercontrol.Invalidate()
	auditUserControl(c, "plan.bind", model.AuditFields{"plan_id": planID, "policy_id": payload.PolicyId})
	common.ApiSuccess(c, binding)
}

func DeleteUserControlPlanBinding(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlPolicyWrite) {
		return
	}
	planID, _ := strconv.Atoi(c.Param("plan_id"))
	if err := model.DeleteUserControlPlanBinding(planID); err != nil {
		userControlWriteError(c, err)
		return
	}
	usercontrol.Invalidate()
	auditUserControl(c, "plan.unbind", model.AuditFields{"plan_id": planID})
	common.ApiSuccess(c, gin.H{"plan_id": planID})
}

// ---------------------------------------------------------------------------
// Effective policy and simulator
// ---------------------------------------------------------------------------

func parseAt(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now(), nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	return time.Parse("2006-01-02T15:04", value)
}

func GetUserControlEffective(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlRead) {
		return
	}
	userID, _ := strconv.Atoi(c.Param("user_id"))
	user, err := model.GetUserById(userID, false)
	if err != nil {
		userControlFail(c, http.StatusNotFound, "User does not exist")
		return
	}
	at, err := parseAt(c.Query("at"))
	if err != nil {
		userControlFail(c, http.StatusBadRequest, "Invalid at timestamp")
		return
	}
	view, err := usercontrol.ExplainAt(user.Id, user.Role, at)
	if err != nil {
		userControlWriteError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func SimulateUserControl(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlRead) {
		return
	}
	userID, _ := strconv.Atoi(c.Query("user_id"))
	if userID <= 0 {
		userControlFail(c, http.StatusBadRequest, "user_id is required")
		return
	}
	user, err := model.GetUserById(userID, false)
	if err != nil {
		userControlFail(c, http.StatusNotFound, "User does not exist")
		return
	}
	at, err := parseAt(c.Query("at"))
	if err != nil {
		userControlFail(c, http.StatusBadRequest, "Invalid at timestamp")
		return
	}
	view, err := usercontrol.ExplainAt(user.Id, user.Role, at)
	if err != nil {
		userControlWriteError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// ---------------------------------------------------------------------------
// Bulk apply and audit
// ---------------------------------------------------------------------------

type userControlApplyPayload struct {
	UserIDs   []int  `json:"user_ids"`
	PolicyID  int    `json:"policy_id"`
	Mode      string `json:"mode"`
	ExpiresAt int64  `json:"expires_at"`
	Reason    string `json:"reason"`
}

func ApplyUserControlToUsers(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlApply) {
		return
	}
	var payload userControlApplyPayload
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil {
		userControlFail(c, http.StatusBadRequest, "Invalid parameters")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(payload.Mode))
	if mode == "" {
		mode = "merge"
	}
	if mode == "replace" && !requireUserControlPermission(c, authz.UserControlOverrideRelax) {
		return
	}
	results, err := usercontrol.ApplyToUsers(c.GetInt("id"), c.GetInt("role"), payload.PolicyID, payload.UserIDs, mode, payload.ExpiresAt, payload.Reason)
	if err != nil {
		userControlWriteError(c, err)
		return
	}
	applied, skipped, failed := 0, 0, 0
	for _, result := range results {
		switch result.Status {
		case "applied":
			applied++
		case "skipped":
			skipped++
		default:
			failed++
		}
	}
	auditUserControl(c, "apply", model.AuditFields{
		"policy_id": payload.PolicyID, "mode": mode, "user_ids": payload.UserIDs,
		"applied": applied, "skipped": skipped, "failed": failed, "expires_at": payload.ExpiresAt,
	})
	common.ApiSuccess(c, gin.H{
		"requested": len(payload.UserIDs), "applied": applied,
		"skipped": skipped, "failed": failed, "items": results,
	})
}

func ListUserControlAudit(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlAuditRead) {
		return
	}
	page := common.GetPageQuery(c)
	filter := model.AuditLogFilter{
		ActionPrefix:   "user_control.",
		StartTimestamp: parseInt64Query(c, "start_timestamp"),
		EndTimestamp:   parseInt64Query(c, "end_timestamp"),
	}
	logs, total, err := model.GetAuditLogs(filter, page.GetStartIdx(), page.GetPageSize(), c.GetInt("role"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(logs)
	common.ApiSuccess(c, page)
}

func parseInt64Query(c *gin.Context, key string) int64 {
	value, _ := strconv.ParseInt(c.Query(key), 10, 64)
	return value
}
