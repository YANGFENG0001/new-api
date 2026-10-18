package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/usercontrol"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserControlDistributorRoutesBothStagesAndChecksFinalChannelLimit(t *testing.T) {
	require.NoError(t, i18n.Init())
	previousDB := model.DB
	_, _, target := setupAdminUserTest(t)
	logConnection, err := model.LOG_DB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = logConnection.Close() })
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}))
	previousCache, previousRedis := common.MemoryCacheEnabled, common.RedisEnabled
	common.MemoryCacheEnabled, common.RedisEnabled = true, false
	t.Cleanup(func() {
		common.MemoryCacheEnabled, common.RedisEnabled = previousCache, previousRedis
		if previousCache && previousDB != nil && previousDB.Migrator().HasTable(&model.Channel{}) && previousDB.Migrator().HasTable(&model.Ability{}) {
			current := model.DB
			model.DB = previousDB
			model.InitChannelCache()
			model.DB = current
		}
	})
	for id, name := range map[int]string{7: "initial-model", 9: "final-model"} {
		priority, weight := int64(0), uint(100)
		require.NoError(t, model.DB.Create(&model.Channel{Id: id, Type: constant.ChannelTypeOpenAI, Key: "local-test-only", Status: common.ChannelStatusEnabled, Name: name, Models: name, Group: "default", Priority: &priority, Weight: &weight}).Error)
		require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: name, ChannelId: id, Enabled: true, Priority: &priority, Weight: weight}).Error)
	}
	model.InitChannelCache()
	ruleID := common.GetUUID()
	effective := &usercontrol.Effective{HasPolicy: true, Limits: usercontrol.Limits{ScopedRules: []usercontrol.ScopedRule{
		{ID: "pre-route", Models: []string{"client-model"}, Actions: &usercontrol.Actions{RouteModel: "initial-model"}},
		{ID: "channel-route", ChannelIDs: []int{7}, Models: []string{"client-model"}, Actions: &usercontrol.Actions{RouteModel: "final-model", Response: &usercontrol.StaticResponse{Mode: "text", Body: "local result"}}},
		{ID: ruleID, ChannelIDs: []int{9}, Models: []string{"client-model"}, Limits: usercontrol.Limits{Rate: &usercontrol.RateLimits{RPD: 1}}},
	}}}
	router := gin.New()
	router.Use(middleware.BodyStorageCleanup(), func(c *gin.Context) {
		c.Set("id", target.Id)
		usercontrol.SetContext(c, effective)
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"client-model": true, "initial-model": true, "final-model": true})
	}, middleware.Distribute())
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		assert.Equal(t, 9, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
		assert.Equal(t, "final-model", c.GetString("original_model"))
		Relay(c, types.RelayFormatOpenAI)
	})
	for _, expected := range []int{200, 429} {
		t.Run(fmt.Sprint(expected), func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"client-model","messages":[{"role":"user","content":"hello"}]}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept-Language", "zh-CN")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, expected, response.Code, response.Body.String())
			if expected == 200 {
				assert.Equal(t, "true", response.Header().Get("X-New-API-Simulated"))
				assert.Contains(t, response.Body.String(), "local result")
			} else {
				assert.Contains(t, response.Body.String(), "管控限额")
			}
		})
	}
}

