package usercontrol

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type DelayAction struct {
	MinMS int `json:"min_ms"`
	MaxMS int `json:"max_ms"`
}

// CPS is Unicode code points per second, not tokenizer-specific tokens.
// A sampled zero skips the additional pacing wait; it never blocks forever.
type DeliveryAction struct {
	MinCPS int `json:"min_cps"`
	MaxCPS int `json:"max_cps"`
}

const maxDeliveryBytes = 16 << 20
const maxDeliveryEventBytes = 1 << 20

var deliverySlots = make(chan struct{}, 32)

func randomRange(low, high int) int {
	width := new(big.Int).Sub(big.NewInt(int64(high)), big.NewInt(int64(low)))
	width.Add(width, big.NewInt(1))
	v, err := rand.Int(rand.Reader, width)
	if err != nil {
		return low
	}
	return low + int(v.Int64())
}

func waitDelivery(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Chunk long administrator-configured delays so converting milliseconds to a
// nanosecond Duration cannot overflow or turn a large wait into an instant one.
func waitUpstreamDelay(ctx context.Context, milliseconds int, wait func(context.Context, time.Duration) error) error {
	const maxChunkMS = int64((1<<63 - 1) / time.Millisecond)
	for remaining := int64(milliseconds); remaining > 0; {
		chunk := min(remaining, maxChunkMS)
		if err := wait(ctx, time.Duration(chunk)*time.Millisecond); err != nil {
			return err
		}
		remaining -= chunk
	}
	return ctx.Err()
}

// Deliver runs after final channel selection and before the relay. No network
// output escapes during capture; temporary bodies are private and always removed.
func Deliver(c *gin.Context) {
	v, ok := c.Get(runtimeKey)
	if !ok {
		c.Next()
		return
	}
	s := v.(*requestState)
	if s.Delay == nil && s.Delivery == nil {
		c.Next()
		return
	}
	requestCtx, cancelRequest := context.WithCancel(c.Request.Context())
	defer cancelRequest()
	c.Request = c.Request.WithContext(requestCtx)
	// Existing concurrency buckets expire after 10m. Keep their leases alive
	// throughout upstream waiting and replay rather than silently freeing a slot.
	keys := []string{ConcurrencyKey(c.GetInt("id"))}
	if effective, ok := FromContext(c); ok {
		for _, rule := range effective.Limits.ScopedRules {
			keys = append(keys, rule.Bucket(c.GetInt("id"))+":concurrent")
		}
	}
	leaseCtx, stopLease := context.WithCancel(requestCtx)
	leaseDone := make(chan struct{})
	go func() {
		defer close(leaseDone)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-leaseCtx.Done():
				return
			case <-ticker.C:
				if err := renewConcurrencyLeases(keys); err != nil {
					cancelRequest()
					return
				}
			}
		}
	}()
	defer func() { stopLease(); <-leaseDone }()
	started := time.Now()
	delayMS := 0
	if s.Delay != nil {
		delayMS = randomRange(s.Delay.MinMS, s.Delay.MaxMS)
	}
	if delayMS > 0 {
		if err := waitUpstreamDelay(c.Request.Context(), delayMS, waitDelivery); err != nil {
			c.Set("user_control_runtime_failed", true)
			c.Abort()
			return
		}
	}
	c.Set("user_control_delay_ms", delayMS)
	if s.Delivery == nil {
		c.Next()
		return
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		abortDelivery(c, http.StatusBadRequest)
		return
	}
	body, err := storage.Bytes()
	// Multi-choice, audio and nonstream bodies cannot be faithfully text-paced.
	modalities := gjson.GetBytes(body, "modalities")
	invalidModalities := modalities.Exists() && modalities.Type != gjson.Null && !modalities.IsArray()
	modalities.ForEach(func(_, value gjson.Result) bool {
		if value.String() != "text" {
			invalidModalities = true
		}
		return true
	})
	audio := gjson.GetBytes(body, "audio")
	if err != nil || !gjson.GetBytes(body, "stream").Bool() || gjson.GetBytes(body, "n").Int() > 1 || (audio.Exists() && audio.Type != gjson.Null) || invalidModalities {
		abortDelivery(c, http.StatusBadRequest)
		return
	}
	select {
	case deliverySlots <- struct{}{}:
		defer func() { <-deliverySlots }()
	default:
		abortDelivery(c, http.StatusServiceUnavailable)
		return
	}
	file, err := os.CreateTemp("", "newapi-delivery-*")
	if err != nil {
		abortDelivery(c, http.StatusServiceUnavailable)
		return
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	if err := file.Chmod(0600); err != nil {
		abortDelivery(c, http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Minute)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	original := c.Writer
	capture := &deliveryWriter{ResponseWriter: original, file: file, header: original.Header().Clone(), status: http.StatusOK, size: -1, cancel: cancel}
	timing := model.DeliveryTiming{Started: started, DelayMS: delayMS}
	model.BeginDeferredConsumeLogs(c)
	defer func() {
		c.Writer = original
		if !timing.Complete {
			c.Set("user_control_runtime_failed", true)
		}
		timing.Finished = time.Now()
		if timing.OutputStarted.IsZero() {
			timing.OutputStarted = timing.Finished
		}
		c.Set("user_control_delivery_complete", timing.Complete)
		c.Set("user_control_delivery_duration_ms", timing.Finished.Sub(timing.OutputStarted).Milliseconds())
		model.FlushDeferredConsumeLogs(c, timing)
	}()
	c.Writer = capture
	upstreamStarted := time.Now()
	c.Next()
	timing.UpstreamDuration = time.Since(upstreamStarted)
	c.Writer = original
	if capture.err != nil {
		abortDelivery(c, http.StatusBadGateway)
		return
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		abortDelivery(c, http.StatusBadGateway)
		return
	}
	isSSE := strings.HasPrefix(capture.header.Get("Content-Type"), "text/event-stream") && capture.status < 400
	if !isSSE && capture.status < 400 {
		abortDelivery(c, http.StatusBadGateway)
		return
	}
	if isSSE {
		// Validate the entire cached stream before exposing any partial success.
		if err = replaySSE(ctx, file, nil, s.Delivery, nil); err != nil {
			abortDelivery(c, http.StatusBadGateway)
			return
		}
		if _, err = file.Seek(0, io.SeekStart); err != nil {
			abortDelivery(c, http.StatusBadGateway)
			return
		}
	}
	for key, values := range capture.header {
		original.Header()[key] = append([]string(nil), values...)
	}
	transport := http.NewResponseController(original)
	stopWrite := context.AfterFunc(ctx, func() { _ = transport.SetWriteDeadline(time.Now()) })
	defer stopWrite()
	original.Header().Del("Content-Length")
	original.Header().Set("Cache-Control", "no-store")
	original.WriteHeader(capture.status)
	timing.OutputStarted = time.Now()
	if isSSE {
		err = replaySSE(ctx, file, func(packet []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			deadline := time.Now().Add(30 * time.Second)
			if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
				deadline = end
			}
			_ = transport.SetWriteDeadline(deadline)
			if _, err := original.Write(packet); err != nil {
				return err
			}
			original.Flush()
			if timing.FirstByte.IsZero() {
				timing.FirstByte = time.Now()
			}
			return nil
		}, s.Delivery, waitDelivery)
	} else {
		_ = transport.SetWriteDeadline(time.Now().Add(30 * time.Second))
		_, err = io.Copy(original, file)
		timing.FirstByte = time.Now()
	}
	timing.Complete = err == nil && ctx.Err() == nil && capture.status < 400 && !c.GetBool("user_control_runtime_failed")
	if !timing.Complete {
		c.Set("user_control_runtime_failed", true)
	}
}

