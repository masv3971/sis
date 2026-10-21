package domain

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProgramValidate(t *testing.T) {
	requireField(t, (&Program{}).Validate(), "title", "missing title")
	require.NoError(t, (&Program{Title: "BSc"}).Validate(), "valid program")
}

func TestResultValidate(t *testing.T) {
	cases := []struct {
		name   string
		result Result
		field  string // "" means valid
	}{
		{"no person", Result{CourseID: "c"}, "personId"},
		{"neither target", Result{PersonID: "p"}, "courseId"},
		{"both targets", Result{PersonID: "p", CourseID: "c", ProgramID: "g"}, "courseId"},
		{"negative credits", Result{PersonID: "p", CourseID: "c", Credits: -1}, "credits"},
		{"NaN credits", Result{PersonID: "p", CourseID: "c", Credits: math.NaN()}, "credits"},
		{"+Inf credits", Result{PersonID: "p", CourseID: "c", Credits: math.Inf(1)}, "credits"},
		{"-Inf credits", Result{PersonID: "p", CourseID: "c", Credits: math.Inf(-1)}, "credits"},
		{"course", Result{PersonID: "p", CourseID: "c", Credits: 7.5}, ""},
		{"program", Result{PersonID: "p", ProgramID: "g"}, ""},
	}
	for _, tc := range cases {
		err := tc.result.Validate()
		if tc.field == "" {
			assert.NoError(t, err, tc.name)
		} else {
			assertField(t, err, tc.field, tc.name)
		}
	}
}

// assertField checks err is a *ValidationError for field.
func assertField(t *testing.T, err error, field, msg string) bool {
	t.Helper()
	var ve *ValidationError
	if !assert.ErrorAs(t, err, &ve, msg) {
		return false
	}
	return assert.Equal(t, field, ve.Field, msg)
}

// requireField is assertField that stops the test on failure.
func requireField(t *testing.T, err error, field, msg string) {
	t.Helper()
	if !assertField(t, err, field, msg) {
		t.FailNow()
	}
}
