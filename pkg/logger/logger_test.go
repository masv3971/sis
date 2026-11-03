package logger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatAndLevelFromConfig(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter("sis", true, "warn", &buf).New("main")
	log.Info("hidden")
	log.Warn("shown")

	out := strings.TrimSpace(buf.String())
	require.NotContains(t, out, "hidden", "info record emitted at warn level")
	var record map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &record), "expected one JSON record, got %q", out)
	assert.Equal(t, "shown", record["msg"])
	assert.Equal(t, "sis.main", record["logger"])
}

func TestTextFormatAndDebug(t *testing.T) {
	var buf bytes.Buffer
	NewWithWriter("sis", false, "debug", &buf).Debug("dbg")
	out := buf.String()
	assert.Contains(t, out, "level=DEBUG", "expected text debug record")
	assert.Contains(t, out, "msg=dbg", "expected text debug record")
}

func TestUnknownLevelDefaultsToInfo(t *testing.T) {
	assert.Equal(t, "INFO", parseLevel("").String(), "empty level")
	assert.Equal(t, "INFO", parseLevel("bogus").String(), "bogus level")
}
