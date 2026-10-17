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

// sanitizeResponsesInput removes encrypted_content from reasoning items that
// the target upstream could not decrypt. When nothing is dropped the original
// bytes are returned untouched.
func sanitizeResponsesInput(input json.RawMessage, strict bool) json.RawMessage {
	if len(input) == 0 || !bytes.Contains(input, []byte(`"encrypted_content"`)) {
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
		raw, ok := item["encrypted_content"]
		if !ok {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil || !shouldDropEncryptedContent(value, strict) {
			continue
		}
		delete(item, "encrypted_content")
		changed = true
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
	if !bytes.Contains(body, []byte(`"encrypted_content"`)) {
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
