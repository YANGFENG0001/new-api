package relay

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

// Codex clients that run with disable_response_storage=true replay every
// reasoning item they have seen, including the encrypted_content produced by
// whichever channel answered the previous turn.
//
// Non-OpenAI channels cannot produce a payload the OpenAI backend can decrypt,
// but several providers/forks still emit a placeholder, for example
//
//	{"type":"reasoning","id":"cd402fe3-...","encrypted_content":"4a7a9364-...-0"}
//
// Replaying that to api.openai.com fails the whole turn with:
//
//	The encrypted content 4a7a9364-...-0 could not be verified.
//	Reason: Encrypted content could not be decrypted or parsed.
//
// Dropping the field (not blanking it) is accepted by OpenAI, verified against
// the live upstream. Genuine OpenAI ciphertext always starts with "gAAAA".
//
// The same replay also carries the provider's raw reasoning text:
//
//	{"type":"reasoning","content":[{"type":"reasoning_text","text":"..."}]}
//
// content is output-only on reasoning items. OpenAI rejects it as input with
//
//	Invalid 'input[N].content': array too long.
//	Expected an array with maximum length 0, but got an array with length 1
//
// so content is normalised to [] and the replayable summary is kept. This
// happens for every reasoning item regardless of channel type because a
// gateway can route the same request to different upstreams between turns.
//
// The sanitiser is switchable; see
// model_setting.ShouldSanitizeResponsesEncryptedContent.

const openAIEncryptedContentPrefix = "gAAAA"

var placeholderEncryptedContent = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}(-\d+)?$`,
)

// channelNeedsRealOpenAIBlob reports whether the target channel forwards the
// value straight to an OpenAI-compatible upstream, which will try to decrypt it.
func channelNeedsRealOpenAIBlob(channelType int) bool {
	switch channelType {
	case constant.ChannelTypeOpenAI,
		constant.ChannelTypeAzure,
		constant.ChannelTypeCodex,
		constant.ChannelTypeOpenAIMax:
		return true
	}
	return false
}

func shouldDropEncryptedContent(value string, strict bool) bool {
	if strings.HasPrefix(value, openAIEncryptedContentPrefix) {
		return false
	}
	if value == "" || placeholderEncryptedContent.MatchString(value) {
		return true
	}
	return strict
}

// sanitizeResponsesInput normalises reasoning items before they are replayed:
// encrypted_content the target upstream could not decrypt is removed, and
// reasoning content, which the Responses API refuses on input, is emptied.
// When nothing changes the original bytes are returned untouched.
func sanitizeResponsesInput(input json.RawMessage, strict bool) json.RawMessage {
	if len(input) == 0 {
		return input
	}
	// Reasoning items are the only items that can need a rewrite. Checking for
	// the item type first keeps the common message/tool payload on the cheap
	// byte-scan path.
	if !bytes.Contains(input, []byte(`"reasoning"`)) {
		return input
	}

	var items []map[string]json.RawMessage
	if err := json.Unmarshal(input, &items); err != nil {
		return input
	}

	changed := false
	for _, item := range items {
		var itemType string
		if raw, ok := item["type"]; !ok || json.Unmarshal(raw, &itemType) != nil || itemType != "reasoning" {
			continue
		}
		itemChanged := false
		if raw, ok := item["encrypted_content"]; ok {
			var value string
			if json.Unmarshal(raw, &value) == nil && shouldDropEncryptedContent(value, strict) {
				delete(item, "encrypted_content")
				itemChanged = true
			}
		}
		// content is output-only on reasoning items; summary is the replayable
		// representation. Foreign providers put the raw trace in content, and
		// OpenAI rejects any non-empty array there even when the ciphertext is
		// already gone.
		if raw, ok := item["content"]; ok && needsReasoningContentReset(raw) {
			item["content"] = json.RawMessage(`[]`)
			itemChanged = true
		}
		if itemChanged {
			changed = true
		}
	}
	if !changed {
		return input
	}

	out, err := json.Marshal(items)
	if err != nil {
		return input
	}
	return out
}

// sanitizeResponsesBody is the pass-through equivalent: it rewrites the whole
// {"model":...,"input":[...]} payload.
func sanitizeResponsesBody(body []byte, strict bool) ([]byte, bool) {
	if !bytes.Contains(body, []byte(`"reasoning"`)) {
		return body, false
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return body, false
	}
	input, ok := root["input"]
	if !ok {
		return body, false
	}
	fixed := sanitizeResponsesInput(input, strict)
	if bytes.Equal(fixed, input) {
		return body, false
	}
	root["input"] = fixed
	out, err := json.Marshal(root)
	if err != nil {
		return body, false
	}
	return out, true
}

// sanitizePassthroughBody rewrites the cached inbound body so channels that
// enable pass_through_body / pass_through_request also send a clean payload.
func sanitizePassthroughBody(c *gin.Context, storage common.BodyStorage, strict bool) (common.BodyStorage, error) {
	raw, err := storage.Bytes()
	if err != nil {
		return storage, err
	}
	fixed, changed := sanitizeResponsesBody(raw, strict)
	if !changed {
		return storage, nil
	}
	next, err := common.CreateBodyStorage(fixed)
	if err != nil {
		return storage, err
	}
	c.Set(common.KeyBodyStorage, next)
	_ = storage.Close()
	return next, nil
}

// needsReasoningContentReset reports whether content must be emptied before the
// item is replayed. Absent and already-empty arrays are left alone; anything
// else (text parts, null, a non-array value) is invalid on a reasoning input
// item and is normalised to [].
func needsReasoningContentReset(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return true
	}
	if len(parts) > 0 {
		return true
	}
	return bytes.Equal(trimmed, []byte("null"))
}
