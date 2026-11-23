package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSubscriptionPoolTest(t *testing.T) (*model.User, model.SubscriptionPlan, int64) {
	t.Helper()
	user, _ := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}, &model.Channel{}))
	// Representative upgrade: existing legacy subscriptions must survive both
	// migrations; the feature adds sidecars, never rewrites their schema or data.
	now := time.Now().Unix()
	plan := model.SubscriptionPlan{Title: "legacy", Enabled: true, DurationUnit: "day", DurationValue: 30, TotalAmount: 1000}
	require.NoError(t, model.DB.Create(&plan).Error)
	legacy := model.UserSubscription{UserId: user.Id, PlanId: plan.Id, EndTime: now + 86400, Status: "active", AmountTotal: 1000, AmountUsed: 123}
	require.NoError(t, model.DB.Create(&legacy).Error)
	require.NoError(t, model.MigrateSubscriptionPools())
	require.NoError(t, model.MigrateSubscriptionPools())
	var retained model.UserSubscription
	require.NoError(t, model.DB.First(&retained, legacy.Id).Error)
	assert.Equal(t, int64(123), retained.AmountUsed)
	require.NoError(t, model.DB.Delete(&legacy).Error)
	return user, plan, now
}

func TestSubscriptionPoolSharedChannelsSelectMemberLedgerAndPinRetries(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	for _, id := range []int{9601, 9602, 9603} {
		require.NoError(t, model.DB.Create(&model.Channel{Id: id, Name: "shared-route", ChannelInfo: model.ChannelInfo{IsMultiKey: id == 9601}}).Error)
	}
	rules := []model.SubscriptionCycleRule{{Key: "c", Name: "quota", Seconds: 3600, NextReset: now + 3500, Limit: 100, Shared: true, Enabled: true}}
	a, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "account-a", Enabled: true, EndTime: now + 86400}, Channels: []int{9601, 9602}, Cycles: rules}, 1)
	require.NoError(t, err, "channel key rotation is not a subscription accounting identity")
	b, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "account-b", Enabled: true, EndTime: now + 86400}, Channels: []int{9601, 9603}, Cycles: rules}, 1)
	require.NoError(t, err, "one route may serve members of different pools")
	subs := []*model.UserSubscription{}
	for index, pool := range []*model.SubscriptionPool{a, b} {
		require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id, Version: int64(index)}, nil))
		require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
			sub, err := model.CreateUserSubscriptionFromPlanTx(tx, user.Id+index, &plan, "admin")
			subs = append(subs, sub)
			return err
		}))
	}
	for index, retryChannel := range []int{9602, 9603} {
		requestID := fmt.Sprintf("shared-route-%d", index)
		blocked, err := model.SubscriptionBlockedChannels(user.Id+index, false)
		require.NoError(t, err)
		assert.NotContains(t, blocked, 9601, "a second pool must not veto an eligible membership")
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		info := &relaycommon.RelayInfo{UserId: user.Id + index, RequestId: requestID, IsPlayground: true, ForcePreConsume: true, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 9601}, UserSetting: dto.UserSetting{BillingPreference: "subscription_only"}}
		require.Nil(t, service.PreConsumeBilling(ctx, 10, info))
		assert.Equal(t, subs[index].Id, info.SubscriptionId)
		filters := service.GetChannelConstraints(ctx).Filters
		for _, id := range []int{9601, retryChannel} {
			allowed, _ := model.ChannelSatisfiesFilters(&model.Channel{Id: id}, "m", filters)
			assert.True(t, allowed)
		}
		wrongChannel := 9605 - (retryChannel - 9600)
		allowed, _ := model.ChannelSatisfiesFilters(&model.Channel{Id: wrongChannel}, "m", filters)
		assert.False(t, allowed)
		require.Error(t, model.ValidateSubscriptionRetry(requestID, wrongChannel, false))
		info.ChannelId = retryChannel
		require.Nil(t, service.PreConsumeBilling(ctx, 20, info))
		replay, err := model.PreConsumeUserSubscriptionForChannel(requestID, user.Id+index, "m", 0, 20, 9601)
		require.NoError(t, err)
		assert.Equal(t, subs[index].Id, replay.UserSubscriptionId)
		require.NoError(t, info.Billing.Settle(25))
	}
	for _, pool := range []*model.SubscriptionPool{a, b} {
		stats, err := model.GetSubscriptionPoolStatistics(pool.Id)
		require.NoError(t, err)
		require.Len(t, stats, 1)
		assert.Equal(t, int64(25), stats[0].Used)
		assert.Zero(t, stats[0].Reserved)
	}
	blocked, err := model.SubscriptionBlockedChannels(user.Id+99, false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int{9601, 9602, 9603}, blocked)
	blocked, err = model.SubscriptionBlockedChannels(user.Id, true)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int{9601, 9602, 9603}, blocked)
	require.Error(t, model.ValidateSubscriptionRetry("", 9601, true))
	_, err = model.PreConsumeUserSubscriptionForChannel("wrong-member-route", user.Id, "m", 0, 1, 9603)
	require.Error(t, err)
	require.Error(t, model.DB.Create(&model.SubscriptionPoolChannel{PoolID: a.Id, ChannelID: 9601}).Error)
	_, err = model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: *a, Channels: []int{9602, 9604}, Cycles: rules}, 1)
	require.ErrorContains(t, err, "Channel does not exist")
	aConfig, err := model.GetSubscriptionPoolConfiguration(a.Id)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int{9601, 9602}, aConfig.Channels, "a failed edit must preserve the original relations")
	_, err = model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: *a, Channels: []int{9602}, Cycles: rules}, 1)
	require.NoError(t, err)
	require.Error(t, model.ValidateSubscriptionRetry("shared-route-0", 9601, false))
	require.NoError(t, model.ValidateSubscriptionRetry("shared-route-1", 9601, false))
	bConfig, err := model.GetSubscriptionPoolConfiguration(b.Id)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int{9601, 9603}, bConfig.Channels, "editing one pool must preserve another pool's links")
}

func TestSubscriptionPoolChannelMigrationPreservesLinksAndPairUniqueness(t *testing.T) {
	setupSubscriptionPoolTest(t)
	require.NoError(t, model.DB.Migrator().DropTable(&model.SubscriptionPoolChannel{}))
	legacy := struct {
		ChannelID int `gorm:"primaryKey;autoIncrement:false"`
		PoolID    int `gorm:"index"`
	}{}
	require.NoError(t, model.DB.Table("subscription_pool_channels").Migrator().CreateTable(&legacy))
	require.NoError(t, model.DB.Table("subscription_pool_channels").Create(map[string]any{"channel_id": 9701, "pool_id": 1}).Error)
	results := make(chan error, 2)
	var migrations sync.WaitGroup
	for range 2 {
		migrations.Go(func() { results <- model.MigrateSubscriptionPools() })
	}
	migrations.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err, "concurrent startup must serialize the key migration")
	}
	var links []model.SubscriptionPoolChannel
	require.NoError(t, model.DB.Find(&links).Error)
	assert.Equal(t, []model.SubscriptionPoolChannel{{ChannelID: 9701, PoolID: 1}}, links)
	require.NoError(t, model.DB.Create(&model.SubscriptionPoolChannel{ChannelID: 9701, PoolID: 2}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionPoolChannel{ChannelID: 9702, PoolID: 1}).Error)
	require.Error(t, model.DB.Create(&model.SubscriptionPoolChannel{ChannelID: 9701, PoolID: 1}).Error)
	require.NoError(t, model.DB.Where("pool_id = ? AND channel_id = ?", 1, 9701).Delete(&model.SubscriptionPoolChannel{}).Error)
	for range 2 {
		require.NoError(t, model.MigrateSubscriptionPools())
	}
	require.NoError(t, model.DB.Find(&links).Error)
	assert.ElementsMatch(t, []model.SubscriptionPoolChannel{{ChannelID: 9701, PoolID: 2}, {ChannelID: 9702, PoolID: 1}}, links, "restarts must not resurrect deleted links")
}

func TestSubscriptionPoolChannelEditAndAdmissionKeepAccountingIsolated(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9703, Name: "editable-route"}).Error)
	rules := []model.SubscriptionCycleRule{{Key: "c", Name: "quota", Seconds: 3600, NextReset: now + 3500, Limit: 100, Shared: true, Enabled: true}}
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "changing", Enabled: true, EndTime: now + 86400}, Channels: []int{9703}, Cycles: rules}, 1)
	require.NoError(t, err)
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id}, nil))
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		_, err := model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
		return err
	}))
	start := make(chan struct{})
	var admitted *model.SubscriptionPreConsumeResult
	var admissionErr, editErr error
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		admitted, admissionErr = model.PreConsumeUserSubscriptionForChannel("relation-edit", user.Id, "m", 0, 10, 9703)
	})
	workers.Go(func() {
		<-start
		_, editErr = model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: *pool, Channels: []int{}, Cycles: rules}, 1)
	})
	close(start)
	workers.Wait()
	require.NoError(t, editErr)
	if admissionErr == nil {
		require.NotNil(t, admitted)
		var reservation model.SubscriptionCycleReservation
		require.NoError(t, model.DB.Where("request_id = ?", "relation-edit").First(&reservation).Error)
		assert.Equal(t, pool.Id, reservation.PoolID)
		require.NoError(t, model.RefundSubscriptionPreConsume("relation-edit"))
	} else {
		require.ErrorContains(t, admissionErr, "subscription quota insufficient")
	}
	_, err = model.PreConsumeUserSubscriptionForChannel("relation-removed", user.Id, "m", 0, 10, 9703)
	require.ErrorContains(t, err, "pool has no bound channels")
	stats, err := model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	assert.Zero(t, stats[0].Used)
	assert.Zero(t, stats[0].Reserved)
}

func TestSubscriptionPoolUnifiedSaveIsAtomicAndHonorsFollowing(t *testing.T) {
	_, _, now := setupSubscriptionPoolTest(t)
	mapping := model.PanelSubscriptionSyncMapping()
	source, err := service.SaveSubscriptionSyncSource(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "unified", URL: "https://example.com/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10}, Mapping: mapping}, 1, true)
	require.NoError(t, err)
	input := model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "unified", Enabled: true, EndTime: now + 86400}, Cycles: []model.SubscriptionCycleRule{{Key: "5h", Name: "5h", Seconds: 18000, NextReset: now + 1000, Limit: 100, Enabled: true}}}
	pool, err := model.SaveSubscriptionPool(input, 1)
	require.NoError(t, err)
	binding := model.SubscriptionSyncBindingInput{SubscriptionSyncBinding: model.SubscriptionSyncBinding{PoolID: pool.Id, SourceID: source.Id, AccountID: "a", Enabled: true, SyncExpiry: true, StalePolicy: "stop"}, Cycles: []model.SubscriptionSyncCycleBinding{{LocalKey: "5h", UpstreamKey: "5h", AlignReset: true, Threshold: 1, MaxShiftSeconds: 300}}}
	require.NoError(t, model.SaveSubscriptionSyncBinding(binding, 1))
	binding, err = model.GetSubscriptionSyncBinding(pool.Id)
	require.NoError(t, err)
	input.SubscriptionPool = *pool
	input.SyncBinding = &binding
	input.Name = "edited"
	pool, err = model.SaveSubscriptionPool(input, 1)
	require.NoError(t, err)
	unchanged, err := model.GetSubscriptionSyncBinding(pool.Id)
	require.NoError(t, err)
	assert.Equal(t, binding.Version, unchanged.Version, "unrelated saves cannot invalidate upstream observations")
	input.SubscriptionPool = *pool
	input.Name = "must-rollback"
	input.SyncBinding.Version--
	_, err = model.SaveSubscriptionPool(input, 1)
	require.ErrorContains(t, err, "reload before saving")
	retained, err := model.GetSubscriptionPoolConfiguration(pool.Id)
	require.NoError(t, err)
	assert.Equal(t, "edited", retained.Name)
	assert.Equal(t, pool.Version, retained.Version)
	input.SyncBinding.Version++
	input.EndTime++
	_, err = model.SaveSubscriptionPool(input, 1)
	require.ErrorContains(t, err, "Turn off upstream following")
	input.EndTime--
	input.Cycles[0].NextReset++
	_, err = model.SaveSubscriptionPool(input, 1)
	require.ErrorContains(t, err, "Turn off upstream following")
	input.SyncBinding.SyncExpiry = false
	input.SyncBinding.Cycles[0].AlignReset = false
	input.EndTime++
	pool, err = model.SaveSubscriptionPool(input, 1)
	require.NoError(t, err, "one atomic save can disable following and change local times")
	retained, err = model.GetSubscriptionPoolConfiguration(pool.Id)
	require.NoError(t, err)
	assert.Equal(t, input.EndTime, retained.EndTime)
	assert.Equal(t, input.Cycles, retained.Cycles)

	// Detail records are pool-scoped, not a leak of other pools' diagnostics.
	require.NoError(t, model.DB.Create(&model.SubscriptionSyncEvent{SourceID: source.Id, PoolID: pool.Id + 100, Message: "unrelated-private-message", CreatedAt: now}).Error)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 1)
	c.Set("role", common.RoleRootUser)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(pool.Id)}}
	GetSubscriptionPoolDetail(c)
	assert.Contains(t, w.Body.String(), `"sync_events":[{`)
	assert.NotContains(t, w.Body.String(), "unrelated-private-message")

	// Cached editor values may straddle a natural reset without requesting a
	// manual time override. Following still advances on the original cadence.
	elapsedInput := model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "elapsed-editor", Enabled: true, EndTime: now + 86400}, Cycles: []model.SubscriptionCycleRule{{Key: "5h", Name: "5h", Seconds: 18000, NextReset: now - 1, Limit: 100, Enabled: true}}}
	elapsed, err := model.SaveSubscriptionPool(elapsedInput, 1)
	require.NoError(t, err)
	elapsedBinding := model.SubscriptionSyncBindingInput{SubscriptionSyncBinding: model.SubscriptionSyncBinding{PoolID: elapsed.Id, SourceID: source.Id, AccountID: "b", Enabled: true, StalePolicy: "stop"}, Cycles: []model.SubscriptionSyncCycleBinding{{LocalKey: "5h", UpstreamKey: "5h", AlignReset: true, Threshold: 1, MaxShiftSeconds: 300}}}
	require.NoError(t, model.SaveSubscriptionSyncBinding(elapsedBinding, 1))
	elapsedInput.SubscriptionPool = *elapsed
	elapsedInput.Note = "not a time edit"
	_, err = model.SaveSubscriptionPool(elapsedInput, 1)
	require.NoError(t, err, "saving after a natural reset must preserve following")
	elapsedConfig, err := model.GetSubscriptionPoolConfiguration(elapsed.Id)
	require.NoError(t, err)
	assert.Equal(t, now-1+18000, elapsedConfig.Cycles[0].NextReset)
}

