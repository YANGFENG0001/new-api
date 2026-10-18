package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/usercontrol"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUserControlV2SchedulingBindingsAndMigration(t *testing.T) {
	operator, _, target := setupAdminUserTest(t)
	conn, err := model.LOG_DB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(); usercontrol.Invalidate() })
	entities := []any{&model.UserControlPolicy{}, &model.UserControlPool{}, &model.UserControlApplication{}, &model.UserControlList{}, &model.UserControlListMember{}, &model.UserControlPlanBinding{}, &model.UserControlUserOverride{}, &model.UserSubscription{}, &model.SubscriptionPlan{}}
	// A representative pre-v2 policy table has no schema_version column.
	legacySchema := struct {
		Id          int    `gorm:"primaryKey"`
		Name        string `gorm:"type:varchar(64);uniqueIndex;not null"`
		Description string `gorm:"type:varchar(255)"`
		Enabled     bool   `gorm:"default:true;index"`
		Priority    int    `gorm:"default:0"`
		Limits      string `gorm:"type:text"`
		Schedule    string `gorm:"type:text"`
		Version     int64  `gorm:"bigint;default:1"`
		CreatedBy   int
		UpdatedBy   int
		CreatedAt   int64 `gorm:"bigint"`
		UpdatedAt   int64 `gorm:"bigint"`
	}{Name: "upgrade-backup", Enabled: true, Limits: `{"rate":{"rpm":17}}`}
	require.NoError(t, model.DB.Table("user_control_policies").AutoMigrate(&legacySchema))
	require.NoError(t, model.DB.Table("user_control_policies").Create(&legacySchema).Error)
	require.NoError(t, model.DB.AutoMigrate(entities...))
	var upgraded model.UserControlPolicy
	require.NoError(t, model.DB.First(&upgraded, legacySchema.Id).Error)
	require.Equal(t, 1, upgraded.SchemaVersion)
	require.Equal(t, legacySchema.Limits, upgraded.Limits)
	// Exercise upgrade/idempotency as well as fresh schema creation.
	require.NoError(t, model.DB.AutoMigrate(entities...))
	raw := []byte(`{"scoped_rules":[{"id":"shared","limits":{"rate":{"rpm":4}},"actions":{"prompt_prefix":"once"}}]}`)
	p, err := usercontrol.BuildPolicy(usercontrol.PolicyPayload{Name: "pure", SchemaVersion: 2, Limits: raw}, operator.Id)
	require.NoError(t, err)
	require.NoError(t, model.CreateUserControlPolicy(p))
	_, err = usercontrol.BuildPolicy(usercontrol.PolicyPayload{Name: "bad", SchemaVersion: 2, Schedule: []byte(`{"timezone":"UTC","rules":[]}`)}, operator.Id)
	require.Error(t, err)
	cfg := usercontrol.PoolConfig{PoolTiming: usercontrol.PoolTiming{Probability: 10000, HoldMin: 10, HoldMax: 10}, Members: []usercontrol.PoolMember{{PolicyID: p.Id, Probability: 10000}}}
	pool, err := usercontrol.BuildPool(usercontrol.PoolPayload{Name: "night", Enabled: true, Config: cfg}, nil, operator.Id)
	require.NoError(t, err)
	require.NoError(t, model.SaveUserControlPool(pool))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", operator.Id); c.Set("role", common.RoleRootUser); c.Next() })
	router.PUT("/applications/:kind/:owner_id", PutUserControlApplication)
	put := func(kind string, id int, body string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/applications/%s/%d", kind, id), strings.NewReader(body))
		router.ServeHTTP(r, req)
		return r
	}
	list := &model.UserControlList{Name: "test-list", PolicyId: p.Id, Enabled: true}
	require.NoError(t, model.CreateUserControlList(list))
	_, err = model.AddUserControlListMembers(list.Id, []int{target.Id}, "", operator.Id)
	require.NoError(t, err)
	body := fmt.Sprintf(`{"targets":[{"kind":"pool","id":%d}],"mode":"merge","version":0}`, pool.Id)
	require.Equal(t, 409, put("list", list.Id, body).Code, "legacy replacement must be acknowledged")
	body = strings.TrimSuffix(body, "}") + `,"confirm_legacy":true}`
	require.Equal(t, 200, put("list", list.Id, body).Code)
	require.Equal(t, 409, put("list", list.Id, body).Code, "stale retries must not overwrite a newer configuration")
	now := time.Unix(pool.CreatedAt, 0).Truncate(time.Minute).Add(2 * time.Minute)
	first, err := usercontrol.ResolveAt(target.Id, target.Role, now)
	require.NoError(t, err)
	require.Len(t, first.Limits.ScopedRules, 1)
	same, err := usercontrol.ResolveAt(target.Id, target.Role, now.Add(6*time.Minute))
	require.NoError(t, err)
	require.Equal(t, first.ScheduleHits, same.ScheduleHits)
	require.Equal(t, 200, put("user", target.Id, fmt.Sprintf(`{"targets":[{"kind":"policy","id":%d}],"mode":"merge","version":0}`, p.Id)).Code)
	both, err := usercontrol.ResolveAt(target.Id, target.Role, now)
	require.NoError(t, err)
	require.Len(t, both.Limits.ScopedRules, 1, "same policy across pool and direct binding must execute once")
	require.Equal(t, first.Limits.ScopedRules[0].Bucket(target.Id), both.Limits.ScopedRules[0].Bucket(target.Id), "switching sources must not reset scoped quotas")
	require.Equal(t, 400, put("user", target.Id, fmt.Sprintf(`{"targets":[{"kind":"policy","id":%d}],"mode":"replace","version":1}`, p.Id)).Code, "replacement requires a reason")
	require.Equal(t, 200, put("user", target.Id, `{"targets":[],"mode":"merge","version":1}`).Code)
	current, err := model.GetUserControlList(list.Id)
	require.NoError(t, err)
	require.Equal(t, p.Id, current.PolicyId, "original legacy record must remain available for rollback")
	// Time outside pool windows must not inherit old wrapper behavior.
	cfg.Schedule = &usercontrol.Schedule{Timezone: "UTC", Rules: []usercontrol.ScheduleRule{{ID: "short", Start: "01:00", End: "02:00"}}}
	updated, err := usercontrol.BuildPool(usercontrol.PoolPayload{Name: pool.Name, Enabled: true, Version: pool.Version, Config: cfg}, pool, operator.Id)
	require.NoError(t, err)
	require.NoError(t, model.SaveUserControlPool(updated))
	outside, err := usercontrol.ResolveAt(target.Id, target.Role, time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Empty(t, outside.Limits.ScopedRules)
	require.Contains(t, strings.Join(outside.ScheduleHits, " "), "outside_window")
	// Disabling a member does not transfer its probability to another member.
	p.Enabled = false
	require.NoError(t, model.UpdateUserControlPolicy(p))
	disabled, err := usercontrol.ResolveAt(target.Id, target.Role, time.Date(2026, 10, 18, 1, 30, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Empty(t, disabled.Limits.ScopedRules)
	require.Contains(t, strings.Join(disabled.ScheduleHits, " "), "disabled")
	router.DELETE("/applications/:kind/:owner_id", RestoreLegacyUserControlApplication)
	rollback := httptest.NewRecorder()
	router.ServeHTTP(rollback, httptest.NewRequest("DELETE", fmt.Sprintf("/applications/list/%d", list.Id), strings.NewReader(`{"version":1}`)))
	require.Equal(t, 200, rollback.Code)
	var restored model.UserControlApplication
	require.NoError(t, model.DB.Where("owner_kind = ? AND owner_id = ?", "list", list.Id).First(&restored).Error)
	require.True(t, restored.UseLegacy)
	require.Equal(t, int64(2), restored.Version)
	require.Equal(t, 409, put("list", list.Id, body).Code, "rollback must not permit an ABA stale-version write")
	p.Enabled = true
	require.NoError(t, model.UpdateUserControlPolicy(p))
	rollbackEffective, err := usercontrol.ResolveAt(target.Id, target.Role, now)
	require.NoError(t, err)
	require.Len(t, rollbackEffective.Limits.ScopedRules, 1, "rollback re-enables the legacy binding")
	// New inherited applications must still precede a legacy personal replacement.
	require.NoError(t, model.SaveUserControlApplicationVersion(&model.UserControlApplication{OwnerKind: "list", OwnerId: list.Id, Targets: fmt.Sprintf(`[{"kind":"policy","id":%d}]`, p.Id), Mode: "merge"}, restored.Version))
	require.NoError(t, model.UpsertUserControlOverride(&model.UserControlUserOverride{UserId: target.Id, Mode: "replace", Limits: `{"rate":{"rpm":9}}`}))
	require.NoError(t, model.DB.Model(&model.UserControlApplication{}).Where("owner_kind = ? AND owner_id = ?", "user", target.Id).Update("use_legacy", true).Error)
	legacyReplace, err := usercontrol.ResolveAt(target.Id, target.Role, now)
	require.NoError(t, err)
	require.Empty(t, legacyReplace.Limits.ScopedRules)
	require.Equal(t, 9, legacyReplace.Rate().RPM)
	// An expired v2 replacement restores inheritance, not the obsolete override.
	personal := model.UserControlApplication{OwnerKind: "user", OwnerId: target.Id, Targets: fmt.Sprintf(`[{"kind":"policy","id":%d}]`, p.Id), Mode: "replace", Reason: "temporary", ExpiresAt: now.Add(-time.Minute).Unix()}
	require.NoError(t, model.SaveUserControlApplicationVersion(&personal, 2))
	expired, err := usercontrol.ResolveAt(target.Id, target.Role, now)
	require.NoError(t, err)
	require.Len(t, expired.Limits.ScopedRules, 1)
	require.Zero(t, expired.Rate().RPM)

}

func TestUserControlV2PoolProbabilityValidationAndReferences(t *testing.T) {
	operator, _, target := setupAdminUserTest(t)
	conn, err := model.LOG_DB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(); usercontrol.Invalidate() })
	require.NoError(t, model.DB.AutoMigrate(&model.UserControlPolicy{}, &model.UserControlPool{}, &model.UserControlApplication{}, &model.UserControlList{}, &model.UserControlListMember{}, &model.UserControlUserOverride{}, &model.UserControlPlanBinding{}, &model.UserSubscription{}, &model.Channel{}, &model.Model{}, &model.SubscriptionPlan{}))
	disabledFlag := false
	disabledPolicy, err := usercontrol.BuildPolicy(usercontrol.PolicyPayload{Name: "disabled-on-create", SchemaVersion: 2, Enabled: &disabledFlag}, operator.Id)
	require.NoError(t, err)
	require.NoError(t, model.CreateUserControlPolicy(disabledPolicy))
	storedDisabled, err := model.GetUserControlPolicy(disabledPolicy.Id)
	require.NoError(t, err)
	require.False(t, storedDisabled.Enabled, "explicit disabled must not be replaced by an ORM default")
	p := &model.UserControlPolicy{Name: "behavior", Enabled: true, Limits: `{"rate":{"rpm":3}}`}
	require.NoError(t, model.CreateUserControlPolicy(p))
	cfg := usercontrol.PoolConfig{PoolTiming: usercontrol.PoolTiming{Probability: 0, HoldMin: 10, HoldMax: 10}, Members: []usercontrol.PoolMember{{PolicyID: p.Id, Probability: 9999}}}
	require.Error(t, usercontrol.ValidatePoolConfig(&cfg))
	cfg.Members[0].Probability = 10000
	require.NoError(t, usercontrol.ValidatePoolConfig(&cfg))
	pool, err := usercontrol.BuildPool(usercontrol.PoolPayload{Name: "zero", Enabled: true, Config: cfg}, nil, operator.Id)
	require.NoError(t, err)
	require.NoError(t, model.SaveUserControlPool(pool))
	app := &model.UserControlApplication{OwnerKind: "user", OwnerId: target.Id, Targets: fmt.Sprintf(`[{"kind":"pool","id":%d}]`, pool.Id), Mode: "merge"}
	require.NoError(t, model.SaveUserControlApplicationVersion(app, 0))
	miss, err := usercontrol.ResolveAt(target.Id, target.Role, time.Now())
	require.NoError(t, err)
	require.Zero(t, miss.Rate().RPM)
	require.Contains(t, strings.Join(miss.ScheduleHits, " "), ":miss")
	cfg.Probability = 10000
	pool, err = usercontrol.BuildPool(usercontrol.PoolPayload{Name: pool.Name, Enabled: true, Version: pool.Version, Config: cfg}, pool, operator.Id)
	require.NoError(t, err)
	require.NoError(t, model.SaveUserControlPool(pool))
	hit, err := usercontrol.ResolveAt(target.Id, target.Role, time.Now())
	require.NoError(t, err)
	require.Equal(t, 3, hit.Rate().RPM)
	used, err := usercontrol.ApplicationReferences("policy", p.Id)
	require.NoError(t, err)
	require.True(t, used)
	// Protected targets cannot be modified even by the root operator itself.
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", operator.Id); c.Set("role", common.RoleRootUser); c.Next() })
	router.PUT("/applications/:kind/:owner_id", PutUserControlApplication)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("PUT", fmt.Sprintf("/applications/user/%d", operator.Id), strings.NewReader(`{"targets":[]}`)))
	require.Equal(t, 400, rr.Code)
	// Read-only delegated users must not receive channel credentials.
	secretChannel := model.Channel{Name: "catalog", Key: "must-not-leak", Models: "model-a,model-b", Group: "default", Type: 1, Status: 1}
	require.NoError(t, model.DB.Create(&secretChannel).Error)
	router.GET("/catalog", GetUserControlResourceCatalog)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/catalog", nil))
	require.Equal(t, 200, rr.Code)
	require.Contains(t, rr.Body.String(), "catalog")
	require.NotContains(t, rr.Body.String(), "must-not-leak")
	require.NotContains(t, rr.Body.String(), `"key"`)
	denied := gin.New()
	denied.Use(func(c *gin.Context) { c.Set("id", target.Id); c.Set("role", common.RoleCommonUser); c.Next() })
	denied.PUT("/applications/:kind/:owner_id", PutUserControlApplication)
	denied.DELETE("/applications/:kind/:owner_id", RestoreLegacyUserControlApplication)
	for _, method := range []string{"PUT", "DELETE"} {
		r := httptest.NewRecorder()
		denied.ServeHTTP(r, httptest.NewRequest(method, fmt.Sprintf("/applications/user/%d", target.Id), strings.NewReader(`{"targets":[]}`)))
		require.Equal(t, 403, r.Code)
	}
}
