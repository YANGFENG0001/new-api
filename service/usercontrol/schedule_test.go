package usercontrol

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScopedRedisConcurrencyIsAtomicAndUsageDoesNotFailOpen(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	previousClient, previousEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = previousClient, previousEnabled; _ = client.Close() })
	rule := ScopedRule{ID: common.GetUUID(), Limits: Limits{Rate: &RateLimits{Concurrent: 2}, Usage: &UsageLimits{DailyQuota: 5}}}
	type result struct {
		release func()
		reason  string
		err     error
	}
	results := make(chan result, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			release, reason, err := AdmitScoped(rule, 123, time.Now(), time.UTC)
			results <- result{release, reason, err}
		})
	}
	wg.Wait()
	close(results)
	var releases []func()
	for result := range results {
		require.NoError(t, result.err)
		if result.reason == "" {
			releases = append(releases, result.release)
		}
	}
	require.Len(t, releases, 2)
	for _, release := range releases {
		release()
		release()
	}
	release, reason, err := AdmitScoped(rule, 123, time.Now(), time.UTC)
	require.NoError(t, err)
	require.Empty(t, reason)
	release()
	recordScopedUsage(rule, 123, time.Now(), time.UTC, 5, 2, 3)
	_, reason, err = AdmitScoped(rule, 123, time.Now(), time.UTC)
	require.NoError(t, err)
	assert.Equal(t, "usage", reason)
	server.Close()
	rule.Limits.Rate = nil
	_, _, err = AdmitScoped(rule, 123, time.Now(), time.UTC)
	require.Error(t, err)
}

func TestScopedDisabledActionsDoNotRejectNontextRequest(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/embeddings", nil)
	SetContext(c, &Effective{HasPolicy: true, Limits: Limits{ScopedRules: []ScopedRule{{ID: "disabled-actions", Actions: &Actions{RequestMode: "merge"}}}}})
	changed, err := ApplyActions(c, "client", 0)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestScopedActionsPriorityAndDuplicateReferences(t *testing.T) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"client","messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	high := ScopedRule{ID: "high", Namespace: "policy:1", PolicyPriority: 10, Actions: &Actions{RouteModel: "high-model"}}
	low := ScopedRule{ID: "low", Namespace: "policy:2", PolicyPriority: 1, Actions: &Actions{RouteModel: "low-model"}}
	SetContext(c, &Effective{HasPolicy: true, Limits: Limits{ScopedRules: []ScopedRule{high, low}}})
	_, err := ApplyActions(c, "client", 0)
	require.NoError(t, err)
	var body struct {
		Model string `json:"model"`
	}
	require.NoError(t, common.UnmarshalBodyReusable(c, &body))
	assert.Equal(t, "high-model", body.Model)
	merged := Merge(Limits{ScopedRules: []ScopedRule{high}}, Limits{ScopedRules: []ScopedRule{high}})
	assert.Len(t, merged.ScopedRules, 1)
}

func TestScopedRulesScheduleIsolationAndAdmission(t *testing.T) {
	now := time.Now()
	rule := ScopedRule{ID: common.GetUUID(), Models: []string{"client-model"}, ChannelIDs: []int{7}, Limits: Limits{Rate: &RateLimits{RPM: 1}, Usage: &UsageLimits{DailyQuota: 10}}}
	assert.True(t, rule.Matches("client-model", 7))
	assert.False(t, rule.Matches("other-model", 7))
	assert.False(t, rule.Matches("client-model", 8))
	schedule := &Schedule{Timezone: "UTC", Rules: []ScheduleRule{{ID: "night", Start: "22:00", End: "06:00", Limits: Limits{ScopedRules: []ScopedRule{rule}}}}}
	active, _, _, err := schedule.ActiveLimits(at(t, "UTC", 2026, 10, 19, 23, 0), Limits{})
	require.NoError(t, err)
	require.Len(t, active.ScopedRules, 1)
	inactive, _, _, err := schedule.ActiveLimits(at(t, "UTC", 2026, 10, 19, 12, 0), Limits{})
	require.NoError(t, err)
	assert.Empty(t, inactive.ScopedRules)
	release, reason, err := AdmitScoped(rule, 731, now, time.UTC)
	require.NoError(t, err)
	require.Empty(t, reason)
	release()
	_, reason, err = AdmitScoped(rule, 731, now, time.UTC)
	require.NoError(t, err)
	assert.Equal(t, "requests", reason)
	release, reason, err = AdmitScoped(rule, 732, now, time.UTC)
	require.NoError(t, err)
	require.Empty(t, reason)
	release()
	// Changing thresholds must not erase already consumed usage.
	rule.Limits.Rate = nil
	recordScopedUsage(rule, 731, now, time.UTC, 10, 3, 4)
	_, reason, err = AdmitScoped(rule, 731, now, time.UTC)
	require.NoError(t, err)
	assert.Equal(t, "usage", reason)
	other := rule
	other.ID = common.GetUUID()
	release, reason, err = AdmitScoped(other, 731, now, time.UTC)
	require.NoError(t, err)
	require.Empty(t, reason)
	release()
}