func TestSubscriptionPoolPlanCatalogDescribesManagedPlansWithoutSecrets(t *testing.T) {
	_, plan, now := setupSubscriptionPoolTest(t)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "private-pool-name", Note: "private-admin-note", ExternalID: "private-account", Enabled: true, EndTime: now + 86400}, Cycles: []model.SubscriptionCycleRule{{Key: "short", Name: "Short", Seconds: 137, NextReset: now + 137, Limit: 123, Shared: true, Enabled: true}}}, 1)
	require.NoError(t, err)
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id}, nil))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	AdminListSubscriptionPlans(c)
	var body struct {
		Success bool `json:"success"`
		Data    []struct {
			Allocation struct {
				Mode         string `json:"mode"`
				Available    bool   `json:"available"`
				FixedEndTime int64  `json:"fixed_end_time"`
				Cycles       []struct {
					Name   string `json:"name"`
					Shared bool   `json:"shared"`
					Limit  int64  `json:"limit"`
				} `json:"cycles"`
			} `json:"allocation"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Success)
	require.Len(t, body.Data, 1)
	assert.Equal(t, "fixed", body.Data[0].Allocation.Mode)
	assert.True(t, body.Data[0].Allocation.Available)
	assert.Equal(t, now+86400, body.Data[0].Allocation.FixedEndTime)
	require.Len(t, body.Data[0].Allocation.Cycles, 1)
	assert.True(t, body.Data[0].Allocation.Cycles[0].Shared)
	assert.EqualValues(t, 123, body.Data[0].Allocation.Cycles[0].Limit)
	for _, secret := range []string{"private-pool-name", "private-admin-note", "private-account", "pool_id", "channels"} {
		assert.NotContains(t, w.Body.String(), secret)
	}
	configured, err := model.GetSubscriptionPoolConfiguration(pool.Id)
	require.NoError(t, err)
	configured.Cycles[0].Limit = 456
	_, err = model.SaveSubscriptionPool(configured, 1)
	require.NoError(t, err)
	current, err := model.GetSubscriptionPlanAllocationSummaries([]int{plan.Id})
	require.NoError(t, err)
	require.Len(t, current[plan.Id].Cycles, 1)
	assert.EqualValues(t, 123, current[plan.Id].Cycles[0].Limit, "purchase terms must describe currently enforced quota, not next-window settings")
	var state model.SubscriptionCycleState
	require.NoError(t, model.DB.First(&state, "owner = ? AND cycle_key = ?", fmt.Sprintf("pool:%d", pool.Id), "short").Error)
	require.NotEmpty(t, state.PendingRule)
	require.NoError(t, model.DB.Model(&state).Update("end_time", now-1).Error)
	current, err = model.GetSubscriptionPlanAllocationSummaries([]int{plan.Id})
	require.NoError(t, err)
	assert.EqualValues(t, 456, current[plan.Id].Cycles[0].Limit)
	var unchanged model.SubscriptionCycleState
	require.NoError(t, model.DB.First(&unchanged, state.Id).Error)
	assert.Equal(t, state.PendingRule, unchanged.PendingRule, "catalog reads must not advance the ledger")
	for index, mode := range []string{"fixed", "independent", "assign", "legacy"} {
		var rules []model.SubscriptionCycleRule
		if mode == "independent" {
			rules = []model.SubscriptionCycleRule{{Key: "personal", Name: "Personal", Seconds: 137, NextReset: now + 137, Limit: 321, Enabled: true}}
		}
		require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: mode, PoolID: pool.Id, FixedEndTime: now + 172800, Version: int64(index + 1)}, rules))
		summaries, err := model.GetSubscriptionPlanAllocationSummaries([]int{plan.Id})
		require.NoError(t, err)
		summary := summaries[plan.Id]
		assert.Equal(t, mode, summary.Mode)
		if mode == "fixed" {
			assert.Equal(t, pool.EndTime, summary.FixedEndTime, "purchases cannot outlive their pool")
		}
		if mode == "independent" {
			require.Len(t, summary.Cycles, 1)
			assert.False(t, summary.Cycles[0].Shared)
			assert.EqualValues(t, 321, summary.Cycles[0].Limit)
		}
		if mode == "assign" || mode == "legacy" {
			assert.Empty(t, summary.Cycles)
		}
	}
}

func TestSubscriptionPoolPlanSaveIsAtomicAndPreservesExistingMembers(t *testing.T) {
	user, _, now := setupSubscriptionPoolTest(t)
	previous := *operation_setting.GetPaymentSetting()
	t.Cleanup(func() { *operation_setting.GetPaymentSetting() = previous })
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	request := func(method, id, payload string, role int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("id", user.Id)
		c.Set("role", role)
		c.Params = gin.Params{{Key: "id", Value: id}}
		c.Request = httptest.NewRequest(method, "/api/subscription/admin/plans", strings.NewReader(payload))
		c.Request.Header.Set("Content-Type", "application/json")
		if method == http.MethodPost {
			AdminCreateSubscriptionPlan(c)
		} else {
			AdminUpdateSubscriptionPlan(c)
		}
		return w
	}
	var before int64
	require.NoError(t, model.DB.Model(&model.SubscriptionPlan{}).Count(&before).Error)
	missing := request(http.MethodPut, "999999", `{"plan":{"title":"must-not-create"}}`, common.RoleRootUser)
	assert.Contains(t, missing.Body.String(), `"success":false`)
	invalid := request(http.MethodPost, "", `{"plan":{"title":"atomic-invalid","enabled":true},"allocation":{"mode":"fixed","pool_id":999999,"version":0,"cycles":[]}}`, common.RoleRootUser)
	assert.Contains(t, invalid.Body.String(), `"success":false`)
	var count int64
	require.NoError(t, model.DB.Model(&model.SubscriptionPlan{}).Count(&count).Error)
	assert.Equal(t, before, count, "invalid allocation must roll back plan creation")
	payload := fmt.Sprintf(`{"plan":{"title":"atomic-plan","enabled":true,"duration_unit":"day","duration_value":30,"total_amount":1234},"allocation":{"mode":"independent","version":0,"cycles":[{"key":"custom","name":"custom","seconds":137,"next_reset":%d,"limit":100,"enabled":true}]}}`, now+137)
	w := request(http.MethodPost, "", payload, common.RoleRootUser)
	var created struct {
		Success bool                   `json:"success"`
		Data    model.SubscriptionPlan `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &created))
	require.True(t, created.Success, w.Body.String())
	var config model.SubscriptionPlanAllocation
	require.NoError(t, model.DB.First(&config, "plan_id = ?", created.Data.Id).Error)
	assert.Equal(t, "independent", config.Mode)
	var existing *model.UserSubscription
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		existing, err = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &created.Data, "admin")
		return err
	}))
	stale := request(http.MethodPut, fmt.Sprint(created.Data.Id), `{"plan":{"title":"must-not-save"},"allocation":{"mode":"legacy","version":0}}`, common.RoleRootUser)
	assert.Contains(t, stale.Body.String(), `"success":false`)
	var retained model.SubscriptionPlan
	require.NoError(t, model.DB.First(&retained, created.Data.Id).Error)
	assert.Equal(t, "atomic-plan", retained.Title)
	updated := request(http.MethodPut, fmt.Sprint(created.Data.Id), `{"plan":{"title":"legacy-future","duration_unit":"day","duration_value":30,"total_amount":1234},"allocation":{"mode":"legacy","version":1}}`, common.RoleRootUser)
	assert.Contains(t, updated.Body.String(), `"success":true`)
	var oldAllocation model.SubscriptionAllocation
	require.NoError(t, model.DB.First(&oldAllocation, "subscription_id = ?", existing.Id).Error)
	assert.Contains(t, oldAllocation.Rules, "custom", "switching defaults must preserve existing subscription rules")
	require.NoError(t, model.DB.First(&retained, created.Data.Id).Error)
	var next *model.UserSubscription
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		next, err = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &retained, "admin")
		return err
	}))
	require.NoError(t, model.DB.Model(&model.SubscriptionAllocation{}).Where("subscription_id = ?", next.Id).Count(&count).Error)
	assert.Zero(t, count, "future legacy subscriptions must not enter the multi-cycle engine")
	denied := request(http.MethodPut, fmt.Sprint(created.Data.Id), `{"plan":{"title":"denied"},"allocation":{"mode":"legacy","version":2}}`, common.RoleCommonUser)
	assert.Equal(t, http.StatusForbidden, denied.Code)
}

func TestSubscriptionPoolPlanAllocationEmptyCyclesRoundTrip(t *testing.T) {
	for _, mode := range []string{"legacy", "independent"} {
		t.Run(mode, func(t *testing.T) {
			user, plan, now := setupSubscriptionPoolTest(t)
			previous := *operation_setting.GetPaymentSetting()
			t.Cleanup(func() { *operation_setting.GetPaymentSetting() = previous })
			operation_setting.GetPaymentSetting().ComplianceConfirmed = true
			operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
			pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "round-trip", Enabled: true, EndTime: now + 86400}, Cycles: []model.SubscriptionCycleRule{{Key: "short", Name: "Short", Seconds: 25200, NextReset: now + 25200, Limit: 100, Shared: true, Enabled: true}}}, user.Id)
			require.NoError(t, err)
			require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id}, nil))
			// Existing installations persist JSON null for unused plan cycles.
			require.NoError(t, model.DB.Model(&model.SubscriptionPlanAllocation{}).Where("plan_id = ?", plan.Id).Update("rules", "null").Error)
			var member *model.UserSubscription
			require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
				var err error
				member, err = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
				return err
			}))
			var original model.SubscriptionAllocation
			require.NoError(t, model.DB.First(&original, "subscription_id = ?", member.Id).Error)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Set("id", user.Id)
			c.Set("role", common.RoleRootUser)
			c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(plan.Id)}}
			GetSubscriptionPlanAllocation(c)
			var response struct {
				Success bool                            `json:"success"`
				Data    subscriptionPlanAllocationInput `json:"data"`
			}
			require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
			require.True(t, response.Success, w.Body.String())
			require.NotNil(t, response.Data.Cycles, "unused cycles must be an empty JSON array, not null")
			assert.Empty(t, response.Data.Cycles)
			response.Data.Mode = mode
			if mode == "independent" {
				response.Data.Cycles = []model.SubscriptionCycleRule{{Key: "personal", Name: "Personal", Seconds: 25200, NextReset: now + 25200, Limit: 200, Enabled: true}}
			}
			payload, err := common.Marshal(AdminUpsertSubscriptionPlanRequest{Plan: plan, Allocation: &response.Data})
			require.NoError(t, err)
			w = httptest.NewRecorder()
			c, _ = gin.CreateTestContext(w)
			c.Set("id", user.Id)
			c.Set("role", common.RoleRootUser)
			c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(plan.Id)}}
			c.Request = httptest.NewRequest(http.MethodPut, "/api/subscription/admin/plans", bytes.NewReader(payload))
			c.Request.Header.Set("Content-Type", "application/json")
			AdminUpdateSubscriptionPlan(c)
			assert.Contains(t, w.Body.String(), `"success":true`)
			var saved model.SubscriptionPlanAllocation
			require.NoError(t, model.DB.First(&saved, "plan_id = ?", plan.Id).Error)
			assert.Equal(t, mode, saved.Mode)
			assert.Zero(t, saved.PoolID)
			assert.EqualValues(t, 2, saved.Version)
			var retained model.SubscriptionAllocation
			require.NoError(t, model.DB.First(&retained, "subscription_id = ?", member.Id).Error)
			assert.Equal(t, original, retained, "editing plan defaults must not migrate existing purchased subscriptions")
			var retainedMember model.UserSubscription
			require.NoError(t, model.DB.First(&retainedMember, member.Id).Error)
			assert.Equal(t, *member, retainedMember)
		})
	}
}

