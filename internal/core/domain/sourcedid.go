package domain

import "strings"

// MsgSourcedID is the ValidationError message for a malformed sourcedId.
const MsgSourcedID = "must be printable ASCII"

// ValidSourcedID reports whether s holds only printable ASCII (0x20 to 0x7E).
// Case folding is then the same in Go and in Postgres.
func ValidSourcedID(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// FoldSourcedID returns the stored form of a sourcedId: lowercase, so ids
// that differ only in case are one key.
func FoldSourcedID(s string) string { return strings.ToLower(s) }

// NormalizeSourcedID rejects a non-ASCII sourcedId and lowercases a valid
// one in place. An empty sourcedId is left alone.
func NormalizeSourcedID(e Entity) error {
	sid := e.GetSourcedID()
	if sid == "" {
		return nil
	}
	if !ValidSourcedID(sid) {
		return &ValidationError{Field: "sourcedId", Msg: MsgSourcedID}
	}
	e.SetSourcedID(FoldSourcedID(sid))
	return nil
}
