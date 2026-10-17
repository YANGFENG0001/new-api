// Package usercontrol implements user-scoped policy resolution and request
// enforcement for the user control feature.
package usercontrol

import (
	_ "time/tzdata"

	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const DefaultTimezone = "Asia/Shanghai"

type Limits struct {
	Blocked         *bool          `json:"blocked,omitempty"`
	AllowRelay      *bool          `json:"allow_relay,omitempty"`
	AllowPlayground *bool          `json:"allow_playground,omitempty"`
	Rate            *RateLimits    `json:"rate,omitempty"`
	Requests        *RequestLimits `json:"requests,omitempty"`
	Usage           *UsageLimits   `json:"usage,omitempty"`
	Models          *ModelLimits   `json:"models,omitempty"`
	Channels        *ChannelLimits `json:"channels,omitempty"`
	APIKeys         *APIKeyLimits  `json:"api_keys,omitempty"`
}

type RateLimits struct {
	RPM        int `json:"rpm,omitempty"`
	RPD        int `json:"rpd,omitempty"`
	Concurrent int `json:"concurrent,omitempty"`
	Burst      int `json:"burst,omitempty"`
}

type RequestLimits struct {
	Daily   int `json:"daily,omitempty"`
	Monthly int `json:"monthly,omitempty"`
}

type UsageLimits struct {
	DailyQuota   int `json:"daily_quota,omitempty"`
	MonthlyQuota int `json:"monthly_quota,omitempty"`
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

type ModelLimits struct {
	Allow []string `json:"allow,omitempty"`
	Block []string `json:"block,omitempty"`
}

type ChannelLimits struct {
	AllowIDs    []int    `json:"allow_ids,omitempty"`
	BlockIDs    []int    `json:"block_ids,omitempty"`
	AllowTypes  []int    `json:"allow_types,omitempty"`
	BlockTypes  []int    `json:"block_types,omitempty"`
	AllowGroups []string `json:"allow_groups,omitempty"`
}

type APIKeyLimits struct {
	MaxActive int `json:"max_active,omitempty"`
	MaxTotal  int `json:"max_total,omitempty"`
}

type Schedule struct {
	Timezone string         `json:"timezone,omitempty"`
	Rules    []ScheduleRule `json:"rules,omitempty"`
	Default  *Limits        `json:"default,omitempty"`
}

type ScheduleRule struct {
	ID        string   `json:"id,omitempty"`
	Name      string   `json:"name,omitempty"`
	Enabled   *bool    `json:"enabled,omitempty"`
	Priority  int      `json:"priority,omitempty"`
	Days      []int    `json:"days,omitempty"`
	Dates     []string `json:"dates,omitempty"`
	StartDate string   `json:"start_date,omitempty"`
	EndDate   string   `json:"end_date,omitempty"`
	Start     string   `json:"start,omitempty"`
	End       string   `json:"end,omitempty"`
	Limits    Limits   `json:"limits"`
}

func ParseLimits(raw string) (Limits, error) {
	var limits Limits
	if strings.TrimSpace(raw) == "" {
		return limits, nil
	}
	if err := json.Unmarshal([]byte(raw), &limits); err != nil {
		return Limits{}, err
	}
	return limits, nil
}

func ParseSchedule(raw string) (*Schedule, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var schedule Schedule
	if err := json.Unmarshal([]byte(raw), &schedule); err != nil {
		return nil, err
	}
	if schedule.Timezone == "" {
		schedule.Timezone = DefaultTimezone
	}
	if _, err := time.LoadLocation(schedule.Timezone); err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", schedule.Timezone, err)
	}
	return &schedule, nil
}

func (l Limits) Empty() bool {
	return l.Blocked == nil && l.AllowRelay == nil && l.AllowPlayground == nil &&
		l.Rate == nil && l.Requests == nil && l.Usage == nil &&
		l.Models == nil && l.Channels == nil && l.APIKeys == nil
}

func boolPtr(v bool) *bool { return &v }

func minPositive(a, b int) int {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func intersectStrings(base, extra []string) []string {
	if len(base) == 0 {
		return append([]string(nil), extra...)
	}
	if len(extra) == 0 {
		return append([]string(nil), base...)
	}
	set := make(map[string]struct{}, len(extra))
	for _, v := range extra {
		set[v] = struct{}{}
	}
	out := make([]string, 0, len(base))
	for _, v := range base {
		if _, ok := set[v]; ok {
			out = append(out, v)
		}
	}
	return out
}

func unionStrings(base, extra []string) []string {
	if len(extra) == 0 {
		return append([]string(nil), base...)
	}
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, v := range append(append([]string(nil), base...), extra...) {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func intersectInts(base, extra []int) []int {
	if len(base) == 0 {
		return append([]int(nil), extra...)
	}
	if len(extra) == 0 {
		return append([]int(nil), base...)
	}
	set := make(map[int]struct{}, len(extra))
	for _, v := range extra {
		set[v] = struct{}{}
	}
	out := make([]int, 0, len(base))
	for _, v := range base {
		if _, ok := set[v]; ok {
			out = append(out, v)
		}
	}
	return out
}

func unionInts(base, extra []int) []int {
	if len(extra) == 0 {
		return append([]int(nil), base...)
	}
	seen := make(map[int]struct{}, len(base)+len(extra))
	out := make([]int, 0, len(base)+len(extra))
	for _, v := range append(append([]int(nil), base...), extra...) {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func mergeRate(base, extra *RateLimits) *RateLimits {
	if base == nil && extra == nil {
		return nil
	}
	out := &RateLimits{}
	if base != nil {
		*out = *base
	}
	if extra == nil {
		return out
	}
	out.RPM = minPositive(out.RPM, extra.RPM)
	out.RPD = minPositive(out.RPD, extra.RPD)
	out.Concurrent = minPositive(out.Concurrent, extra.Concurrent)
	out.Burst = minPositive(out.Burst, extra.Burst)
	return out
}

func mergeRequests(base, extra *RequestLimits) *RequestLimits {
	if base == nil && extra == nil {
		return nil
	}
	out := &RequestLimits{}
	if base != nil {
		*out = *base
	}
	if extra == nil {
		return out
	}
	out.Daily = minPositive(out.Daily, extra.Daily)
	out.Monthly = minPositive(out.Monthly, extra.Monthly)
	return out
}

func mergeUsage(base, extra *UsageLimits) *UsageLimits {
	if base == nil && extra == nil {
		return nil
	}
	out := &UsageLimits{}
	if base != nil {
		*out = *base
	}
	if extra == nil {
		return out
	}
	out.DailyQuota = minPositive(out.DailyQuota, extra.DailyQuota)
	out.MonthlyQuota = minPositive(out.MonthlyQuota, extra.MonthlyQuota)
	out.InputTokens = minPositive(out.InputTokens, extra.InputTokens)
	out.OutputTokens = minPositive(out.OutputTokens, extra.OutputTokens)
	return out
}

func mergeModels(base, extra *ModelLimits) *ModelLimits {
	if base == nil && extra == nil {
		return nil
	}
	out := &ModelLimits{}
	if base != nil {
		out.Allow = append([]string(nil), base.Allow...)
		out.Block = append([]string(nil), base.Block...)
	}
	if extra == nil {
		return out
	}
	out.Allow = intersectStrings(out.Allow, extra.Allow)
	out.Block = unionStrings(out.Block, extra.Block)
	return out
}

func mergeChannels(base, extra *ChannelLimits) *ChannelLimits {
	if base == nil && extra == nil {
		return nil
	}
	out := &ChannelLimits{}
	if base != nil {
		*out = *base
		out.AllowIDs = append([]int(nil), base.AllowIDs...)
		out.BlockIDs = append([]int(nil), base.BlockIDs...)
		out.AllowTypes = append([]int(nil), base.AllowTypes...)
		out.BlockTypes = append([]int(nil), base.BlockTypes...)
		out.AllowGroups = append([]string(nil), base.AllowGroups...)
	}
	if extra == nil {
		return out
	}
	out.AllowIDs = intersectInts(out.AllowIDs, extra.AllowIDs)
	out.BlockIDs = unionInts(out.BlockIDs, extra.BlockIDs)
	out.AllowTypes = intersectInts(out.AllowTypes, extra.AllowTypes)
	out.BlockTypes = unionInts(out.BlockTypes, extra.BlockTypes)
	out.AllowGroups = intersectStrings(out.AllowGroups, extra.AllowGroups)
	return out
}

func mergeAPIKeys(base, extra *APIKeyLimits) *APIKeyLimits {
	if base == nil && extra == nil {
		return nil
	}
	out := &APIKeyLimits{}
	if base != nil {
		*out = *base
	}
	if extra == nil {
		return out
	}
	out.MaxActive = minPositive(out.MaxActive, extra.MaxActive)
	out.MaxTotal = minPositive(out.MaxTotal, extra.MaxTotal)
	return out
}

// Merge tightens base with extra. Numeric limits take the smallest non-zero
// value; allow-lists intersect; block-lists union; booleans AND.
func Merge(base, extra Limits) Limits {
	out := base
	if extra.Blocked != nil {
		out.Blocked = boolPtr((base.Blocked != nil && *base.Blocked) || *extra.Blocked)
	}
	if extra.AllowRelay != nil {
		out.AllowRelay = boolPtr((base.AllowRelay == nil || *base.AllowRelay) && *extra.AllowRelay)
	}
	if extra.AllowPlayground != nil {
		out.AllowPlayground = boolPtr((base.AllowPlayground == nil || *base.AllowPlayground) && *extra.AllowPlayground)
	}
	out.Rate = mergeRate(base.Rate, extra.Rate)
	out.Requests = mergeRequests(base.Requests, extra.Requests)
	out.Usage = mergeUsage(base.Usage, extra.Usage)
	out.Models = mergeModels(base.Models, extra.Models)
	out.Channels = mergeChannels(base.Channels, extra.Channels)
	out.APIKeys = mergeAPIKeys(base.APIKeys, extra.APIKeys)
	return out
}

func parseClock(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, fmt.Errorf("invalid clock %q", value)
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

// ActiveLimits returns the limits that apply at now for one policy. Base
// limits are always applied; the matching rules at the highest priority are
// merged on top with the usual "stricter wins" rules. Rules with lower
// priority that also match are ignored. Schedule.Default is applied only when
// no rule matches, i.e. it is the fallback for uncovered time windows.
//
// A rule whose end time is earlier than its start time crosses midnight. Such
// a window is attributed to the day it starts on: a rule for Friday
// 22:00-06:00 is active on Saturday 02:00 and still counts as Friday for the
// weekday, date, and date-range filters.
func (s *Schedule) ActiveLimits(now time.Time, base Limits) (Limits, []string, string, error) {
	if s == nil {
		return base, nil, DefaultTimezone, nil
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return Limits{}, nil, "", err
	}
	local := now.In(loc)
	clock := local.Hour()*60 + local.Minute()

	type matchedRule struct {
		rule     ScheduleRule
		priority int
	}
	var matched []matchedRule
	maxPriority := 0
	for _, rule := range s.Rules {
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}
		start, err := parseClock(rule.Start)
		if err != nil {
			return Limits{}, nil, "", err
		}
		end, err := parseClock(rule.End)
		if err != nil {
			return Limits{}, nil, "", err
		}
		// anchor is the date the matching window starts on. It differs from
		// the current date only in the after-midnight part of an overnight
		// window.
		anchor := local
		if start != 0 || end != 0 {
			switch {
			case start == end:
				// A full-day window is expressed as 00:00-00:00.
			case start < end:
				if clock < start || clock >= end {
					continue
				}
			case clock >= start:
				// Evening part of an overnight window.
			case clock < end:
				// Morning part: the window started on the previous day.
				anchor = local.AddDate(0, 0, -1)
			default:
				continue
			}
		}
		dateKey := anchor.Format("2006-01-02")
		weekday := int(anchor.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		if len(rule.Days) > 0 && !containsInt(rule.Days, weekday) {
			continue
		}
		if len(rule.Dates) > 0 && !containsString(rule.Dates, dateKey) {
			continue
		}
		if rule.StartDate != "" && dateKey < rule.StartDate {
			continue
		}
		if rule.EndDate != "" && dateKey > rule.EndDate {
			continue
		}
		matched = append(matched, matchedRule{rule: rule, priority: rule.Priority})
		if rule.Priority > maxPriority || len(matched) == 1 {
			maxPriority = rule.Priority
		}
	}

	result := base
	if len(matched) == 0 && s.Default != nil {
		result = Merge(base, *s.Default)
	}
	hits := make([]string, 0, len(matched))
	for _, item := range matched {
		if item.priority != maxPriority {
			continue
		}
		result = Merge(result, item.rule.Limits)
		name := item.rule.Name
		if name == "" {
			name = item.rule.ID
		}
		if name == "" {
			name = "rule"
		}
		hits = append(hits, name)
	}
	return result, hits, s.Timezone, nil
}
func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (l Limits) AllowsModel(model string) bool {
	if l.Models == nil {
		return true
	}
	if len(l.Models.Allow) > 0 && !containsString(l.Models.Allow, model) {
		return false
	}
	return !containsString(l.Models.Block, model)
}

func (l Limits) AllowsChannel(channelID int, channelType int, groups string) bool {
	c := l.Channels
	if c == nil {
		return true
	}
	if len(c.BlockIDs) > 0 && containsInt(c.BlockIDs, channelID) {
		return false
	}
	if len(c.AllowIDs) > 0 && !containsInt(c.AllowIDs, channelID) {
		return false
	}
	if len(c.BlockTypes) > 0 && containsInt(c.BlockTypes, channelType) {
		return false
	}
	if len(c.AllowTypes) > 0 && !containsInt(c.AllowTypes, channelType) {
		return false
	}
	if len(c.AllowGroups) > 0 {
		allowed := false
		for _, group := range strings.Split(groups, ",") {
			group = strings.TrimSpace(group)
			if group != "" && containsString(c.AllowGroups, group) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}
