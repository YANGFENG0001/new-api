package usercontrol

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// Counters use Redis when available so limits hold across instances. Without
// Redis they degrade to a process-local window (documented behaviour).
type memCounter struct {
	value   int64
	expires time.Time
}

var (
	memMu       sync.Mutex
	memCounters = make(map[string]memCounter)
	memHashes   = make(map[string]map[string]int64)
	memHashTTL  = make(map[string]time.Time)
)

func redisReady() bool {
	return common.RedisEnabled && common.RDB != nil
}

func memGet(key string) (int64, bool) {
	memMu.Lock()
	defer memMu.Unlock()
	item, ok := memCounters[key]
	if !ok {
		return 0, false
	}
	if time.Now().After(item.expires) {
		delete(memCounters, key)
		return 0, false
	}
	return item.value, true
}

func incrCounter(key string, ttl time.Duration) (int64, error) {
	if redisReady() {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		value, err := common.RDB.Incr(ctx, key).Result()
		if err != nil {
			return 0, err
		}
		if value == 1 {
			common.RDB.Expire(ctx, key, ttl)
		}
		return value, nil
	}
	memMu.Lock()
	defer memMu.Unlock()
	now := time.Now()
	if len(memCounters) > 100000 {
		for k, v := range memCounters {
			if now.After(v.expires) {
				delete(memCounters, k)
			}
		}
	}
	item := memCounters[key]
	if now.After(item.expires) {
		item = memCounter{}
	}
	item.value++
	item.expires = now.Add(ttl)
	memCounters[key] = item
	return item.value, nil
}

func decrCounter(key string) {
	if redisReady() {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		common.RDB.Decr(ctx, key)
		return
	}
	memMu.Lock()
	defer memMu.Unlock()
	item, ok := memCounters[key]
	if !ok {
		return
	}
	item.value--
	if item.value <= 0 {
		delete(memCounters, key)
		return
	}
	memCounters[key] = item
}

// AllowFixedWindow increments a bucket and reports whether it is within limit.
// A limit <= 0 means unlimited.
func AllowFixedWindow(key string, limit int, ttl time.Duration) (bool, error) {
	if limit <= 0 {
		return true, nil
	}
	value, err := incrCounter(key, ttl)
	if err != nil {
		return true, err // fail-open
	}
	if value > int64(limit) {
		decrCounter(key)
		return false, nil
	}
	return true, nil
}

// AcquireConcurrent reserves one concurrency slot. The returned release
// function must be called exactly once.
func AcquireConcurrent(key string, limit int) (func(), bool, error) {
	if limit <= 0 {
		return func() {}, true, nil
	}
	value, err := incrCounter(key, 10*time.Minute)
	if err != nil {
		return func() {}, true, err // fail-open
	}
	if value > int64(limit) {
		decrCounter(key)
		return func() {}, false, nil
	}
	var once sync.Once
	return func() { once.Do(func() { decrCounter(key) }) }, true, nil
}

func hashIncr(key, field string, delta int64, ttl time.Duration) error {
	if delta == 0 {
		return nil
	}
	if redisReady() {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		if err := common.RDB.HIncrBy(ctx, key, field, delta).Err(); err != nil {
			return err
		}
		common.RDB.Expire(ctx, key, ttl)
		return nil
	}
	memMu.Lock()
	defer memMu.Unlock()
	now := time.Now()
	if len(memHashes) > 20000 {
		for k, exp := range memHashTTL {
			if now.After(exp) {
				delete(memHashes, k)
				delete(memHashTTL, k)
			}
		}
	}
	hash := memHashes[key]
	if hash == nil || now.After(memHashTTL[key]) {
		hash = make(map[string]int64)
		memHashes[key] = hash
	}
	hash[field] += delta
	memHashTTL[key] = now.Add(ttl)
	return nil
}

func hashGetAll(key string) map[string]int64 {
	if redisReady() {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		values, err := common.RDB.HGetAll(ctx, key).Result()
		if err != nil {
			return nil
		}
		out := make(map[string]int64, len(values))
		for field, raw := range values {
			var parsed int64
			for _, ch := range raw {
				if ch < '0' || ch > '9' {
					parsed = 0
					break
				}
				parsed = parsed*10 + int64(ch-'0')
			}
			out[field] = parsed
		}
		return out
	}
	memMu.Lock()
	defer memMu.Unlock()
	if time.Now().After(memHashTTL[key]) {
		return nil
	}
	hash := memHashes[key]
	out := make(map[string]int64, len(hash))
	for field, value := range hash {
		out[field] = value
	}
	return out
}

