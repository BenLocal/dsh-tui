package sdk

import (
	"context"
	"encoding/json"
)

// InitializeParams selects the model route and workspace for every session.
type InitializeParams struct {
	Cwd             string `json:"cwd"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

// InitializeResult identifies the server.
type InitializeResult struct {
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

// ContentBlock is one block of a message: text, reasoning, tool-call, ...
type ContentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// Initialize performs the handshake; prompts are rejected before it succeeds.
func (c *Client) Initialize(ctx context.Context, p InitializeParams) (InitializeResult, error) {
	var r InitializeResult
	err := c.Call(ctx, "initialize", p, &r)
	return r, err
}

// Prompt enqueues one user message; it returns before the turn runs.
func (c *Client) Prompt(ctx context.Context, sessionID, text string) error {
	return c.Call(ctx, "session/prompt", map[string]any{
		"sessionId":     sessionID,
		"contentBlocks": []ContentBlock{{Type: "text", Text: text}},
	}, nil)
}

// Resume restores a persisted session so later prompts continue it.
func (c *Client) Resume(ctx context.Context, sessionID string) error {
	return c.Call(ctx, "session/resume", map[string]string{"sessionId": sessionID}, nil)
}

// Event is one session.event notification: a durable session log record.
type Event struct {
	SessionID string
	Type      string          `json:"type"`
	Seq       int64           `json:"seq"`
	Time      int64           `json:"time"`
	Data      json.RawMessage `json:"data"`
}

// StatusChange is one session.status notification.
type StatusChange struct {
	SessionID string `json:"sessionId"`
	Status    string `json:"status"` // running | idle
}

// Subagent is a subagent.started / subagent.finished notification.
type Subagent struct {
	Finished bool
	Raw      json.RawMessage
}

// Decode turns a notification into *Event, *StatusChange, *Subagent, or nil.
func Decode(n Notification) any {
	switch n.Method {
	case "session.event":
		var p struct {
			SessionID string `json:"sessionId"`
			Event     Event  `json:"event"`
		}
		if json.Unmarshal(n.Params, &p) != nil {
			return nil
		}
		p.Event.SessionID = p.SessionID
		return &p.Event
	case "session.status":
		var s StatusChange
		if json.Unmarshal(n.Params, &s) != nil {
			return nil
		}
		return &s
	case "subagent.started", "subagent.finished":
		return &Subagent{Finished: n.Method == "subagent.finished", Raw: n.Params}
	}
	return nil
}

// Message is the message payload of user/assistant/tool events.
type Message struct {
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	IsError    bool           `json:"isError"`
	ToolCallID string         `json:"toolCallId"`
	Source     struct {
		Kind     string `json:"kind"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	} `json:"source"`
}

// Usage is the token accounting on an assistant/message event.
type Usage struct {
	InputTokens     int `json:"inputTokens"`
	OutputTokens    int `json:"outputTokens"`
	CacheReadTokens int `json:"cacheReadTokens"`
}

// EventData is the union of the data fields the client renders.
type EventData struct {
	Turn      int      `json:"turn"`
	Step      int      `json:"step"`
	Message   *Message `json:"message"`
	Usage     *Usage   `json:"usage"`
	CallID    string   `json:"callId"`
	Name      string   `json:"name"`
	Arguments string   `json:"arguments"`
	Reason    *struct {
		Kind  string `json:"kind"`
		Error *struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	} `json:"reason"`
	// user/message carries its message inline rather than under "message".
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
	Source  *struct {
		Kind string `json:"kind"`
	} `json:"source"`
	Title string `json:"title"`
}

// ParseData decodes the event's data; unknown shapes yield a zero value.
func (e *Event) ParseData() EventData {
	var d EventData
	_ = json.Unmarshal(e.Data, &d)
	return d
}
