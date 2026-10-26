package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"dsh-tui/internal/sdk"
)

func ev(typ string, data any) *sdk.Event {
	b, _ := json.Marshal(data)
	return &sdk.Event{Type: typ, Data: b}
}

func text(t, s string) map[string]string { return map[string]string{"type": t, "text": s} }

// Shapes copied from a real `dsh --profile sdk` run.
func conversation() *Transcript {
	tr := NewTranscript()
	tr.Apply(ev("user/message", map[string]any{"role": "user", "source": map[string]string{"kind": "user"}, "content": []any{text("text", "列出文件")}}))
	tr.Apply(ev("user/message", map[string]any{"role": "user", "source": map[string]string{"kind": "context"}, "content": []any{text("text", "Current runtime context")}}))
	tr.Apply(ev("assistant/message", map[string]any{
		"message": map[string]any{"role": "assistant", "content": []any{
			text("reasoning", "一\n二\n三\n四\n五"),
			map[string]string{"type": "tool-call", "id": "c1", "name": "bash", "arguments": `{"command":"ls -la"}`},
		}},
		"usage": map[string]int{"inputTokens": 100, "outputTokens": 7, "cacheReadTokens": 50},
	}))
	tr.Apply(ev("tool/call", map[string]any{"callId": "c1", "name": "bash", "arguments": `{"command":"ls -la","description":"List"}`}))
	tr.Apply(ev("tool/result", map[string]any{"message": map[string]any{
		"role": "tool", "toolCallId": "c1", "isError": false,
		"content": []any{text("text", "1\n2\n3\n4\n5\n6\n7\n")},
	}}))
	tr.Apply(ev("assistant/message", map[string]any{
		"message": map[string]any{"role": "assistant", "content": []any{text("text", "有 7 个文件。")}},
		"usage":   map[string]int{"inputTokens": 20, "outputTokens": 5},
	}))
	return tr
}

func TestRendersConversation(t *testing.T) {
	out := conversation().Render(80)
	for _, want := range []string{"❯ 列出文件", "bash ls -la", "⎿ 1", "… 还有 2 行", "有 7 个文件。", "ctrl+t 展开思考"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Current runtime context") {
		t.Error("runtime context message shown as a user message")
	}
	if strings.Contains(out, "四") {
		t.Error("collapsed reasoning shows more than its preview")
	}
}

func TestReasoningToggle(t *testing.T) {
	tr := conversation()
	tr.ShowReasoning = true
	if out := tr.Render(80); !strings.Contains(out, "五") || strings.Contains(out, "展开思考") {
		t.Errorf("expanded reasoning not shown whole:\n%s", out)
	}
}

func TestUsageAccumulates(t *testing.T) {
	u := conversation().Usage
	if u.InputTokens != 120 || u.OutputTokens != 12 || u.CacheReadTokens != 50 {
		t.Fatalf("usage %+v", u)
	}
}

func TestFailedToolAndTurnError(t *testing.T) {
	tr := NewTranscript()
	tr.Apply(ev("tool/call", map[string]any{"callId": "c9", "name": "read", "arguments": `{"file_path":"/x"}`}))
	tr.Apply(ev("tool/result", map[string]any{"message": map[string]any{"toolCallId": "c9", "isError": true, "content": []any{text("text", "no such file")}}}))
	tr.Apply(ev("turn/end", map[string]any{"reason": map[string]any{"kind": "error", "error": map[string]string{"code": "MISSING_CREDENTIAL", "message": "no API key"}}}))
	tr.Apply(ev("turn/end", map[string]any{"reason": map[string]any{"kind": "completed"}}))
	out := tr.Render(80)
	if !strings.Contains(out, "read /x") || !strings.Contains(out, "no such file") {
		t.Errorf("failed tool not rendered:\n%s", out)
	}
	if !strings.Contains(out, "本轮结束：error：no API key") || strings.Count(out, "本轮结束") != 1 {
		t.Errorf("turn end rendering wrong:\n%s", out)
	}
}

func TestSummarizeFallsBackToCompactJSON(t *testing.T) {
	if got := summarize(`{"todos":[1,2]}`); got != `{"todos":[1,2]}` {
		t.Fatalf("got %q", got)
	}
	if got := summarize("not json"); got != "not json" {
		t.Fatalf("got %q", got)
	}
}