func TestUserControlModelRoutingChecksBothOriginalAndTargetTokenAccess(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, allowed := range []string{"client-model", "target-model"} {
		t.Run(allowed, func(t *testing.T) {
			router := gin.New()
			router.Use(middleware.BodyStorageCleanup(), func(c *gin.Context) {
				common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
				common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{allowed: true})
				usercontrol.SetContext(c, &usercontrol.Effective{HasPolicy: true, Limits: usercontrol.Limits{ScopedRules: []usercontrol.ScopedRule{{ID: "route", Actions: &usercontrol.Actions{RouteModel: "target-model"}}}}})
			}, middleware.Distribute())
			called := false
			router.POST("/v1/chat/completions", func(c *gin.Context) { called = true; c.Status(200) })
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"client-model","messages":[{"role":"user","content":"hi"}]}`))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			assert.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
			assert.False(t, called)
		})
	}
}

func TestUserControlScopedPolicyStorageRuntimeAuditAndLogProjection(t *testing.T) {
	require.NoError(t, i18n.Init())
	_, identity, target := setupAdminUserTest(t)
	logConnection, err := model.LOG_DB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = logConnection.Close(); usercontrol.Invalidate() })
	require.NoError(t, model.DB.AutoMigrate(&model.UserControlPolicy{}, &model.UserControlList{}, &model.UserControlListMember{}, &model.UserControlUserOverride{}, &model.UserControlPlanBinding{}, &model.UserSubscription{}))
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.Log{}))
	limits := usercontrol.Limits{ScopedRules: []usercontrol.ScopedRule{{ID: "scope-runtime", Models: []string{"client-model"}, Limits: usercontrol.Limits{Rate: &usercontrol.RateLimits{RPM: 10}}, Actions: &usercontrol.Actions{
		RouteModel: "routed-model", PromptPrefix: "secret-prompt-not-in-audit", Response: &usercontrol.StaticResponse{Mode: "text", Body: "replacement-not-in-audit"},
	}}}}
	raw, err := common.Marshal(limits)
	require.NoError(t, err)
	policy, err := usercontrol.BuildPolicy(usercontrol.PolicyPayload{Name: "Scoped runtime", Limits: raw}, identity.UserID)
	require.NoError(t, err)
	require.NoError(t, model.CreateUserControlPolicy(policy))
	require.NoError(t, model.UpsertUserControlOverride(&model.UserControlUserOverride{UserId: target.Id, PolicyId: policy.Id, Mode: "merge"}))
	effective, err := usercontrol.ResolveAt(target.Id, target.Role, time.Now())
	require.NoError(t, err)
	require.Len(t, effective.Limits.ScopedRules, 1)
	assert.Equal(t, "routed-model", effective.Limits.ScopedRules[0].Actions.RouteModel)
	router := gin.New()
	router.Use(middleware.BodyStorageCleanup())
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("id", target.Id)
		c.Set("role", target.Role)
		c.Set("username", target.Username)
		usercontrol.SetContext(c, effective)
		defer usercontrol.FinishRequest(c)
		_, err := usercontrol.ApplyActions(c, "client-model", 0)
		require.NoError(t, err)
		require.NoError(t, usercontrol.AdmitChannel(c, "routed-model", 7))
		Relay(c, types.RelayFormatOpenAI)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"client-model","messages":[{"role":"user","content":"secret-client-body"}]}`))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	require.Equal(t, 200, response.Code, response.Body.String())
	assert.Equal(t, "true", response.Header().Get("X-New-API-Simulated"))
	assert.Contains(t, response.Body.String(), "replacement-not-in-audit")
	stored, err := model.GetUserById(target.Id, false)
	require.NoError(t, err)
	assert.Equal(t, target.Quota, stored.Quota)
	var count int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&count).Error)
	assert.Zero(t, count)
	filter := model.AuditLogFilter{UserId: target.Id, ActionPrefix: "user_control.runtime."}
	logs, total, err := model.GetAuditLogs(filter, 0, 20, common.RoleRootUser)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, logs, 1)
	assert.True(t, logs[0].Success)
	encoded, err := common.Marshal(logs)
	require.NoError(t, err)
	for _, secret := range []string{"secret-prompt-not-in-audit", "replacement-not-in-audit", "secret-client-body"} {
		assert.NotContains(t, string(encoded), secret)
	}
	assert.Equal(t, "client-model", logs[0].Other.AuditInfo.Params["original_model"])
	assert.Equal(t, "routed-model", logs[0].Other.AuditInfo.Params["actual_model"])
	assert.Equal(t, "true", logs[0].Other.AuditInfo.Params["simulated"])
	wsRouter := gin.New()
	wsRouter.GET("/v1/responses", func(c *gin.Context) { c.Set("id", target.Id); c.Set("role", target.Role) }, middleware.UserControl(), func(c *gin.Context) { c.Status(http.StatusSwitchingProtocols) })
	wsRequest := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	wsRequest.Header.Set("Upgrade", "websocket")
	wsResponse := httptest.NewRecorder()
	wsRouter.ServeHTTP(wsResponse, wsRequest)
	assert.Equal(t, http.StatusForbidden, wsResponse.Code)
	filter.SelfView = true
	logs, total, err = model.GetAuditLogs(filter, 0, 20, common.RoleRootUser)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, logs)
	filter.SelfView = false
	logs, total, err = model.GetAuditLogs(filter, 0, 20, common.RoleCommonUser)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, logs)
	// Store accurate execution and accounting; only the self-view projects the
	// client model. No quota, token count, error or success field is rewritten.
	other := model.NewLogOther()
	other.SetAdmin("user_control_original_model", "client-model")
	entry := model.Log{UserId: target.Id, Username: target.Username, CreatedAt: time.Now().Unix(), Type: model.LogTypeConsume, ModelName: "routed-model", PromptTokens: 13, CompletionTokens: 9, Quota: 42, Other: other.JSONString()}
	require.NoError(t, model.LOG_DB.Create(&entry).Error)
	self, total, err := model.GetUserLogs(target.Id, model.LogFilter{}, 0, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, self, 1)
	assert.Equal(t, "client-model", self[0].ModelName)
	assert.Equal(t, 42, self[0].Quota)
	assert.Equal(t, 13, self[0].PromptTokens)
	assert.Equal(t, 9, self[0].CompletionTokens)
	assert.NotContains(t, self[0].Other, "user_control_original_model")
	var actual model.Log
	require.NoError(t, model.LOG_DB.First(&actual, entry.Id).Error)
	assert.Equal(t, "routed-model", actual.ModelName)
}

func TestUserControlRewriteCannotBypassTokenValidationOrFalsifyFailure(t *testing.T) {
	for _, test := range []struct {
		name    string
		actions usercontrol.Actions
		want    int
	}{
		{"oversized-token-budget", usercontrol.Actions{RequestBody: `{"max_tokens":18446744073709551615}`, Response: &usercontrol.StaticResponse{Mode: "text", Body: "not returned"}}, 400},
		{"custom-error", usercontrol.Actions{Response: &usercontrol.StaticResponse{Mode: "error", Status: 503, ErrorCode: "maintenance", Body: "maintenance"}}, 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"client-model","messages":[{"role":"user","content":"hello"}]}`))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			usercontrol.SetContext(c, &usercontrol.Effective{HasPolicy: true, Limits: usercontrol.Limits{ScopedRules: []usercontrol.ScopedRule{{ID: "rewrite", Actions: &test.actions}}}})
			_, err := usercontrol.ApplyActions(c, "client-model", 0)
			require.NoError(t, err)
			Relay(c, types.RelayFormatOpenAI)
			assert.Equal(t, test.want, response.Code, response.Body.String())
			if test.want == 400 {
				assert.True(t, c.GetBool("user_control_runtime_failed"))
				assert.Empty(t, response.Header().Get("X-New-API-Simulated"))
			}
		})
	}
}