func TestSubscriptionPoolSaveReturnsEditableConfiguration(t *testing.T) {
	_, _, now := setupSubscriptionPoolTest(t)
	previous := *operation_setting.GetPaymentSetting()
	t.Cleanup(func() { *operation_setting.GetPaymentSetting() = previous })
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	input := model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "save-contract", Enabled: true, EndTime: now + 86400}, Cycles: []model.SubscriptionCycleRule{{Key: "5h", Name: "5h", Seconds: 18000, NextReset: now + 18000, Unlimited: true, Enabled: true}}, Channels: []int{}}
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		raw, err := common.Marshal(input)
		require.NoError(t, err)
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Set("id", 1)
		c.Set("role", common.RoleRootUser)
		if method == http.MethodPut {
			c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(input.Id)}}
		}
		c.Request = httptest.NewRequest(method, "/api/subscription/admin/pools", strings.NewReader(string(raw)))
		c.Request.Header.Set("Content-Type", "application/json")
		SaveSubscriptionPool(c)
		var body struct {
			Success bool                        `json:"success"`
			Data    model.SubscriptionPoolInput `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
		require.True(t, body.Success, response.Body.String())
		require.Len(t, body.Data.Cycles, 1, "save must return cycles rather than the raw database row")
		require.NotNil(t, body.Data.Channels, "empty channel lists must be arrays")
		input = body.Data
	}
}

func TestSubscriptionPoolBillingPinsRetriesAndChargesEachCycleOnce(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	for _, id := range []int{9021, 9022, 9023} {
		require.NoError(t, model.DB.Create(&model.Channel{Id: id, Name: "billing"}).Error)
	}
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "billing", Enabled: true, EndTime: now + 86400}, Channels: []int{9021, 9022}, Cycles: []model.SubscriptionCycleRule{
		{Key: "short", Name: "short", Seconds: 137, NextReset: now + 100, Limit: 100, Shared: true, Enabled: true},
		{Key: "long", Name: "long", Seconds: 604800, NextReset: now + 604000, Limit: 200, Enabled: true},
	}}, 1)
	require.NoError(t, err)
	other, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "other", Enabled: true, EndTime: now + 86400}, Channels: []int{9023}, Cycles: []model.SubscriptionCycleRule{{Key: "short", Name: "short", Seconds: 137, NextReset: now + 100, Limit: 100, Enabled: true}}}, 1)
	require.NoError(t, err)
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id}, nil))
	var sub *model.UserSubscription
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		sub, err = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
		return err
	}))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{UserId: user.Id, RequestId: "integrated-billing", IsPlayground: true, ForcePreConsume: true, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 9021}, UserSetting: dto.UserSetting{BillingPreference: "subscription_only"}}
	require.Nil(t, service.PreConsumeBilling(c, 10, info))
	assert.Equal(t, int64(0), info.SubscriptionAmountTotal, "legacy single-limit log metadata must not misrepresent multiple cycles")
	filters := service.GetChannelConstraints(c).Filters
	allowed, _ := model.ChannelSatisfiesFilters(&model.Channel{Id: 9022}, "m", filters)
	assert.True(t, allowed)
	allowed, _ = model.ChannelSatisfiesFilters(&model.Channel{Id: 9023}, "m", filters)
	assert.False(t, allowed)
	allowed, _ = model.ChannelSatisfiesFilters(&model.Channel{Id: 9999}, "m", filters)
	assert.False(t, allowed)
	info.ChannelId = 9022
	require.Nil(t, service.PreConsumeBilling(c, 20, info))
	// Key rotation does not change the request's logical subscription ledger.
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 9022).Update("channel_info", model.ChannelInfo{IsMultiKey: true}).Error)
	assert.Nil(t, service.PreConsumeBilling(c, 30, info))
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 9022).Update("channel_info", model.ChannelInfo{}).Error)
	info.ChannelId = 9023
	require.NotNil(t, service.PreConsumeBilling(c, 30, info))
	// Administrative migration cannot move an already admitted request's ledger.
	require.NoError(t, model.AssignSubscriptionPool(sub.Id, other.Id, 1, 0, 1, true, other.Version))
	assert.Error(t, model.ResetSubscriptionCycle(sub.Id, pool.Id, user.Id, "long", 1), "a stale member reset must not clear the previous pool counters")
	require.NoError(t, info.Billing.Settle(25))
	stats, err := model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	for _, cycle := range stats {
		assert.Equal(t, int64(25), cycle.Used)
		assert.Zero(t, cycle.Reserved)
	}
	stats, err = model.GetSubscriptionPoolStatistics(other.Id)
	require.NoError(t, err)
	assert.Zero(t, stats[0].Used)
}

func TestSubscriptionPoolMigrationRetainsLegacyInflightSettlementAndRefund(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "target", Enabled: true, EndTime: now + 86400}, Cycles: []model.SubscriptionCycleRule{{Key: "c", Name: "c", Seconds: 3600, NextReset: now + 3500, Limit: 100, Enabled: true}}}, 1)
	require.NoError(t, err)
	for _, operation := range []string{"settle", "refund"} {
		t.Run(operation, func(t *testing.T) {
			var sub *model.UserSubscription
			require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
				var e error
				sub, e = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
				return e
			}))
			requestID := "legacy-move-" + operation
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{UserId: user.Id, RequestId: requestID, IsPlayground: true, ForcePreConsume: true, UserSetting: dto.UserSetting{BillingPreference: "subscription_only"}}
			require.Nil(t, service.PreConsumeBilling(ctx, 20, info))
			require.Equal(t, sub.Id, info.SubscriptionId)
			require.NoError(t, info.Billing.ReserveBeforeSubmit(30))
			require.NoError(t, model.AssignSubscriptionPool(sub.Id, pool.Id, 0, 0, 1, true, pool.Version))
			expected := int64(35)
			if operation == "settle" {
				assert.NoError(t, info.Billing.Settle(35))
				assert.NoError(t, model.SettleSubscriptionRequest(requestID, sub.Id, 5), "settlement retries remain idempotent")
			} else {
				expected = 0
				info.Billing.Refund(ctx)
				require.Eventually(t, func() bool {
					var current model.UserSubscription
					return model.DB.First(&current, sub.Id).Error == nil && current.AmountUsed == 0
				}, 3*time.Second, 10*time.Millisecond)
				assert.NoError(t, model.RefundSubscriptionPreConsume(requestID))
				assert.NoError(t, model.RefundSubscriptionPreConsume(requestID), "refund retries remain idempotent")
			}
			require.NoError(t, model.DB.First(sub, sub.Id).Error)
			assert.Equal(t, expected, sub.AmountUsed, "the original legacy funding, including extra reserves, remains accountable")
			stats, err := model.GetSubscriptionPoolStatistics(pool.Id)
			require.NoError(t, err)
			assert.Zero(t, stats[0].Used, "inflight legacy usage is not charged to the target pool")
			assert.Zero(t, stats[0].Reserved)
		})
	}
}

func TestSubscriptionPoolAdmissionReportsConfigurationAndCycleFailures(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9599, Name: "diagnostics"}).Error)
	input := model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "diagnostics", Enabled: true, EndTime: now + 86400}, Cycles: []model.SubscriptionCycleRule{{Key: "c", Name: "c", Seconds: 3600, NextReset: now + 3500, Limit: 10, Enabled: true}}}
	pool, err := model.SaveSubscriptionPool(input, 1)
	require.NoError(t, err)
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id}, nil))
	var sub *model.UserSubscription
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		var e error
		sub, e = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
		return e
	}))
	_, err = model.PreConsumeUserSubscriptionForChannel("missing-channel", user.Id, "m", 0, 1, 9599)
	require.ErrorContains(t, err, "pool has no bound channels")
	input.SubscriptionPool = *pool
	input.Channels = []int{9599}
	_, err = model.SaveSubscriptionPool(input, 1)
	require.NoError(t, err)
	_, err = model.PreConsumeUserSubscriptionForChannel("actual-limit", user.Id, "m", 0, 11, 9599)
	require.ErrorContains(t, err, "cycle limit reached")
	require.NoError(t, model.DB.Model(&model.SubscriptionAllocation{}).Where("subscription_id = ?", sub.Id).Update("pending", true).Error)
	_, err = model.PreConsumeUserSubscriptionForChannel("pending-pool", user.Id, "m", 0, 1, 9599)
	require.ErrorContains(t, err, "pool assignment pending")
}

func TestSubscriptionPoolLegacyCarryPreservesSettledUsageAndRejectsAmbiguousWindows(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "carry", Enabled: true, EndTime: now + 86400}, Cycles: []model.SubscriptionCycleRule{
		{Key: "shared", Name: "shared", Seconds: 3600, NextReset: now + 2600, Limit: 1000, Shared: true, Enabled: true},
		{Key: "personal", Name: "personal", Seconds: 3600, NextReset: now + 2600, Limit: 1000, Enabled: true},
	}}, 1)
	require.NoError(t, err)
	sub := model.UserSubscription{UserId: user.Id, PlanId: plan.Id, StartTime: now - 1000, EndTime: now + 86400, Status: "active", AmountTotal: 1000, AmountUsed: 100, LastResetTime: now - 1000, NextResetTime: now + 2600}
	require.NoError(t, model.DB.Create(&sub).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{RequestId: "carry-open", UserId: user.Id, UserSubscriptionId: sub.Id, PreConsumed: 30, Status: "consumed"}).Error)
	preview, err := model.PreviewSubscriptionPoolAssignment(sub.Id, pool.Id, 0)
	require.NoError(t, err)
	assert.True(t, preview.UsageTransfer)
	require.NoError(t, model.DB.Model(&sub).Update("amount_used", 101).Error)
	require.ErrorContains(t, model.AssignSubscriptionPool(sub.Id, pool.Id, 0, 0, 1, true, pool.Version, preview.Carry.Digest), "Legacy usage changed")
	require.NoError(t, model.DB.Model(&sub).Update("amount_used", 100).Error)
	require.NoError(t, model.AssignSubscriptionPool(sub.Id, pool.Id, 0, 0, 1, true, pool.Version, preview.Carry.Digest))
	require.NoError(t, model.AssignSubscriptionPool(sub.Id, pool.Id, 0, 0, 1, true, pool.Version))
	stats, err := model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	for _, c := range stats {
		assert.EqualValues(t, 70, c.Used)
	}
	views, err := model.GetSubscriptionUsage(user.Id)
	require.NoError(t, err)
	for _, c := range views[0].Cycles {
		assert.EqualValues(t, 70, c.Used)
	}
	require.NoError(t, model.RefundSubscriptionPreConsume("carry-open"))
	require.NoError(t, model.RefundSubscriptionPreConsume("carry-open"))
	require.NoError(t, model.DB.First(&sub, sub.Id).Error)
	assert.EqualValues(t, 70, sub.AmountUsed)
	stats, err = model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	for _, c := range stats {
		assert.EqualValues(t, 70, c.Used, "legacy inflight refunds do not undo settled carry")
	}
	var events int64
	require.NoError(t, model.DB.Model(&model.SubscriptionPoolEvent{}).Where("subscription_id = ? AND action = ?", sub.Id, "legacy_carry").Count(&events).Error)
	assert.EqualValues(t, 1, events)
	// Equal durations alone are insufficient: copying the whole source balance
	// into a differently aligned current window invents consumption.
	sub.Id = 0
	sub.LastResetTime--
	sub.NextResetTime--
	require.NoError(t, model.DB.Create(&sub).Error)
	require.ErrorContains(t, model.AssignSubscriptionPool(sub.Id, pool.Id, 0, 0, 1, true, pool.Version), "Legacy usage window does not match")
	var count int64
	require.NoError(t, model.DB.Model(&model.SubscriptionAllocation{}).Where("subscription_id = ?", sub.Id).Count(&count).Error)
	assert.Zero(t, count, "an ambiguous migration cannot silently start at zero")
}

func TestSubscriptionPoolCleanupRetainsLegacyInflightProvenance(t *testing.T) {
	user, _, now := setupSubscriptionPoolTest(t)
	for _, status := range []string{"consumed", "settled", "refunded"} {
		record := model.SubscriptionPreConsumeRecord{RequestId: "cleanup-" + status, UserId: user.Id, Status: status, PreConsumed: 1}
		require.NoError(t, model.DB.Create(&record).Error)
		require.NoError(t, model.DB.Model(&record).Update("updated_at", now-1000).Error)
	}
	n, err := model.CleanupSubscriptionPreConsumeRecords(100)
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)
	var retained []model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Find(&retained).Error)
	require.Len(t, retained, 1)
	assert.Equal(t, "consumed", retained[0].Status)
}

func TestSubscriptionPoolLegacyCarryCalibrationAndOverflowAreAtomic(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "calibrated-carry", Enabled: true, EndTime: now + 86400}, Cycles: []model.SubscriptionCycleRule{{Key: "c", Name: "c", Seconds: 3600, NextReset: now + 2600, Limit: 1000, Enabled: true}}}, 1)
	require.NoError(t, err)
	var state model.SubscriptionCycleState
	require.NoError(t, model.DB.Where("owner = ?", fmt.Sprintf("pool:%d", pool.Id)).First(&state).Error)
	aggregate := model.SubscriptionCycleUsage{StateID: state.Id, Epoch: state.Epoch, Used: 5, Adjustment: 40}
	require.NoError(t, model.DB.Create(&aggregate).Error)
	source := model.SubscriptionSyncSource{Name: "carry-cache", Version: 1, IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10}
	require.NoError(t, model.DB.Create(&source).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionSyncBinding{PoolID: pool.Id, SourceID: source.Id, AccountID: "a", Enabled: true, Version: 1, CyclesJSON: "[]"}).Error)
	payload := fmt.Sprintf(`{"key":"c","seconds":3600,"utilization":0.6,"window_start":%d,"window_end":%d,"sampled_at":%d,"local_baseline":{"usage_id":%d,"used":5,"capacity":100}}`, now-1000, now+2600, now, aggregate.Id)
	require.NoError(t, model.DB.Create(&model.SubscriptionSyncObservation{PoolID: pool.Id, LocalKey: "c", Payload: payload, SampledAt: now, FetchedAt: now, BindingVersion: 1, SourceVersion: 1}).Error)
	sub := model.UserSubscription{UserId: user.Id, PlanId: plan.Id, StartTime: now - 1000, EndTime: now + 86400, Status: "active", AmountTotal: 1000, AmountUsed: 70, LastResetTime: now - 1000, NextResetTime: now + 2600}
	require.NoError(t, model.DB.Create(&sub).Error)
	require.NoError(t, model.AssignSubscriptionPool(sub.Id, pool.Id, 0, 0, 1, true, pool.Version))
	stats, err := model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 75, stats[0].Used, "existing local usage is not overwritten")
	assert.Zero(t, stats[0].Adjustment, "known consumption replaces overlapping calibration")
	views, err := model.GetSubscriptionSyncViews(pool.Id)
	require.NoError(t, err)
	require.NotNil(t, views[0].EstimatedUtilization)
	assert.Equal(t, .6, *views[0].EstimatedUtilization, "reclassification is not another upstream request")
	sub.Id = 0
	sub.AmountUsed = common.MaxWalletQuota
	sub.NextResetTime = now + 2600
	require.NoError(t, model.DB.Create(&sub).Error)
	require.ErrorContains(t, model.AssignSubscriptionPool(sub.Id, pool.Id, 0, 0, 1, true, pool.Version), "overflow")
	var allocations, events int64
	require.NoError(t, model.DB.Model(&model.SubscriptionAllocation{}).Where("subscription_id = ?", sub.Id).Count(&allocations).Error)
	require.NoError(t, model.DB.Model(&model.SubscriptionPoolEvent{}).Where("subscription_id = ?", sub.Id).Count(&events).Error)
	assert.Zero(t, allocations)
	assert.Zero(t, events)
	require.NoError(t, model.DB.First(&aggregate, aggregate.Id).Error)
	assert.EqualValues(t, 75, aggregate.Used)
}

func TestSubscriptionPoolNaturalResetUsesLocalCapacityWithoutRefreshingUpstreamFacts(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9598, Name: "local-reset"}).Error)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "local-reset", Enabled: true, EndTime: now + 86400}, Channels: []int{9598}, Cycles: []model.SubscriptionCycleRule{{Key: "c", Name: "c", Seconds: 3600, NextReset: now + 3599, Unlimited: true, Shared: true, Enabled: true}}}, 1)
	require.NoError(t, err)
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id}, nil))
	var sub *model.UserSubscription
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		var e error
		sub, e = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
		return e
	}))
	var state model.SubscriptionCycleState
	require.NoError(t, model.DB.Where("owner = ?", fmt.Sprintf("pool:%d", pool.Id)).First(&state).Error)
	require.NoError(t, model.DB.Model(&state).Updates(map[string]any{"start_time": now - 3601, "end_time": now - 1}).Error)
	old := model.SubscriptionCycleUsage{StateID: state.Id, Epoch: state.Epoch, UserID: 0, Used: 40}
	require.NoError(t, model.DB.Create(&old).Error)
	source := model.SubscriptionSyncSource{Name: "cached-source", Version: 1, IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10}
	require.NoError(t, model.DB.Create(&source).Error)
	bindings, err := common.Marshal([]model.SubscriptionSyncCycleBinding{{LocalKey: "c", UpstreamKey: "c", QuotaPerFullWindow: 100}})
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.SubscriptionSyncBinding{PoolID: pool.Id, SourceID: source.Id, AccountID: "a", Enabled: true, Version: 1, CyclesJSON: string(bindings)}).Error)
	payload := fmt.Sprintf(`{"key":"c","seconds":3600,"utilization":0.4,"window_start":%d,"window_end":%d,"sampled_at":%d,"local_baseline":{"usage_id":%d,"used":40,"capacity":100}}`, now-3601, now-1, now-10, old.Id)
	require.NoError(t, model.DB.Create(&model.SubscriptionSyncObservation{PoolID: pool.Id, LocalKey: "c", Payload: payload, SampledAt: now - 10, FetchedAt: now, SourceVersion: 1, BindingVersion: 1}).Error)
	_, err = model.PreConsumeUserSubscriptionForChannel("local-window", user.Id, "m", 0, 10, 9598)
	require.NoError(t, err)
	require.NoError(t, model.SettleSubscriptionRequest("local-window", sub.Id, 0))
	views, err := model.GetSubscriptionSyncViews(pool.Id)
	require.NoError(t, err)
	raw, err := common.Marshal(views)
	require.NoError(t, err)
	var result []map[string]any
	require.NoError(t, common.Unmarshal(raw, &result))
	require.Len(t, result, 1)
	assert.EqualValues(t, now+3599, result[0]["local_window_end"])
	assert.Equal(t, .1, result[0]["local_estimated_utilization"])
	assert.Equal(t, .4, result[0]["utilization"], "a local estimate never fabricates a new upstream snapshot")
	assert.EqualValues(t, now-1, result[0]["window_end"])
	assert.EqualValues(t, now-10, result[0]["sampled_at"])
	require.NoError(t, model.ResetSubscriptionCycle(0, pool.Id, 0, "c", 1))
	views, err = model.GetSubscriptionSyncViews(pool.Id)
	require.NoError(t, err)
	raw, err = common.Marshal(views)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(raw, &result))
	assert.Nil(t, result[0]["local_estimated_utilization"], "manual reset invalidates the baseline")
}

func TestSubscriptionPoolExpiryInheritanceAndResetHeadEditing(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	input := model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "expiry", Enabled: true, EndTime: now + 1000}, Cycles: []model.SubscriptionCycleRule{{Key: "c", Name: "c", Seconds: 137, NextReset: now - 140, Limit: 100, Enabled: true}}}
	pool, err := model.SaveSubscriptionPool(input, 1)
	require.NoError(t, err)
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id}, nil))
	var subs []*model.UserSubscription
	for range 3 {
		require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
			sub, err := model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
			subs = append(subs, sub)
			return err
		}))
	}
	require.NoError(t, model.AssignSubscriptionPool(subs[1].Id, pool.Id, 1, now+500, 1, true, pool.Version))
	require.NoError(t, model.DB.Model(subs[2]).Update("status", "cancelled").Error)
	config, err := model.GetSubscriptionPoolConfiguration(pool.Id)
	require.NoError(t, err)
	assert.Greater(t, config.Cycles[0].NextReset, now)
	config.EndTime = now + 2000
	_, err = model.SaveSubscriptionPool(config, 1)
	require.NoError(t, err)
	for i, expected := range []int64{now + 2000, now + 500, now + 1000} {
		var sub model.UserSubscription
		require.NoError(t, model.DB.First(&sub, subs[i].Id).Error)
		assert.Equal(t, expected, sub.EndTime)
	}
	stats, err := model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	assert.False(t, stats[0].Pending, "saving a naturally advanced reset head is not a policy change")
	preview, err := model.PreviewSubscriptionPoolAssignment(subs[0].Id, pool.Id, 0)
	require.NoError(t, err)
	require.NoError(t, model.AssignSubscriptionPool(subs[0].Id, pool.Id, preview.Current.Version, 0, 1, true, preview.PoolVersion))
	require.NoError(t, model.AssignSubscriptionPool(subs[0].Id, pool.Id, preview.Current.Version, 0, 1, true, preview.PoolVersion), "identical HTTP retries are idempotent")
	require.Error(t, model.AssignSubscriptionPool(subs[0].Id, pool.Id, preview.Current.Version, 0, 1, true, preview.PoolVersion-1))
	// A member's custom deadline may outlive a shortened upstream pool. The
	// export and wallet must show the effective expiry, not an active entitlement.
	require.NoError(t, model.DB.Model(pool).Update("end_time", now-1).Error)
	views, err := model.GetSubscriptionUsage(user.Id)
	require.NoError(t, err)
	for _, view := range views {
		if view.SubscriptionID == subs[1].Id {
			assert.Equal(t, now-1, view.EndTime)
			assert.Equal(t, "expired", view.Status, "the effective upstream expiry governs a custom member deadline")
		}
	}
}

func TestSubscriptionPoolConcurrentAdmissionsAndReplays(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9011, Name: "concurrent"}).Error)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "concurrent", Enabled: true, EndTime: now + 86400}, Channels: []int{9011}, Cycles: []model.SubscriptionCycleRule{{Key: "c", Name: "c", Seconds: 3600, NextReset: now + 3500, Limit: 100, Shared: true, Enabled: true}}}, 1)
	require.NoError(t, err)
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id}, nil))
	var sub *model.UserSubscription
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		sub, err = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
		return err
	}))
	start := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			<-start
			_, err := model.PreConsumeUserSubscriptionForChannel(fmt.Sprintf("concurrent-%d", i), user.Id, "m", 0, 25, 9011)
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else {
			assert.ErrorContains(t, err, "subscription quota insufficient")
		}
	}
	require.Equal(t, 4, success, "admission must never overbook a shared window")
	stats, err := model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(100), stats[0].Reserved)
	var reservations []model.SubscriptionCycleReservation
	require.NoError(t, model.DB.Find(&reservations).Error)
	for _, r := range reservations {
		require.NoError(t, model.RefundSubscriptionPreConsume(r.RequestID))
	}
	start = make(chan struct{})
	results = make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			<-start
			_, err := model.PreConsumeUserSubscriptionForChannel("same-request", user.Id, "m", 0, 30, 9011)
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		assert.NoError(t, err)
	}
	stats, err = model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(30), stats[0].Reserved)
	require.NoError(t, model.SettleSubscriptionRequest("same-request", sub.Id, 0))
	stats, err = model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(30), stats[0].Used)
	assert.Zero(t, stats[0].Reserved)
}

func TestSubscriptionPoolPersonalResetPreservesInflightAndMissingProvenanceFails(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9012, Name: "reset"}).Error)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "reset", Enabled: true, EndTime: now + 86400}, Channels: []int{9012}, Cycles: []model.SubscriptionCycleRule{{Key: "c", Name: "c", Seconds: 3600, NextReset: now + 3500, Limit: 100, Enabled: true}}}, 1)
	require.NoError(t, err)
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id}, nil))
	var sub *model.UserSubscription
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		sub, err = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
		return err
	}))
	_, err = model.PreConsumeUserSubscriptionForChannel("reset-inflight", user.Id, "m", 0, 60, 9012)
	require.NoError(t, err)
	require.NoError(t, model.ResetSubscriptionCycle(sub.Id, pool.Id, user.Id, "c", 1))
	require.NoError(t, model.SettleSubscriptionRequest("reset-inflight", sub.Id, 10))
	views, err := model.GetSubscriptionUsage(user.Id)
	require.NoError(t, err)
	assert.Zero(t, views[0].Cycles[0].Used)
	stats, err := model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(70), stats[0].Used)
	assert.True(t, stats[0].LimitUnknown, "unknown upstream capacity must not be shown as unlimited")
	require.Error(t, model.AdjustSubscriptionRequest("", sub.Id, 10), "managed tasks cannot silently fall back to legacy counters")
	require.NoError(t, model.DB.Model(sub).Update("end_time", now-1).Error)
	views, err = model.GetSubscriptionUsage(user.Id)
	require.NoError(t, err)
	assert.Equal(t, "expired", views[0].Status)
}

func TestSubscriptionPoolRejectsMissingAndMismatchedSettlementProvenance(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "independent"}, []model.SubscriptionCycleRule{{Key: "c", Name: "c", Seconds: 3600, NextReset: now + 3500, Limit: 100, Enabled: true}}))
	var sub *model.UserSubscription
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		sub, err = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
		return err
	}))
	_, err := model.PreConsumeUserSubscription("provenance", user.Id, "m", 0, 20)
	require.NoError(t, err)
	assert.Error(t, model.SettleSubscriptionRequest("", sub.Id, 0), "zero-delta settlement still requires managed request provenance")
	assert.Error(t, model.AdjustSubscriptionRequest("", sub.Id, 0))
	assert.Error(t, model.SettleSubscriptionRequest("provenance", sub.Id+9999, 0), "the request cannot settle another subscription")
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: user.Id, BillingSource: service.BillingSourceSubscription, SubscriptionId: sub.Id, FinalPreConsumedQuota: 20, IsPlayground: true}
	assert.Error(t, service.SettleBilling(ctx, info, 20), "sessionless zero-delta settlement cannot bypass provenance checks")
	info.RequestId = "provenance"
	require.NoError(t, service.SettleBilling(ctx, info, 20))
	var reservation model.SubscriptionCycleReservation
	require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&reservation).Error)
	assert.Equal(t, "settled", reservation.Status, "an exact-price request must finalize its reservation even without a billing session")
	require.NoError(t, model.RefundSubscriptionPreConsume(info.RequestId), "a completed request must not be refunded")
	require.NoError(t, model.DB.Where("request_id = ?", "provenance").Delete(&model.SubscriptionCycleReservation{}).Error)
	assert.Error(t, model.AdjustSubscriptionRequest("provenance", sub.Id, 0), "a lost managed ledger cannot be treated as legacy provenance")
	views, err := model.GetSubscriptionUsage(user.Id)
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, int64(20), views[0].Cycles[0].Used)
	assert.Zero(t, views[0].Cycles[0].Reserved)
}

func TestSubscriptionPoolQuotaExportAndRootOnlyManagement(t *testing.T) {
	user, _, _ := setupSubscriptionPoolTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}))
	token := model.Token{UserId: user.Id, Key: "test-subscription-export", AllowUserQuotaQuery: false}
	require.NoError(t, model.DB.Create(&token).Error)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", user.Id)
	ctx.Set("token_id", token.Id)
	GetTokenSubscriptionUsage(ctx)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
	require.NoError(t, model.DB.Model(&token).Update("allow_user_quota_query", true).Error)
	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Set("id", user.Id)
	ctx.Set("token_id", token.Id)
	GetTokenSubscriptionUsage(ctx)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "pool_id")
	assert.NotContains(t, recorder.Body.String(), "observations")
	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Set("id", user.Id)
	ctx.Set("role", common.RoleAdminUser)
	ListSubscriptionPools(ctx)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestSubscriptionPoolWindowsSettlementAndIsolation(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9001, Name: "pool-one"}).Error)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "pool", Note: "private", Enabled: true, EndTime: now + 86400}, Channels: []int{9001}, Cycles: []model.SubscriptionCycleRule{
		{Key: "short", Name: "five hours", Seconds: 18000, NextReset: now + 100, Limit: 100, Shared: true, Enabled: true},
		{Key: "week", Name: "seven days", Seconds: 604800, NextReset: now + 500, Limit: 80, Enabled: true},
	}}, 1)
	require.NoError(t, err)
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id}, nil))
	var subs []*model.UserSubscription
	for _, id := range []int{user.Id, user.Id + 1, user.Id} {
		require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
			sub, e := model.CreateUserSubscriptionFromPlanTx(tx, id, &plan, "admin")
			subs = append(subs, sub)
			return e
		}))
	}
	reserve := func(request string, uid int, amount int64) error {
		_, e := model.PreConsumeUserSubscriptionForChannel(request, uid, "m", 0, amount, 9001)
		return e
	}
	require.NoError(t, reserve("first", user.Id, 60))
	require.NoError(t, model.SettleSubscriptionRequest("first", subs[0].Id, -10)) // actual 50
	require.NoError(t, model.SettleSubscriptionRequest("first", subs[0].Id, -10)) // idempotent
	require.NoError(t, reserve("second", user.Id+1, 40))
	require.ErrorContains(t, reserve("full", user.Id, 20), "subscription quota insufficient")
	require.NoError(t, model.RefundSubscriptionPreConsume("second"))
	require.NoError(t, model.RefundSubscriptionPreConsume("second"))
	require.NoError(t, reserve("third", user.Id, 25))
	require.NoError(t, model.SettleSubscriptionRequest("third", subs[0].Id, 0))
	require.ErrorContains(t, reserve("personal-full", user.Id, 10), "subscription quota insufficient")
	// Duplicated instances share personal counters rather than multiplying quota.
	views, err := model.GetSubscriptionUsage(user.Id)
	require.NoError(t, err)
	require.Len(t, views, 2)
	for _, view := range views {
		require.Len(t, view.Cycles, 2)
		assert.Equal(t, int64(75), view.Cycles[1].Used)
		assert.Equal(t, int64(0), view.Cycles[1].Reserved)
	}
	overview, err := model.GetSubscriptionPoolOverview(user.Id)
	require.NoError(t, err)
	require.Len(t, overview, 1)
	raw, err := common.Marshal(views)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "private")
	assert.NotContains(t, string(raw), "pool_id")
	assert.NotContains(t, string(raw), "channels")
	// Current-window reset creates a new epoch; refunding an old reservation
	// must not credit that new epoch, and real over-estimate excess is retained.
	require.NoError(t, reserve("old-window", user.Id+1, 20))
	require.NoError(t, model.ResetSubscriptionCycle(0, pool.Id, 0, "short", 1))
	require.NoError(t, model.RefundSubscriptionPreConsume("old-window"))
	require.NoError(t, reserve("excess", user.Id+1, 10))
	require.NoError(t, model.SettleSubscriptionRequest("excess", subs[1].Id, 120)) // 130 > limit
	overview, err = model.GetSubscriptionPoolOverview(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(130), overview[0].Cycles[0].Used)
	require.ErrorContains(t, reserve("blocked-after-excess", user.Id, 1), "subscription quota insufficient")
	blocked, err := model.SubscriptionBlockedChannels(user.Id+77, false)
	require.NoError(t, err)
	assert.Equal(t, []int{9001}, blocked)
	require.Error(t, model.ValidateSubscriptionRetry("excess", 0, false))
	require.Error(t, model.ValidateSubscriptionRetry("", 9001, true))
	require.Error(t, model.AssignSubscriptionPool(subs[0].Id, pool.Id, 99, 0, 1, true, pool.Version))
}

func TestSubscriptionPoolAnchorsSnapshotsAndPlanDefaults(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	rules := []model.SubscriptionCycleRule{{Key: "custom", Name: "custom", Seconds: 18000, NextReset: now - 36001, Limit: 100, Enabled: true}}
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "independent"}, rules))
	var sub *model.UserSubscription
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		var e error
		sub, e = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
		return e
	}))
	views, err := model.GetSubscriptionUsage(user.Id)
	require.NoError(t, err)
	assert.Equal(t, now+17999, views[0].Cycles[0].NextReset)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9002, Name: "two"}).Error)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "pool2", Enabled: true, EndTime: now + 86400}, Channels: []int{9002}, Cycles: []model.SubscriptionCycleRule{{Key: "c", Name: "c", Seconds: 3600, NextReset: now + 100, Limit: 100, Shared: true, Enabled: true}}}, 1)
	require.NoError(t, err)
	// Editing the plan default cannot move an already purchased instance.
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id, Version: 1}, nil))
	var allocation model.SubscriptionAllocation
	require.NoError(t, model.DB.First(&allocation, "subscription_id = ?", sub.Id).Error)
	assert.Zero(t, allocation.PoolID)
	require.NoError(t, model.AssignSubscriptionPool(sub.Id, pool.Id, allocation.Version, 0, 1, true, pool.Version))
	_, err = model.PreConsumeUserSubscriptionForChannel("calibration", user.Id, "m", 0, 10, 9002)
	require.NoError(t, err)
	require.NoError(t, model.SettleSubscriptionRequest("calibration", sub.Id, 0))
	overview, err := model.GetSubscriptionPoolOverview(user.Id)
	require.NoError(t, err)
	cycle := overview[0].Cycles[0]
	observation := model.SubscriptionUpstreamSnapshot{PoolID: pool.Id, EventID: "event-one", CycleKey: "c", Source: "manual", Unit: "quota", Used: 30, Limit: 100, SampledAt: now, WindowStart: cycle.StartTime, WindowEnd: cycle.NextReset}
	require.NoError(t, model.IngestSubscriptionSnapshot(observation, 1, true))
	require.NoError(t, model.IngestSubscriptionSnapshot(observation, 1, true))
	require.ErrorContains(t, model.IngestSubscriptionSnapshot(observation, 1, false), "conflicts")
	observation.EventID = "event-two"
	observation.SampledAt = now - 1
	require.ErrorContains(t, model.IngestSubscriptionSnapshot(observation, 1, true), "stale")
	overview, err = model.GetSubscriptionPoolOverview(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(10), overview[0].Cycles[0].Used)
	assert.Equal(t, int64(20), overview[0].Cycles[0].Adjustment)
	poolInput := model.SubscriptionPoolInput{SubscriptionPool: *pool, Channels: []int{9002}, Cycles: []model.SubscriptionCycleRule{{Key: "c", Name: "changed", Seconds: 7200, NextReset: now + 200, Limit: 50, Shared: false, Enabled: true}}}
	_, err = model.SaveSubscriptionPool(poolInput, 1)
	require.NoError(t, err)
	overview, err = model.GetSubscriptionPoolOverview(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(10), overview[0].Cycles[0].Used)
	assert.Equal(t, now+200, overview[0].Cycles[0].NextReset)
	assert.True(t, overview[0].Cycles[0].Shared)
	assert.True(t, overview[0].Cycles[0].Pending)
	// Migrations must retain the new ledger too and preserve uniqueness.
	require.NoError(t, model.MigrateSubscriptionPools())
	require.NoError(t, model.MigrateSubscriptionPools())
	require.Error(t, model.DB.Create(&model.SubscriptionPoolChannel{ChannelID: 9002, PoolID: pool.Id}).Error)
	require.Error(t, model.DB.Create(&model.SubscriptionCycleReservation{RequestID: "calibration"}).Error)
	var count int64
	require.NoError(t, model.DB.Model(&model.SubscriptionCycleReservation{}).Where("request_id = ?", "calibration").Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", sub.Id).Update("status", "cancelled").Error)
	overview, err = model.GetSubscriptionPoolOverview(user.Id)
	require.NoError(t, err)
	assert.Empty(t, overview)
}

func TestSubscriptionPoolClosedMembershipPreservesInflightLedger(t *testing.T) {
	for _, pooled := range []bool{false, true} {
		for _, closeAction := range []string{"cancel", "delete"} {
			t.Run(fmt.Sprintf("pooled_%t/%s", pooled, closeAction), func(t *testing.T) {
				user, plan, now := setupSubscriptionPoolTest(t)
				rules := []model.SubscriptionCycleRule{{Key: "c", Name: "cycle", Seconds: 3600, NextReset: now + 3500, Limit: 100, Enabled: true}}
				cfg := model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "independent"}
				channelID := 0
				if pooled {
					channelID = 9031
					require.NoError(t, model.DB.Create(&model.Channel{Id: channelID, Name: "closed-membership"}).Error)
					pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "closed-membership", Enabled: true, EndTime: now + 86400}, Channels: []int{channelID}, Cycles: rules}, 1)
					require.NoError(t, err)
					cfg.Mode, cfg.PoolID = "fixed", pool.Id
					rules = nil
				}
				require.NoError(t, model.SaveSubscriptionPlanAllocation(cfg, rules))
				var sub *model.UserSubscription
				require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
					var err error
					sub, err = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
					return err
				}))
				_, err := model.PreConsumeUserSubscriptionForChannel("closing-settle", user.Id, "m", 0, 20, channelID)
				require.NoError(t, err)
				_, err = model.PreConsumeUserSubscriptionForChannel("closing-refund", user.Id, "m", 0, 10, channelID)
				require.NoError(t, err)
				if closeAction == "cancel" {
					_, err = model.AdminInvalidateUserSubscription(sub.Id)
				} else {
					_, err = model.AdminDeleteUserSubscription(sub.Id)
				}
				require.NoError(t, err)
				_, err = model.PreConsumeUserSubscriptionForChannel("after-close", user.Id, "m", 0, 1, channelID)
				assert.Error(t, err, "closed membership cannot admit a new request")
				require.NoError(t, model.SettleSubscriptionRequest("closing-settle", sub.Id, 5))
				require.NoError(t, model.RefundSubscriptionPreConsume("closing-refund"))
				require.NoError(t, model.RefundSubscriptionPreConsume("closing-settle"))
				var usage []model.SubscriptionCycleUsage
				require.NoError(t, model.DB.Find(&usage).Error)
				require.NotEmpty(t, usage)
				for _, row := range usage {
					assert.Equal(t, int64(25), row.Used, "original cycle usage survives membership closure")
					assert.Zero(t, row.Reserved)
				}
			})
		}
	}
}

func TestSubscriptionPoolSyncParsesPanelWithoutUsingBillingEstimates(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	body := fmt.Sprintf(`{"ok":true,"data":{"accounts":[{"account_id":"a","vm_id":"vm-01","utilization_5h":0.62,"reset_5h":%q,"last_probe":%q,"last_probe_check":%q,"estimate_5h":{"total_usd":999999},"utilization_7d":null}]}}`, now.Add(time.Hour).Format(time.RFC3339), now.Add(-time.Hour).Format(time.RFC3339), now.Format(time.RFC3339))
	accounts, err := service.ParseSubscriptionSyncResponse([]byte(body), model.PanelSubscriptionSyncMapping(), now.Unix())
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	assert.Equal(t, "a", accounts[0].ID)
	require.Len(t, accounts[0].Cycles, 1, "unreported optional windows are not returned")
	assert.InDelta(t, 0.62, *accounts[0].Cycles[0].Utilization, 0.000001)
	assert.Equal(t, now.Add(-time.Hour).Unix(), accounts[0].Cycles[0].SampledAt, "a recent check does not refresh the observation")
	_, err = service.ParseSubscriptionSyncResponse([]byte(`{"ok":false,"data":{"accounts":[]}}`), model.PanelSubscriptionSyncMapping(), now.Unix())
	assert.Error(t, err)
	_, err = service.ParseSubscriptionSyncResponse([]byte(`{"ok":true,"data":{"accounts":[{"account_id":"a"},{"account_id":"a"}]}}`), model.PanelSubscriptionSyncMapping(), now.Unix())
	assert.Error(t, err, "duplicate stable IDs cannot be bound ambiguously")
}

func TestSubscriptionPoolSyncReadIsExplicitAndSecretsStayPrivate(t *testing.T) {
	setupSubscriptionPoolTest(t)
	t.Setenv("SUBSCRIPTION_SYNC_SECRET_KEY", strings.Repeat("test-key-", 8))
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "/api/panel/usage", r.URL.Path)
		assert.Equal(t, "Bearer never-return-this", r.Header.Get("Authorization"))
		fmt.Fprint(w, `{"ok":true,"data":{"accounts":[]}}`)
	}))
	defer server.Close()
	input := model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "panel", URL: server.URL + "/api/panel/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10, AllowHTTP: true, PrivateIPs: "127.0.0.1"}, Mapping: model.PanelSubscriptionSyncMapping(), Request: &model.SubscriptionSyncRequest{AuthType: "bearer", Token: "never-return-this"}}
	_, err := service.SaveSubscriptionSyncSource(input, 1, false)
	assert.Error(t, err, "delegated managers cannot authorize private destinations")
	source, err := service.SaveSubscriptionSyncSource(input, 1, true)
	require.NoError(t, err)
	encoded, err := common.Marshal(source)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "never-return-this")
	assert.NotContains(t, source.SecretPayload, "never-return-this")
	_, err = service.RunSubscriptionSync(context.Background(), source.Id, 1, true)
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	var stored model.SubscriptionSyncSource
	require.NoError(t, model.DB.First(&stored, source.Id).Error)
	assert.Zero(t, stored.LastFetch, "test reads do not mutate synchronization state")
	input.SubscriptionSyncSource = *source
	input.URL = server.URL + "/api/panel/usage-probe"
	_, err = service.SaveSubscriptionSyncSource(input, 1, true)
	assert.Error(t, err, "known active probes cannot be configured as polling sources")
	input.URL = "https://169.254.169.254/latest/meta-data"
	input.PrivateIPs = "169.254.169.254"
	_, err = service.SaveSubscriptionSyncSource(input, 1, true)
	assert.Error(t, err, "metadata targets remain forbidden even when listed")
}

func TestSubscriptionPoolSyncKeepsLedgerAndUsesActualObservationAge(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9401, Name: "sync"}).Error)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "sync", Enabled: true, EndTime: now + 86400}, Channels: []int{9401}, Cycles: []model.SubscriptionCycleRule{{Key: "local", Name: "local", Seconds: 18000, NextReset: now + 2000, Limit: 100, Shared: true, Enabled: true}}}, 1)
	require.NoError(t, err)
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id}, nil))
	var sub *model.UserSubscription
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		var e error
		sub, e = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
		return e
	}))
	_, err = model.PreConsumeUserSubscriptionForChannel("sync-before", user.Id, "m", 0, 10, 9401)
	require.NoError(t, err)
	require.NoError(t, model.SettleSubscriptionRequest("sync-before", sub.Id, 0))
	mapping := model.PanelSubscriptionSyncMapping()
	mapping.Cycles = mapping.Cycles[:1]
	source, err := service.SaveSubscriptionSyncSource(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "source", URL: "https://example.com/api/panel/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10}, Mapping: mapping}, 1, true)
	require.NoError(t, err)
	binding := model.SubscriptionSyncBindingInput{SubscriptionSyncBinding: model.SubscriptionSyncBinding{PoolID: pool.Id, SourceID: source.Id, AccountID: "a", Enabled: true, StalePolicy: "stop"}, Cycles: []model.SubscriptionSyncCycleBinding{{LocalKey: "local", UpstreamKey: "5h", AlignReset: true, Enforce: true, Threshold: 0.9, MaxShiftSeconds: 300}}}
	binding.Cycles[0].QuotaPerFullWindow = 1000
	require.NoError(t, model.SaveSubscriptionSyncBinding(binding, 1))
	binding, err = model.GetSubscriptionSyncBinding(pool.Id)
	require.NoError(t, err)
	ratio := 0.62
	account := model.SubscriptionSyncAccount{ID: "a", SampledAt: now, Cycles: []model.SubscriptionSyncCycle{{Key: "5h", Name: "short", Seconds: 18000, Utilization: &ratio, WindowStart: now + 2100 - 18000, WindowEnd: now + 2100, SampledAt: now}}}
	_, err = model.ApplySubscriptionSyncAccount(*source, "", binding, account, 1, true)
	require.NoError(t, err)
	var count int64
	require.NoError(t, model.DB.Model(&model.SubscriptionSyncObservation{}).Count(&count).Error)
	assert.Zero(t, count, "preview cannot insert observations")
	claimed, err := model.ClaimSubscriptionSyncSource(source.Id, "worker", true)
	require.NoError(t, err)
	_, err = model.ApplySubscriptionSyncAccount(*claimed, "worker", binding, account, 1, false)
	require.NoError(t, err)
	require.NoError(t, model.FinishSubscriptionSyncSource(*claimed, "worker", []model.SubscriptionSyncAccount{account}, "", true, 1))
	views, err := model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(10), views[0].Used)
	assert.Zero(t, views[0].Adjustment, "62 percent cannot be converted to local quota")
	assert.Equal(t, now+2100, views[0].NextReset)
	_, err = model.PreConsumeUserSubscriptionForChannel("sync-inflight", user.Id, "m", 0, 5, 9401)
	require.NoError(t, err)
	// Duplicate observations must not create more records or refresh sampled_at.
	claimed, err = model.ClaimSubscriptionSyncSource(source.Id, "again", true)
	require.NoError(t, err)
	_, err = model.ApplySubscriptionSyncAccount(*claimed, "again", binding, account, 1, false)
	require.NoError(t, err)
	require.NoError(t, model.FinishSubscriptionSyncSource(*claimed, "again", []model.SubscriptionSyncAccount{account}, "", false, 1))
	require.NoError(t, model.DB.Model(&model.SubscriptionSyncObservation{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	// A fresh HTTP fetch with an old actual observation blocks new admissions.
	require.NoError(t, model.DB.Model(&model.SubscriptionSyncObservation{}).Where("pool_id = ?", pool.Id).Update("sampled_at", now-301).Error)
	_, err = model.PreConsumeUserSubscriptionForChannel("sync-stale", user.Id, "m", 0, 1, 9401)
	require.ErrorContains(t, err, "subscription quota insufficient")
	require.NoError(t, model.SettleSubscriptionRequest("sync-inflight", sub.Id, 0), "stale upstream data cannot prevent finalization of admitted requests")
	views, err = model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(15), views[0].Used)
	syncViews, err := model.GetSubscriptionSyncViews(pool.Id)
	require.NoError(t, err)
	syncJSON, err := common.Marshal(syncViews)
	require.NoError(t, err)
	var estimates []struct {
		Estimated *float64 `json:"estimated_utilization"`
		Delta     int64    `json:"local_delta"`
	}
	require.NoError(t, common.Unmarshal(syncJSON, &estimates))
	require.Len(t, estimates, 1)
	require.NotNil(t, estimates[0].Estimated, "configured capacity enables upstream baseline plus settled local delta")
	assert.InDelta(t, 0.625, *estimates[0].Estimated, 0.000001)
	assert.EqualValues(t, 5, estimates[0].Delta, "fetching an unchanged snapshot must not discard local usage since calibration")
	// A configuration edit invalidates old workers and prevents stale imports.
	edit := model.SubscriptionSyncSourceInput{SubscriptionSyncSource: *source, Mapping: mapping}
	claimed, err = model.ClaimSubscriptionSyncSource(source.Id, "old-worker", true)
	require.NoError(t, err)
	_, err = service.SaveSubscriptionSyncSource(edit, 1, true)
	require.NoError(t, err)
	_, err = model.ApplySubscriptionSyncAccount(*claimed, "old-worker", binding, account, 1, false)
	assert.Error(t, err)
	personal, err := model.GetSubscriptionUsage(user.Id)
	require.NoError(t, err)
	raw, err := common.Marshal(personal)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "source_id")
	assert.NotContains(t, string(raw), "example.com")
	assert.NotContains(t, string(raw), "account_id")
}

func TestSubscriptionPoolSyncRequiresConfirmationForLargeResetShift(t *testing.T) {
	_, _, now := setupSubscriptionPoolTest(t)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "pending", Enabled: true, EndTime: now + 86400}, Cycles: []model.SubscriptionCycleRule{{Key: "c", Name: "c", Seconds: 18000, NextReset: now + 100, Limit: 100, Shared: true, Enabled: true}}}, 1)
	require.NoError(t, err)
	mapping := model.PanelSubscriptionSyncMapping()
	mapping.Cycles = mapping.Cycles[:1]
	mapping.Cycles[0].Seconds = 25200
	source, err := service.SaveSubscriptionSyncSource(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "pending", URL: "https://example.com/api/panel/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10}, Mapping: mapping}, 1, true)
	require.NoError(t, err)
	binding := model.SubscriptionSyncBindingInput{SubscriptionSyncBinding: model.SubscriptionSyncBinding{PoolID: pool.Id, SourceID: source.Id, AccountID: "a", Enabled: true, StalePolicy: "stop"}, Cycles: []model.SubscriptionSyncCycleBinding{{LocalKey: "c", UpstreamKey: "5h", AlignReset: true, Threshold: 1, MaxShiftSeconds: 60}}}
	require.NoError(t, model.SaveSubscriptionSyncBinding(binding, 1))
	binding, err = model.GetSubscriptionSyncBinding(pool.Id)
	require.NoError(t, err)
	ratio := 0.5
	account := model.SubscriptionSyncAccount{ID: "a", Cycles: []model.SubscriptionSyncCycle{{Key: "5h", Name: "7h", Seconds: 25200, Utilization: &ratio, WindowStart: now + 3600 - 25200, WindowEnd: now + 3600, SampledAt: now}}}
	claimed, err := model.ClaimSubscriptionSyncSource(source.Id, "pending", true)
	require.NoError(t, err)
	_, err = model.ApplySubscriptionSyncAccount(*claimed, "pending", binding, account, 1, false)
	require.NoError(t, err)
	views, err := model.GetSubscriptionSyncViews(pool.Id)
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "Upstream reset shift requires confirmation", views[0].Pending)
	stats, err := model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	assert.Equal(t, now+100, stats[0].NextReset)
	require.Error(t, model.ConfirmSubscriptionSyncReset(pool.Id, "c", "wrong", pool.Version, 1))
	require.NoError(t, model.ConfirmSubscriptionSyncReset(pool.Id, "c", views[0].Digest, pool.Version, 1))
	stats, err = model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	assert.Equal(t, now+3600, stats[0].NextReset)
	assert.EqualValues(t, 25200, stats[0].Seconds)
	assert.False(t, stats[0].Pending)
	require.Error(t, model.ConfirmSubscriptionSyncReset(pool.Id, "c", views[0].Digest, pool.Version, 1), "old pool versions cannot confirm a later state")
}

func TestSubscriptionPoolSyncRejectsRedirectsAndPreservesSecretsOnDestinationEdit(t *testing.T) {
	setupSubscriptionPoolTest(t)
	t.Setenv("SUBSCRIPTION_SYNC_SECRET_KEY", strings.Repeat("private-key-", 4))
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		fmt.Fprint(w, `{"ok":true,"data":{"accounts":[]}}`)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	input := model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "redirect", URL: redirect.URL + "/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10, AllowHTTP: true, PrivateIPs: "127.0.0.1"}, Mapping: model.PanelSubscriptionSyncMapping(), Request: &model.SubscriptionSyncRequest{AuthType: "bearer", Token: "private"}}
	source, err := service.SaveSubscriptionSyncSource(input, 1, true)
	require.NoError(t, err)
	_, err = service.RunSubscriptionSync(context.Background(), source.Id, 1, true)
	require.Error(t, err)
	assert.False(t, reached, "redirect targets must receive neither requests nor credentials")
	input.SubscriptionSyncSource = *source
	input.URL = target.URL + "/usage"
	input.Request = nil
	_, err = service.SaveSubscriptionSyncSource(input, 1, true)
	require.ErrorContains(t, err, "Replace saved request settings")
	var stored model.SubscriptionSyncSource
	require.NoError(t, model.DB.First(&stored, source.Id).Error)
	assert.Equal(t, source.URL, stored.URL)
}

func TestSubscriptionPoolSyncFetchesOnceForMultiplePoolsAndKeepsMissingAccountHistory(t *testing.T) {
	_, _, now := setupSubscriptionPoolTest(t)
	calls := 0
	missing := false
	rateLimited := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if rateLimited {
			w.Header().Set("Retry-After", "900")
			w.WriteHeader(429)
			return
		}
		account := fmt.Sprintf(`{"account_id":"a","utilization_5h":0.5,"reset_5h":%q,"last_probe":%q}`, time.Unix(now+3600, 0).UTC().Format(time.RFC3339), time.Unix(now, 0).UTC().Format(time.RFC3339))
		if !missing {
			account += "," + strings.Replace(account, `"a"`, `"b"`, 1)
		}
		fmt.Fprintf(w, `{"ok":true,"data":{"accounts":[%s]}}`, account)
	}))
	defer server.Close()
	mapping := model.PanelSubscriptionSyncMapping()
	mapping.Cycles = mapping.Cycles[:1]
	source, err := service.SaveSubscriptionSyncSource(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "multi", URL: server.URL + "/api/panel/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10, AllowHTTP: true, PrivateIPs: "127.0.0.1", Enabled: true}, Mapping: mapping}, 1, true)
	require.NoError(t, err)
	ids := []int{}
	for _, id := range []string{"a", "b"} {
		pool, e := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: id, Enabled: true, EndTime: now + 86400}, Cycles: []model.SubscriptionCycleRule{{Key: "c", Name: "c", Seconds: 18000, NextReset: now + 3600, Limit: 100, Enabled: true}}}, 1)
		require.NoError(t, e)
		ids = append(ids, pool.Id)
		binding := model.SubscriptionSyncBindingInput{SubscriptionSyncBinding: model.SubscriptionSyncBinding{PoolID: pool.Id, SourceID: source.Id, AccountID: id, Enabled: true, StalePolicy: "stop"}, Cycles: []model.SubscriptionSyncCycleBinding{{LocalKey: "c", UpstreamKey: "5h", Threshold: 1}}}
		require.NoError(t, model.SaveSubscriptionSyncBinding(binding, 1))
	}
	_, err = service.RunSubscriptionSync(context.Background(), source.Id, 1, true)
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	var count int64
	require.NoError(t, model.DB.Model(&model.SubscriptionSyncObservation{}).Count(&count).Error)
	assert.Zero(t, count)
	_, err = service.RunSubscriptionSync(context.Background(), source.Id, 1, false)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	require.NoError(t, model.DB.Model(&model.SubscriptionSyncObservation{}).Count(&count).Error)
	assert.Equal(t, int64(2), count)
	missing = true
	_, err = service.RunSubscriptionSync(context.Background(), source.Id, 1, false)
	require.NoError(t, err)
	binding, err := model.GetSubscriptionSyncBinding(ids[1])
	require.NoError(t, err)
	assert.Equal(t, "Bound upstream account was not found", binding.LastError)
	observations, err := model.GetSubscriptionSyncViews(ids[1])
	require.NoError(t, err)
	require.Empty(t, observations, "unreported accounts are hidden; immutable observations remain in storage")
	require.NoError(t, model.MigrateSubscriptionPools())
	require.NoError(t, model.MigrateSubscriptionPools())
	require.NoError(t, model.DB.Model(&model.SubscriptionSyncObservation{}).Count(&count).Error)
	assert.Equal(t, int64(2), count, "repeated migrations preserve observations and bindings")
	rateLimited = true
	_, err = service.RunSubscriptionSync(context.Background(), source.Id, 1, false)
	require.Error(t, err)
	var stored model.SubscriptionSyncSource
	require.NoError(t, model.DB.First(&stored, source.Id).Error)
	assert.GreaterOrEqual(t, stored.NextRun, now+900, "Retry-After is honored without an immediate retry")
	assert.Equal(t, "Synchronization upstream rate limit reached", stored.LastError)
	_, err = model.ClaimSubscriptionSyncSource(source.Id, "not-due", false)
	assert.Error(t, err)
	first, err := model.GetSubscriptionSyncBinding(ids[0])
	require.NoError(t, err)
	second, err := model.GetSubscriptionSyncBinding(ids[1])
	require.NoError(t, err)
	second.AccountID = "a"
	require.Error(t, model.SaveSubscriptionSyncBinding(second, 1), "a physical upstream resource cannot be allocated twice")
	require.NoError(t, model.UnbindSubscriptionSyncPool(ids[0], first.Version, 1))
	require.Error(t, model.UnbindSubscriptionSyncPool(ids[0], first.Version, 1), "old versions cannot repeat an unbind")
	require.NoError(t, model.SaveSubscriptionSyncBinding(second, 1), "unbinding explicitly releases the upstream resource")
	observations, err = model.GetSubscriptionSyncViews(ids[1])
	require.NoError(t, err)
	require.Len(t, observations, 1, "rebinding immediately displays the new account cache, not the prior account")
	assert.InDelta(t, 0.5, *observations[0].Utilization, 1e-9)
	require.NoError(t, model.UnbindSubscriptionSyncPool(ids[1], second.Version+1, 1))
	require.NoError(t, model.MigrateSubscriptionPools())
	require.NoError(t, model.MigrateSubscriptionPools())
}

func TestSubscriptionPoolSyncPermissionsDefaultToSuperAdministrator(t *testing.T) {
	setupSubscriptionPoolTest(t)
	for _, action := range []string{"sync_read", "sync_run", "sync_credentials", "sync_binding", "sync_apply"} {
		permission := authz.SubscriptionPoolPermission(action)
		assert.False(t, authz.Can(31, common.RoleAdminUser, permission), action)
		assert.True(t, authz.Can(1, common.RoleRootUser, permission), action)
	}
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Set("id", 1)
		c.Set("role", role)
		c.Request = httptest.NewRequest("GET", "/api/subscription/admin/sync-sources", nil)
		ListSubscriptionSyncSources(c)
		if role == common.RoleRootUser {
			assert.Equal(t, http.StatusOK, response.Code)
		} else {
			assert.Equal(t, http.StatusForbidden, response.Code)
		}
	}
}

func TestSubscriptionPoolSyncRejectsMetadataAndActiveProbeAliases(t *testing.T) {
	input := model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "blocked", URL: "https://example.com/api/panel/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10}, Mapping: model.PanelSubscriptionSyncMapping()}
	for _, address := range []string{"https://169.254.169.254/", "https://100.100.100.200/", "https://168.63.129.16/", "https://[fd00:ec2::254]/", "https://[64:ff9b::a9fe:a9fe]/", "https://example.com/a/../api/panel/usage-probe", "https://example.com//api/panel/probe/", "https://example.com/admin/vms/probe-all/"} {
		input.URL = address
		assert.Error(t, service.ValidateSubscriptionSyncSource(input), address)
	}
}

func TestSubscriptionPoolSyncStatusDoesNotTreatRecentFetchAsFreshObservation(t *testing.T) {
	_, _, now := setupSubscriptionPoolTest(t)
	mapping := model.PanelSubscriptionSyncMapping()
	mapping.Cycles = mapping.Cycles[:1]
	source, err := service.SaveSubscriptionSyncSource(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "age", URL: "https://example.com/api/panel/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10}, Mapping: mapping}, 1, true)
	require.NoError(t, err)
	ratio := 0.5
	accounts := []model.SubscriptionSyncAccount{{ID: "a", Cycles: []model.SubscriptionSyncCycle{{Key: "5h", Name: "short", Seconds: 18000, Utilization: &ratio, WindowStart: now - 10000, WindowEnd: now + 8000, SampledAt: now - 600}}}}
	raw, err := common.Marshal(accounts)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(source).Updates(map[string]any{"catalog_json": string(raw), "last_fetch": now}).Error)
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Set("id", 1)
	c.Set("role", common.RoleRootUser)
	c.Request = httptest.NewRequest("GET", "/api/subscription/admin/sync-sources", nil)
	ListSubscriptionSyncSources(c)
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Sources []model.SubscriptionSyncSourceInput `json:"sources"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	require.True(t, body.Success)
	require.Len(t, body.Data.Sources, 1)
	assert.Equal(t, "Upstream observation is stale", body.Data.Sources[0].ObservationStatus)
	assert.Equal(t, now-600, body.Data.Sources[0].ObservedAt)
	assert.Equal(t, now, body.Data.Sources[0].LastFetch)
}

