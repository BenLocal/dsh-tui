// Package ui is the Bubble Tea front end: a conversation pane over an input box.
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"dsh-tui/internal/sdk"
)

// Session is the running conversation the UI talks to.
type Session struct {
	Client    *sdk.Client
	ID        string
	Model     string // shown in the status bar
	Mode      string // permission mode
	Cwd       string
	Resumed   bool
	PriorLogs []sdk.Event // history drawn before live events
}

type (
	noteMsg      sdk.Notification
	exitMsg      struct{}
	promptErrMsg struct{ err error }
)

// Model is the Bubble Tea model.
type Model struct {
	s          Session
	transcript *Transcript
	view       viewport.Model
	input      textarea.Model
	spin       spinner.Model

	running      bool
	disconnected bool
	follow       bool // keep the pane scrolled to the newest line
	width        int
	height       int
	ready        bool
}

const inputHeight = 3

// New builds the UI for a started session.
func New(s Session) Model {
	in := textarea.New()
	in.Placeholder = "输入任务，回车发送；ctrl+j 换行"
	in.ShowLineNumbers = false
	in.Prompt = "┃ "
	in.CharLimit = 0
	in.SetHeight(inputHeight)
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j", "alt+enter"))
	in.Focus()

	t := NewTranscript()
	for i := range s.PriorLogs {
		t.Apply(&s.PriorLogs[i])
	}
	if s.Resumed {
		t.Notice(fmt.Sprintf("已恢复会话 %s，以上是历史记录", s.ID))
	}
	return Model{
		s:          s,
		transcript: t,
		input:      in,
		spin:       spinner.New(spinner.WithSpinner(spinner.Dot)),
		follow:     true,
	}
}

// Init starts the cursor, the spinner and the notification pump.
func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spin.Tick, m.waitNote())
}

func (m Model) waitNote() tea.Cmd {
	ch := m.s.Client.Notifications()
	return func() tea.Msg {
		n, ok := <-ch
		if !ok {
			return exitMsg{}
		}
		return noteMsg(n)
	}
}

func (m Model) send(text string) tea.Cmd {
	client, id := m.s.Client, m.s.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := client.Prompt(ctx, id, text); err != nil {
			return promptErrMsg{err}
		}
		return nil
	}
}

// Update handles keys, resizes and server notifications.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.refresh()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+d":
			return m, tea.Quit
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text == "" || m.disconnected {
				return m, nil
			}
			m.input.Reset()
			if m.running {
				m.transcript.Notice("已排队，当前这一轮结束后处理")
			}
			m.follow = true
			m.refresh()
			return m, m.send(text)
		case "ctrl+t":
			m.transcript.ShowReasoning = !m.transcript.ShowReasoning
			m.refresh()
			return m, nil
		case "pgup", "pgdown", "ctrl+u", "ctrl+b", "ctrl+f", "home", "end":
			var cmd tea.Cmd
			m.view, cmd = m.view.Update(msg)
			m.follow = m.view.AtBottom()
			return m, cmd
		}

	case noteMsg:
		switch v := sdk.Decode(sdk.Notification(msg)).(type) {
		case *sdk.Event:
			if v.SessionID == m.s.ID {
				m.transcript.Apply(v)
			}
		case *sdk.StatusChange:
			if v.SessionID == m.s.ID {
				m.running = v.Status == "running"
			}
		case *sdk.Subagent:
			if v.Finished {
				m.transcript.Notice("子 agent 已结束")
			} else {
				m.transcript.Notice("子 agent 已启动")
			}
		}
		m.refresh()
		return m, m.waitNote()

	case exitMsg:
		m.disconnected, m.running = true, false
		text := "dsh 进程已退出"
		if err := m.s.Client.Err(); err != nil {
			text += "：" + err.Error()
		}
		if tail := strings.TrimSpace(m.s.Client.Stderr()); tail != "" {
			text += "\n" + lastLines(tail, 5)
		}
		m.transcript.Error(text)
		m.refresh()
		return m, nil

	case promptErrMsg:
		m.transcript.Error("发送失败：" + msg.err.Error())
		m.refresh()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *Model) layout() {
	h := m.height - inputHeight - 2 // input box border + status line
	if h < 3 {
		h = 3
	}
	if !m.ready {
		m.view = viewport.New(m.width, h)
		m.view.KeyMap = viewport.KeyMap{} // scrolling keys are routed explicitly
		m.ready = true
	} else {
		m.view.Width, m.view.Height = m.width, h
	}
	m.input.SetWidth(m.width)
}

func (m *Model) refresh() {
	if !m.ready {
		return
	}
	m.view.SetContent(m.transcript.Render(m.width - 1))
	if m.follow {
		m.view.GotoBottom()
	}
}

var (
	styleBar   = lipgloss.NewStyle().Faint(true)
	styleState = lipgloss.NewStyle().Bold(true)
	styleBox   = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderTop(true).BorderForeground(lipgloss.Color("8"))
)

// View draws pane, input and status bar.
func (m Model) View() string {
	if !m.ready {
		return "启动中…"
	}
	state := styleState.Render("空闲")
	switch {
	case m.disconnected:
		state = styleError.Render("已断开")
	case m.running:
		state = m.spin.View() + styleState.Render("运行中")
	}
	u := m.transcript.Usage
	// The full session id is printed on exit; the bar keeps its unique tail.
	bar := fmt.Sprintf(" %s · %s · %s · …%s · tokens %d/%d · ctrl+t 思考 · ctrl+c 退出",
		state, m.s.Model, m.s.Mode, tail(m.s.ID, 8), u.InputTokens, u.OutputTokens)
	if !m.follow {
		bar += " · ↓ 有新内容，end 回到底部"
	}
	return m.view.View() + "\n" + styleBox.Width(m.width).Render(m.input.View()) + "\n" + styleBar.MaxWidth(m.width).Render(bar)
}

func tail(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[len(r)-n:])
	}
	return s
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