// Window keys -----------------------------------------------------------------

func RPMKey(userID int, now time.Time) (string, time.Duration) {
	minute := now.Unix() / 60
	return fmtKey("uc:rpm", userID, minute), 2 * time.Minute
}

func RPDKey(userID int, now time.Time, loc *time.Location) (string, time.Duration) {
	local := now.In(loc)
	return fmtKey("uc:rpd", userID, local.Format("20060102")), 48 * time.Hour
}

func DailyRequestKey(userID int, now time.Time, loc *time.Location) (string, time.Duration) {
	local := now.In(loc)
	return fmtKey("uc:req:d", userID, local.Format("20060102")), 48 * time.Hour
}

func MonthlyRequestKey(userID int, now time.Time, loc *time.Location) (string, time.Duration) {
	local := now.In(loc)
	return fmtKey("uc:req:m", userID, local.Format("200601")), 62 * 24 * time.Hour
}

func ConcurrencyKey(userID int) string {
	return fmtKey("uc:conc", userID, "")
}

func DailyUsageKey(userID int, now time.Time, loc *time.Location) string {
	return fmtKey("uc:usage:d", userID, now.In(loc).Format("20060102"))
}

func MonthlyUsageKey(userID int, now time.Time, loc *time.Location) string {
	return fmtKey("uc:usage:m", userID, now.In(loc).Format("200601"))
}

func fmtKey(prefix string, userID int, suffix any) string {
	if suffix == "" || suffix == nil {
		return prefix + ":" + strconv.Itoa(userID)
	}
	return prefix + ":" + strconv.Itoa(userID) + ":" + fmt.Sprint(suffix)
}

// RecordUsage adds quota/token usage to the daily and monthly buckets.
func RecordUsage(userID int, now time.Time, loc *time.Location, quota, inputTokens, outputTokens int64) error {
	if userID <= 0 || (quota == 0 && inputTokens == 0 && outputTokens == 0) {
		return nil
	}
	daily := DailyUsageKey(userID, now, loc)
	monthly := MonthlyUsageKey(userID, now, loc)
	fields := []struct {
		name  string
		value int64
	}{
		{"quota", quota},
		{"input", inputTokens},
		{"output", outputTokens},
	}
	for _, field := range fields {
		if field.value == 0 {
			continue
		}
		if err := hashIncr(daily, field.name, field.value, 48*time.Hour); err != nil {
			return err
		}
		if err := hashIncr(monthly, field.name, field.value, 62*24*time.Hour); err != nil {
			return err
		}
	}
	return nil
}

type UsageSnapshot struct {
	DailyQuota   int64 `json:"daily_quota"`
	MonthlyQuota int64 `json:"monthly_quota"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

func GetUsageSnapshot(userID int, now time.Time, loc *time.Location) UsageSnapshot {
	daily := hashGetAll(DailyUsageKey(userID, now, loc))
	monthly := hashGetAll(MonthlyUsageKey(userID, now, loc))
	return UsageSnapshot{
		DailyQuota:   daily["quota"],
		MonthlyQuota: monthly["quota"],
		InputTokens:  monthly["input"],
		OutputTokens: monthly["output"],
	}
}

func CheckUsage(limits UsageLimits, userID int, now time.Time, loc *time.Location) (bool, string) {
	if limits.DailyQuota <= 0 && limits.MonthlyQuota <= 0 && limits.InputTokens <= 0 && limits.OutputTokens <= 0 {
		return true, ""
	}
	snapshot := GetUsageSnapshot(userID, now, loc)
	switch {
	case limits.DailyQuota > 0 && snapshot.DailyQuota >= int64(limits.DailyQuota):
		return false, "daily_quota"
	case limits.MonthlyQuota > 0 && snapshot.MonthlyQuota >= int64(limits.MonthlyQuota):
		return false, "monthly_quota"
	case limits.InputTokens > 0 && snapshot.InputTokens >= int64(limits.InputTokens):
		return false, "input_tokens"
	case limits.OutputTokens > 0 && snapshot.OutputTokens >= int64(limits.OutputTokens):
		return false, "output_tokens"
	}
	return true, ""
}
