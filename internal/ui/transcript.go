package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"dsh-tui/internal/sdk"
)

type entryKind int

const (
	kindUser entryKind = iota
	kindAssistant
	kindReasoning
	kindTool
	kindNotice
	kindError
)

type toolState int

const (
	toolRunning toolState = iota
	toolDone
	toolFailed
)

type entry struct {
	kind   entryKind
	text   string
	tool   string // tool name
	state  toolState
	output string
}

// Transcript folds session events into what the conversation pane shows.
type Transcript struct {
	entries       []*entry
	tools         map[string]*entry // by tool call id
	Usage         sdk.Usage
	Title         string
	ShowReasoning bool
}

// NewTranscript returns an empty transcript.
func NewTranscript() *Transcript {
	return &Transcript{tools: map[string]*entry{}}
}

// Apply adds one session event. Events the pane does not show are ignored.
func (t *Transcript) Apply(ev *sdk.Event) {
	d := ev.ParseData()
	switch ev.Type {
	case "user/message":
		// Runtime context and skill reminders are user-role too; only the
		// person's own messages carry source kind "user".
		if d.Source != nil && d.Source.Kind == "user" {
			t.add(&entry{kind: kindUser, text: joinText(d.Content, "text")})
		}
	case "assistant/message":
		if d.Message == nil {
			return
		}
		for _, b := range d.Message.Content {
			switch b.Type {
			case "reasoning":
				if strings.TrimSpace(b.Text) != "" {
					t.add(&entry{kind: kindReasoning, text: strings.TrimSpace(b.Text)})
				}
			case "text":
				if strings.TrimSpace(b.Text) != "" {
					t.add(&entry{kind: kindAssistant, text: strings.TrimSpace(b.Text)})
				}
			}
		}
		if d.Usage != nil {
			t.Usage.InputTokens += d.Usage.InputTokens
			t.Usage.OutputTokens += d.Usage.OutputTokens
			t.Usage.CacheReadTokens += d.Usage.CacheReadTokens
		}
	case "tool/call":
		e := &entry{kind: kindTool, tool: d.Name, text: summarize(d.Arguments)}
		t.tools[d.CallID] = e
		t.add(e)
	case "tool/result":
		if d.Message == nil {
			return
		}
		e := t.tools[d.Message.ToolCallID]
		if e == nil {
			return
		}
		e.output = strings.TrimRight(joinText(d.Message.Content, "text"), "\n")
		e.state = toolDone
		if d.Message.IsError {
			e.state = toolFailed
		}
	case "turn/end":
		if d.Reason == nil || d.Reason.Kind == "completed" {
			return
		}
		msg := "本轮结束：" + d.Reason.Kind
		if d.Reason.Error != nil {
			msg += "：" + d.Reason.Error.Message
		}
		t.add(&entry{kind: kindError, text: msg})
	case "session/title":
		t.Title = d.Title
	}
}

// Notice adds a line from the client itself.
func (t *Transcript) Notice(text string) { t.add(&entry{kind: kindNotice, text: text}) }

// Error adds an error line from the client itself.
func (t *Transcript) Error(text string) { t.add(&entry{kind: kindError, text: text}) }

func (t *Transcript) add(e *entry) { t.entries = append(t.entries, e) }

func joinText(blocks []sdk.ContentBlock, kind string) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == kind && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// summarize picks the argument a reader recognises a tool call by.
func summarize(arguments string) string {
	var args map[string]any
	if json.Unmarshal([]byte(arguments), &args) != nil {
		return oneLine(arguments, 120)
	}
	for _, key := range []string{"command", "file_path", "path", "pattern", "url", "query", "description"} {
		if v, ok := args[key].(string); ok && v != "" {
			return oneLine(v, 120)
		}
	}
	b, _ := json.Marshal(args)
	return oneLine(string(b), 120)
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

var (
	styleUser      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	styleReasoning = lipgloss.NewStyle().Faint(true).Italic(true)
	styleFaint     = lipgloss.NewStyle().Faint(true)
	styleToolName  = lipgloss.NewStyle().Bold(true)
	styleRunning   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleDone      = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleFailed    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleError     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

const (
	reasoningPreview = 3 // lines of collapsed reasoning
	outputPreview    = 5 // lines of tool output
)

// Render draws the whole transcript at the given width.
func (t *Transcript) Render(width int) string {
	if width < 20 {
		width = 20
	}
	wrap := lipgloss.NewStyle().Width(width)
	var b strings.Builder
	for i, e := range t.entries {
		if i > 0 {
			b.WriteString("\n")
		}
		switch e.kind {
		case kindUser:
			b.WriteString(wrap.Render(styleUser.Render("❯ ") + e.text))
		case kindAssistant:
			b.WriteString(wrap.Render(e.text))
		case kindReasoning:
			text := e.text
			if !t.ShowReasoning {
				text = clip(text, reasoningPreview, "（ctrl+t 展开思考）")
			}
			b.WriteString(styleReasoning.Width(width).Render("✻ " + text))
		case kindTool:
			marker := map[toolState]string{
				toolRunning: styleRunning.Render("●"),
				toolDone:    styleDone.Render("●"),
				toolFailed:  styleFailed.Render("●"),
			}[e.state]
			b.WriteString(wrap.Render(marker + " " + styleToolName.Render(e.tool) + " " + e.text))
			if e.output != "" {
				out := clip(e.output, outputPreview, "")
				b.WriteString("\n" + styleFaint.Width(width).Render(indent(out, "  ⎿ ", "    ")))
			}
		case kindNotice:
			b.WriteString(styleFaint.Width(width).Render("· " + e.text))
		case kindError:
			b.WriteString(styleError.Width(width).Render("✗ " + e.text))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func clip(text string, lines int, hint string) string {
	all := strings.Split(text, "\n")
	if len(all) <= lines {
		return text
	}
	more := fmt.Sprintf("… 还有 %d 行", len(all)-lines)
	if hint != "" {
		more += hint
	}
	return strings.Join(all[:lines], "\n") + "\n" + more
}

func indent(text, first, rest string) string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = first + lines[i]
		} else {
			lines[i] = rest + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
