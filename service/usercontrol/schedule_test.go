package usercontrol

import (
	"context"
	"errors"
	"fmt"
	"github.com/tidwall/gjson"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserControlDisjointPoolBaselineAllowlistsNeverBecomeUnlimited(t *testing.T) {
	modelLimits := Merge(Limits{Models: &ModelLimits{Allow: []string{"a"}}}, Limits{Models: &ModelLimits{Allow: []string{"b"}}})
	require.NotNil(t, modelLimits.Blocked)
	assert.True(t, *modelLimits.Blocked)
	channelLimits := Merge(Limits{Channels: &ChannelLimits{AllowIDs: []int{1}}}, Limits{Channels: &ChannelLimits{AllowIDs: []int{2}}})
	require.NotNil(t, channelLimits.Blocked)
	assert.True(t, *channelLimits.Blocked)
	ordinary := Merge(Limits{}, Limits{Models: &ModelLimits{Allow: []string{"a"}}})
	assert.True(t, ordinary.AllowsModel("a"))
	assert.False(t, ordinary.AllowsModel("b"))
	assert.Nil(t, ordinary.Blocked)
}

func TestUserControlDeliveryMiddlewareCapturesAndRejectsNonstreamFallback(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, tc := range []struct {
		name, body, kind, response string
		status, want               int
		called                     bool
	}{
		{"stream", `{"stream":true}`, "text/event-stream", "data: [DONE]\n\n", 200, 200, true},
		{"explicit text", `{"stream":true,"modalities":["text"],"audio":null}`, "text/event-stream", "data: [DONE]\n\n", 200, 200, true},
		{"audio", `{"stream":true,"modalities":["audio"]}`, "text/event-stream", "data: [DONE]\n\n", 200, 400, false},
		{"nonstream fallback", `{"stream":true}`, "application/json", `{"text":"unpaced"}`, 200, 502, true},
		{"invalid stream", `{"stream":true}`, "text/event-stream", "data: {}\n\n", 200, 502, true},
		{"upstream error", `{"stream":true}`, "application/json", `{"error":"upstream unavailable"}`, 503, 503, true},
		{"request without stream", `{"stream":false}`, "text/event-stream", "data: [DONE]\n\n", 200, 400, false},
		{"multiple choices", `{"stream":true,"n":2}`, "text/event-stream", "data: [DONE]\n\n", 200, 400, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router := gin.New()
			router.Use(func(c *gin.Context) { state(c).Delivery = &DeliveryAction{MinCPS: 10, MaxCPS: 20}; Deliver(c) })
			called := false
			router.POST("/v1/chat/completions", func(c *gin.Context) {
				called = true
				c.Header("Content-Type", tc.kind)
				c.Status(tc.status)
				_, err := c.Writer.WriteString(tc.response)
				require.NoError(t, err)
				c.Writer.Flush()
				assert.Empty(t, recorder.Body.String(), "no response may escape until upstream finishes")
			})
			router.ServeHTTP(recorder, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(tc.body)))
			assert.Equal(t, tc.called, called)
			assert.Equal(t, tc.want, recorder.Code, recorder.Body.String())
			if tc.want < 400 {
				assert.Equal(t, tc.response, recorder.Body.String())
			}
		})
	}
}

func TestUserControlDelayOnlyAllowsZeroAndAbortsCanceledLongWaitBeforeUpstream(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprint(canceled), func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				milliseconds := 0
				if canceled {
					milliseconds = int(^uint(0) >> 1)
				}
				state(c).Delay = &DelayAction{MinMS: milliseconds, MaxMS: milliseconds}
				Deliver(c)
				assert.Equal(t, canceled, c.GetBool("user_control_runtime_failed"))
			})
			called := false
			router.POST("/v1/chat/completions", func(c *gin.Context) {
				called = true
				c.Status(204)
			})
			request := httptest.NewRequest("POST", "/v1/chat/completions", nil)
			if canceled {
				ctx, cancel := context.WithCancel(request.Context())
				cancel()
				request = request.WithContext(ctx)
			}
			router.ServeHTTP(httptest.NewRecorder(), request)
			assert.Equal(t, !canceled, called)
		})
	}
}

func TestUserControlDeliveryLeaseRenewalKeepsRedisConcurrentSlotUntilRelease(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousClient, previousEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = previousClient, previousEnabled; _ = client.Close() })
	release, allowed, err := AcquireConcurrent("delivery-lease", 1)
	require.NoError(t, err)
	require.True(t, allowed)
	server.FastForward(9 * time.Minute)
	require.NoError(t, renewConcurrencyLeases([]string{"delivery-lease"}))
	server.FastForward(2 * time.Minute)
	_, allowed, err = AcquireConcurrent("delivery-lease", 1)
	require.NoError(t, err)
	assert.False(t, allowed)
	release()
	next, allowed, err := AcquireConcurrent("delivery-lease", 1)
	require.NoError(t, err)
	assert.True(t, allowed)
	next()
}

