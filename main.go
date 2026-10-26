// dsh-tui is a terminal client for DeepSeek Harness, driving `dsh --profile sdk`.
//
// It never edits the user's dsh configuration: a provider that only another
// profile defines is passed in through a temporary --patch file, removed on exit.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"dsh-tui/internal/history"
	"dsh-tui/internal/profile"
	"dsh-tui/internal/sdk"
	"dsh-tui/internal/ui"
)

type multiFlag []string

func (m *multiFlag) String() string     { return fmt.Sprint(*m) }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

type options struct {
	cwd, model, reasoning, modelFrom, baseURL, keyEnv, mode, resume, dsh string
	patches                                                              multiFlag
}

func main() {
	var o options
	flag.StringVar(&o.cwd, "cwd", ".", "workspace root for the agent")
	flag.StringVar(&o.model, "model", "", "<provider>/<model>; default: the web profile's model")
	flag.StringVar(&o.reasoning, "reasoning", "", "reasoning effort, e.g. high")
	flag.StringVar(&o.modelFrom, "model-from", "", "reuse this profile's model and provider definition (default: web, when it sets one)")
	flag.StringVar(&o.baseURL, "provider-base-url", "", "define the -model provider: OpenAI-compatible base URL")
	flag.StringVar(&o.keyEnv, "provider-key-env", "", "env var holding that provider's API key")
	flag.StringVar(&o.mode, "mode", "workspace-write", "sandbox: read-only, workspace-write or danger-full-access")
	flag.StringVar(&o.resume, "resume", "", "continue this persisted session id")
	flag.StringVar(&o.dsh, "dsh", "dsh", "dsh executable")
	flag.Var(&o.patches, "patch", "extra dsh --patch overlay (repeatable)")
	flag.Parse()

	code, err := run(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dsh-tui:", err)
	}
	os.Exit(code)
}

func run(o options) (int, error) {
	switch o.mode {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return 2, fmt.Errorf("-mode must be read-only, workspace-write or danger-full-access")
	}
	cwd, err := filepath.Abs(o.cwd)
	if err != nil {
		return 2, err
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return 2, fmt.Errorf("-cwd %s is not a directory", cwd)
	}

	model, entries, err := chooseModel(o)
	if err != nil {
		return 2, err
	}

	var prior []sdk.Event
	if o.resume != "" {
		path, err := history.Find(profile.Home(), o.resume)
		if err != nil {
			return 2, err
		}
		hdr, events, err := history.Load(path)
		if err != nil {
			return 1, fmt.Errorf("read session log: %w", err)
		}
		if hdr.Cwd != "" && hdr.Cwd != cwd {
			return 2, fmt.Errorf("session %s was recorded in %s; rerun with -cwd %s", o.resume, hdr.Cwd, hdr.Cwd)
		}
		prior = events
	}

	args := []string{"--profile", "sdk"}
	if len(entries) > 0 {
		patch, err := profile.WritePatch(entries)
		if err != nil {
			return 1, err
		}
		defer func() { _ = os.Remove(patch) }()
		args = append(args, "--patch", patch)
	}
	for _, p := range o.patches {
		abs, _ := filepath.Abs(p)
		args = append(args, "--patch", abs)
	}

	client, err := sdk.Start(sdk.Options{
		Bin:  o.dsh,
		Args: args,
		Dir:  cwd,
		Env:  append(os.Environ(), "DSH_PERMISSION_MODE="+o.mode),
	})
	if err != nil {
		return 1, err
	}
	defer client.Close(10 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := client.Initialize(ctx, sdk.InitializeParams{
		Cwd: cwd, Provider: model.Provider, Model: model.Model, ReasoningEffort: model.Reasoning,
	}); err != nil {
		if strings.Contains(err.Error(), "no adapter registered for provider") {
			err = fmt.Errorf("%w\n提示：sdk profile 不认识 provider %q。用 -model-from <profile> 复用定义了它的 profile（如 web），"+
				"或用 -provider-base-url 和 -provider-key-env 定义它", err, model.Provider)
		}
		return 1, withStderr(err, client)
	}

	sessionID := o.resume
	if sessionID != "" {
		if err := client.Resume(ctx, sessionID); err != nil {
			return 1, withStderr(err, client)
		}
	} else {
		sessionID = newSessionID()
	}

	p := tea.NewProgram(ui.New(ui.Session{
		Client:    client,
		ID:        sessionID,
		Model:     model.String(),
		Mode:      o.mode,
		Cwd:       cwd,
		Resumed:   o.resume != "",
		PriorLogs: prior,
	}), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return 1, err
	}
	fmt.Printf("会话 %s\n继续：dsh-tui -cwd %s -resume %s\n", sessionID, cwd, sessionID)
	return 0, nil
}

// chooseModel resolves the route and the patch entries it needs.
func chooseModel(o options) (profile.Model, []string, error) {
	var model profile.Model
	var entries []string
	from := o.modelFrom
	if from == "" && o.model == "" {
		if _, err := os.Stat(profile.PatchPath("web")); err == nil {
			from = "web"
		}
	}
	if from != "" {
		fp, err := profile.Read(from)
		if err != nil && (o.modelFrom != "" || !errors.Is(err, os.ErrNotExist)) {
			return model, nil, fmt.Errorf("-model-from %s: %w", from, err)
		}
		model, entries = fp.Model, fp.ProviderEntry
	}
	if o.model != "" {
		m, err := profile.ParseModel(o.model)
		if err != nil {
			return model, nil, err
		}
		model = m
	}
	if model.Provider == "" {
		model = profile.Model{Provider: "deepseek-official", Model: "deepseek-flash"}
	}
	if o.reasoning != "" {
		model.Reasoning = o.reasoning
	}
	if o.baseURL != "" {
		key := o.keyEnv
		if key == "" {
			key = "DEEPSEEK_API_KEY"
		}
		entries = profile.ProviderEntry(model.Provider, o.baseURL, key)
	}
	return model, entries, nil
}

func withStderr(err error, c *sdk.Client) error {
	if tail := c.Stderr(); tail != "" {
		return fmt.Errorf("%w\n%s", err, tail)
	}
	return err
}

func newSessionID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "tui-" + time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}
