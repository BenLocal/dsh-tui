package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func home(t *testing.T, webPatch string) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("DSH_HOME", h)
	if webPatch != "" {
		dir := filepath.Join(h, "profiles", "web")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cordis.patch.yml"), []byte(webPatch), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const web = "- id: llm-pi-ai\n  config:\n    providers:\n      opencode-go:\n        baseURL: https://o/v1\n" +
	"- id: agent-default-model\n  config:\n    provider: opencode-go\n    model: m1\n    reasoningEffort: high\n"

func TestDefaultsToWebProfile(t *testing.T) {
	home(t, web)
	m, entries, err := chooseModel(options{})
	if err != nil {
		t.Fatal(err)
	}
	if m.String() != "opencode-go/m1" || m.Reasoning != "high" || !strings.Contains(strings.Join(entries, "\n"), "opencode-go") {
		t.Fatalf("%+v %v", m, entries)
	}
}

func TestFallsBackWithoutWebProfile(t *testing.T) {
	home(t, "")
	m, entries, err := chooseModel(options{})
	if err != nil || m.String() != "deepseek-official/deepseek-flash" || entries != nil {
		t.Fatalf("%+v %v %v", m, entries, err)
	}
}

func TestExplicitModelSkipsProfile(t *testing.T) {
	home(t, web)
	m, entries, err := chooseModel(options{model: "deepseek-official/deepseek-pro", reasoning: "max"})
	if err != nil || m.String() != "deepseek-official/deepseek-pro" || m.Reasoning != "max" || entries != nil {
		t.Fatalf("%+v %v %v", m, entries, err)
	}
}

func TestExplicitProviderDefinition(t *testing.T) {
	home(t, "")
	_, entries, err := chooseModel(options{model: "acme/x", baseURL: "https://a/v1", keyEnv: "ACME_KEY"})
	if err != nil || !strings.Contains(strings.Join(entries, "\n"), `apiKeyEnv: "ACME_KEY"`) {
		t.Fatalf("%v %v", entries, err)
	}
}

func TestMissingNamedProfileIsAnError(t *testing.T) {
	home(t, "")
	if _, _, err := chooseModel(options{modelFrom: "nosuch"}); err == nil {
		t.Fatal("expected error")
	}
}
