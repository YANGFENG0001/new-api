package usercontrol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

var ErrInvalidInput = errors.New("invalid user control input")

// ParseLimitsJSON validates and normalizes a limits JSON object.
func ParseLimitsJSON(raw json.RawMessage) (Limits, string, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return Limits{}, "", nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var limits Limits
	if err := decoder.Decode(&limits); err != nil {
		return Limits{}, "", fmt.Errorf("%w: limits: %v", ErrInvalidInput, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Limits{}, "", fmt.Errorf("%w: limits: %v", ErrInvalidInput, err)
	}
	if err := NormalizeLimits(&limits); err != nil {
		return Limits{}, "", err
	}
	encoded, err := common.Marshal(limits)
	if err != nil {
		return Limits{}, "", err
	}
	return limits, string(encoded), nil
}

// ParseScheduleJSON validates and normalizes a schedule JSON object. A nil or
// empty value means that the policy has no schedule.
func ParseScheduleJSON(raw json.RawMessage) (*Schedule, string, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, "", nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var schedule Schedule
	if err := decoder.Decode(&schedule); err != nil {
		return nil, "", fmt.Errorf("%w: schedule: %v", ErrInvalidInput, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, "", fmt.Errorf("%w: schedule: %v", ErrInvalidInput, err)
	}
	if err := NormalizeSchedule(&schedule); err != nil {
		return nil, "", err
	}
	encoded, err := common.Marshal(schedule)
	if err != nil {
		return nil, "", err
	}
	return &schedule, string(encoded), nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func NormalizeLimits(limits *Limits) error {
	if limits == nil {
		return nil
	}
	if err := normalizeScopedRules(limits.ScopedRules); err != nil {
		return err
	}
	validateNonNegative := func(name string, values ...int) error {
		for _, value := range values {
			if value < 0 {
				return fmt.Errorf("%w: %s cannot be negative", ErrInvalidInput, name)
			}
		}
		return nil
	}
	if limits.Rate != nil {
		if err := validateNonNegative("rate", limits.Rate.RPM, limits.Rate.RPD, limits.Rate.Concurrent, limits.Rate.Burst); err != nil {
			return err
		}
	}
	if limits.Requests != nil {
		if err := validateNonNegative("requests", limits.Requests.Daily, limits.Requests.Monthly); err != nil {
			return err
		}
	}
	if limits.Usage != nil {
		if err := validateNonNegative("usage", limits.Usage.DailyQuota, limits.Usage.MonthlyQuota, limits.Usage.InputTokens, limits.Usage.OutputTokens); err != nil {
			return err
		}
	}
	if limits.APIKeys != nil {
		if err := validateNonNegative("api_keys", limits.APIKeys.MaxActive, limits.APIKeys.MaxTotal); err != nil {
			return err
		}
	}
	if limits.Models != nil {
		normalizedAllow, err := normalizeStringList(limits.Models.Allow)
		if err != nil {
			return fmt.Errorf("%w: models.allow: %v", ErrInvalidInput, err)
		}
		normalizedBlock, err := normalizeStringList(limits.Models.Block)
		if err != nil {
			return fmt.Errorf("%w: models.block: %v", ErrInvalidInput, err)
		}
		if overlap := stringOverlap(normalizedAllow, normalizedBlock); len(overlap) > 0 {
			return fmt.Errorf("%w: models cannot be both allowed and blocked: %s", ErrInvalidInput, strings.Join(overlap, ", "))
		}
		limits.Models.Allow = normalizedAllow
		limits.Models.Block = normalizedBlock
	}
	if limits.Channels != nil {
		limits.Channels.AllowIDs = normalizePositiveInts(limits.Channels.AllowIDs)
		limits.Channels.BlockIDs = normalizePositiveInts(limits.Channels.BlockIDs)
		limits.Channels.AllowTypes = normalizePositiveInts(limits.Channels.AllowTypes)
		limits.Channels.BlockTypes = normalizePositiveInts(limits.Channels.BlockTypes)
		for name, values := range map[string][]int{
			"channels.allow_ids":   limits.Channels.AllowIDs,
			"channels.block_ids":   limits.Channels.BlockIDs,
			"channels.allow_types": limits.Channels.AllowTypes,
			"channels.block_types": limits.Channels.BlockTypes,
		} {
			for _, value := range values {
				if value <= 0 {
					return fmt.Errorf("%w: %s must contain positive integers", ErrInvalidInput, name)
				}
			}
		}
		if overlap := intOverlap(limits.Channels.AllowIDs, limits.Channels.BlockIDs); len(overlap) > 0 {
			return fmt.Errorf("%w: channel IDs cannot be both allowed and blocked", ErrInvalidInput)
		}
		if overlap := intOverlap(limits.Channels.AllowTypes, limits.Channels.BlockTypes); len(overlap) > 0 {
			return fmt.Errorf("%w: channel types cannot be both allowed and blocked", ErrInvalidInput)
		}
		groups, err := normalizeStringList(limits.Channels.AllowGroups)
		if err != nil {
			return fmt.Errorf("%w: channels.allow_groups: %v", ErrInvalidInput, err)
		}
		limits.Channels.AllowGroups = groups
	}
	return nil
}

func NormalizeSchedule(schedule *Schedule) error {
	if schedule == nil {
		return nil
	}
	schedule.Timezone = strings.TrimSpace(schedule.Timezone)
	if schedule.Timezone == "" {
		schedule.Timezone = DefaultTimezone
	}
	if _, err := time.LoadLocation(schedule.Timezone); err != nil {
		return fmt.Errorf("%w: invalid timezone %q", ErrInvalidInput, schedule.Timezone)
	}
	if schedule.Default != nil {
		if err := NormalizeLimits(schedule.Default); err != nil {
			return err
		}
	}
	seenIDs := make(map[string]struct{}, len(schedule.Rules))
	for index := range schedule.Rules {
		rule := &schedule.Rules[index]
		rule.ID = strings.TrimSpace(rule.ID)
		rule.Name = strings.TrimSpace(rule.Name)
		if len(rule.Name) > 64 {
			return fmt.Errorf("%w: schedule rule name is too long", ErrInvalidInput)
		}
		if rule.ID == "" {
			rule.ID = "rule-" + common.GetUUID()
		}
		if _, exists := seenIDs[rule.ID]; exists {
			return fmt.Errorf("%w: duplicate schedule rule id %q", ErrInvalidInput, rule.ID)
		}
		seenIDs[rule.ID] = struct{}{}
		if rule.Enabled == nil {
			enabled := true
			rule.Enabled = &enabled
		}
		days := normalizePositiveInts(rule.Days)
		for _, day := range days {
			if day < 1 || day > 7 {
				return fmt.Errorf("%w: schedule rule weekdays must be between 1 and 7", ErrInvalidInput)
			}
		}
		rule.Days = days
		dates, err := normalizeStringList(rule.Dates)
		if err != nil {
			return fmt.Errorf("%w: schedule rule dates: %v", ErrInvalidInput, err)
		}
		rule.Dates = dates
		for _, date := range dates {
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return fmt.Errorf("%w: invalid schedule date %q", ErrInvalidInput, date)
			}
		}
		rule.StartDate = strings.TrimSpace(rule.StartDate)
		rule.EndDate = strings.TrimSpace(rule.EndDate)
		if rule.StartDate != "" {
			if _, err := time.Parse("2006-01-02", rule.StartDate); err != nil {
				return fmt.Errorf("%w: invalid start_date %q", ErrInvalidInput, rule.StartDate)
			}
		}
		if rule.EndDate != "" {
			if _, err := time.Parse("2006-01-02", rule.EndDate); err != nil {
				return fmt.Errorf("%w: invalid end_date %q", ErrInvalidInput, rule.EndDate)
			}
		}
		if rule.StartDate != "" && rule.EndDate != "" && rule.StartDate > rule.EndDate {
			return fmt.Errorf("%w: start_date must not be after end_date", ErrInvalidInput)
		}
		rule.Start = strings.TrimSpace(rule.Start)
		rule.End = strings.TrimSpace(rule.End)
		if _, err := parseClock(rule.Start); err != nil {
			return fmt.Errorf("%w: invalid start time %q", ErrInvalidInput, rule.Start)
		}
		if _, err := parseClock(rule.End); err != nil {
			return fmt.Errorf("%w: invalid end time %q", ErrInvalidInput, rule.End)
		}
		if err := NormalizeLimits(&rule.Limits); err != nil {
			return err
		}
	}
	return nil
}

func normalizeStringList(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len(value) > 128 {
			return nil, errors.New("value is too long")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

func normalizePositiveInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	out := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}

func stringOverlap(left, right []string) []string {
	set := make(map[string]struct{}, len(left))
	for _, value := range left {
		set[value] = struct{}{}
	}
	var overlap []string
	for _, value := range right {
		if _, ok := set[value]; ok {
			overlap = append(overlap, value)
		}
	}
	return overlap
}

func intOverlap(left, right []int) []int {
	set := make(map[int]struct{}, len(left))
	for _, value := range left {
		set[value] = struct{}{}
	}
	var overlap []int
	for _, value := range right {
		if _, ok := set[value]; ok {
			overlap = append(overlap, value)
		}
	}
	return overlap
}
