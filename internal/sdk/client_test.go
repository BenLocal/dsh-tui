package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess is the fake `dsh --profile sdk` the tests start: the test
// binary re-executes itself with DSH_TUI_FAKE=1.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("DSH_TUI_FAKE") != "1" {
		return
	}
	out := bufio.NewWriter(os.Stdout)
	send := func(v any) {
		b, _ := json.Marshal(v)
		// A failed write means the client is gone; the next read exits.
		_, _ = out.Write(append(b, '\n'))
		_ = out.Flush()
	}
	note := func(method string, params any) {
		send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	}
	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			os.Exit(0)
		}
		var req struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &req) != nil {
			continue
		}
		reply := func(result any) { send(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}) }
		switch req.Method {
		case "initialize":
			fmt.Println("not json: ignored by the client")
			reply(map[string]any{"serverInfo": map[string]string{"name": "deepseek-harness-sdk-runtime", "version": "test"}})
		case "session/prompt":
			var p struct {
				SessionID string `json:"sessionId"`
			}
			_ = json.Unmarshal(req.Params, &p) // an empty session id is fine for the fake
			reply(map[string]string{"messageId": "m1"})
			note("session.status", map[string]string{"sessionId": p.SessionID, "status": "running"})
			big := strings.Repeat("x", 200_000) // longer than bufio.Scanner's default line cap
			note("session.event", map[string]any{"sessionId": p.SessionID, "event": map[string]any{
				"type": "assistant/message", "seq": 1,
				"data": map[string]any{"message": map[string]any{"role": "assistant", "content": []map[string]string{{"type": "text", "text": big}}}},
			}})
			note("session.status", map[string]string{"sessionId": p.SessionID, "status": "idle"})
		case "fail":
			send(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32603, "message": "boom"}})
		case "crash":
			os.Exit(3)
		case "shutdown":
			reply(map[string]any{})
			os.Exit(0)
		}
	}
}

func startFake(t *testing.T) *Client {
	t.Helper()
	c, err := Start(Options{
		Bin:  os.Args[0],
		Args: []string{"-test.run=TestHelperProcess", "--"},
		Env:  append(os.Environ(), "DSH_TUI_FAKE=1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close(5 * time.Second) })
	return c
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestInitializeSkipsNonProtocolLines(t *testing.T) {
	c := startFake(t)
	r, err := c.Initialize(ctx(t), InitializeParams{Cwd: "/w", Provider: "p", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if r.ServerInfo.Name != "deepseek-harness-sdk-runtime" {
		t.Fatalf("server name %q", r.ServerInfo.Name)
	}
}

func TestErrorResponseIsAnRPCError(t *testing.T) {
	c := startFake(t)
	err := c.Call(ctx(t), "fail", nil, nil)
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Message != "boom" || rpc.Code != -32603 {
		t.Fatalf("got %v", err)
	}
}

func TestNotificationsArriveInOrderAndDecode(t *testing.T) {
	c := startFake(t)
	if err := c.Prompt(ctx(t), "s1", "hi"); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for len(kinds) < 3 {
		select {
		case n := <-c.Notifications():
			switch v := Decode(n).(type) {
			case *StatusChange:
				kinds = append(kinds, "status:"+v.Status)
			case *Event:
				if v.SessionID != "s1" {
					t.Fatalf("event session %q", v.SessionID)
				}
				msg := v.ParseData().Message
				if msg == nil || len(msg.Content[0].Text) != 200_000 {
					t.Fatal("long event line was not decoded whole")
				}
				kinds = append(kinds, "event:"+v.Type)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out after %v", kinds)
		}
	}
	want := "status:running event:assistant/message status:idle"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestProcessExitFailsCallsAndClosesNotifications(t *testing.T) {
	c := startFake(t)
	err := c.Call(ctx(t), "crash", nil, nil)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done not closed")
	}
	if _, ok := <-c.Notifications(); ok {
		t.Fatal("notifications still open")
	}
	if c.Err() == nil {
		t.Fatal("exit status 3 not reported")
	}
	if err := c.Prompt(ctx(t), "s1", "late"); !errors.Is(err, ErrClosed) {
		t.Fatalf("call after exit: %v", err)
	}
}

func TestCloseShutsDown(t *testing.T) {
	c := startFake(t)
	c.Close(5 * time.Second)
	select {
	case <-c.Done():
	default:
		t.Fatal("process still running after Close")
	}
}