func TestSubscriptionPoolSyncApplicationFailureIsNotReportedAsSuccess(t *testing.T) {
	_, _, now := setupSubscriptionPoolTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"ok":true,"data":{"accounts":[{"account_id":"a","utilization_5h":0.5,"reset_5h":%q,"last_probe":%q}]}}`, time.Unix(now+3600, 0).UTC().Format(time.RFC3339), time.Unix(now, 0).UTC().Format(time.RFC3339))
	}))
	defer server.Close()
	mapping := model.PanelSubscriptionSyncMapping()
	mapping.Cycles = mapping.Cycles[:1]
	source, err := service.SaveSubscriptionSyncSource(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "failure", URL: server.URL + "/api/panel/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10, AllowHTTP: true, PrivateIPs: "127.0.0.1"}, Mapping: mapping}, 1, true)
	require.NoError(t, err)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "failure", Enabled: true, EndTime: now + 86400}, Cycles: []model.SubscriptionCycleRule{{Key: "c", Name: "c", Seconds: 18000, NextReset: now + 3600, Limit: 100, Enabled: true}}}, 1)
	require.NoError(t, err)
	binding := model.SubscriptionSyncBindingInput{SubscriptionSyncBinding: model.SubscriptionSyncBinding{PoolID: pool.Id, SourceID: source.Id, AccountID: "a", Enabled: true, StalePolicy: "stop"}, Cycles: []model.SubscriptionSyncCycleBinding{{LocalKey: "c", UpstreamKey: "5h", Threshold: 1}}}
	require.NoError(t, model.SaveSubscriptionSyncBinding(binding, 1))
	// Simulate unreadable persisted state, not a legitimate configuration race.
	require.NoError(t, model.DB.Model(&model.SubscriptionSyncBinding{}).Where("pool_id = ?", pool.Id).Update("cycles_json", "invalid").Error)
	_, err = service.RunSubscriptionSync(context.Background(), source.Id, 1, false)
	require.Error(t, err, "application failures must not produce successful sync responses")
	assert.EqualError(t, err, "Synchronization could not apply pool changes")
	var stored model.SubscriptionSyncSource
	require.NoError(t, model.DB.First(&stored, source.Id).Error)
	assert.Equal(t, "Synchronization could not apply pool changes", stored.LastError)
	assert.Zero(t, stored.LeaseUntil, "a failed application releases the scheduling lease")
	assert.Empty(t, stored.LeaseToken)
	assert.Equal(t, 1, stored.Failures)
	assert.Zero(t, stored.LastFetch)
	assert.Greater(t, stored.NextRun, now)
	var events []model.SubscriptionSyncEvent
	require.NoError(t, model.DB.Where("source_id = ? AND action = ?", source.Id, "sync_failed").Find(&events).Error)
	require.Len(t, events, 1)
	assert.Equal(t, stored.LastError, events[0].Message)
}

func TestSubscriptionPoolSyncTokenRecoveryDoesNotLoginOnPreviewForbiddenOrRateLimit(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			setupSubscriptionPoolTest(t)
			t.Setenv("SUBSCRIPTION_SYNC_SECRET_KEY", strings.Repeat("k", 32))
			logins := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/panel/login" {
					logins++
					fmt.Fprint(w, `{"ok":true,"token":"new-token","expires_in":604800}`)
					return
				}
				if r.Header.Get("Authorization") == "Bearer new-token" {
					fmt.Fprint(w, `{"ok":true,"data":{"accounts":[]}}`)
					return
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			source, err := service.SaveSubscriptionSyncSource(model.SubscriptionSyncSourceInput{
				SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "auth", URL: server.URL + "/api/panel/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10, AllowHTTP: true, PrivateIPs: "127.0.0.1", RefreshMode: "login", RefreshURL: server.URL + "/api/panel/login", AutoRefresh: true, RefreshBefore: 86400},
				Mapping:                model.PanelSubscriptionSyncMapping(), Request: &model.SubscriptionSyncRequest{AuthType: "bearer", Token: "old-token", Username: "quota", Password: "password-secret"},
			}, 1, true)
			require.NoError(t, err)
			_, err = service.RunSubscriptionSync(context.Background(), source.Id, 1, true)
			require.Error(t, err)
			assert.Zero(t, logins, "preview must never reauthenticate")
			_, err = service.RunSubscriptionSync(context.Background(), source.Id, 1, false)
			if status == 401 {
				require.NoError(t, err)
				assert.Equal(t, 1, logins)
			} else {
				require.Error(t, err)
				assert.Zero(t, logins)
			}
			var stored model.SubscriptionSyncSource
			require.NoError(t, model.DB.First(&stored, source.Id).Error)
			if status == 429 {
				_, refreshErr := service.ManageSubscriptionSyncAuthentication(context.Background(), source.Id, 1, "refresh", "", stored.Version, stored.CredentialRevision)
				require.Error(t, refreshErr, "manual renewal must respect upstream throttling")
				assert.Zero(t, logins)
			}
			assert.Equal(t, source.Version, stored.Version, "credential rotation must not invalidate observations")
			encoded, err := common.Marshal(stored)
			require.NoError(t, err)
			for _, secret := range []string{"old-token", "new-token", "password-secret"} {
				assert.NotContains(t, string(encoded), secret)
				assert.NotContains(t, stored.SecretPayload, secret)
			}
		})
	}
}

func TestSubscriptionPoolSyncTokenValidationFailurePreservesPriorCredentials(t *testing.T) {
	setupSubscriptionPoolTest(t)
	t.Setenv("SUBSCRIPTION_SYNC_SECRET_KEY", strings.Repeat("k", 32))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/panel/login" {
			fmt.Fprint(w, `{"ok":true,"token":"rejected","expires_in":604800}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer valid" {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"ok":true,"data":{"accounts":[]}}`)
	}))
	defer server.Close()
	source, err := service.SaveSubscriptionSyncSource(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "auth", URL: server.URL + "/api/panel/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10, AllowHTTP: true, PrivateIPs: "127.0.0.1", RefreshMode: "login", RefreshURL: server.URL + "/api/panel/login"}, Mapping: model.PanelSubscriptionSyncMapping(), Request: &model.SubscriptionSyncRequest{AuthType: "bearer", Token: "valid", Username: "quota", Password: "password"}}, 1, true)
	require.NoError(t, err)
	_, err = service.ManageSubscriptionSyncAuthentication(context.Background(), source.Id, 1, "refresh", "", source.Version, source.CredentialRevision)
	require.Error(t, err)
	var stored model.SubscriptionSyncSource
	require.NoError(t, model.DB.First(&stored, source.Id).Error)
	assert.Equal(t, source.SecretPayload, stored.SecretPayload)
	assert.Equal(t, source.CredentialRevision, stored.CredentialRevision)
	assert.Zero(t, stored.TokenExpiresAt)
	_, err = service.RunSubscriptionSync(context.Background(), source.Id, 1, true)
	require.NoError(t, err)
}

