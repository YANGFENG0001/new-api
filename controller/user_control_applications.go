package controller

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/service/usercontrol"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func ListUserControlPools(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlRead) {
		return
	}
	rows, err := model.ListUserControlPools()
	if err != nil {
		userControlV2WriteError(c, err)
		return
	}
	views := []usercontrol.PoolView{}
	for _, row := range rows {
		var config usercontrol.PoolConfig
		if err := common.UnmarshalJsonStr(row.Config, &config); err != nil {
			userControlV2WriteError(c, err)
			return
		}
		views = append(views, usercontrol.PoolView{Pool: row, Config: config})
	}
	common.ApiSuccess(c, gin.H{"items": views})
}

func PutUserControlPool(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlPolicyWrite) {
		return
	}
	var payload usercontrol.PoolPayload
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil {
		userControlFail(c, 400, "Invalid parameters")
		return
	}
	var existing *model.UserControlPool
	if c.Param("id") != "" {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil || id <= 0 {
			userControlFail(c, 400, "Invalid parameters")
			return
		}
		existing = &model.UserControlPool{}
		if err := model.DB.First(existing, id).Error; err != nil {
			userControlV2WriteError(c, err)
			return
		}
	}
	row, err := usercontrol.BuildPool(payload, existing, c.GetInt("id"))
	if err != nil {
		userControlV2WriteError(c, err)
		return
	}
	if err := model.SaveUserControlPool(row); err != nil {
		userControlV2WriteError(c, err)
		return
	}
	usercontrol.Invalidate()
	auditUserControl(c, "pool.save", model.AuditFields{"pool_id": row.Id, "version": row.Version})
	var config usercontrol.PoolConfig
	_ = common.UnmarshalJsonStr(row.Config, &config)
	common.ApiSuccess(c, usercontrol.PoolView{Pool: *row, Config: config})
}

func DeleteUserControlPool(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlPolicyWrite) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		userControlFail(c, 400, "Invalid parameters")
		return
	}
	used, err := usercontrol.ApplicationReferences("pool", id)
	if err != nil {
		userControlV2WriteError(c, err)
		return
	}
	if used {
		userControlV2WriteError(c, usercontrol.ErrPolicyInUse)
		return
	}
	if err := model.DB.Delete(&model.UserControlPool{}, id).Error; err != nil {
		userControlV2WriteError(c, err)
		return
	}
	model.BumpUserControlEpoch()
	usercontrol.Invalidate()
	auditUserControl(c, "pool.delete", model.AuditFields{"pool_id": id})
	common.ApiSuccess(c, gin.H{"id": id})
}

func ListUserControlApplications(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlRead) {
		return
	}
	rows, err := model.ListUserControlApplications()
	if err != nil {
		userControlV2WriteError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": rows})
}

// Restoring a legacy backup may relax limits; this explicit rollback is root-only.
func RestoreLegacyUserControlApplication(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		userControlFail(c, 403, "Unauthorized, insufficient privileges")
		return
	}
	kind := c.Param("kind")
	id, err := strconv.Atoi(c.Param("owner_id"))
	if err != nil || id <= 0 || (kind != "user" && kind != "list" && kind != "plan") {
		userControlFail(c, 400, "Invalid parameters")
		return
	}
	if kind == "user" {
		if err := usercontrol.CheckControlTarget(c.GetInt("id"), c.GetInt("role"), id); err != nil {
			userControlV2WriteError(c, err)
			return
		}
	}
	var payload struct {
		Version int64 `json:"version"`
	}
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil || payload.Version <= 0 {
		userControlFail(c, 400, "Invalid parameters")
		return
	}
	result := model.DB.Where("owner_kind = ? AND owner_id = ? AND version = ?", kind, id, payload.Version).Model(&model.UserControlApplication{}).Updates(map[string]any{"use_legacy": true, "targets": "[]", "mode": "merge", "expires_at": 0, "updated_by": c.GetInt("id"), "updated_at": common.GetTimestamp(), "version": payload.Version + 1})
	if result.Error != nil {
		userControlV2WriteError(c, result.Error)
		return
	}
	if result.RowsAffected != 1 {
		userControlFail(c, 409, "Configuration changed, reload before saving")
		return
	}
	model.BumpUserControlEpoch()
	usercontrol.Invalidate()
	auditUserControl(c, "application.restore_legacy", model.AuditFields{"owner_kind": kind, "owner_id": id, "version": payload.Version})
	common.ApiSuccess(c, gin.H{"restored": true})
}

type userControlApplicationPayload struct {
	Version       int64                     `json:"version"`
	Operation     string                    `json:"operation"`
	Targets       []model.UserControlTarget `json:"targets"`
	Mode          string                    `json:"mode"`
	ExpiresAt     int64                     `json:"expires_at"`
	Reason        string                    `json:"reason"`
	ConfirmLegacy bool                      `json:"confirm_legacy"`
}

