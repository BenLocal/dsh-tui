// Package history reads a persisted dsh session log, read-only.
//
// session/resume does not replay earlier events as notifications, so a
// resumed conversation is drawn from the log on disk:
// $DSH_HOME/sessions/<cwd slug>/<session id>/session.v4.jsonl.zstd,
// one session event per line, zstd-compressed (possibly several frames).
package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"

	"dsh-tui/internal/sdk"
)

const logName = "session.v4.jsonl.zstd"

// Find locates the log of a session id under any workspace slug.
func Find(dshHome, sessionID string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dshHome, "sessions", "*", sessionID, logName))
	if err != nil {
		return "", err
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no session log for %q under %s", sessionID, filepath.Join(dshHome, "sessions"))
	case 1:
		return matches[0], nil
	}
	return "", fmt.Errorf("session id %q is ambiguous: %d logs", sessionID, len(matches))
}

// Header is the first record of a log.
type Header struct {
	ID  string `json:"id"`
	Cwd string `json:"cwd"`
}

// Load decodes a log into its header and events.
func Load(path string) (Header, []sdk.Event, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Header{}, nil, err
	}
	dec, err := zstd.NewReader(bytes.NewReader(raw))
	if err != nil {
		return Header{}, nil, err
	}
	defer dec.Close()
	var hdr Header
	var events []sdk.Event
	r := bufio.NewReader(dec)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var probe struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(line, &probe) == nil {
				if probe.Type == "session" {
					_ = json.Unmarshal(line, &hdr)
				} else {
					var ev sdk.Event
					if json.Unmarshal(line, &ev) == nil {
						ev.SessionID = hdr.ID
						events = append(events, ev)
					}
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A log still being appended may end mid-frame: keep what decoded.
			if hdr.ID == "" {
				return hdr, events, err
			}
			break
		}
	}
	return hdr, events, nil
}
