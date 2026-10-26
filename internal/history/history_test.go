package history

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func writeLog(t *testing.T, path string, frames ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// dsh appends to the log: each append is its own zstd frame.
	for _, frame := range frames {
		enc, err := zstd.NewWriter(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := enc.Write([]byte(frame)); err != nil {
			t.Fatal(err)
		}
		if err := enc.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFindAndLoadMultiFrameLog(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "--w--", "tui-1", logName)
	writeLog(t, path,
		`{"type":"session","version":4,"id":"tui-1","cwd":"/w"}`+"\n"+`{"type":"turn/start","seq":5,"data":{"turn":1}}`+"\n",
		`{"type":"turn/end","seq":9,"data":{"turn":1,"reason":{"kind":"completed"}}}`+"\n",
	)
	found, err := Find(home, "tui-1")
	if err != nil || found != path {
		t.Fatalf("find: %q %v", found, err)
	}
	hdr, events, err := Load(found)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.ID != "tui-1" || hdr.Cwd != "/w" {
		t.Fatalf("header %+v", hdr)
	}
	if len(events) != 2 || events[0].Type != "turn/start" || events[1].Seq != 9 || events[1].SessionID != "tui-1" {
		t.Fatalf("events %+v", events)
	}
}

func TestFindUnknownSession(t *testing.T) {
	if _, err := Find(t.TempDir(), "nope"); err == nil {
		t.Fatal("expected error")
	}
}
