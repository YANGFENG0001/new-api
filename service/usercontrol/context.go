package usercontrol

import (
	"time"

	"github.com/gin-gonic/gin"
)

// ContextKeyEffective is where the request middleware stores the resolved
// policy so downstream stages (distributor, usage settlement) reuse it without
// another database lookup.
const ContextKeyEffective = "user_control_effective"

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
}