func renewConcurrencyLeases(keys []string) error {
	if redisReady() {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		pipe := common.RDB.Pipeline()
		for _, key := range keys {
			pipe.Expire(ctx, key, 10*time.Minute)
		}
		_, err := pipe.Exec(ctx)
		return err
	}
	memMu.Lock()
	defer memMu.Unlock()
	now := time.Now()
	for _, key := range keys {
		if item, ok := memCounters[key]; ok && item.value > 0 && now.Before(item.expires) {
			item.expires = now.Add(10 * time.Minute)
			memCounters[key] = item
		}
	}
	return nil
}

func abortDelivery(c *gin.Context, status int) {
	c.Set("user_control_runtime_failed", true)
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"message": i18n.T(c, i18n.MsgUserControlActionInvalid), "type": "user_control_delivery_failed"}})
}

type deliveryWriter struct {
	gin.ResponseWriter
	file   *os.File
	header http.Header
	status int
	size   int
	err    error
	cancel context.CancelFunc
}

func (w *deliveryWriter) Header() http.Header { return w.header }
func (w *deliveryWriter) Status() int         { return w.status }
func (w *deliveryWriter) Size() int           { return w.size }
func (w *deliveryWriter) Written() bool       { return w.size >= 0 }
func (w *deliveryWriter) WriteHeader(code int) {
	if !w.Written() {
		w.status = code
	}
}
func (w *deliveryWriter) WriteHeaderNow() {
	if w.size < 0 {
		w.size = 0
	}
}
func (w *deliveryWriter) Flush()                            { w.WriteHeaderNow() }
func (w *deliveryWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *deliveryWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if max(w.size, 0)+len(p) > maxDeliveryBytes {
		w.err = errors.New("delivery cache limit exceeded")
		w.cancel()
		return 0, w.err
	}
	w.WriteHeaderNow()
	n, err := w.file.Write(p)
	w.size += n
	if err != nil {
		w.err = err
		w.cancel()
	}
	return n, err
}
func (w *deliveryWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("buffered delivery does not support hijacking")
}
func (w *deliveryWriter) Pusher() http.Pusher { return nil }