func TestScopedActionsPreserveProtocolNumbersAndUseOnePassRouting(t *testing.T) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"client-model","stream":false,"instructions":"original","input":"hello","reasoning":{"summary":"auto"},"exact":9007199254740993}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	rules := []ScopedRule{
		{ID: "model", Models: []string{"client-model"}, Actions: &Actions{RouteModel: "target-model", PromptPrefix: "before", PromptSuffix: "after", ReasoningEffort: "high", RequestBody: `{"temperature":0,"reasoning":{"summary":null}}`}},
		{ID: "channel", ChannelIDs: []int{7}, Actions: &Actions{RouteModel: "final-model"}},
	}
	SetContext(c, &Effective{HasPolicy: true, Limits: Limits{ScopedRules: rules}})
	changed, err := ApplyActions(c, "client-model", 0)
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = ApplyActions(c, "client-model", 7)
	require.NoError(t, err)
	assert.True(t, changed)
	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	raw, err := storage.Bytes()
	require.NoError(t, err)
	assert.Contains(t, string(raw), `9007199254740993`)
	var body map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(raw, &body))
	assert.JSONEq(t, `"final-model"`, string(body["model"]))
	assert.JSONEq(t, `false`, string(body["stream"]))
	assert.JSONEq(t, `0`, string(body["temperature"]))
	assert.JSONEq(t, `{"effort":"high"}`, string(body["reasoning"]))
	assert.JSONEq(t, `"before\noriginal\nafter"`, string(body["instructions"]))
	assert.Equal(t, "client-model", c.GetString(OriginalModelKey))
}

func TestScopedActionValidationRejectsBypassesAndMalformedTemplates(t *testing.T) {
	for _, raw := range []string{
		`{"scoped_rules":[{"id":"a","limits":{"rate":{"rpm":-1}}}]}`,
		`{"scoped_rules":[{"id":"a","limits":{"api_keys":{"max_total":1}}}]}`,
		`{"scoped_rules":[{"id":"a","limits":{"scoped_rules":[{"id":"b","limits":{}}]}}]}`,
		`{"scoped_rules":[{"id":"a","limits":{},"actions":{"request_body":"[]"}}]}`,
		`{"scoped_rules":[{"id":"a","limits":{},"actions":{"request_body":"{\"model\":\"bypass\"}"}}]}`,
		`{"scoped_rules":[{"id":"a","limits":{},"actions":{"request_body":"{\"ſtream\":true}"}}]}`,
		`{"scoped_rules":[{"id":"a","limits":{},"actions":{"response":{"mode":"error","status":200,"body":"failed"}}}]}`,
		`{"scoped_rules":[{"id":"a","limits":{},"actions":{"reasoning_effort":"arbitrary"}}]}`,
	} {
		_, _, err := ParseLimitsJSON([]byte(raw))
		require.ErrorIs(t, err, ErrInvalidInput, raw)
	}
}