func PutUserControlApplication(c *gin.Context) {
	kind := c.Param("kind")
	id, err := strconv.Atoi(c.Param("owner_id"))
	if err != nil || id <= 0 {
		userControlFail(c, 400, "Invalid parameters")
		return
	}
	switch kind {
	case "list":
		if !requireUserControlPermission(c, authz.UserControlListWrite) {
			return
		}
		if _, err := model.GetUserControlList(id); err != nil {
			userControlV2WriteError(c, err)
			return
		}
	case "plan":
		if !requireUserControlPermission(c, authz.UserControlPolicyWrite) {
			return
		}
		if _, err := model.GetSubscriptionPlanById(id); err != nil {
			userControlV2WriteError(c, err)
			return
		}
	case "user":
		if !requireUserControlPermission(c, authz.UserControlOverrideWrite) {
			return
		}
		if err := usercontrol.CheckControlTarget(c.GetInt("id"), c.GetInt("role"), id); err != nil {
			userControlV2WriteError(c, err)
			return
		}
	default:
		userControlFail(c, 400, "Invalid parameters")
		return
	}
	var payload userControlApplicationPayload
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil {
		userControlFail(c, 400, "Invalid parameters")
		return
	}
	var previous model.UserControlApplication
	lookup := model.DB.Where("owner_kind = ? AND owner_id = ?", kind, id).Find(&previous)
	if lookup.Error != nil {
		userControlV2WriteError(c, lookup.Error)
		return
	}
	if previous.Version != payload.Version {
		userControlFail(c, 409, "Application changed, reload before saving")
		return
	}
	if payload.Operation == "append" || payload.Operation == "remove" {
		var old []model.UserControlTarget
		if previous.Id > 0 {
			if err := common.UnmarshalJsonStr(previous.Targets, &old); err != nil {
				userControlV2WriteError(c, err)
				return
			}
		}
		if previous.Id == 0 || previous.UseLegacy {
			// Appending cannot silently suppress a legacy override.
			var legacy int64
			switch kind {
			case "user":
				lookup = model.DB.Model(&model.UserControlUserOverride{}).Where("user_id = ?", id).Count(&legacy)
			case "list":
				lookup = model.DB.Model(&model.UserControlList{}).Where("id = ? AND policy_id > 0", id).Count(&legacy)
			case "plan":
				lookup = model.DB.Model(&model.UserControlPlanBinding{}).Where("plan_id = ?", id).Count(&legacy)
			}
			if lookup.Error != nil {
				userControlV2WriteError(c, lookup.Error)
				return
			}
			if legacy > 0 {
				userControlFail(c, 409, "Migrate legacy settings before appending or removing bindings")
				return
			}
		}
		if payload.Operation == "append" {
			for _, target := range payload.Targets {
				found := false
				for _, value := range old {
					if value == target {
						found = true
					}
				}
				if !found {
					old = append(old, target)
				}
			}
		} else {
			kept := old[:0]
			for _, value := range old {
				remove := false
				for _, target := range payload.Targets {
					if value == target {
						remove = true
					}
				}
				if !remove {
					kept = append(kept, value)
				}
			}
			old = kept
		}
		payload.Targets = old
		if previous.Id > 0 && !previous.UseLegacy {
			payload.Mode = previous.Mode
			payload.ExpiresAt = previous.ExpiresAt
			payload.Reason = previous.Reason
		}
	} else if payload.Operation != "" && payload.Operation != "set" {
		userControlFail(c, 400, "Invalid parameters")
		return
	}
	if payload.Mode == "" {
		payload.Mode = "merge"
	}
	if payload.Mode != "merge" && payload.Mode != "replace" {
		userControlFail(c, 400, "Invalid parameters")
		return
	}
	if kind != "user" && (payload.Mode != "merge" || payload.ExpiresAt != 0) {
		userControlFail(c, 400, "Invalid parameters")
		return
	}
	if payload.Mode == "replace" {
		if !requireUserControlPermission(c, authz.UserControlOverrideRelax) {
			return
		}
		if strings.TrimSpace(payload.Reason) == "" || len(payload.Targets) == 0 {
			userControlFail(c, 400, "Replacement requires a reason and at least one target")
			return
		}
	}
	if payload.ExpiresAt < 0 || (payload.ExpiresAt > 0 && payload.ExpiresAt <= common.GetTimestamp()) || len(payload.Targets) > 64 {
		userControlFail(c, 400, "Invalid parameters")
		return
	}
	seen := map[string]bool{}
	for _, target := range payload.Targets {
		key := fmt.Sprintf("%s:%d", target.Kind, target.Id)
		if target.Id <= 0 || seen[key] {
			userControlFail(c, 400, "Invalid or duplicate application target")
			return
		}
		seen[key] = true
		switch target.Kind {
		case "policy":
			p, err := model.GetUserControlPolicy(target.Id)
			if err != nil || !usercontrol.BehaviorOnly(*p) {
				userControlFail(c, 400, "Select a behavior-only policy")
				return
			}
		case "pool":
			var row model.UserControlPool
			if err := model.DB.First(&row, target.Id).Error; err != nil {
				userControlV2WriteError(c, err)
				return
			}
		default:
			userControlFail(c, 400, "Invalid parameters")
			return
		}
	}
	// Legacy rows are retained but no longer executed once this owner switches.
	// Require explicit acknowledgement, including for an empty binding list.
	var count int64
	if err := model.DB.Model(&model.UserControlApplication{}).Where("owner_kind = ? AND owner_id = ?", kind, id).Count(&count).Error; err != nil {
		userControlV2WriteError(c, err)
		return
	}
	if (count == 0 || previous.UseLegacy) && !payload.ConfirmLegacy {
		var legacy int64
		var err error
		switch kind {
		case "user":
			err = model.DB.Model(&model.UserControlUserOverride{}).Where("user_id = ?", id).Count(&legacy).Error
		case "list":
			err = model.DB.Model(&model.UserControlList{}).Where("id = ? AND policy_id > 0", id).Count(&legacy).Error
		case "plan":
			err = model.DB.Model(&model.UserControlPlanBinding{}).Where("plan_id = ?", id).Count(&legacy).Error
		}
		if err != nil {
			userControlV2WriteError(c, err)
			return
		}
		if legacy > 0 {
			userControlFail(c, 409, "Review and confirm replacement of the legacy configuration")
			return
		}
	}
	raw, err := common.Marshal(payload.Targets)
	if err != nil {
		userControlV2WriteError(c, err)
		return
	}
	row := model.UserControlApplication{OwnerKind: kind, OwnerId: id, Targets: string(raw), Mode: payload.Mode, ExpiresAt: payload.ExpiresAt, Reason: strings.TrimSpace(payload.Reason), CreatedBy: c.GetInt("id"), UpdatedBy: c.GetInt("id")}
	if previous.Id > 0 {
		row.Id = previous.Id
		row.CreatedBy = previous.CreatedBy
		row.CreatedAt = previous.CreatedAt
	}
	if len(row.Reason) > 255 {
		userControlFail(c, 400, "Invalid parameters")
		return
	}
	if err := model.SaveUserControlApplicationVersion(&row, payload.Version); err != nil {
		userControlV2WriteError(c, err)
		return
	}
	usercontrol.Invalidate()
	auditUserControl(c, "application.save", model.AuditFields{"owner_kind": kind, "owner_id": id, "mode": row.Mode, "targets": payload.Targets, "expires_at": row.ExpiresAt, "reason": row.Reason, "legacy_confirmed": payload.ConfirmLegacy, "operation": payload.Operation})
	common.ApiSuccess(c, row)
}