func TestSubscriptionPoolSyncTokenReplacementAndLoginExpiry(t *testing.T) {
	setupSubscriptionPoolTest(t)
	t.Setenv("SUBSCRIPTION_SYNC_SECRET_KEY", strings.Repeat("k", 32))
	loginCalls, readCalls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/panel/login" {
			var body map[string]string
			require.NoError(t, common.DecodeJson(r.Body, &body))
			assert.Equal(t, map[string]string{"username": "quota", "password": "login-password"}, body)
			loginCalls++
			fmt.Fprint(w, `{"ok":true,"token":"fresh-token","expires_in":604800}`)
			return
		}
		readCalls++
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"ok":true,"data":{"accounts":[]}}`)
	}))
	defer server.Close()
	input := model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "auth", URL: server.URL + "/api/panel/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10, AllowHTTP: true, PrivateIPs: "127.0.0.1", RefreshMode: "login", RefreshURL: server.URL + "/api/panel/login", AutoRefresh: true}, Mapping: model.PanelSubscriptionSyncMapping(), Request: &model.SubscriptionSyncRequest{AuthType: "bearer", Token: "imported", Username: "quota", Password: "login-password"}}
	source, err := service.SaveSubscriptionSyncSource(input, 1, true)
	require.NoError(t, err)
	checked, err := service.ManageSubscriptionSyncAuthentication(context.Background(), source.Id, 1, "check", "", source.Version, source.CredentialRevision)
	require.NoError(t, err)
	assert.Zero(t, checked.TokenExpiresAt, "import time is not issue time")
	assert.Equal(t, source.CredentialRevision, checked.CredentialRevision)
	before := model.GetDBTimestamp()
	updated, err := service.ManageSubscriptionSyncAuthentication(context.Background(), source.Id, 1, "refresh", "", source.Version, source.CredentialRevision)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, updated.TokenExpiresAt, before+604800)
	assert.LessOrEqual(t, updated.TokenExpiresAt, model.GetDBTimestamp()+604800)
	assert.Equal(t, source.Version, updated.Version)
	assert.Equal(t, source.CredentialRevision+1, updated.CredentialRevision)
	assert.Equal(t, 1, loginCalls)
	assert.Equal(t, 2, readCalls, "check and candidate validation each use a single cached read")
	// Imports are validated and explicitly lose unknown expiry information.
	replaced, err := service.ManageSubscriptionSyncAuthentication(context.Background(), source.Id, 1, "replace", "manual-token", updated.Version, updated.CredentialRevision)
	require.NoError(t, err)
	assert.Zero(t, replaced.TokenExpiresAt)
	_, err = service.ManageSubscriptionSyncAuthentication(context.Background(), source.Id, 1, "replace", "stale-token", updated.Version, updated.CredentialRevision)
	require.Error(t, err)
	input.SubscriptionSyncSource = *source
	_, err = service.SaveSubscriptionSyncSource(input, 1, true)
	require.Error(t, err, "a stale credentials editor cannot overwrite a rotated token")
	var events []model.SubscriptionSyncEvent
	require.NoError(t, model.DB.Where("source_id = ?", source.Id).Find(&events).Error)
	raw, err := common.Marshal(events)
	require.NoError(t, err)
	for _, secret := range []string{"imported", "fresh-token", "manual-token", "login-password"} {
		assert.NotContains(t, string(raw), secret)
	}
}

func TestSubscriptionPoolSyncTokenRejectsUnsafeRenewalDestinations(t *testing.T) {
	base := model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "auth", URL: "https://panel.example/api/panel/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10, RefreshMode: "login", RefreshURL: "https://panel.example/api/panel/login"}, Mapping: model.PanelSubscriptionSyncMapping(), Request: &model.SubscriptionSyncRequest{AuthType: "bearer", Username: "quota", Password: "password"}}
	require.NoError(t, service.ValidateSubscriptionSyncSource(base))
	for _, change := range []func(*model.SubscriptionSyncSourceInput){
		func(s *model.SubscriptionSyncSourceInput) { s.RefreshURL = "https://other.example/login" },
		func(s *model.SubscriptionSyncSourceInput) {
			s.URL = "http://panel.example/api/panel/usage"
			s.RefreshURL = "http://panel.example/api/panel/login"
			s.AllowHTTP = true
		},
		func(s *model.SubscriptionSyncSourceInput) {
			s.RefreshURL = "https://panel.example/api/panel/usage-probe"
		},
		func(s *model.SubscriptionSyncSourceInput) {
			s.RefreshURL = "https://panel.example/api/panel/login?token=secret"
		},
		func(s *model.SubscriptionSyncSourceInput) { s.Method = "POST" },
	} {
		input := base
		change(&input)
		assert.Error(t, service.ValidateSubscriptionSyncSource(input))
	}
}

func TestSubscriptionPoolSyncTokenConcurrentRefreshAndConfigurationCAS(t *testing.T) {
	setupSubscriptionPoolTest(t)
	t.Setenv("SUBSCRIPTION_SYNC_SECRET_KEY", strings.Repeat("k", 32))
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			close(entered)
			<-release
			fmt.Fprint(w, `{"ok":true,"token":"candidate","expires_in":604800}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"data":{"accounts":[]}}`)
	}))
	defer server.Close()
	input := model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "auth", URL: server.URL + "/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10, AllowHTTP: true, PrivateIPs: "127.0.0.1", RefreshMode: "login", RefreshURL: server.URL + "/login"}, Mapping: model.PanelSubscriptionSyncMapping(), Request: &model.SubscriptionSyncRequest{AuthType: "bearer", Token: "original", Username: "quota", Password: "password"}}
	source, err := service.SaveSubscriptionSyncSource(input, 1, true)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, e := service.ManageSubscriptionSyncAuthentication(context.Background(), source.Id, 1, "refresh", "", source.Version, source.CredentialRevision)
		done <- e
	}()
	<-entered
	_, err = service.ManageSubscriptionSyncAuthentication(context.Background(), source.Id, 1, "refresh", "", source.Version, source.CredentialRevision)
	assert.Error(t, err, "multiple instances cannot issue concurrently")
	input.SubscriptionSyncSource = *source
	input.Request = nil
	input.Name = "edited while issuing"
	saved, saveErr := service.SaveSubscriptionSyncSource(input, 1, true)
	close(release)
	require.NoError(t, saveErr)
	require.Error(t, <-done, "late network completion cannot overwrite a configuration edit")
	var stored model.SubscriptionSyncSource
	require.NoError(t, model.DB.First(&stored, source.Id).Error)
	assert.Equal(t, saved.Version, stored.Version)
	assert.Equal(t, source.SecretPayload, stored.SecretPayload)
}

func TestSubscriptionPoolSyncOAuthRotationAndAmbiguousResponseAreNotReplayed(t *testing.T) {
	for _, failValidation := range []bool{false, true} {
		t.Run(fmt.Sprint(failValidation), func(t *testing.T) {
			setupSubscriptionPoolTest(t)
			t.Setenv("SUBSCRIPTION_SYNC_SECRET_KEY", strings.Repeat("k", 32))
			received := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					require.NoError(t, r.ParseForm())
					assert.Equal(t, "refresh_token", r.Form.Get("grant_type"))
					received = append(received, r.Form.Get("refresh_token"))
					fmt.Fprint(w, `{"access_token":"access-next","token_type":"Bearer","expires_in":3600,"refresh_token":"refresh-next"}`)
					return
				}
				if failValidation {
					w.WriteHeader(401)
					return
				}
				fmt.Fprint(w, `{"ok":true,"data":{"accounts":[]}}`)
			}))
			defer server.Close()
			source, err := service.SaveSubscriptionSyncSource(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "oauth", URL: server.URL + "/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10, AllowHTTP: true, PrivateIPs: "127.0.0.1", RefreshMode: "oauth", RefreshURL: server.URL + "/token"}, Mapping: model.PanelSubscriptionSyncMapping(), Request: &model.SubscriptionSyncRequest{AuthType: "bearer", Token: "access-old", RefreshToken: "refresh-old", ClientID: "client", ClientSecret: "secret"}}, 1, true)
			require.NoError(t, err)
			updated, err := service.ManageSubscriptionSyncAuthentication(context.Background(), source.Id, 1, "refresh", "", source.Version, source.CredentialRevision)
			if failValidation {
				require.Error(t, err)
				var stored model.SubscriptionSyncSource
				require.NoError(t, model.DB.First(&stored, source.Id).Error)
				assert.True(t, stored.RefreshBlocked)
				assert.Equal(t, source.SecretPayload, stored.SecretPayload)
				require.NoError(t, model.DB.Model(&stored).Update("auth_next_attempt", 0).Error)
				_, err = service.ManageSubscriptionSyncAuthentication(context.Background(), source.Id, 1, "refresh", "", source.Version, source.CredentialRevision)
				require.Error(t, err)
				assert.Equal(t, []string{"refresh-old"}, received)
			} else {
				require.NoError(t, err)
				assert.False(t, updated.RefreshBlocked)
				require.NoError(t, model.DB.Model(updated).Update("auth_next_attempt", 0).Error)
				_, err = service.ManageSubscriptionSyncAuthentication(context.Background(), source.Id, 1, "refresh", "", updated.Version, updated.CredentialRevision)
				require.NoError(t, err)
				assert.Equal(t, []string{"refresh-old", "refresh-next"}, received)
			}
		})
	}
}

func TestSubscriptionPoolSyncCredentialActionsRequireBoundSingleUseProof(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleRootUser).Error)
	require.NoError(t, model.MigrateSubscriptionPools())
	t.Setenv("SUBSCRIPTION_SYNC_SECRET_KEY", strings.Repeat("k", 32))
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		fmt.Fprint(w, `{"ok":true,"data":{"accounts":[]}}`)
	}))
	defer server.Close()
	source, err := service.SaveSubscriptionSyncSource(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "proof", URL: server.URL + "/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10, AllowHTTP: true, PrivateIPs: "127.0.0.1"}, Mapping: model.PanelSubscriptionSyncMapping(), Request: &model.SubscriptionSyncRequest{AuthType: "bearer", Token: "imported"}}, user.Id, true)
	require.NoError(t, err)
	context := service.SubscriptionSyncCredentialContext{SourceID: source.Id, Action: "check", Version: source.Version, Revision: source.CredentialRevision}
	raw, err := common.Marshal(context)
	require.NoError(t, err)
	proof := issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopeSubscriptionSyncCredentials, Context: raw}, "password")
	handler := func(c *gin.Context) {
		c.Set("role", common.RoleRootUser)
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(source.Id)}}
		ManageSubscriptionSyncAuthentication(c)
	}
	body := fmt.Sprintf(`{"action":"check","version":%d,"credential_revision":%d,"confirm":true}`, source.Version, source.CredentialRevision)
	missing := securityEnrollmentRequest("POST", "/sync/auth", body, "", identity, handler)
	assert.Equal(t, 403, missing.Code)
	assert.Zero(t, reads)
	wrong := strings.Replace(body, `"action":"check"`, `"action":"refresh"`, 1)
	rejected := securityEnrollmentRequest("POST", "/sync/auth", wrong, proof, identity, handler)
	assert.Equal(t, 403, rejected.Code)
	assert.Zero(t, reads)
	success := securityEnrollmentRequest("POST", "/sync/auth", body, proof, identity, handler)
	assert.Equal(t, 200, success.Code)
	assert.Contains(t, success.Body.String(), `"success":true`)
	assert.Equal(t, 1, reads)
	replay := securityEnrollmentRequest("POST", "/sync/auth", body, proof, identity, handler)
	assert.Equal(t, 403, replay.Code)
	assert.Equal(t, 1, reads)
}

func TestSubscriptionPoolSyncAuthenticationMigrationPreservesLegacyCredentials(t *testing.T) {
	setupSubscriptionPoolTest(t)
	t.Setenv("SUBSCRIPTION_SYNC_SECRET_KEY", strings.Repeat("k", 32))
	input := model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "legacy-auth", URL: "https://example.com/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10}, Mapping: model.PanelSubscriptionSyncMapping(), Request: &model.SubscriptionSyncRequest{AuthType: "bearer", Token: "legacy-private-token"}}
	source, err := service.SaveSubscriptionSyncSource(input, 1, true)
	require.NoError(t, err)
	// Simulate the previous synchronization schema with representative encrypted
	// data, then run the real startup migration twice on each database dialect.
	for _, column := range []string{"RefreshMode", "RefreshURL", "AutoRefresh", "RefreshBefore", "CredentialRevision", "TokenExpiresAt", "LastAuthSuccess", "LastTokenRefresh", "AuthError", "AuthNextAttempt", "AuthLeaseUntil", "AuthLeaseToken", "RefreshBlocked"} {
		require.NoError(t, model.DB.Migrator().DropColumn(&model.SubscriptionSyncSource{}, column))
	}
	require.NoError(t, model.MigrateSubscriptionPools())
	require.NoError(t, model.MigrateSubscriptionPools())
	var stored model.SubscriptionSyncSource
	require.NoError(t, model.DB.First(&stored, source.Id).Error)
	assert.Equal(t, source.SecretPayload, stored.SecretPayload)
	assert.Equal(t, source.Version, stored.Version)
	assert.Zero(t, stored.TokenExpiresAt)
	assert.False(t, stored.AutoRefresh)
	require.NoError(t, model.ClaimSubscriptionSyncAuthentication(stored, "legacy-check", "check"))
	require.NoError(t, model.FinishSubscriptionSyncAuthentication(stored, "legacy-check", "check", nil, 0, "", 0, 1))
	input.SubscriptionSyncSource = stored
	input.Request = nil
	saved, err := service.SaveSubscriptionSyncSource(input, 1, true)
	require.NoError(t, err)
	assert.Equal(t, "manual", saved.RefreshMode)
	assert.Equal(t, source.SecretPayload, saved.SecretPayload)
	assert.Equal(t, source.Version+1, saved.Version)
}

func TestSubscriptionPoolSyncCreateImportsCyclesAtomically(t *testing.T) {
	_, _, now := setupSubscriptionPoolTest(t)
	ratio := 0.4
	accounts := []model.SubscriptionSyncAccount{{ID: "car-account", Name: "Car", Expiry: now + 86400, SampledAt: now, Cycles: []model.SubscriptionSyncCycle{
		{Key: "5h", Name: "5h", Seconds: 18000, WindowStart: now - 17000, WindowEnd: now + 1000, SampledAt: now, Utilization: &ratio},
		{Key: "missing", Name: "Missing", Seconds: 604800, Error: "Missing or inconsistent upstream cycle data"},
	}}}
	catalog, err := common.Marshal(accounts)
	require.NoError(t, err)
	mapping, err := common.Marshal(model.PanelSubscriptionSyncMapping())
	require.NoError(t, err)
	source := model.SubscriptionSyncSource{Name: "create-source", URL: "https://example.com/usage", Version: 1, IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10, MappingJSON: string(mapping), CatalogJSON: string(catalog), LastFetch: now}
	require.NoError(t, model.DB.Create(&source).Error)
	var input model.SubscriptionPoolInput
	require.NoError(t, common.Unmarshal([]byte(fmt.Sprintf(`{"name":"auto-car","enabled":true,"sync":{"source_id":%d,"source_version":1,"account_id":"car-account","sync_expiry":true}}`, source.Id)), &input))
	pool, err := model.SaveSubscriptionPool(input, 1)
	require.NoError(t, err, "creating from upstream does not require manually typed cycles or expiry")
	config, err := model.GetSubscriptionPoolConfiguration(pool.Id)
	require.NoError(t, err)
	require.Len(t, config.Cycles, 1, "unreported optional model windows must not be invented")
	assert.Equal(t, now+1000, config.Cycles[0].NextReset)
	assert.Equal(t, int64(18000), config.Cycles[0].Seconds)
	assert.Equal(t, now+86400, config.EndTime)
	assert.True(t, config.Cycles[0].Unlimited, "percentages must never invent quota unit capacities")
	assert.False(t, config.Cycles[0].Shared, "cycle scope remains independently configurable")
	binding, err := model.GetSubscriptionSyncBinding(pool.Id)
	require.NoError(t, err)
	assert.Equal(t, "car-account", binding.AccountID)
	require.Len(t, binding.Cycles, 1)
	assert.True(t, binding.Cycles[0].AlignReset)
	assert.True(t, binding.Cycles[0].Enforce)
	views, err := model.GetSubscriptionSyncViews(pool.Id)
	require.NoError(t, err)
	require.Len(t, views, 1, "the first saved snapshot is visible without a second manual sync")
	assert.InDelta(t, 0.4, *views[0].Utilization, 0.000001)
	_, err = model.SaveSubscriptionPool(input, 1)
	require.Error(t, err, "one upstream account must not be assigned to duplicate pools")
	var count int64
	require.NoError(t, model.DB.Model(&model.SubscriptionPool{}).Count(&count).Error)
	assert.EqualValues(t, 1, count, "a failed binding must roll back the newly created pool")

	// A second physical account can import defaults while retaining explicitly
	// edited local behavior and extra local-only cycles.
	accounts[0].ID = "manual-account"
	accounts[0].Expiry = 0
	catalog, err = common.Marshal(accounts)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&source).Update("catalog_json", string(catalog)).Error)
	input.Sync.AccountID = "manual-account"
	input.Sync.SyncExpiry = false
	input.Cycles = []model.SubscriptionCycleRule{
		{Key: "5h", Name: "Edited cycle", Seconds: 3600, NextReset: now + 500, Limit: 200, Shared: true, Enabled: true},
		{Key: "local", Name: "Local budget", Seconds: 86400, NextReset: now + 2000, Limit: 500, Enabled: true},
	}
	manual, err := model.SaveSubscriptionPool(input, 1)
	require.NoError(t, err)
	assert.EqualValues(t, 253402300799, manual.EndTime, "missing upstream expiry does not invent a fixed lifetime")
	manualConfig, err := model.GetSubscriptionPoolConfiguration(manual.Id)
	require.NoError(t, err)
	assert.Equal(t, input.Cycles, manualConfig.Cycles)
	manualBinding, err := model.GetSubscriptionSyncBinding(manual.Id)
	require.NoError(t, err)
	require.Len(t, manualBinding.Cycles, 1, "extra local-only cycles do not acquire implicit upstream bindings")
	assert.False(t, manualBinding.Cycles[0].AlignReset, "explicit reset and duration overrides disable automatic alignment")
	assert.False(t, manualBinding.SyncExpiry)
}

func TestSubscriptionPoolSyncStaleThresholdIncludesPollingHeadroom(t *testing.T) {
	_, _, now := setupSubscriptionPoolTest(t)
	ratio := 0.5
	catalog, err := common.Marshal([]model.SubscriptionSyncAccount{{ID: "a", Cycles: []model.SubscriptionSyncCycle{{Key: "5h", SampledAt: now - 65, WindowEnd: now + 1000, Utilization: &ratio}}}})
	require.NoError(t, err)
	mapping, err := common.Marshal(model.PanelSubscriptionSyncMapping())
	require.NoError(t, err)
	source := model.SubscriptionSyncSource{Name: "threshold", IntervalSeconds: 60, StaleSeconds: 60, TimeoutSeconds: 10, MappingJSON: string(mapping), CatalogJSON: string(catalog), LastFetch: now}
	require.NoError(t, model.DB.Create(&source).Error)
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Set("id", 1)
	c.Set("role", common.RoleRootUser)
	c.Request = httptest.NewRequest("GET", "/sync-sources", nil)
	ListSubscriptionSyncSources(c)
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Sources []struct {
				ObservationStatus     string `json:"observation_status"`
				EffectiveStaleSeconds int64  `json:"effective_stale_seconds"`
				FetchStatus           string `json:"fetch_status"`
			} `json:"sources"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	require.True(t, body.Success)
	require.Len(t, body.Data.Sources, 1)
	assert.Equal(t, "Upstream observations are current", body.Data.Sources[0].ObservationStatus)
	assert.GreaterOrEqual(t, body.Data.Sources[0].EffectiveStaleSeconds, int64(140))
	assert.Equal(t, "Synchronization is on schedule", body.Data.Sources[0].FetchStatus)
}