// replaySSE splits only typed text deltas. All other fields, including opaque
// reasoning content, signatures, tool calls, usage and end events, are retained.
// A nil emit performs a validation-only pass, with no waits or allocation per rune.
func replaySSE(ctx context.Context, reader io.Reader, emit func([]byte) error, rate *DeliveryAction, wait func(context.Context, time.Duration) error) error {
	if err := normalizeActions(&Actions{Delivery: rate}); err != nil {
		return err
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxDeliveryEventBytes+1)
	var lines []string
	eventBytes := 0
	sequence := int64(0)
	terminal := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line != "" {
			eventBytes += len(line) + 1
			if eventBytes > maxDeliveryEventBytes {
				return fmt.Errorf("SSE event exceeds delivery limit")
			}
			lines = append(lines, line)
			continue
		}
		if len(lines) == 0 {
			continue
		}
		var data string
		for _, l := range lines {
			if value, ok := strings.CutPrefix(l, "data:"); ok {
				if data != "" {
					data += "\n"
				}
				data += strings.TrimPrefix(value, " ")
			}
		}
		packet := strings.Join(lines, "\n") + "\n\n"
		var metadata []string
		for _, l := range lines {
			if !strings.HasPrefix(l, "data:") {
				metadata = append(metadata, l)
			}
		}
		lines = nil
		eventBytes = 0
		if data == "[DONE]" {
			terminal = true
		}
		if data == "" || data == "[DONE]" {
			if emit != nil {
				if err := emit([]byte(packet)); err != nil {
					return err
				}
			}
			continue
		}
		if !gjson.Valid(data) {
			return fmt.Errorf("invalid SSE JSON")
		}
		root := gjson.Parse(data)
		kind := root.Get("type").String()
		if kind == "response.completed" || kind == "message_stop" {
			terminal = true
		}
		if kind == "error" || kind == "response.failed" || kind == "response.incomplete" || root.Get("error").Exists() {
			return fmt.Errorf("upstream stream failed")
		}
		paths := []string{"choices.0.delta.content", "choices.0.delta.reasoning_content", "choices.0.delta.reasoning", "choices.0.delta.refusal"}
		if root.Get("choices.#").Int() > 1 {
			return fmt.Errorf("multi-choice delivery is not supported")
		}
		if kind == "response.output_text.delta" || kind == "response.reasoning_summary_text.delta" || kind == "response.reasoning_text.delta" || kind == "response.refusal.delta" {
			paths = []string{"delta"}
		}
		if kind == "content_block_delta" {
			paths = []string{"delta.text", "delta.thinking"}
		}
		var path, text string
		for _, p := range paths {
			v := root.Get(p)
			if v.Exists() && v.Type != gjson.Null {
				if v.Type != gjson.String {
					return fmt.Errorf("non-text delta is not supported")
				}
				if v.String() != "" {
					if path != "" {
						return fmt.Errorf("multiple text delta fields are not supported")
					}
					path, text = p, v.String()
				}
			}
		}
		if emit == nil {
			continue
		}
		runes := []rune(text)
		count := max(len(runes), 1)
		template := data
		if path != "" {
			template, _ = sjson.Set(data, path, "")
		}
		hasChoices := root.Get("choices").Exists()
		hasSequence := root.Get("sequence_number").Exists()
		continuation := template
		if hasChoices && len(runes) > 1 {
			root.Get("choices.0.delta").ForEach(func(k, v gjson.Result) bool {
				if "choices.0.delta."+k.String() != path {
					continuation, _ = sjson.Delete(continuation, "choices.0.delta."+k.String())
				}
				return true
			})
		}
		for i := range count {
			part := template
			if i > 0 {
				part = continuation
			}
			if len(runes) > 0 {
				if wait != nil && rate != nil {
					cps := randomRange(rate.MinCPS, rate.MaxCPS)
					var duration time.Duration
					if cps > 0 {
						duration = time.Second / time.Duration(cps)
					}
					if duration > 0 {
						if err := wait(ctx, duration); err != nil {
							return err
						}
					}
				}
				var err error
				part, err = sjson.Set(part, path, string(runes[i]))
				if err != nil {
					return err
				}
				if i < len(runes)-1 && hasChoices {
					part, _ = sjson.Set(part, "choices.0.finish_reason", nil)
					part, _ = sjson.Delete(part, "usage")
				}
			}
			if hasSequence {
				part, _ = sjson.Set(part, "sequence_number", sequence)
				sequence++
			}
			prefix := ""
			if len(metadata) > 0 {
				prefix = strings.Join(metadata, "\n") + "\n"
			}
			if err := emit([]byte(prefix + "data: " + part + "\n\n")); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(lines) > 0 || !terminal {
		return fmt.Errorf("incomplete upstream SSE stream")
	}
	return nil
}