func TestScopedStaticResponsesAreMarkedAndPreserveStreamingProtocols(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", path, stream), func(t *testing.T) {
				response := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(response)
				c.Request = httptest.NewRequest("POST", path, strings.NewReader(fmt.Sprintf(`{"model":"client","stream":%t}`, stream)))
				c.Request.Header.Set("Content-Type", "application/json")
				t.Cleanup(func() { common.CleanupBodyStorage(c) })
				c.Set(OriginalModelKey, "client")
				state(c).Response = &StaticResponse{Mode: "text", Body: "replacement"}
				handled, err := WriteStaticResponse(c)
				require.NoError(t, err)
				assert.True(t, handled)
				assert.Equal(t, "true", response.Header().Get("X-New-API-Simulated"))
				assert.Contains(t, response.Body.String(), "replacement")
				if stream {
					assert.Contains(t, response.Header().Get("Content-Type"), "text/event-stream")
				}
				assert.True(t, state(c).Synthetic)
			})
		}
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	state(c).Response = &StaticResponse{Mode: "json", Body: `{"ok":true}`}
	handled, err := WriteStaticResponse(c)
	require.Error(t, err)
	assert.False(t, handled)
	assert.False(t, state(c).Synthetic)
}

func TestScopedRetryCountsRuleOnceAndEnforcesNewChannel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("id", 98765)
	rule := ScopedRule{ID: common.GetUUID(), Limits: Limits{Rate: &RateLimits{RPM: 1, Concurrent: 1}, Usage: &UsageLimits{DailyQuota: 10}}}
	channel := ScopedRule{ID: common.GetUUID(), ChannelIDs: []int{9}, Limits: Limits{Blocked: boolPtr(true)}}
	SetContext(c, &Effective{HasPolicy: true, Timezone: "UTC", Limits: Limits{ScopedRules: []ScopedRule{rule, channel}}})
	require.NoError(t, AdmitChannel(c, "client", 7))
	require.NoError(t, AdmitChannel(c, "client", 7))
	RecordContextUsage(c, 98765, 10, 2, 3)
	require.ErrorContains(t, AdmitChannel(c, "client", 9), "blocked")
	// Release without database audit; the database-backed integration covers it.
	for _, release := range state(c).Releases {
		release()
	}
	rule.Limits.Rate = nil
	_, reason, err := AdmitScoped(rule, 98765, time.Now(), time.UTC)
	require.NoError(t, err)
	assert.Equal(t, "usage", reason)
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err)
	return loc
}

func at(t *testing.T, zone string, year int, month time.Month, day, hour, minute int) time.Time {
	t.Helper()
	return time.Date(year, month, day, hour, minute, 0, 0, mustLocation(t, zone))
}

func TestActiveLimitsPriorityMergeAndIgnoreLowerPriority(t *testing.T) {
	schedule := &Schedule{
		Timezone: "Asia/Shanghai",
		Rules: []ScheduleRule{
			{ID: "low", Name: "low", Priority: 1, Limits: Limits{Rate: &RateLimits{RPM: 5}}},
			{ID: "high-a", Name: "high-a", Priority: 10, Limits: Limits{Rate: &RateLimits{RPM: 20, RPD: 100}}},
			{ID: "high-b", Name: "high-b", Priority: 10, Limits: Limits{Rate: &RateLimits{RPM: 30}}},
		},
	}
	base := Limits{Rate: &RateLimits{RPM: 60}, Requests: &RequestLimits{Daily: 7}}

	limits, hits, timezone, err := schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 19, 12, 0), base)
	require.NoError(t, err)
	assert.Equal(t, "Asia/Shanghai", timezone)
	assert.ElementsMatch(t, []string{"high-a", "high-b"}, hits)
	require.NotNil(t, limits.Rate)
	// Two rules at the same priority merge with the strictest value; the
	// lower-priority rule must not contribute its tighter RPM.
	assert.Equal(t, 20, limits.Rate.RPM)
	assert.Equal(t, 100, limits.Rate.RPD)
	// Base limits survive the merge.
	require.NotNil(t, limits.Requests)
	assert.Equal(t, 7, limits.Requests.Daily)
}

