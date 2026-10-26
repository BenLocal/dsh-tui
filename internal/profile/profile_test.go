package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const webPatch = `# Your patch layer for this dsh profile
- id: connection
  config:
    trustedHosts: !!js ctx.webStartup.trustedHosts
- id: llm-pi-ai
  name: "@deepseek-ai/dsh-llm-pi-ai"
  config:
    providers:
      opencode-go:
        baseURL: https://opencode.ai/zen/go/v1
        apiKeyEnv: OPENCODE_GO_API_KEY
- id: agent-default-model
  name: "@deepseek-ai/dsh-agent-default-model"
  config:
    provider: opencode-go
    model: deepseek-v4.1-flash
    reasoningEffort: high

- id: ui-theme
  config:
    fontSize: 17
`

func withHome(t *testing.T, patch string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	dir := filepath.Join(home, "profiles", "web")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cordis.patch.yml"), []byte(patch), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadWebProfile(t *testing.T) {
	withHome(t, webPatch)
	fp, err := Read("web")
	if err != nil {
		t.Fatal(err)
	}
	if fp.Model != (Model{Provider: "opencode-go", Model: "deepseek-v4.1-flash", Reasoning: "high"}) {
		t.Fatalf("model %+v", fp.Model)
	}
	got := strings.Join(fp.ProviderEntry, "\n")
	if !strings.HasPrefix(got, "- id: llm-pi-ai") || !strings.Contains(got, "apiKeyEnv: OPENCODE_GO_API_KEY") {
		t.Fatalf("provider entry:\n%s", got)
	}
	if strings.Contains(got, "agent-default-model") || strings.Contains(got, "trustedHosts") {
		t.Fatalf("provider entry leaks other entries:\n%s", got)
	}
}

func TestReadWithoutModelEntry(t *testing.T) {
	withHome(t, "- id: ui-theme\n  config:\n    fontSize: 17\n")
	if _, err := Read("web"); err == nil {
		t.Fatal("expected an error for a profile without agent-default-model")
	}
}

func TestParseModel(t *testing.T) {
	if m, err := ParseModel("deepseek-official/deepseek-flash"); err != nil || m.Provider != "deepseek-official" {
		t.Fatalf("%+v %v", m, err)
	}
	for _, bad := range []string{"", "nomodel", "/m", "p/"} {
		if _, err := ParseModel(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestWritePatch(t *testing.T) {
	path, err := WritePatch(ProviderEntry("acme", "https://x/v1", "ACME_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "- id: llm-pi-ai\n  config:\n    providers:\n      \"acme\":\n        baseURL: \"https://x/v1\"\n        apiKeyEnv: \"ACME_KEY\"\n"
	if string(b) != want {
		t.Fatalf("got:\n%s", b)
	}
}
