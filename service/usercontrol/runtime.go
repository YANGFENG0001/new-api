package usercontrol

import (
	"cmp"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const OriginalModelKey = "user_control_original_model"
const runtimeKey = "user_control_runtime"

type requestState struct {
	Delay        *DelayAction
	Delivery     *DeliveryAction
	Rules        []string
	Actions      []string
	Admitted     map[string]bool
	Releases     []func()
	Response     *StaticResponse
	FinalModel   string
	FinalChannel int
	Synthetic    bool
}

func state(c *gin.Context) *requestState {
	if value, ok := c.Get(runtimeKey); ok {
		return value.(*requestState)
	}
	s := &requestState{Admitted: make(map[string]bool)}
	c.Set(runtimeKey, s)
	return s
}

func textualPath(path string) bool {
	return strings.HasSuffix(path, "/chat/completions") || strings.HasSuffix(path, "/responses") || strings.HasSuffix(path, "/messages")
}

// ApplyActions runs once before routing for model-only rules, then once after
// initial channel selection for channel rules. Routing is one-pass, never recursive.
// Changed bodies go back through the ordinary request/billing validators.
func ApplyActions(c *gin.Context, clientModel string, channelID int) (bool, error) {
	e, ok := FromContext(c)
	if !ok || len(e.Limits.ScopedRules) == 0 {
		return false, nil
	}
	if c.GetString(OriginalModelKey) == "" {
		c.Set(OriginalModelKey, clientModel)
	}
	s := state(c)
	var body map[string]common.RawMessage
	changed := false
	rules := append([]ScopedRule(nil), e.Limits.ScopedRules...)
	slices.SortStableFunc(rules, func(a, b ScopedRule) int {
		if a.Override != b.Override {
			if a.Override {
				return 1
			}
			return -1
		}
		if order := cmp.Compare(a.PolicyPriority, b.PolicyPriority); order != 0 {
			return order
		}
		return cmp.Compare(a.Priority, b.Priority)
	})
	for _, r := range rules {
		if !r.Matches(clientModel, channelID) || (channelID == 0) != (len(r.ChannelIDs) == 0) || r.Actions == nil {
			continue
		}
		a := r.Actions
		if a.RequestBody == "" && a.RouteModel == "" && a.ReasoningEffort == "" && a.PromptPrefix == "" && a.PromptSuffix == "" && a.Response == nil && a.Delay == nil && a.Delivery == nil {
			continue
		}
		s.Rules = append(s.Rules, r.Namespace+":"+r.ID)
		if c.GetString("expected_task_plugin_key") != "" {
			return false, fmt.Errorf("user control actions do not support task plugin endpoints")
		}
		if !textualPath(c.Request.URL.Path) || c.Request.Method != http.MethodPost {
			return false, fmt.Errorf("user control actions require a text JSON endpoint")
		}
		if body == nil {
			storage, err := common.GetBodyStorage(c)
			if err != nil {
				return false, err
			}
			if storage.Size() > 2<<20 {
				return false, fmt.Errorf("user control rewrite body exceeds 2 MiB")
			}
			raw, err := storage.Bytes()
			if err != nil {
				return false, err
			}
			if err := common.Unmarshal(raw, &body); err != nil || body == nil {
				return false, fmt.Errorf("user control actions require a JSON object")
			}
			for key := range body {
				for _, protected := range []string{"model", "group", "stream"} {
					if key != protected && strings.EqualFold(key, protected) {
						return false, fmt.Errorf("ambiguous protected request field")
					}
				}
			}
		}
		if a.RequestBody != "" {
			var patch map[string]common.RawMessage
			if err := common.Unmarshal([]byte(a.RequestBody), &patch); err != nil {
				return false, err
			}
			if a.RequestMode == "replace" {
				for _, key := range []string{"model", "stream", "group"} {
					if v, ok := body[key]; ok {
						patch[key] = v
					}
				}
				body = patch
			} else {
				var err error
				body, err = mergeJSONBody(body, patch, 0)
				if err != nil {
					return false, err
				}
			}
			s.Actions = append(s.Actions, "request_body")
			changed = true
		}
		if a.RouteModel != "" {
			body["model"], _ = common.Marshal(a.RouteModel)
			s.Actions = append(s.Actions, "route_model")
			changed = true
		}
		if a.ReasoningEffort != "" {
			if strings.HasSuffix(c.Request.URL.Path, "/messages") {
				return false, fmt.Errorf("reasoning effort override is supported on Chat Completions and Responses only")
			}
			if strings.HasSuffix(c.Request.URL.Path, "/responses") {
				var reasoning map[string]common.RawMessage
				if v := body["reasoning"]; len(v) > 0 && string(v) != "null" {
					if err := common.Unmarshal(v, &reasoning); err != nil {
						return false, fmt.Errorf("invalid reasoning object")
					}
				}
				if reasoning == nil {
					reasoning = make(map[string]common.RawMessage)
				}
				reasoning["effort"], _ = common.Marshal(a.ReasoningEffort)
				body["reasoning"], _ = common.Marshal(reasoning)
			} else {
				body["reasoning_effort"], _ = common.Marshal(a.ReasoningEffort)
			}
			s.Actions = append(s.Actions, "reasoning_effort")
			changed = true
		}
		if a.PromptPrefix != "" || a.PromptSuffix != "" {
			if err := appendPrompt(body, c.Request.URL.Path, a.PromptPrefix, a.PromptSuffix); err != nil {
				return false, err
			}
			s.Actions = append(s.Actions, "prompt")
			changed = true
		}
		if a.Response != nil {
			s.Response = a.Response
			s.Actions = append(s.Actions, "response")
		}
		if a.Delay != nil {
			s.Delay = a.Delay
			s.Actions = append(s.Actions, "delay")
		}
		if a.Delivery != nil {
			s.Delivery = a.Delivery
			s.Actions = append(s.Actions, "delivery")
		}
	}
	if !changed {
		return false, nil
	}
	raw, err := common.Marshal(body)
	if err != nil {
		return false, err
	}
	if len(raw) > 2<<20 {
		return false, fmt.Errorf("rewritten request exceeds 2 MiB")
	}
	storage, err := common.CreateBodyStorage(raw)
	if err != nil {
		return false, err
	}
	if old, ok := c.Get(common.KeyBodyStorage); ok {
		if old, ok := old.(common.BodyStorage); ok {
			_ = old.Close()
		}
	}
	c.Set(common.KeyBodyStorage, storage)
	c.Set(common.KeyRequestBody, nil)
	c.Request.Body = io.NopCloser(storage)
	c.Request.ContentLength = int64(len(raw))
	c.Request.Header.Del("Content-Length")
	return true, nil
}

// mergeJSONBody implements JSON Merge Patch, preserving exact encoded numbers.
func mergeJSONBody(body, patch map[string]common.RawMessage, depth int) (map[string]common.RawMessage, error) {
	if depth > 32 {
		return nil, fmt.Errorf("request patch exceeds maximum depth")
	}
	if body == nil {
		body = make(map[string]common.RawMessage)
	}
	for key, value := range patch {
		if string(value) == "null" {
			delete(body, key)
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(string(value)), "{") {
			var old, nested map[string]common.RawMessage
			_ = common.Unmarshal(body[key], &old)
			if err := common.Unmarshal(value, &nested); err != nil {
				return nil, err
			}
			merged, err := mergeJSONBody(old, nested, depth+1)
			if err != nil {
				return nil, err
			}
			body[key], _ = common.Marshal(merged)
		} else {
			body[key] = value
		}
	}
	return body, nil
}

func appendPrompt(body map[string]common.RawMessage, path, prefix, suffix string) error {
	if strings.HasSuffix(path, "/chat/completions") {
		var messages []common.RawMessage
		if err := common.Unmarshal(body["messages"], &messages); err != nil {
			return fmt.Errorf("invalid messages for prompt concatenation")
		}
		if prefix != "" {
			message, _ := common.Marshal(gin.H{"role": "system", "content": prefix})
			messages = append([]common.RawMessage{message}, messages...)
		}
		if suffix != "" {
			message, _ := common.Marshal(gin.H{"role": "system", "content": suffix})
			messages = append(messages, message)
		}
		body["messages"], _ = common.Marshal(messages)
		return nil
	}
	key := "instructions"
	if strings.HasSuffix(path, "/messages") {
		key = "system"
	}
	var text string
	if raw := body[key]; len(raw) > 0 && string(raw) != "null" {
		if err := common.Unmarshal(raw, &text); err != nil {
			// Claude system blocks may contain cache-control markers: retain them.
			if key != "system" {
				return fmt.Errorf("invalid instructions for prompt concatenation")
			}
			var blocks []common.RawMessage
			if err := common.Unmarshal(raw, &blocks); err != nil {
				return fmt.Errorf("invalid system prompt")
			}
			if prefix != "" {
				b, _ := common.Marshal(gin.H{"type": "text", "text": prefix})
				blocks = append([]common.RawMessage{b}, blocks...)
			}
			if suffix != "" {
				b, _ := common.Marshal(gin.H{"type": "text", "text": suffix})
				blocks = append(blocks, b)
			}
			body[key], _ = common.Marshal(blocks)
			return nil
		}
	}
	parts := make([]string, 0, 3)
	for _, v := range []string{prefix, text, suffix} {
		if v != "" {
			parts = append(parts, v)
		}
	}
	body[key], _ = common.Marshal(strings.Join(parts, "\n"))
	return nil
}

// AdmitChannel must also be called on retries; changing channels cannot bypass
// a scoped quota. An already admitted rule is counted once per client request.
func AdmitChannel(c *gin.Context, actualModel string, channelID int) error {
	e, ok := FromContext(c)
	if !ok {
		return nil
	}
	s := state(c)
	s.FinalModel, s.FinalChannel = actualModel, channelID
	clientModel := c.GetString(OriginalModelKey)
	if clientModel == "" {
		clientModel = actualModel
		c.Set(OriginalModelKey, clientModel)
	}
	for _, r := range e.Limits.ScopedRules {
		if !r.Matches(clientModel, channelID) {
			continue
		}
		key := r.Bucket(c.GetInt("id"))
		if s.Admitted[key] {
			continue
		}
		s.Rules = append(s.Rules, r.Namespace+":"+r.ID)
		if r.Limits.Usage != nil && (!textualPath(c.Request.URL.Path) || c.GetString("expected_task_plugin_key") != "") {
			return fmt.Errorf("scoped usage limits require a text endpoint")
		}
		release, reason, err := AdmitScoped(r, c.GetInt("id"), time.Now(), e.Location())
		if err != nil {
			return fmt.Errorf("user control counter unavailable")
		}
		if reason != "" {
			return fmt.Errorf("scoped user control limit reached: %s", reason)
		}
		s.Admitted[key] = true
		s.Releases = append(s.Releases, release)
	}
	return nil
}

// FinishRequest releases all slots and writes only non-content audit metadata.
func FinishRequest(c *gin.Context) {
	v, ok := c.Get(runtimeKey)
	if !ok {
		return
	}
	s := v.(*requestState)
	for _, release := range s.Releases {
		release()
	}
	if len(s.Rules) == 0 {
		return
	}
	e, _ := FromContext(c)
	params := map[string]string{
		"original_model": c.GetString(OriginalModelKey), "actual_model": s.FinalModel,
		"channel_id": fmt.Sprint(s.FinalChannel), "simulated": fmt.Sprint(s.Synthetic),
	}
	if s.Delay != nil {
		params["delay_ms"] = fmt.Sprint(c.GetInt("user_control_delay_ms"))
	}
	if s.Delivery != nil {
		params["delivery_complete"] = fmt.Sprint(c.GetBool("user_control_delivery_complete"))
		params["delivery_duration_ms"] = fmt.Sprint(c.GetInt64("user_control_delivery_duration_ms"))
		params["upstream_duration_ms"] = fmt.Sprint(c.GetInt64("user_control_upstream_duration_ms"))
		params["upstream_output_tps"] = fmt.Sprint(c.GetFloat64("user_control_upstream_output_tps"))
		if c.GetBool("user_control_delivery_complete") {
			params["client_output_tps"] = fmt.Sprint(c.GetFloat64("user_control_client_output_tps"))
		}
	}
	uniqueRules := unionStrings(nil, s.Rules)
	if len(uniqueRules) > 128 {
		uniqueRules = uniqueRules[:128]
		params["truncated"] = "true"
	}
	rules, _ := common.Marshal(uniqueRules)
	params["rules"] = string(rules)
	actions, _ := common.Marshal(unionStrings(nil, s.Actions))
	params["actions"] = string(actions)
	if e != nil {
		hits, _ := common.Marshal(e.ScheduleHits)
		params["schedule_hits"] = string(hits)
	}
	status := c.Writer.Status()
	success := status < 400 && !c.GetBool("user_control_runtime_failed")
	model.RecordAuditLog(c, model.AuditLog{
		UserId: c.GetInt("id"), ActorRole: c.GetInt("role"), AuthMethod: "api_token",
		Category: model.AuditCategoryOperation, Action: "user_control.runtime.request",
		Content: "User control rule applied", Status: status, Success: success,
		Other: model.AuditOther{AuditInfo: &model.AuditRequestInfo{Status: status, Success: success, Params: params}},
	})
}