func TestActiveLimitsWeekdayAndSpecificDates(t *testing.T) {
	schedule := &Schedule{
		Timezone: "Asia/Shanghai",
		Rules: []ScheduleRule{
			{ID: "monday", Name: "monday", Days: []int{1}, Limits: Limits{Rate: &RateLimits{RPM: 10}}},
			{ID: "one-off", Name: "one-off", Dates: []string{"2026-10-21"}, Limits: Limits{Rate: &RateLimits{RPM: 15}}},
		},
		Default: &Limits{Rate: &RateLimits{RPM: 99}},
	}

	// 2026-10-19 is a Monday.
	monday, hits, _, err := schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 19, 12, 0), Limits{})
	require.NoError(t, err)
	assert.Equal(t, []string{"monday"}, hits)
	require.NotNil(t, monday.Rate)
	assert.Equal(t, 10, monday.Rate.RPM)

	// Tuesday has no matching rule, so the fallback applies.
	tuesday, hits, _, err := schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 20, 12, 0), Limits{})
	require.NoError(t, err)
	assert.Empty(t, hits)
	require.NotNil(t, tuesday.Rate)
	assert.Equal(t, 99, tuesday.Rate.RPM)

	// Wednesday matches only the one-off date rule, and the fallback does not
	// stack on top of a matching rule.
	wednesday, hits, _, err := schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 21, 12, 0), Limits{})
	require.NoError(t, err)
	assert.Equal(t, []string{"one-off"}, hits)
	require.NotNil(t, wednesday.Rate)
	assert.Equal(t, 15, wednesday.Rate.RPM)
}

func TestActiveLimitsDateRange(t *testing.T) {
	schedule := &Schedule{
		Timezone: "Asia/Shanghai",
		Rules: []ScheduleRule{
			{
				ID:        "campaign",
				Name:      "campaign",
				StartDate: "2026-10-20",
				EndDate:   "2026-10-22",
				Limits:    Limits{Rate: &RateLimits{RPM: 8}},
			},
		},
	}
	_, hits, _, err := schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 19, 12, 0), Limits{})
	require.NoError(t, err)
	assert.Empty(t, hits)

	_, hits, _, err = schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 20, 0, 0), Limits{})
	require.NoError(t, err)
	assert.Equal(t, []string{"campaign"}, hits)

	_, hits, _, err = schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 22, 23, 59), Limits{})
	require.NoError(t, err)
	assert.Equal(t, []string{"campaign"}, hits)

	_, hits, _, err = schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 23, 0, 0), Limits{})
	require.NoError(t, err)
	assert.Empty(t, hits)
}

func TestActiveLimitsCrossMidnightUsesStartDay(t *testing.T) {
	schedule := &Schedule{
		Timezone: "Asia/Shanghai",
		Rules: []ScheduleRule{
			{
				ID:     "friday-night",
				Name:   "friday-night",
				Days:   []int{5},
				Start:  "22:00",
				End:    "06:00",
				Limits: Limits{Rate: &RateLimits{RPM: 5}},
			},
		},
	}

	cases := []struct {
		name string
		when time.Time
		hit  bool
	}{
		// Friday evening: the window starts today.
		{name: "friday 23:00", when: at(t, "Asia/Shanghai", 2026, time.October, 23, 23, 0), hit: true},
		// Saturday morning: the window started on Friday.
		{name: "saturday 02:00", when: at(t, "Asia/Shanghai", 2026, time.October, 24, 2, 0), hit: true},
		// Saturday evening: a Friday rule is not active any more.
		{name: "saturday 23:00", when: at(t, "Asia/Shanghai", 2026, time.October, 24, 23, 0), hit: false},
		// Friday noon is outside the window.
		{name: "friday 12:00", when: at(t, "Asia/Shanghai", 2026, time.October, 23, 12, 0), hit: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, hits, _, err := schedule.ActiveLimits(tc.when, Limits{})
			require.NoError(t, err)
			if tc.hit {
				assert.Equal(t, []string{"friday-night"}, hits)
			} else {
				assert.Empty(t, hits)
			}
		})
	}
}

func TestActiveLimitsCrossMidnightDateAnchor(t *testing.T) {
	schedule := &Schedule{
		Timezone: "Asia/Shanghai",
		Rules: []ScheduleRule{
			{
				ID:     "patch-night",
				Name:   "patch-night",
				Dates:  []string{"2026-10-23"},
				Start:  "22:00",
				End:    "06:00",
				Limits: Limits{Rate: &RateLimits{RPM: 1}},
			},
		},
	}

	_, hits, _, err := schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 24, 1, 0), Limits{})
	require.NoError(t, err)
	assert.Equal(t, []string{"patch-night"}, hits)

	_, hits, _, err = schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 25, 1, 0), Limits{})
	require.NoError(t, err)
	assert.Empty(t, hits)
}

