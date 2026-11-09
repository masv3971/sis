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

func TestNewEntitiesValidate(t *testing.T) {
	cases := []struct {
		name   string
		entity Entity
		field  string // "" means valid
	}{
		{"affiliation without person", &Affiliation{OrgID: "o"}, "personId"},
		{"affiliation without org", &Affiliation{PersonID: "p"}, "orgId"},
		{"affiliation", &Affiliation{PersonID: "p", OrgID: "o", Role: RoleStaff}, ""},
		{"affiliation bad status", &Affiliation{Base: Base{Status: "x"}, PersonID: "p", OrgID: "o"}, "status"},
		{"collection offering without program", &CollectionOffering{}, "programId"},
		{"collection offering", &CollectionOffering{ProgramID: "g"}, ""},
		{"component template without title", &ComponentTemplate{}, "title"},
		{"component template empty course id", &ComponentTemplate{Title: "T", CourseIDs: []string{"c", ""}}, "courseIds"},
		{"component template", &ComponentTemplate{Title: "T", CourseIDs: []string{"c"}}, ""},
		{"component offering without component", &ComponentOffering{}, "componentId"},
		{"component offering empty offering id", &ComponentOffering{ComponentID: "t", CourseOfferingIDs: []string{""}}, "courseOfferingIds"},
		{"component offering", &ComponentOffering{ComponentID: "t", CourseOfferingIDs: []string{"o"}}, ""},
		{"course empty program id", &Course{Title: "T", ProgramIDs: []string{""}}, "programIds"},
		{"course with programs", &Course{Title: "T", ProgramIDs: []string{"g"}}, ""},
		{"offering empty collection offering id", &Offering{CourseID: "c", CollectionOfferingIDs: []string{""}}, "collectionOfferingIds"},
		{"offering with collection offerings", &Offering{CourseID: "c", CollectionOfferingIDs: []string{"k"}}, ""},
	}
	for _, tc := range cases {
		err := tc.entity.Validate()
		if tc.field == "" {
			assert.NoError(t, err, tc.name)
		} else {
			assertField(t, err, tc.field, tc.name)
		}
	}
}

func TestNewEntityTypeTags(t *testing.T) {
	assert.Equal(t, "affiliation", (&Affiliation{}).EntityType())
	assert.Equal(t, "collectionOffering", (&CollectionOffering{}).EntityType())
	assert.Equal(t, "componentTemplate", (&ComponentTemplate{}).EntityType())
	assert.Equal(t, "componentOffering", (&ComponentOffering{}).EntityType())
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