func TestSubscriptionPoolSyncLocalEstimatesRebaseAndInvalidateAfterReset(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9499, Name: "estimate"}).Error)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "estimate", Enabled: true, EndTime: now + 86400}, Channels: []int{9499}, Cycles: []model.SubscriptionCycleRule{{Key: "5h", Name: "5h", Seconds: 18000, NextReset: now + 1000, Unlimited: true, Enabled: true}}}, 1)
	require.NoError(t, err)
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id}, nil))
	var sub *model.UserSubscription
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		var e error
		sub, e = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
		return e
	}))
	mapping := model.PanelSubscriptionSyncMapping()
	mapping.Cycles = mapping.Cycles[:1]
	source, err := service.SaveSubscriptionSyncSource(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "estimate-source", URL: "https://example.com/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10}, Mapping: mapping}, 1, true)
	require.NoError(t, err)
	binding := model.SubscriptionSyncBindingInput{SubscriptionSyncBinding: model.SubscriptionSyncBinding{PoolID: pool.Id, SourceID: source.Id, AccountID: "a", Enabled: true, StalePolicy: "stop"}, Cycles: []model.SubscriptionSyncCycleBinding{{LocalKey: "5h", UpstreamKey: "5h", AlignReset: true, Enforce: true, Threshold: 0.9, MaxShiftSeconds: 300, QuotaPerFullWindow: 100}}}
	require.NoError(t, model.SaveSubscriptionSyncBinding(binding, 1))
	binding, err = model.GetSubscriptionSyncBinding(pool.Id)
	require.NoError(t, err)
	ratio := 0.4
	account := model.SubscriptionSyncAccount{ID: "a", SampledAt: now, Cycles: []model.SubscriptionSyncCycle{{Key: "5h", Seconds: 18000, WindowStart: now - 17000, WindowEnd: now + 1000, SampledAt: now, Utilization: &ratio}}}
	apply := func(token string) {
		t.Helper()
		claimed, e := model.ClaimSubscriptionSyncSource(source.Id, token, true)
		require.NoError(t, e)
		_, e = model.ApplySubscriptionSyncAccount(*claimed, token, binding, account, 1, false)
		require.NoError(t, e)
		require.NoError(t, model.FinishSubscriptionSyncSource(*claimed, token, []model.SubscriptionSyncAccount{account}, "", true, 1))
	}
	apply("initial")
	// A pre-upgrade worker may have persisted the old, shorter age threshold.
	// This derived status must not outlive the newly effective freshness window.
	require.NoError(t, model.DB.Model(&model.SubscriptionSyncObservation{}).Where("pool_id = ?", pool.Id).Update("pending", "Upstream observation is stale").Error)
	legacyViews, err := model.GetSubscriptionSyncViews(pool.Id)
	require.NoError(t, err)
	require.Empty(t, legacyViews[0].Pending)
	_, err = model.PreConsumeUserSubscriptionForChannel("estimate-actual", user.Id, "m", 0, 10, 9499)
	require.NoError(t, err)
	views, err := model.GetSubscriptionSyncViews(pool.Id)
	require.NoError(t, err)
	require.NotNil(t, views[0].EstimatedUtilization)
	assert.InDelta(t, 0.4, *views[0].EstimatedUtilization, 0.000001, "inflight reservations are not displayed as settled use")
	require.NoError(t, model.SettleSubscriptionRequest("estimate-actual", sub.Id, 5))
	views, err = model.GetSubscriptionSyncViews(pool.Id)
	require.NoError(t, err)
	assert.InDelta(t, 0.55, *views[0].EstimatedUtilization, 0.000001)
	_, err = model.PreConsumeUserSubscriptionForChannel("estimate-too-large", user.Id, "m", 0, 36, 9499)
	require.ErrorContains(t, err, "subscription quota insufficient")
	ratio = 0.5
	account.Cycles[0].SampledAt = now + 1
	apply("rebase")
	views, err = model.GetSubscriptionSyncViews(pool.Id)
	require.NoError(t, err)
	assert.InDelta(t, 0.5, *views[0].EstimatedUtilization, 0.000001)
	assert.Zero(t, views[0].LocalDelta)
	statistics, err := model.GetSubscriptionPoolStatistics(pool.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 15, statistics[0].Used)
	assert.Zero(t, statistics[0].Adjustment, "calibration never rewrites real charges")
	require.NoError(t, model.ResetSubscriptionCycle(0, pool.Id, 0, "5h", 1))
	views, err = model.GetSubscriptionSyncViews(pool.Id)
	require.NoError(t, err)
	assert.Nil(t, views[0].EstimatedUtilization, "reset generations cannot reuse a previous local baseline")
}