func TestUserControlRandomWindowContinuousAndOvernightAnchor(t *testing.T) {
	r := &RandomWindow{Enabled: true, ActiveMin: 3, ActiveMax: 3, IdleMin: 2, IdleMax: 2, Seed: "stable"}
	s := &Schedule{Timezone: "UTC", Rules: []ScheduleRule{{ID: "night", Days: []int{5}, Start: "23:58", End: "01:00", Random: r, Limits: Limits{Rate: &RateLimits{RPM: 1}}}}}
	base := Limits{Rate: &RateLimits{RPM: 10}}
	for _, tc := range []struct{ hour, minute, rpm int }{{23, 58, 10}, {23, 59, 10}, {0, 0, 1}, {0, 1, 1}, {0, 2, 1}, {0, 3, 10}} {
		day := 23
		if tc.hour == 0 {
			day = 24
		}
		when := time.Date(2026, 10, day, tc.hour, tc.minute, 30, 0, time.UTC)
		got, _, _, err := s.ActiveLimitsForUser(when, base, 73)
		require.NoError(t, err)
		assert.Equal(t, tc.rpm, got.Rate.RPM)
		copy := *s
		again, _, _, err := copy.ActiveLimitsForUser(when.Add(10*time.Second), base, 73)
		require.NoError(t, err)
		assert.Equal(t, got, again)
	}
	r.Enabled = false
	got, _, _, err := s.ActiveLimitsForUser(time.Date(2026, 10, 23, 23, 58, 0, 0, time.UTC), base, 73)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Rate.RPM)
}

func TestUserControlTransportAndRandomValidation(t *testing.T) {
	for _, raw := range []string{
		`{"scoped_rules":[{"id":"x","limits":{},"actions":{"delay":{"min_ms":10,"max_ms":9}}}]}`,
		`{"scoped_rules":[{"id":"x","limits":{},"actions":{"delivery":{"min_cps":-1,"max_cps":9}}}]}`,
		`{"scoped_rules":[{"id":"x","limits":{},"actions":{"delivery":{"min_cps":10,"max_cps":9}}}]}`,
		`{"random_pool":{"policy_ids":[],"hold_min":1,"hold_max":2}}`,
		`{"random_pool":{"policy_ids":[1],"hold_min":1441,"hold_max":1441}}`,
	} {
		_, _, err := ParseLimitsJSON([]byte(raw))
		require.Error(t, err, raw)
	}
	_, _, err := ParseScheduleJSON([]byte(`{"rules":[{"limits":{},"random":{"enabled":true,"active_min":0,"active_max":2,"idle_min":1,"idle_max":1}}]}`))
	require.Error(t, err)
	s, _, err := ParseScheduleJSON([]byte(`{"rules":[{"limits":{},"random":{"enabled":true,"active_min":1,"active_max":2,"idle_min":1,"idle_max":2}}]}`))
	require.NoError(t, err)
	assert.NotEmpty(t, s.Rules[0].Random.Seed)
}

func TestUserControlTransportAcceptsZeroAndRangesBeyondPreviousCaps(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, values := range []struct{ low, high int }{{0, 0}, {0, 2000000}, {2000000, 2000000}, {0, maxInt}, {maxInt, maxInt}} {
		t.Run(fmt.Sprintf("%d-%d", values.low, values.high), func(t *testing.T) {
			require.NoError(t, normalizeActions(&Actions{
				Delay:    &DelayAction{MinMS: values.low, MaxMS: values.high},
				Delivery: &DeliveryAction{MinCPS: values.low, MaxCPS: values.high},
			}))
			actual := randomRange(values.low, values.high)
			assert.GreaterOrEqual(t, actual, values.low)
			assert.LessOrEqual(t, actual, values.high)
		})
	}
}

