package model

import (
	"time"

	"github.com/gin-gonic/gin"
)

const deferredConsumeKey = "deferred_consume_logs"

type deferredConsumeLog struct {
	userID int
	params RecordConsumeLogParams
}

// DeliveryTiming describes actual client transport, never provider generation.
type DeliveryTiming struct {
	UpstreamDuration time.Duration
	Started          time.Time
	FirstByte        time.Time
	Finished         time.Time
	OutputStarted    time.Time
	Complete         bool
	DelayMS          int
}

// Only persistence of the consumption log is deferred. Upstream settlement,
// quota deduction and per-user usage counters remain unchanged.
func BeginDeferredConsumeLogs(c *gin.Context) {
	c.Set(deferredConsumeKey, []deferredConsumeLog{})
}

func FlushDeferredConsumeLogs(c *gin.Context, timing DeliveryTiming) {
	v, ok := c.Get(deferredConsumeKey)
	if !ok || v == nil {
		return
	}
	c.Set(deferredConsumeKey, nil)
	for _, pending := range v.([]deferredConsumeLog) {
		p := pending.params
		if p.Other == nil {
			p.Other = NewLogOther()
		}
		p.Other.MergeAdmin(map[string]any{
			"upstream_use_time": p.UseTimeSeconds, "upstream_frt": p.Other.public["frt"],
			"delivery_complete": timing.Complete, "control_delay_ms": timing.DelayMS,
			"delivery_duration_ms": timing.Finished.Sub(timing.OutputStarted).Milliseconds(),
		})
		if timing.UpstreamDuration > 0 {
			upstreamTPS := float64(p.CompletionTokens) / timing.UpstreamDuration.Seconds()
			p.Other.SetAdmin("upstream_duration_ms", timing.UpstreamDuration.Milliseconds())
			p.Other.SetAdmin("upstream_output_tps", upstreamTPS)
			c.Set("user_control_upstream_duration_ms", timing.UpstreamDuration.Milliseconds())
			c.Set("user_control_upstream_output_tps", upstreamTPS)
		}
		p.UseTimeSeconds = int(timing.Finished.Sub(timing.Started) / time.Second)
		if !timing.FirstByte.IsZero() {
			p.Other.SetPublic("frt", timing.FirstByte.Sub(timing.Started).Milliseconds())
		}
		if duration := timing.Finished.Sub(timing.OutputStarted).Seconds(); timing.Complete && duration > 0 {
			p.Other.SetPublic("client_output_tps", float64(p.CompletionTokens)/duration)
			c.Set("user_control_client_output_tps", float64(p.CompletionTokens)/duration)
		} else {
			p.Other.SetPublic("client_output_tps", nil)
		}
		if !timing.Complete {
			p.Other.SetPublic("stream_status", map[string]any{"status": "error", "end_reason": "client_delivery_incomplete", "error_count": 1})
		}
		RecordConsumeLog(c, pending.userID, p)
	}
}
