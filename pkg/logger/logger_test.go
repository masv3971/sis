package logger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestFormatAndLevelFromConfig(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter("sis", true, "warn", &buf).New("main")
	log.Info("hidden")
	log.Warn("shown")

	out := strings.TrimSpace(buf.String())
	if strings.Contains(out, "hidden") {
		t.Fatalf("info record emitted at warn level: %s", out)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("expected one JSON record, got %q: %v", out, err)
	}
	if rec["msg"] != "shown" || rec["logger"] != "sis.main" {
		t.Fatalf("unexpected record: %v", rec)
	}
}

func TestTextFormatAndDebug(t *testing.T) {
	var buf bytes.Buffer
	NewWithWriter("sis", false, "debug", &buf).Debug("dbg")
	out := buf.String()
	if !strings.Contains(out, "level=DEBUG") || !strings.Contains(out, "msg=dbg") {
		t.Fatalf("expected text debug record, got %q", out)
	}
}

func TestUnknownLevelDefaultsToInfo(t *testing.T) {
	if got := parseLevel(""); got.String() != "INFO" {
		t.Fatalf("empty level: got %s", got)
	}
	if got := parseLevel("bogus"); got.String() != "INFO" {
		t.Fatalf("bogus level: got %s", got)
	}
}
