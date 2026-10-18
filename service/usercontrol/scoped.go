package usercontrol

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

// ScopedRule is one independent per-user bucket. Models are client model names;
// channels are actual selected channel IDs. Both filters, when set, must match.
// Put rules in ScheduleRule.Limits to activate them only during that window.
type ScopedRule struct {
	ID             string   `json:"id"`
	Name           string   `json:"name,omitempty"`
	Enabled        *bool    `json:"enabled,omitempty"`
	Models         []string `json:"models,omitempty"`
	ChannelIDs     []int    `json:"channel_ids,omitempty"`
	Limits         Limits   `json:"limits"`
	Actions        *Actions `json:"actions,omitempty"`
	Namespace      string   `json:"-"`
	Priority       int      `json:"priority,omitempty"`
	PolicyPriority int      `json:"-"`
	Override       bool     `json:"-"`
}

type Actions struct {
	Delay           *DelayAction    `json:"delay,omitempty"`
	Delivery        *DeliveryAction `json:"delivery,omitempty"`
	RequestMode     string          `json:"request_mode,omitempty"`
	RequestBody     string          `json:"request_body,omitempty"`
	PromptPrefix    string          `json:"prompt_prefix,omitempty"`
	PromptSuffix    string          `json:"prompt_suffix,omitempty"`
	RouteModel      string          `json:"route_model,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	Response        *StaticResponse `json:"response,omitempty"`
}

// A static response is explicitly synthetic, never an upstream success or usage.
// JSON responses are non-streaming; text responses support Chat Completions SSE.
type StaticResponse struct {
	Mode      string `json:"mode"` // text, json, error
	Body      string `json:"body"`
	Status    int    `json:"status,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

// Repeated references to the same rule tighten its limits without counting or
// applying prompt injection twice. Separate policies retain separate namespaces.
func mergeScopedRules(base, extra []ScopedRule) []ScopedRule {
	out := append([]ScopedRule(nil), base...)
	indexes := make(map[string]int, len(base)+len(extra))
	for i, rule := range out {
		indexes[rule.Bucket(0)] = i
	}
	for _, rule := range extra {
		key := rule.Bucket(0)
		if i, ok := indexes[key]; ok {
			out[i].Limits = Merge(out[i].Limits, rule.Limits)
			if rule.Actions != nil {
				out[i].Actions = rule.Actions
			}
		} else {
			indexes[key] = len(out)
			out = append(out, rule)
		}
	}
	return out
}

func (r ScopedRule) Matches(model string, channelID int) bool {
	return (r.Enabled == nil || *r.Enabled) &&
		(len(r.Models) == 0 || containsString(r.Models, model)) &&
		(len(r.ChannelIDs) == 0 || containsInt(r.ChannelIDs, channelID))
}

func (r ScopedRule) Bucket(userID int) string {
	// Exclude thresholds/actions so editing a limit does not reset its bucket.
	identity, _ := common.Marshal([]any{r.Namespace, r.ID, r.Models, r.ChannelIDs})
	return fmt.Sprintf("uc:scope:%d:%x", userID, sha256.Sum256(identity))
}

func normalizeScopedRules(rules []ScopedRule) error {
	if len(rules) > 64 {
		return fmt.Errorf("%w: at most 64 scoped rules are allowed", ErrInvalidInput)
	}
	seen := make(map[string]bool)
	for i := range rules {
		r := &rules[i]
		if r.ID == "" {
			r.ID = common.GetUUID()
		}
		if len(r.ID) > 128 || utf8.RuneCountInString(r.Name) > 128 || seen[r.ID] {
			return fmt.Errorf("%w: invalid or duplicate scoped rule identifier", ErrInvalidInput)
		}
		seen[r.ID] = true
		var err error
		r.Models, err = normalizeStringList(r.Models)
		if err != nil {
			return fmt.Errorf("%w: scoped model list", ErrInvalidInput)
		}
		r.ChannelIDs = normalizePositiveInts(r.ChannelIDs)
		for _, id := range r.ChannelIDs {
			if id <= 0 {
				return fmt.Errorf("%w: scoped channel IDs must be positive", ErrInvalidInput)
			}
		}
		l := &r.Limits
		if l.RandomPool != nil || len(l.ScopedRules) > 0 || l.Models != nil || l.Channels != nil || l.APIKeys != nil || l.AllowPlayground != nil || l.AllowRelay != nil {
			return fmt.Errorf("%w: scoped rules support blocking, rate, requests and usage only", ErrInvalidInput)
		}
		if err := NormalizeLimits(l); err != nil {
			return err
		}
		if l.Rate != nil && l.Rate.Burst != 0 {
			return fmt.Errorf("%w: scoped burst is not supported; use RPM", ErrInvalidInput)
		}
		if r.Actions != nil {
			if err := normalizeActions(r.Actions); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeActions(a *Actions) error {
	if d := a.Delay; d != nil && (d.MinMS < 0 || d.MaxMS < d.MinMS || d.MaxMS > 120000) {
		return fmt.Errorf("%w: delay must be between 0 and 120000 ms", ErrInvalidInput)
	}
	if d := a.Delivery; d != nil && (d.MinCPS < 1 || d.MaxCPS < d.MinCPS || d.MaxCPS > 1000) {
		return fmt.Errorf("%w: delivery rate must be between 1 and 1000 characters/s", ErrInvalidInput)
	}
	if a.RequestMode != "" && a.RequestMode != "merge" && a.RequestMode != "replace" {
		return fmt.Errorf("%w: invalid request replacement mode", ErrInvalidInput)
	}
	for _, value := range []string{a.RequestBody, a.PromptPrefix, a.PromptSuffix} {
		if len(value) > 65536 {
			return fmt.Errorf("%w: action content exceeds 64 KiB", ErrInvalidInput)
		}
	}
	if a.RequestBody != "" {
		var body map[string]common.RawMessage
		if err := common.Unmarshal([]byte(a.RequestBody), &body); err != nil || body == nil {
			return fmt.Errorf("%w: request replacement must be a JSON object", ErrInvalidInput)
		}
		// These fields affect access checks, protocol or routing. Dedicated controls
		// must be used so a JSON template cannot silently bypass those checks.
		for key := range body {
			for _, protected := range []string{"model", "group", "stream"} {
				if strings.EqualFold(key, protected) {
					return fmt.Errorf("%w: request template cannot set %s", ErrInvalidInput, protected)
				}
			}
		}
	}
	if len(a.RouteModel) > 128 || strings.TrimSpace(a.RouteModel) != a.RouteModel {
		return fmt.Errorf("%w: invalid routed model", ErrInvalidInput)
	}
	switch a.ReasoningEffort {
	case "", "none", "minimal", "low", "medium", "high", "xhigh":
	default:
		return fmt.Errorf("%w: invalid reasoning effort", ErrInvalidInput)
	}
	if r := a.Response; r != nil {
		if len(r.Body) > 65536 || len(r.ErrorCode) > 128 {
			return fmt.Errorf("%w: response content exceeds limit", ErrInvalidInput)
		}
		switch r.Mode {
		case "text", "json":
			if r.Status == 0 {
				r.Status = 200
			}
			if r.Status != 200 {
				return fmt.Errorf("%w: synthetic content requires status 200", ErrInvalidInput)
			}
			if r.Mode == "json" {
				var value any
				if err := common.Unmarshal([]byte(r.Body), &value); err != nil {
					return fmt.Errorf("%w: invalid synthetic JSON", ErrInvalidInput)
				}
			}
		case "error":
			if r.Status == 0 {
				r.Status = 429
			}
			if r.Status < 400 || r.Status > 599 {
				return fmt.Errorf("%w: error status must be 400..599", ErrInvalidInput)
			}
		default:
			return fmt.Errorf("%w: invalid synthetic response mode", ErrInvalidInput)
		}
	}
	return nil
}

// AdmitScoped uses distinct buckets for every user and rule. Settlement limits
// are admission checks, not hard reservations: in-flight requests may overshoot.
func AdmitScoped(r ScopedRule, userID int, now time.Time, loc *time.Location) (func(), string, error) {
	key := r.Bucket(userID)
	if r.Limits.Blocked != nil && *r.Limits.Blocked {
		return func() {}, "blocked", nil
	}
	rate, requests := RateLimits{}, RequestLimits{}
	if r.Limits.Rate != nil {
		rate = *r.Limits.Rate
	}
	if r.Limits.Requests != nil {
		requests = *r.Limits.Requests
	}
	release, allowed, err := AcquireConcurrent(key+":concurrent", rate.Concurrent)
	if err != nil {
		return release, "", err
	}
	if !allowed {
		return release, "concurrent", nil
	}
	windows := []struct {
		suffix string
		limit  int
		ttl    time.Duration
	}{
		{fmt.Sprintf(":rpm:%d", now.Unix()/60), rate.RPM, 2 * time.Minute},
		{":rpd:" + now.In(loc).Format("20060102"), rate.RPD, 48 * time.Hour},
		{":daily:" + now.In(loc).Format("20060102"), requests.Daily, 48 * time.Hour},
		{":monthly:" + now.In(loc).Format("200601"), requests.Monthly, 62 * 24 * time.Hour},
	}
	for _, w := range windows {
		allowed, err = AllowFixedWindow(key+w.suffix, w.limit, w.ttl)
		if err != nil || !allowed {
			release()
			return func() {}, "requests", err
		}
	}
	if l := r.Limits.Usage; l != nil {
		d, err := scopedUsageHash(key + ":usage:d:" + now.In(loc).Format("20060102"))
		if err != nil {
			release()
			return func() {}, "", err
		}
		m, err := scopedUsageHash(key + ":usage:m:" + now.In(loc).Format("200601"))
		if err != nil {
			release()
			return func() {}, "", err
		}
		if (l.DailyQuota > 0 && d["quota"] >= int64(l.DailyQuota)) ||
			(l.MonthlyQuota > 0 && m["quota"] >= int64(l.MonthlyQuota)) ||
			(l.InputTokens > 0 && m["input"] >= int64(l.InputTokens)) ||
			(l.OutputTokens > 0 && m["output"] >= int64(l.OutputTokens)) {
			release()
			return func() {}, "usage", nil
		}
	}
	return release, "", nil
}

// Unlike legacy aggregate usage counters, new scoped limits fail closed when
// Redis is configured but unavailable; an outage must not reset a user's quota.
func scopedUsageHash(key string) (map[string]int64, error) {
	if !redisReady() {
		return hashGetAll(key), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	values, err := common.RDB.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(values))
	for key, value := range values {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("invalid scoped usage counter")
		}
		out[key] = parsed
	}
	return out, nil
}

func recordScopedUsage(r ScopedRule, userID int, now time.Time, loc *time.Location, quota, input, output int64) {
	key := r.Bucket(userID)
	for field, value := range map[string]int64{"quota": quota, "input": input, "output": output} {
		if value <= 0 {
			continue
		}
		if err := hashIncr(key+":usage:d:"+now.In(loc).Format("20060102"), field, value, 48*time.Hour); err != nil {
			common.SysError("user control scoped usage write failed")
		}
		if err := hashIncr(key+":usage:m:"+now.In(loc).Format("200601"), field, value, 62*24*time.Hour); err != nil {
			common.SysError("user control scoped usage write failed")
		}
	}
}