func TestSubscriptionPoolSyncFiltersNoiseAndKeepsAccountFreshnessSeparate(t *testing.T) {
	_, _, now := setupSubscriptionPoolTest(t)
	mapping := model.PanelSubscriptionSyncMapping()
	mapping.Cycles[0].Seconds = 7 * 3600
	body := fmt.Sprintf(`{"ok":true,"data":{"accounts":[{"account_id":"a","vm_id":"vm-01","utilization_5h":0,"reset_5h":%q,"last_probe":%q},{"account_id":"b","vm_id":"vm-02","utilization_5h":0.2,"reset_5h":%q,"last_probe":%q},{"account_id":"empty","vm_id":"empty","utilization_5h":null}]}}`, time.Unix(now+1000, 0).UTC().Format(time.RFC3339), time.Unix(now-30, 0).UTC().Format(time.RFC3339), time.Unix(now+1000, 0).UTC().Format(time.RFC3339), time.Unix(now-400, 0).UTC().Format(time.RFC3339))
	accounts, err := service.ParseSubscriptionSyncResponse([]byte(body), mapping, now)
	require.NoError(t, err)
	require.Len(t, accounts, 2, "empty accounts are not subscriptions")
	require.Len(t, accounts[0].Cycles, 1)
	assert.Equal(t, "7h", accounts[0].Cycles[0].Name, "duration is a value, not the protocol field name")
	assert.Zero(t, *accounts[0].Cycles[0].Utilization, "an explicit zero is reported usage")
	raw, err := common.Marshal(accounts)
	require.NoError(t, err)
	config, err := common.Marshal(mapping)
	require.NoError(t, err)
	source := model.SubscriptionSyncSource{Name: "freshness", Version: 1, MappingJSON: string(config), CatalogJSON: string(raw), LastFetch: now, IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10}
	require.NoError(t, model.DB.Create(&source).Error)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 1)
	c.Set("role", common.RoleRootUser)
	ListSubscriptionSyncSources(c)
	var result struct {
		Data struct {
			Sources []struct {
				FreshCycles      int      `json:"fresh_cycles"`
				ReportedCycles   int      `json:"reported_cycles"`
				OldestObservedAt int64    `json:"oldest_observed_at"`
				StaleAccounts    []string `json:"stale_accounts"`
			} `json:"sources"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result.Data.Sources, 1)
	assert.Equal(t, 1, result.Data.Sources[0].FreshCycles)
	assert.Equal(t, 2, result.Data.Sources[0].ReportedCycles)
	assert.Equal(t, now-400, result.Data.Sources[0].OldestObservedAt)
	assert.Equal(t, []string{"vm-02"}, result.Data.Sources[0].StaleAccounts)
}

func TestSubscriptionPoolSyncHidesPlaceholderZeroCatalogsWithoutDiscardingRealZero(t *testing.T) {
	_, _, now := setupSubscriptionPoolTest(t)
	zero := 0.0
	accounts := []model.SubscriptionSyncAccount{
		{ID: "shell", Name: "vm-shell", Cycles: []model.SubscriptionSyncCycle{{Key: "short", Utilization: &zero}}},
		{ID: "null", Cycles: []model.SubscriptionSyncCycle{{Key: "short"}}},
		{ID: "real", Name: "vm-02", Cycles: []model.SubscriptionSyncCycle{{Key: "short", Utilization: &zero, SampledAt: now - 400, WindowEnd: now + 1000}}},
	}
	raw, err := common.Marshal(accounts)
	require.NoError(t, err)
	source := model.SubscriptionSyncSource{Name: "cached", CatalogJSON: string(raw)}
	require.NoError(t, model.DB.Create(&source).Error)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 1)
	c.Set("role", common.RoleRootUser)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", source.Id)}}
	GetSubscriptionSyncSourceState(c)
	var result struct {
		Data struct {
			Accounts []model.SubscriptionSyncAccount `json:"accounts"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result.Data.Accounts, 1)
	assert.Equal(t, "real", result.Data.Accounts[0].ID)
	assert.Zero(t, *result.Data.Accounts[0].Cycles[0].Utilization)
	assert.Equal(t, now-400, result.Data.Accounts[0].Cycles[0].SampledAt, "a stale real account remains visible with its true observation time")
}

