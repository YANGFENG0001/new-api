package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/service/usercontrol"

	"github.com/gin-gonic/gin"
)

// UserControl enforces per-user limits before channel selection. It is
// fail-open when the policy store is unavailable so an outage cannot take the
// relay down. Users without any policy and root users are untouched.
func UserControl() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt("id")
		role := c.GetInt("role")
		if userID <= 0 || role == common.RoleRootUser {
			c.Next()
			return
		}
		effective, err := usercontrol.Resolve(userID, role)
		if err != nil {
			common.SysLog(common.LogText("user control resolve failed for user %d: %s", userID, err.Error()))
			c.Next()
			return
		}
		if !effective.HasPolicy {
			c.Next()
			return
		}
		usercontrol.SetContext(c, effective)
		defer usercontrol.FinishRequest(c)
		// A long-lived socket could outlive a time window and bypass per-message
		// policies. Keep scoped control HTTP-only until the WS runner supports it.
		if effective.HasScopedRules && strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
			abortUserControl(c, http.StatusForbidden, i18n.T(c, i18n.MsgUserControlWebsocketUnavailable))
			return
		}

		if effective.Blocked() {
			abortUserControl(c, http.StatusForbidden, "This account has been blocked by an administrator.")
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/pg/") {
			if !effective.PlaygroundAllowed() {
				abortUserControl(c, http.StatusForbidden, "Playground access is disabled for this account.")
				return
			}
		} else if !effective.RelayAllowed() {
			abortUserControl(c, http.StatusForbidden, "Relay access is disabled for this account.")
			return
		}

		now := time.Now()
		loc := effective.Location()
		rate := effective.Rate()

		release, ok, err := usercontrol.AcquireConcurrent(usercontrol.ConcurrencyKey(userID), rate.Concurrent)
		if err != nil {
			common.SysLog(common.LogText("user control concurrency check failed for user %d: %s", userID, err.Error()))
		} else if !ok {
			c.Header("Retry-After", "1")
			abortUserControl(c, http.StatusTooManyRequests, "Too many concurrent requests. Please retry later.")
			return
		} else {
			defer release()
		}

		rpmKey, _ := usercontrol.RPMKey(userID, now)
		if allowed, err := checkedWindow(rpmKey, rate.RPM, 60); err != nil {
			common.SysLog(common.LogText("user control rpm check failed for user %d: %s", userID, err.Error()))
		} else if !allowed {
			c.Header("Retry-After", "60")
			abortUserControl(c, http.StatusTooManyRequests, "Per-minute request limit reached. Please retry later.")
			return
		}

		rpdKey, _ := usercontrol.RPDKey(userID, now, loc)
		if allowed, err := checkedWindow(rpdKey, rate.RPD, 600); err != nil {
			common.SysLog(common.LogText("user control rpd check failed for user %d: %s", userID, err.Error()))
		} else if !allowed {
			c.Header("Retry-After", "600")
			abortUserControl(c, http.StatusTooManyRequests, "Daily request limit reached.")
			return
		}

		requests := effective.Requests()
		dailyKey, _ := usercontrol.DailyRequestKey(userID, now, loc)
		if allowed, err := checkedWindow(dailyKey, requests.Daily, 600); err != nil {
			common.SysLog(common.LogText("user control daily request check failed for user %d: %s", userID, err.Error()))
		} else if !allowed {
			c.Header("Retry-After", "600")
			abortUserControl(c, http.StatusTooManyRequests, "Daily request quota reached.")
			return
		}
		monthlyKey, _ := usercontrol.MonthlyRequestKey(userID, now, loc)
		if allowed, err := checkedWindow(monthlyKey, requests.Monthly, 600); err != nil {
			common.SysLog(common.LogText("user control monthly request check failed for user %d: %s", userID, err.Error()))
		} else if !allowed {
			c.Header("Retry-After", "3600")
			abortUserControl(c, http.StatusTooManyRequests, "Monthly request quota reached.")
			return
		}

		if ok, reason := usercontrol.CheckUsage(effective.Usage(), userID, now, loc); !ok {
			c.Header("Retry-After", "600")
			abortUserControl(c, http.StatusTooManyRequests, fmt.Sprintf("Usage limit reached (%s).", strings.ReplaceAll(reason, "_", " ")))
			return
		}

		c.Next()
	}
}

func checkedWindow(key string, limit int, retryAfter int) (bool, error) {
	if limit <= 0 {
		return true, nil
	}
	if allowed, err := usercontrol.AllowFixedWindow(key, limit, windowTTL(key)); err != nil {
		return true, err
	} else if !allowed {
		return false, nil
	}
	return true, nil
}

func windowTTL(key string) time.Duration {
	switch {
	case strings.HasPrefix(key, "uc:rpm:"):
		return 2 * time.Minute
	case strings.HasPrefix(key, "uc:rpd:"):
		return 48 * time.Hour
	case strings.HasPrefix(key, "uc:req:d:"):
		return 48 * time.Hour
	case strings.HasPrefix(key, "uc:req:m:"):
		return 62 * 24 * time.Hour
	default:
		return time.Hour
	}
}

func abortUserControl(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "user_control_limit",
			"code":    "user_control_limit",
		},
	})
}
