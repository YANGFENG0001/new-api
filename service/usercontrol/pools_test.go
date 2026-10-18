package usercontrol

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPoolContinuousSegmentsSeekWithoutMidnightReset(t *testing.T) {
	require.Equal(t, int64(0), poolSegment("stable", 9, 10, 10))
	require.Equal(t, int64(1), poolSegment("stable", 10, 10, 10))
	require.Equal(t, int64(14400), poolSegment("stable", 144000, 10, 10))

}

func TestPoolWindowOvernightUsesStartDayAndHasNoOutsideBehavior(t *testing.T) {
	cfg := PoolConfig{PoolTiming: PoolTiming{Probability: 10000, HoldMin: 10, HoldMax: 10}, Schedule: &Schedule{Timezone: "UTC", Rules: []ScheduleRule{{ID: "friday", Days: []int{5}, Start: "22:00", End: "06:00"}}}}
	anchor := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	_, start, id, active, err := poolWindow(cfg, anchor.Add(5*time.Hour), time.Time{})
	require.NoError(t, err)
	require.True(t, active)
	require.Equal(t, "friday", id)
	require.Equal(t, anchor, start)
	_, _, _, active, err = poolWindow(cfg, anchor.Add(8*time.Hour), time.Time{})
	require.NoError(t, err)
	require.False(t, active)
}

func TestPoolOverlappingWindowsUsePriorityThenIdentifier(t *testing.T) {
	cfg := PoolConfig{PoolTiming: PoolTiming{Probability: 10000, HoldMin: 1, HoldMax: 1}, Schedule: &Schedule{Timezone: "UTC", Rules: []ScheduleRule{{ID: "z", Priority: 2}, {ID: "a", Priority: 2}, {ID: "higher", Priority: 3, Enabled: boolPtr(false)}}}, WindowOptions: map[string]PoolTiming{"a": {Probability: 0, HoldMin: 5, HoldMax: 5}}}
	opt, _, id, active, err := poolWindow(cfg, time.Now(), time.Time{})
	require.NoError(t, err)
	require.True(t, active)
	require.Equal(t, "a", id)
	require.Zero(t, opt.Probability)
}