func TestUserControlLongUpstreamDelayDoesNotOverflowAndRemainsCancelable(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	errCanceled := errors.New("request canceled while waiting")
	err := waitUpstreamDelay(context.Background(), maxInt, func(_ context.Context, duration time.Duration) error {
		assert.Positive(t, duration)
		assert.Equal(t, time.Duration(min(int64(maxInt), int64((1<<63-1)/time.Millisecond)))*time.Millisecond, duration)
		return errCanceled
	})
	require.ErrorIs(t, err, errCanceled)
	require.NoError(t, waitUpstreamDelay(context.Background(), 0, func(context.Context, time.Duration) error {
		t.Fatal("zero delay must not wait")
		return nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, waitUpstreamDelay(ctx, maxInt, waitDelivery), context.Canceled)
}

func TestUserControlReplayPreservesTextWithoutWaitingAtZeroAndAtLargeRates(t *testing.T) {
	input := "data: " + `{"choices":[{"delta":{"content":"中🙂"}}]}` + "\n\ndata: [DONE]\n\n"
	for _, cps := range []int{0, 2000000, int(^uint(0) >> 1)} {
		t.Run(fmt.Sprint(cps), func(t *testing.T) {
			var output strings.Builder
			err := replaySSE(context.Background(), strings.NewReader(input), func(packet []byte) error {
				output.Write(packet)
				return nil
			}, &DeliveryAction{MinCPS: cps, MaxCPS: cps}, func(_ context.Context, duration time.Duration) error {
				assert.NotZero(t, cps, "a sampled zero must not wait")
				assert.Positive(t, duration)
				return nil
			})
			require.NoError(t, err)
			assert.Contains(t, output.String(), "中")
			assert.Contains(t, output.String(), "🙂")
			assert.Contains(t, output.String(), "[DONE]")
		})
	}
}

func TestUserControlReplaySplitsUnicodeAndPreservesToolCallsUsageAndOpaqueContent(t *testing.T) {
	input := "data: " + `{"choices":[{"delta":{"content":"中🙂","tool_calls":[{"index":0,"id":"call1"}]},"finish_reason":"stop"}],"usage":{"completion_tokens":2},"encrypted_content":"opaque"}` + "\n\ndata: [DONE]\n\n"
	var packets []string
	var waits []time.Duration
	err := replaySSE(context.Background(), strings.NewReader(input), func(p []byte) error { packets = append(packets, string(p)); return nil }, &DeliveryAction{MinCPS: 20, MaxCPS: 20}, func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil })
	require.NoError(t, err)
	require.Len(t, packets, 3)
	assert.Equal(t, []time.Duration{50 * time.Millisecond, 50 * time.Millisecond}, waits)
	first := strings.TrimSpace(strings.TrimPrefix(packets[0], "data: "))
	last := strings.TrimSpace(strings.TrimPrefix(packets[1], "data: "))
	assert.Equal(t, "中", gjson.Get(first, "choices.0.delta.content").String())
	assert.Equal(t, "🙂", gjson.Get(last, "choices.0.delta.content").String())
	assert.True(t, gjson.Get(first, "choices.0.delta.tool_calls").Exists())
	assert.False(t, gjson.Get(last, "choices.0.delta.tool_calls").Exists())
	assert.False(t, gjson.Get(first, "usage").Exists())
	assert.Equal(t, "stop", gjson.Get(last, "choices.0.finish_reason").String())
	assert.Equal(t, "opaque", gjson.Get(last, "encrypted_content").String())
}

func TestUserControlReplayResponsesSequenceAndClaudeThinking(t *testing.T) {
	for _, input := range []string{
		"event: response.output_text.delta\nid: a\ndata: {\"type\":\"response.output_text.delta\",\"sequence_number\":7,\"delta\":\"ab\"}\n\ndata: {\"type\":\"response.completed\",\"sequence_number\":8}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"ab\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n",
	} {
		var packets []string
		err := replaySSE(context.Background(), strings.NewReader(input), func(p []byte) error { packets = append(packets, string(p)); return nil }, &DeliveryAction{MinCPS: 5, MaxCPS: 10}, func(_ context.Context, d time.Duration) error {
			assert.GreaterOrEqual(t, d, 100*time.Millisecond)
			assert.LessOrEqual(t, d, 200*time.Millisecond)
			return nil
		})
		require.NoError(t, err)
		require.Len(t, packets, 3)
		if strings.Contains(input, "sequence_number") {
			for i, p := range packets {
				assert.Contains(t, p, fmt.Sprintf(`"sequence_number":%d`, i))
			}
			assert.Contains(t, packets[0], "id: a")
		}
	}
}

func TestUserControlReplayFailsClosedOnInvalidMissingTerminalAndCancellation(t *testing.T) {
	require.Error(t, replaySSE(context.Background(), strings.NewReader("data: "+strings.Repeat("x", maxDeliveryEventBytes)+"\n\n"), nil, nil, nil))
	for _, input := range []string{
		"data: not-json\n\n", "data: {}\n\n", "data: [DONE]\n",
		"data: {\"choices\":[{},{}]}\n\ndata: [DONE]\n\n",
		"data: {\"error\":{\"message\":\"failed\"}}\n\ndata: [DONE]\n\n",
	} {
		require.Error(t, replaySSE(context.Background(), strings.NewReader(input), nil, nil, nil), input)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, waitDelivery(ctx, time.Hour), context.Canceled)
	emitErr := errors.New("client disconnected")
	require.ErrorIs(t, replaySSE(context.Background(), strings.NewReader("data: [DONE]\n\n"), func([]byte) error { return emitErr }, nil, nil), emitErr)
}

func TestUserControlDeliveryWriterNeverLeaksBeforeReplayAndCapsCache(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "cache")
	require.NoError(t, err)
	defer file.Close()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	cancelled := false
	w := &deliveryWriter{ResponseWriter: c.Writer, file: file, header: c.Writer.Header().Clone(), status: 200, size: -1, cancel: func() { cancelled = true }}
	w.WriteHeader(201)
	_, err = w.WriteString("data: [DONE]\n\n")
	require.NoError(t, err)
	w.Flush()
	assert.Equal(t, 201, w.Status())
	assert.True(t, w.Written())
	assert.Empty(t, recorder.Body.String())
	_, err = w.Write(make([]byte, maxDeliveryBytes))
	require.Error(t, err)
	assert.True(t, cancelled)
	_, err = file.Seek(0, io.SeekStart)
	require.NoError(t, err)
	stored, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, "data: [DONE]\n\n", string(stored))
}

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
