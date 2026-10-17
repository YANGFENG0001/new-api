package relay

import (
	"encoding/json"
	"strings"
	"testing"
)

func encOf(t *testing.T, in string) map[string]any {
	t.Helper()
	var items []map[string]any
	if err := json.Unmarshal([]byte(in), &items); err != nil {
		t.Fatalf("bad input: %v", err)
	}
	return items[0]
}

func decodeItems(t *testing.T, raw json.RawMessage) []map[string]json.RawMessage {
	t.Helper()
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	return items
}

func TestSanitizeResponsesInput(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		strict  bool
		wantHas bool
	}{
		{"uuid-0 placeholder removed", `[{"type":"reasoning","id":"a","encrypted_content":"4a7a9364-82ce-452a-b324-b74275dcee30-0"}]`, false, false},
		{"bare uuid removed", `[{"type":"reasoning","id":"a","encrypted_content":"4a7a9364-82ce-452a-b324-b74275dcee30"}]`, false, false},
		{"empty removed", `[{"type":"reasoning","id":"a","encrypted_content":""}]`, false, false},
		{"openai blob kept", `[{"type":"reasoning","id":"rs_1","encrypted_content":"gAAAAABq0h8EMQO_1EqyGwe1"}]`, true, true},
		{"foreign blob kept in safe mode", `[{"type":"reasoning","id":"a","encrypted_content":"Q-PaDgEIqhRVJ8lSrnRIn5zL"}]`, false, true},
		{"foreign blob dropped in strict mode", `[{"type":"reasoning","id":"a","encrypted_content":"Q-PaDgEIqhRVJ8lSrnRIn5zL"}]`, true, false},
		{"message item untouched", `[{"type":"message","role":"user","content":[]}]`, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := sanitizeResponsesInput(json.RawMessage(tc.in), tc.strict)
			item := encOf(t, string(out))
			_, has := item["encrypted_content"]
			if has != tc.wantHas {
				t.Fatalf("encrypted_content present=%v, want %v (out=%s)", has, tc.wantHas, out)
			}
			if _, err := json.Marshal(item); err != nil {
				t.Fatalf("output is not valid JSON: %v", err)
			}
		})
	}

	// plain string input must pass through untouched
	plain := json.RawMessage(`"hello"`)
	if got := sanitizeResponsesInput(plain, true); string(got) != `"hello"` {
		t.Fatalf("plain string mangled: %s", got)
	}

	// body shapes without a reasoning item must be returned byte-for-byte.
	noReasoning := json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"reasoning about this"}]}]`)
	if got := sanitizeResponsesInput(noReasoning, true); string(got) != string(noReasoning) {
		t.Fatalf("non-reasoning payload rewritten: %s", got)
	}
}

func TestSanitizeResponsesBody(t *testing.T) {
	body := []byte(`{"model":"gpt-6.1-sol","input":[{"type":"reasoning","id":"a","summary":[],"encrypted_content":"4a7a9364-82ce-452a-b324-b74275dcee30-0"},{"type":"message","role":"user","content":[]}]}`)
	out, changed := sanitizeResponsesBody(body, false)
	if !changed {
		t.Fatal("expected body to change")
	}
	var root map[string]any
	if err := json.Unmarshal(out, &root); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if root["model"] != "gpt-6.1-sol" {
		t.Fatalf("model lost: %v", root["model"])
	}
	items := root["input"].([]any)
	if _, has := items[0].(map[string]any)["encrypted_content"]; has {
		t.Fatal("placeholder not removed")
	}
	if _, has := items[1].(map[string]any)["content"]; !has {
		t.Fatal("message item damaged")
	}

	if _, changed := sanitizeResponsesBody([]byte(`{"model":"x","input":[]}`), false); changed {
		t.Fatal("no-op body should report unchanged")
	}
}

func TestSanitizeResponsesInputNormalizesReplayedReasoningContent(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		strict      bool
		wantHasEnc  bool
		wantSummary string
	}{
		{
			name:        "placeholder dropped clears reasoning content",
			in:          `[{"type":"reasoning","id":"a","summary":[{"type":"summary_text","text":"plan"}],"content":[{"type":"reasoning_text","text":"raw"}],"encrypted_content":"4a7a9364-82ce-452a-b324-b74275dcee30-0"}]`,
			strict:      false,
			wantSummary: "plan",
		},
		{
			name:   "strict mode clears content without encrypted_content",
			in:     `[{"type":"reasoning","id":"a","summary":[],"content":[{"type":"reasoning_text","text":"raw"}]}]`,
			strict: true,
		},
		{
			name:       "safe mode keeps genuine ciphertext but clears content",
			in:         `[{"type":"reasoning","id":"a","summary":[],"content":[{"type":"reasoning_text","text":"raw"}],"encrypted_content":"gAAAAABq0h8EMQO_1EqyGwe1"}]`,
			strict:     false,
			wantHasEnc: true,
		},
		{
			name:   "safe mode clears content without any ciphertext",
			in:     `[{"type":"reasoning","id":"a","content":[{"type":"reasoning_text","text":"raw"}]}]`,
			strict: false,
		},
		{
			name:   "null content is normalized",
			in:     `[{"type":"reasoning","id":"a","content":null}]`,
			strict: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := sanitizeResponsesInput(json.RawMessage(tc.in), tc.strict)
			items := decodeItems(t, out)
			if len(items) != 1 {
				t.Fatalf("unexpected item count: %d", len(items))
			}
			item := items[0]
			if content := strings.TrimSpace(string(item["content"])); content != "[]" {
				t.Fatalf("reasoning content not cleared: %s", content)
			}
			if _, has := item["encrypted_content"]; has != tc.wantHasEnc {
				t.Fatalf("encrypted_content present=%v, want %v (out=%s)", has, tc.wantHasEnc, out)
			}
			if tc.wantSummary != "" {
				var summaryParts []map[string]string
				if err := json.Unmarshal(item["summary"], &summaryParts); err != nil || len(summaryParts) == 0 || summaryParts[0]["text"] != tc.wantSummary {
					t.Fatalf("summary must be preserved: %s", out)
				}
			}
		})
	}
}

func TestSanitizeResponsesInputKeepsAlreadyEmptyReasoningContent(t *testing.T) {
	in := json.RawMessage(`[{"type":"reasoning","id":"a","summary":[],"content":[],"encrypted_content":"gAAAAABq0h8EMQO_1EqyGwe1"}]`)
	if got := sanitizeResponsesInput(in, false); string(got) != string(in) {
		t.Fatalf("clean reasoning item rewritten: %s", got)
	}
}

func TestSanitizeResponsesInputLeavesMessageContentAlone(t *testing.T) {
	in := json.RawMessage(`[{"type":"reasoning","id":"a","summary":[],"content":[{"type":"reasoning_text","text":"raw"}],"encrypted_content":"4a7a9364-82ce-452a-b324-b74275dcee30-0"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}]`)
	out := sanitizeResponsesInput(in, true)
	items := decodeItems(t, out)
	if content := strings.TrimSpace(string(items[0]["content"])); content != "[]" {
		t.Fatalf("reasoning content not cleared: %s", out)
	}
	if content := strings.TrimSpace(string(items[1]["content"])); content == "[]" {
		t.Fatalf("message content must not be cleared: %s", out)
	}
}

func TestSanitizeResponsesBodyClearsReasoningContentWithoutCiphertext(t *testing.T) {
	body := []byte(`{"model":"gpt-6.1-sol","input":[{"type":"reasoning","id":"a","content":[{"type":"reasoning_text","text":"raw"}]}]}`)
	for _, strict := range []bool{false, true} {
		out, changed := sanitizeResponsesBody(body, strict)
		if !changed {
			t.Fatalf("expected body to change (strict=%v)", strict)
		}
		var root map[string]any
		if err := json.Unmarshal(out, &root); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		item := root["input"].([]any)[0].(map[string]any)
		if content, ok := item["content"].([]any); !ok || len(content) != 0 {
			t.Fatalf("reasoning content not cleared (strict=%v): %v", strict, item["content"])
		}
	}
}