func TestSubscriptionPoolSyncBindingImmediatelyFeedsPurchasedMemberViews(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	ratio := 0.37
	mapping := model.PanelSubscriptionSyncMapping()
	mapping.Cycles[0].Seconds = 25200
	rawMapping, err := common.Marshal(mapping)
	require.NoError(t, err)
	accounts := []model.SubscriptionSyncAccount{{ID: "member-account", Name: "vm-02", Expiry: now + 172800, SampledAt: now, Cycles: []model.SubscriptionSyncCycle{{Key: "5h", Name: "7h", Seconds: 25200, WindowStart: now - 24200, WindowEnd: now + 1000, SampledAt: now, Utilization: &ratio}}}}
	raw, err := common.Marshal(accounts)
	require.NoError(t, err)
	source := model.SubscriptionSyncSource{Name: "member-source", URL: "https://example.com/usage", Version: 1, MappingJSON: string(rawMapping), CatalogJSON: string(raw), LastFetch: now, IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10}
	require.NoError(t, model.DB.Create(&source).Error)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "member-pool", Enabled: true, EndTime: now + 86400}, Cycles: []model.SubscriptionCycleRule{{Key: "local", Name: "5h", Seconds: 18000, NextReset: now + 1100, Limit: 1000, Enabled: true}}}, 1)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9501, Name: "adoption"}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionPoolChannel{PoolID: pool.Id, ChannelID: 9501}).Error)
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id}, nil))
	var member *model.UserSubscription
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		var e error
		member, e = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
		return e
	}))
	_, err = model.PreConsumeUserSubscriptionForChannel("adopt-settled", user.Id, "m", 0, 10, 9501)
	require.NoError(t, err)
	require.NoError(t, model.SettleSubscriptionRequest("adopt-settled", member.Id, 0))
	_, err = model.PreConsumeUserSubscriptionForChannel("adopt-inflight", user.Id, "m", 0, 5, 9501)
	require.NoError(t, err)
	binding := model.SubscriptionSyncBindingInput{SubscriptionSyncBinding: model.SubscriptionSyncBinding{PoolID: pool.Id, SourceID: source.Id, AccountID: "member-account", Enabled: true, SyncExpiry: true, StalePolicy: "stop"}, Cycles: []model.SubscriptionSyncCycleBinding{{LocalKey: "local", UpstreamKey: "5h", AlignReset: true, Enforce: true, Threshold: 1, MaxShiftSeconds: 300}}}
	require.NoError(t, model.SaveSubscriptionSyncBinding(binding, 1))
	views, err := model.GetSubscriptionSyncViews(pool.Id)
	require.NoError(t, err)
	require.Len(t, views, 1, "binding save immediately imports the cached snapshot, without HTTP or another poll")
	assert.InDelta(t, ratio, *views[0].Utilization, 1e-9)
	cfg, err := model.GetSubscriptionPoolConfiguration(pool.Id)
	require.NoError(t, err)
	assert.Equal(t, now+1000, cfg.Cycles[0].NextReset)
	assert.Equal(t, accounts[0].Expiry, cfg.EndTime)
	assert.EqualValues(t, 25200, cfg.Cycles[0].Seconds)
	assert.Equal(t, "7h", cfg.Cycles[0].Name)
	require.NoError(t, model.SettleSubscriptionRequest("adopt-inflight", member.Id, 0), "adopting 7h must preserve reservation provenance")
	priorUsage, err := model.GetSubscriptionUsage(user.Id)
	require.NoError(t, err)
	require.Len(t, priorUsage, 1)
	assert.EqualValues(t, 15, priorUsage[0].Cycles[0].Used)
	require.NoError(t, model.DB.Model(member).Update("status", "cancelled").Error)

	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		var e error
		member, e = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
		return e
	}))
	usage, err := model.GetSubscriptionUsage(user.Id)
	require.NoError(t, err)
	require.Len(t, usage, 2)
	assert.True(t, usage[0].Managed)
	assert.Equal(t, member.Id, usage[0].SubscriptionID)
	require.Len(t, usage[0].Cycles, 1)
	assert.EqualValues(t, 25200, usage[0].Cycles[0].Seconds)
	assert.Equal(t, now+1000, usage[0].Cycles[0].NextReset)
	overview, err := model.GetSubscriptionPoolOverview(user.Id)
	require.NoError(t, err)
	require.Len(t, overview, 1)
	assert.Equal(t, []int{member.Id}, overview[0].SubscriptionIDs)
	require.Len(t, overview[0].SyncObservations, 1)
	assert.InDelta(t, ratio, *overview[0].SyncObservations[0].Utilization, 1e-9)
	// A later response omitting a previously reported cycle hides the old card,
	// but must never make upstream enforcement or recorded local use disappear.
	storedBinding, err := model.GetSubscriptionSyncBinding(pool.Id)
	require.NoError(t, err)
	claimed, err := model.ClaimSubscriptionSyncSource(source.Id, "missing-cycle", true)
	require.NoError(t, err)
	missing := accounts[0]
	missing.Cycles = nil
	_, err = model.ApplySubscriptionSyncAccount(*claimed, "missing-cycle", storedBinding, missing, 1, false)
	require.NoError(t, err)
	require.NoError(t, model.FinishSubscriptionSyncSource(*claimed, "missing-cycle", []model.SubscriptionSyncAccount{missing}, "", true, 1))
	hidden, err := model.GetSubscriptionSyncViews(pool.Id)
	require.NoError(t, err)
	assert.Empty(t, hidden)
	_, err = model.PreConsumeUserSubscriptionForChannel("unreported-denied", user.Id, "m", 0, 1, 9501)
	require.ErrorContains(t, err, "subscription quota insufficient")
	var retained int64
	require.NoError(t, model.DB.Model(&model.SubscriptionSyncObservation{}).Where("pool_id = ?", pool.Id).Count(&retained).Error)
	assert.EqualValues(t, 1, retained, "hiding noise does not delete accounting evidence")

}

func TestSubscriptionPoolDeletionProtectsReferencesAndPreservesAccounting(t *testing.T) {
	user, plan, now := setupSubscriptionPoolTest(t)
	source, err := service.SaveSubscriptionSyncSource(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "delete-source", URL: "https://example.com/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10}, Mapping: model.PanelSubscriptionSyncMapping()}, 1, true)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 9502, Name: "delete"}).Error)
	pool, err := model.SaveSubscriptionPool(model.SubscriptionPoolInput{SubscriptionPool: model.SubscriptionPool{Name: "delete-pool", Enabled: true, EndTime: now + 86400}, Channels: []int{9502}, Cycles: []model.SubscriptionCycleRule{{Key: "c", Name: "5h", Seconds: 18000, NextReset: now + 1000, Limit: 100, Enabled: true, Shared: true}}}, 1)
	require.NoError(t, err)
	binding := model.SubscriptionSyncBindingInput{SubscriptionSyncBinding: model.SubscriptionSyncBinding{PoolID: pool.Id, SourceID: source.Id, AccountID: "a", Enabled: true, StalePolicy: "stop"}}
	require.NoError(t, model.SaveSubscriptionSyncBinding(binding, 1))
	require.ErrorContains(t, model.DeleteSubscriptionSyncSource(source.Id, source.Version, source.CredentialRevision, 1), "Unbind")
	require.ErrorContains(t, model.DeleteSubscriptionPool(pool.Id, pool.Version, 0, 1), "Synchronization binding changed", "a binding created after the controller inspected permissions cannot be implicitly detached")
	require.ErrorContains(t, model.DeleteSubscriptionPool(pool.Id, pool.Version-1, 1, 1), "reload")
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "fixed", PoolID: pool.Id}, nil))
	require.ErrorContains(t, model.DeleteSubscriptionPool(pool.Id, pool.Version, 1, 1), "subscription plans")
	var member *model.UserSubscription
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		var e error
		member, e = model.CreateUserSubscriptionFromPlanTx(tx, user.Id, &plan, "admin")
		return e
	}))
	require.NoError(t, model.SaveSubscriptionPlanAllocation(model.SubscriptionPlanAllocation{PlanID: plan.Id, Mode: "legacy", Version: 1}, nil))
	require.ErrorContains(t, model.DeleteSubscriptionPool(pool.Id, pool.Version, 1, 1), "active members")
	_, err = model.PreConsumeUserSubscriptionForChannel("delete-inflight", user.Id, "m", 0, 10, 9502)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(member).Update("status", "cancelled").Error)
	require.ErrorContains(t, model.DeleteSubscriptionPool(pool.Id, pool.Version, 1, 1), "in-flight")
	require.NoError(t, model.SettleSubscriptionRequest("delete-inflight", member.Id, 0))
	require.NoError(t, model.DeleteSubscriptionPool(pool.Id, pool.Version, 1, 1))
	var count int64
	require.NoError(t, model.DB.Model(&model.SubscriptionCycleReservation{}).Where("pool_id = ?", pool.Id).Count(&count).Error)
	assert.Positive(t, count, "settled reservation history is retained")
	require.NoError(t, model.DB.Model(&model.SubscriptionPoolEvent{}).Where("pool_id = ? AND action = ?", pool.Id, "delete").Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, model.DB.Model(&model.SubscriptionPoolChannel{}).Where("pool_id = ?", pool.Id).Count(&count).Error)
	assert.Zero(t, count)
	claimed, err := model.ClaimSubscriptionSyncSource(source.Id, "deleted-worker", true)
	require.NoError(t, err)
	require.ErrorContains(t, model.DeleteSubscriptionSyncSource(source.Id, source.Version-1, source.CredentialRevision, 1), "reload")
	require.NoError(t, model.DeleteSubscriptionSyncSource(source.Id, source.Version, source.CredentialRevision, 1))
	require.Error(t, model.FinishSubscriptionSyncSource(*claimed, "deleted-worker", nil, "", false, 1), "deleted workers cannot resurrect credentials or catalogs")
	require.NoError(t, model.DB.Model(&model.SubscriptionSyncEvent{}).Where("source_id = ? AND action = ?", source.Id, "delete").Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestSubscriptionPoolSourceDeleteRequiresScopedSingleUseProof(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleRootUser).Error)
	require.NoError(t, model.MigrateSubscriptionPools())
	source, err := service.SaveSubscriptionSyncSource(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: model.SubscriptionSyncSource{Name: "proof-delete", URL: "https://example.com/usage", Method: "GET", IntervalSeconds: 60, StaleSeconds: 300, TimeoutSeconds: 10}, Mapping: model.PanelSubscriptionSyncMapping()}, user.Id, true)
	require.NoError(t, err)
	raw, err := common.Marshal(service.SubscriptionSyncCredentialContext{SourceID: source.Id, Action: "delete", Version: source.Version, Revision: source.CredentialRevision})
	require.NoError(t, err)
	proof := issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopeSubscriptionSyncCredentials, Context: raw}, "password")
	handler := func(c *gin.Context) {
		c.Set("role", common.RoleRootUser)
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(source.Id)}}
		DeleteSubscriptionSyncSource(c)
	}
	body := fmt.Sprintf(`{"version":%d,"credential_revision":%d,"confirm":true}`, source.Version, source.CredentialRevision)
	missing := securityEnrollmentRequest("DELETE", "/sync", body, "", identity, handler)
	assert.Equal(t, 403, missing.Code)
	wrong := securityEnrollmentRequest("DELETE", "/sync", strings.Replace(body, `"version":1`, `"version":2`, 1), proof, identity, handler)
	assert.Equal(t, 403, wrong.Code)
	success := securityEnrollmentRequest("DELETE", "/sync", body, proof, identity, handler)
	assert.Equal(t, 200, success.Code)
	assert.Contains(t, success.Body.String(), `"success":true`)
	replay := securityEnrollmentRequest("DELETE", "/sync", body, proof, identity, handler)
	assert.Equal(t, 403, replay.Code)
}
