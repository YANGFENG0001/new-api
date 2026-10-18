package usercontrol

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
)

// ContextKeyEffective is where the request middleware stores the resolved
// policy so downstream stages (distributor, usage settlement) reuse it without
// another database lookup.
const ContextKeyEffective = "user_control_effective"

// ClientErrorMessage exposes a stable translated category, never internal
// parser errors or administrator-authored request/prompt content.
func ClientErrorMessage(c *gin.Context, err error) string {
	key := i18n.MsgUserControlActionInvalid
	switch message := err.Error(); {
	case message == "user control counter unavailable":
		key = i18n.MsgUserControlCounterUnavailable
	case strings.HasPrefix(message, "scoped user control limit reached:"):
		key = i18n.MsgUserControlScopedLimitReached
	case message == "synthetic JSON responses require stream=false":
		key = i18n.MsgUserControlSimulationNeedsNonstream
	}
	return i18n.T(c, key)
}

func SetContext(c *gin.Context, effective *Effective) {
	if c == nil || effective == nil {
		return
	}
	c.Set(ContextKeyEffective, effective)
}

func FromContext(c *gin.Context) (*Effective, bool) {
	if c == nil {
		return nil, false
	}
	value, ok := c.Get(ContextKeyEffective)
	if !ok {
		return nil, false
	}
	effective, ok := value.(*Effective)
	if !ok || effective == nil {
		return nil, false
	}
	return effective, true
}

// RecordContextUsage records settled usage only for users that actually have a
// policy. Users without a policy pay no storage cost.
func RecordContextUsage(c *gin.Context, userID int, quota, inputTokens, outputTokens int64) {
	effective, ok := FromContext(c)
	if !ok || effective == nil || !effective.HasPolicy || userID <= 0 {
		return
	}
	_ = RecordUsage(userID, time.Now(), effective.Location(), quota, inputTokens, outputTokens)
	if value, ok := c.Get(runtimeKey); ok {
		s := value.(*requestState)
		for _, rule := range effective.Limits.ScopedRules {
			if rule.Matches(c.GetString(OriginalModelKey), s.FinalChannel) && s.Admitted[rule.Bucket(userID)] {
				recordScopedUsage(rule, userID, time.Now(), effective.Location(), quota, inputTokens, outputTokens)
			}
		}
	}
}