// Resource discovery is local and metadata-only: never SELECT * on channels.
func GetUserControlResourceCatalog(c *gin.Context) {
	if !requireUserControlPermission(c, authz.UserControlRead) {
		return
	}
	type channel struct {
		Id     int    `json:"id"`
		Name   string `json:"name"`
		Type   int    `json:"type"`
		Status int    `json:"status"`
		Models string `json:"models"`
		Group  string `json:"group"`
	}
	rows := []channel{}
	if err := model.DB.Model(&model.Channel{}).Select("id", "name", "type", "status", "models", "group").Order("id ASC").Find(&rows).Error; err != nil {
		userControlV2WriteError(c, err)
		return
	}
	plans := []struct {
		Id    int    `json:"id"`
		Title string `json:"title"`
	}{}
	if err := model.DB.Model(&model.SubscriptionPlan{}).Select("id", "title").Order("id ASC").Find(&plans).Error; err != nil {
		userControlV2WriteError(c, err)
		return
	}
	models := []string{}
	if err := model.DB.Model(&model.Model{}).Order("model_name ASC").Pluck("model_name", &models).Error; err != nil {
		userControlV2WriteError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"channels": rows, "plans": plans, "models": models})
}

func userControlV2WriteError(c *gin.Context, err error) {
	if errors.Is(err, usercontrol.ErrInvalidInput) {
		userControlFail(c, 400, strings.TrimPrefix(err.Error(), usercontrol.ErrInvalidInput.Error()+": "))
		return
	}
	if errors.Is(err, gorm.ErrInvalidTransaction) {
		userControlFail(c, 409, "Configuration changed, reload before saving")
		return
	}
	userControlWriteError(c, err)
}

func migratedApplication(c *gin.Context, kind string, id int) bool {
	var count int64
	if err := model.DB.Model(&model.UserControlApplication{}).Where("owner_kind = ? AND owner_id = ? AND use_legacy = ?", kind, id, false).Count(&count).Error; err != nil {
		userControlV2WriteError(c, err)
		return true
	}
	if count > 0 {
		userControlFail(c, 409, "Use the application editor for migrated settings")
		return true
	}
	return false
}