func TestActiveLimitsDefaultIsFallbackOnly(t *testing.T) {
	schedule := &Schedule{
		Timezone: "Asia/Shanghai",
		Rules: []ScheduleRule{
			{
				ID:     "office",
				Name:   "office",
				Start:  "09:00",
				End:    "18:00",
				Limits: Limits{Rate: &RateLimits{RPM: 10}},
			},
		},
		Default: &Limits{Rate: &RateLimits{RPM: 3}},
	}

	inside, hits, _, err := schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 19, 12, 0), Limits{})
	require.NoError(t, err)
	assert.Equal(t, []string{"office"}, hits)
	require.NotNil(t, inside.Rate)
	assert.Equal(t, 10, inside.Rate.RPM, "the fallback must not be applied when a rule matches")

	outside, hits, _, err := schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 19, 20, 0), Limits{})
	require.NoError(t, err)
	assert.Empty(t, hits)
	require.NotNil(t, outside.Rate)
	assert.Equal(t, 3, outside.Rate.RPM)
}

func TestActiveLimitsDefaultTimezoneIsAsiaShanghai(t *testing.T) {
	schedule, err := ParseSchedule(`{"rules":[{"id":"office","start":"09:00","end":"18:00","limits":{"rate":{"rpm":10}}}]}`)
	require.NoError(t, err)
	require.NotNil(t, schedule)
	assert.Equal(t, DefaultTimezone, schedule.Timezone)

	// 03:30 UTC is 11:30 in Asia/Shanghai.
	_, hits, timezone, err := schedule.ActiveLimits(at(t, "UTC", 2026, time.October, 19, 3, 30), Limits{})
	require.NoError(t, err)
	assert.Equal(t, DefaultTimezone, timezone)
	assert.Equal(t, []string{"office"}, hits)
}

func TestActiveLimitsOpenEndedWindows(t *testing.T) {
	schedule := &Schedule{
		Timezone: "Asia/Shanghai",
		Rules: []ScheduleRule{
			{ID: "late", Name: "late", Start: "22:00", Limits: Limits{Rate: &RateLimits{RPM: 1}}},
			{ID: "early", Name: "early", End: "06:00", Limits: Limits{Rate: &RateLimits{RPD: 2}}},
		},
	}

	_, hits, _, err := schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 19, 23, 0), Limits{})
	require.NoError(t, err)
	assert.Equal(t, []string{"late"}, hits)

	_, hits, _, err = schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 19, 3, 0), Limits{})
	require.NoError(t, err)
	assert.Equal(t, []string{"early"}, hits)

	_, hits, _, err = schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 19, 12, 0), Limits{})
	require.NoError(t, err)
	assert.Empty(t, hits)
}

func TestActiveLimitsDisabledRuleIsIgnored(t *testing.T) {
	disabled := false
	schedule := &Schedule{
		Timezone: "Asia/Shanghai",
		Rules: []ScheduleRule{
			{ID: "off", Name: "off", Enabled: &disabled, Limits: Limits{Rate: &RateLimits{RPM: 1}}},
		},
	}
	_, hits, _, err := schedule.ActiveLimits(at(t, "Asia/Shanghai", 2026, time.October, 19, 12, 0), Limits{})
	require.NoError(t, err)
	assert.Empty(t, hits)
}

func TestNormalizeScheduleKeepsMultipleRules(t *testing.T) {
	schedule := &Schedule{
		Timezone: "Asia/Shanghai",
		Rules: []ScheduleRule{
			{Name: "first", Priority: 5, Start: "08:00", End: "12:00"},
			{Name: "second", Priority: 5, Start: "13:00", End: "18:00"},
			{Name: "third", Priority: 9, Start: "20:00", End: "02:00"},
		},
	}
	require.NoError(t, NormalizeSchedule(schedule))
	require.Len(t, schedule.Rules, 3)
	assert.NotEmpty(t, schedule.Rules[0].ID)
	assert.NotEqual(t, schedule.Rules[0].ID, schedule.Rules[1].ID)
	require.NotNil(t, schedule.Rules[0].Enabled)
	assert.True(t, *schedule.Rules[0].Enabled)
}
