package convert

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/core/domain"
)

// TestParseELMEnvelopes checks a credential converts to the same records
// whether it comes plain, in EDCI's uploadable wrapper, or sealed (payload
// as JSON text or base64url), and that only a seal adds a warning.
func TestParseELMEnvelopes(t *testing.T) {
	plain := readFixture(t, "edc_bsc.json")
	asString, err := json.Marshal(string(plain))
	require.NoError(t, err)
	encoded, err := json.Marshal(base64.RawURLEncoding.EncodeToString(plain))
	require.NoError(t, err)
	uploadable := `{"credential": ` + string(plain) + `, "deliveryDetails": {"displayDetails": []}}`
	signed := func(payload []byte) string {
		return `{"payload": ` + string(payload) + `, "signatures": [{"protected": "e30", "signature": "x"}]}`
	}

	client := newClient(t)
	want, err := client.Parse(FormatELM, plain, "data")
	require.NoError(t, err)

	cases := []struct {
		name, doc  string
		sealWarned bool
	}{
		{"uploadable", uploadable, false},
		{"signed", signed(asString), true},
		{"signed base64url", signed(encoded), true},
		{"signed uploadable", signed(mustMarshal(t, uploadable)), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := client.Parse(FormatELM, []byte(tc.doc), "data")
			require.NoError(t, err)
			assert.Equal(t, want.Bundle, got.Bundle)
			var seal []string
			for _, warning := range got.Warnings {
				if warning == "data: signed credential; the seal was not verified" {
					seal = append(seal, warning)
				}
			}
			if tc.sealWarned {
				assert.Len(t, seal, 1, "warnings = %v", got.Warnings)
				assert.Len(t, got.Warnings, len(want.Warnings)+1)
			} else {
				assert.Empty(t, seal)
				assert.Len(t, got.Warnings, len(want.Warnings))
			}
		})
	}
}

func mustMarshal(t *testing.T, text string) []byte {
	t.Helper()
	raw, err := json.Marshal(text)
	require.NoError(t, err)
	return raw
}

// TestParseELMEnvelopeErrors checks malformed envelopes are reported against
// the part of the document that is wrong.
func TestParseELMEnvelopeErrors(t *testing.T) {
	cases := []struct{ name, doc, field string }{
		{"payload not a string", `{"payload": {"credentialSubject": {}}, "signatures": []}`, "data.payload"},
		{"payload not a credential", `{"payload": "not json, not base64!", "signatures": []}`, "data.payload"},
		{"payload a JSON array", `{"payload": "[1]", "signatures": []}`, "data.payload"},
		{"credential not an object", `{"credential": "x"}`, "data.credential"},
		{"empty credential", `{"credential": {}}`, "data.credential.credentialSubject"},
		{"signed, not a credential", `{"payload": "{\"type\": \"x\"}", "signatures": []}`, "data.payload.credentialSubject"},
		{"payload without signatures", `{"payload": "{}"}`, "data.credentialSubject"},
	}
	client := newClient(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.Parse(FormatELM, []byte(tc.doc), "data")
			var invalid *domain.ValidationError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, tc.field, invalid.Field)
		})
	}
}

// TestParseELMEnvelopePaths checks errors inside a sealed credential name
// their place within the envelope.
func TestParseELMEnvelopePaths(t *testing.T) {
	inner := `{"credentialSubject": {"id": "urn:p"}}`
	doc := `{"payload": ` + string(mustMarshal(t, inner)) + `, "signatures": []}`
	_, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
	var invalid *domain.ValidationError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "data.payload.credentialSubject", invalid.Field)
}
