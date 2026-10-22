package convert

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/SUNET/sis/internal/core/domain"
)

// maxEnvelopes bounds how many wrappers unwrapELM peels off (a signed
// envelope around an uploadable one is the deepest EDCI produces).
const maxEnvelopes = 2

// unwrapELM returns the credential inside the envelopes the European Digital
// Credentials infrastructure (EDCI) puts around one, and the document path
// it was found at:
//
//	{"credentialSubject": ...}                  a plain credential: returned as is
//	{"credential": {...}, "deliveryDetails": ...} EDCI's upload-for-sealing format
//	{"payload": "...", "signatures": [...]}     a sealed credential (JAdES, JWS JSON
//	                                            serialization): payload is the
//	                                            credential as a JSON string, or
//	                                            base64url-encoded
//
// The seal is not verified: SIS trusts the caller's token, not the issuer's
// seal, and says so in a warning. Anything else (not an object, or an object
// that is none of the above) is returned unchanged so parseELM reports it.
func unwrapELM(data []byte, path string) ([]byte, string, []string, error) {
	var warnings []string
	for range maxEnvelopes {
		var top map[string]json.RawMessage
		if err := json.Unmarshal(data, &top); err != nil {
			return data, path, warnings, nil // let elm.Parse report it
		}
		if _, ok := top["credentialSubject"]; ok {
			return data, path, warnings, nil
		}
		payload, signed := top["payload"]
		_, hasSignatures := top["signatures"]
		credential, uploadable := top["credential"]
		switch {
		case signed && hasSignatures:
			inner, err := signedPayload(payload, path+".payload")
			if err != nil {
				return nil, "", nil, err
			}
			warnings = append(warnings, path+": signed credential; the seal was not verified")
			data, path = inner, path+".payload"
		case uploadable:
			if !isObject(credential) {
				return nil, "", nil, &domain.ValidationError{Field: path + ".credential", Msg: "must be an ELM credential object"}
			}
			data, path = credential, path+".credential"
		default:
			return data, path, warnings, nil
		}
	}
	return data, path, warnings, nil
}

// signedPayload decodes the payload of a sealed credential: a JSON string
// holding the credential, either as JSON text (unencoded payload, "b64":
// false, which is what EDCI issues) or base64url-encoded.
func signedPayload(raw json.RawMessage, path string) ([]byte, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil, &domain.ValidationError{Field: path, Msg: "must be a string holding the credential"}
	}
	inner := []byte(text)
	if !isObject(inner) {
		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(text, "="))
		if err != nil || !isObject(decoded) {
			return nil, &domain.ValidationError{Field: path, Msg: "must hold the credential as JSON or base64url-encoded JSON"}
		}
		inner = decoded
	}
	return inner, nil
}

// isObject reports whether data is a single JSON object.
func isObject(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) > 0 && trimmed[0] == '{' && json.Valid(trimmed)
}
