// Package profile reads a dsh profile's own model choice without changing it.
//
// The sdk profile takes provider/model in its initialize request, but it only
// knows built-in providers. A provider the user added in another profile (the
// web profile's opencode-go, say) lives in that profile's llm-pi-ai entry, so
// that entry is copied verbatim into a temporary --patch overlay.
package profile

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Home is $DSH_HOME, defaulting to ~/.dsh.
func Home() string {
	if h := os.Getenv("DSH_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".dsh")
}

// Model is a provider/model route plus optional reasoning effort.
type Model struct {
	Provider  string
	Model     string
	Reasoning string
}

func (m Model) String() string { return m.Provider + "/" + m.Model }

// ParseModel parses "provider/model".
func ParseModel(s string) (Model, error) {
	provider, model, ok := strings.Cut(s, "/")
	if !ok || provider == "" || model == "" {
		return Model{}, fmt.Errorf("model must be <provider>/<model>, got %q", s)
	}
	return Model{Provider: provider, Model: model}, nil
}

// FromProfile is what a profile's patch says about models.
type FromProfile struct {
	Model         Model    // from its agent-default-model entry
	ProviderEntry []string // its llm-pi-ai entry, verbatim; nil if absent
}

// PatchPath is the profile's user patch layer.
func PatchPath(name string) string {
	return filepath.Join(Home(), "profiles", name, "cordis.patch.yml")
}

// Read extracts the model choice from a profile's cordis.patch.yml.
func Read(name string) (FromProfile, error) {
	text, err := os.ReadFile(PatchPath(name))
	if err != nil {
		return FromProfile{}, err
	}
	lines := strings.Split(string(text), "\n")
	var fp FromProfile
	block := EntryBlock(lines, "agent-default-model")
	if block == nil {
		return fp, fmt.Errorf("profile %q sets no agent-default-model", name)
	}
	for _, l := range block {
		key, value, ok := strings.Cut(strings.TrimSpace(l), ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "provider":
			fp.Model.Provider = value
		case "model":
			fp.Model.Model = value
		case "reasoningEffort":
			fp.Model.Reasoning = value
		}
	}
	if fp.Model.Provider == "" || fp.Model.Model == "" {
		return fp, fmt.Errorf("profile %q: agent-default-model lacks provider or model", name)
	}
	fp.ProviderEntry = EntryBlock(lines, "llm-pi-ai")
	return fp, nil
}

// EntryBlock returns the lines of the top-level "- id: <id>" entry, or nil.
func EntryBlock(lines []string, id string) []string {
	for i, l := range lines {
		if strings.TrimSpace(l) != "- id: "+id || strings.HasPrefix(l, " ") {
			continue
		}
		block := []string{l}
		for _, next := range lines[i+1:] {
			if strings.HasPrefix(next, "- ") || (next != "" && !strings.HasPrefix(next, " ") && !strings.HasPrefix(next, "#")) {
				break
			}
			block = append(block, next)
		}
		for len(block) > 1 && strings.TrimSpace(block[len(block)-1]) == "" {
			block = block[:len(block)-1]
		}
		return block
	}
	return nil
}

// ProviderEntry builds an llm-pi-ai entry defining one OpenAI-compatible provider.
func ProviderEntry(provider, baseURL, keyEnv string) []string {
	return []string{
		"- id: llm-pi-ai",
		"  config:",
		"    providers:",
		fmt.Sprintf("      %q:", provider),
		fmt.Sprintf("        baseURL: %q", baseURL),
		fmt.Sprintf("        apiKeyEnv: %q", keyEnv),
	}
}

// WritePatch writes entries to a fresh temp file; the caller removes it.
func WritePatch(entries []string) (string, error) {
	f, err := os.CreateTemp("", "dsh-tui-*.patch.yml")
	if err != nil {
		return "", err
	}
	w := bufio.NewWriter(f)
	for _, l := range entries {
		_, _ = fmt.Fprintln(w, l) // bufio keeps the first error; Flush reports it
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), f.Close()
}
