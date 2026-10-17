package usercontrol

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
