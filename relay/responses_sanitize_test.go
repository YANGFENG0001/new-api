package relay

import (
	"encoding/json"
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
			var check []map[string]any
			if err := json.Unmarshal(out, &check); err != nil {
				t.Fatalf("output is not valid JSON: %v", err)
			}
		})
	}

	// plain string input must pass through untouched
	plain := json.RawMessage(`"hello"`)
	if got := sanitizeResponsesInput(plain, true); string(got) != `"hello"` {
		t.Fatalf("plain string mangled: %s", got)
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
