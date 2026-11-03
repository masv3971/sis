package convert

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/pkg/logger"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden.json")

func newClient(t *testing.T) *Client {
	t.Helper()
	client, err := New(t.Context(), nil, logger.NewWithWriter("test", false, "error", io.Discard))
	require.NoError(t, err)
	return client
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name)) // #nosec G304 -- fixed test fixture
	require.NoError(t, err)
	return data
}

// TestParseELMMapping checks the mapping rules on a credential with a
// qualification whose part is a graded course with an activity.
func TestParseELMMapping(t *testing.T) {
	parsed, err := newClient(t).Parse(FormatELM, readFixture(t, "edc_bsc.json"), "data")
	require.NoError(t, err)
	bundle := parsed.Bundle

	require.Len(t, bundle.Persons, 1)
	person := bundle.Persons[0]
	assert.Equal(t, "urn:epass:person:liu-student-4711", person.SourcedID)
	assert.Equal(t, "Ada", person.GivenName)
	assert.Equal(t, "Lovelace", person.FamilyName)
	assert.Equal(t, "2001-12-10", person.DateOfBirth)
	if assert.Len(t, person.Emails, 1) {
		assert.Equal(t, "adalo123@student.liu.se", person.Emails[0].Value)
	}
	assert.Equal(t, []domain.Identifier{{Type: "LiU-ID", Value: "adalo123"}}, person.Identifiers)

	orgs := map[string]*domain.Organization{}
	for _, org := range bundle.Organizations {
		orgs[org.SourcedID] = org
	}
	assert.Len(t, orgs, 2)
	require.Contains(t, orgs, "urn:epass:org:liu-ifm")
	require.Contains(t, orgs, "urn:epass:org:liu")
	assert.Equal(t, "urn:epass:org:liu", orgs["urn:epass:org:liu-ifm"].ParentID)
	assert.Equal(t, "Linköping University", orgs["urn:epass:org:liu"].Name)

	require.Len(t, bundle.Programs, 1)
	assert.Equal(t, "NBIO", bundle.Programs[0].Code)
	assert.Equal(t, "6", bundle.Programs[0].Level)
	assert.Equal(t, "urn:epass:org:liu", bundle.Programs[0].OrgID)

	require.Len(t, bundle.Courses, 1)
	course := bundle.Courses[0]
	assert.Equal(t, "urn:epass:learningAchievementSpec:bi101", course.SourcedID)
	assert.Equal(t, "BI101", course.Code)
	assert.Equal(t, "Biology 101", course.Title)
	assert.Equal(t, "urn:epass:org:liu-ifm", course.OrgID)

	require.Len(t, bundle.AcademicSessions, 1)
	assert.Equal(t, "Autumn 2025", bundle.AcademicSessions[0].Title)
	assert.Equal(t, "2025-09-01", bundle.AcademicSessions[0].StartDate)

	require.Len(t, bundle.Offerings, 1)
	offering := bundle.Offerings[0]
	assert.Equal(t, course.SourcedID, offering.CourseID)
	assert.Equal(t, "urn:epass:period:ht25", offering.AcademicSessionID)

	require.Len(t, bundle.Enrollments, 1)
	assert.Equal(t, person.SourcedID, bundle.Enrollments[0].PersonID)
	assert.Equal(t, offering.SourcedID, bundle.Enrollments[0].OfferingID)

	results := map[string]*domain.Result{}
	for _, result := range bundle.Results {
		results[result.SourcedID] = result
	}
	bsc, bi := results["urn:epass:learningAchievement:bsc"], results["urn:epass:learningAchievement:bi101"]
	if assert.NotNil(t, bsc, "program result") {
		assert.Equal(t, "urn:epass:qualification:bsc-bio", bsc.ProgramID)
		assert.InDelta(t, 180, bsc.Credits, 0)
		assert.Equal(t, "2026-01-31", bsc.ResultDate)
	}
	if assert.NotNil(t, bi, "course result") {
		assert.Equal(t, course.SourcedID, bi.CourseID)
		assert.Equal(t, "A", bi.Grade)
		assert.InDelta(t, 7.5, bi.Credits, 0)
		assert.Equal(t, offering.SourcedID, bi.OfferingID)
		assert.Equal(t, "2026-01-20", bi.ResultDate)
	}

	assert.Len(t, parsed.Warnings, 1, "want one for the LearningEntitlement claim")
}

// TestRenderGolden pins the full Edu-API and OEAPI renderings of the fixture.
// Run `go test ./internal/convert -update` after an intended change.
func TestRenderGolden(t *testing.T) {
	client := newClient(t)
	parsed, err := client.Parse(FormatELM, readFixture(t, "edc_bsc.json"), "data")
	require.NoError(t, err)
	for _, format := range client.OutputFormats() {
		t.Run(format, func(t *testing.T) {
			out, err := client.Render(format, parsed.Bundle)
			require.NoError(t, err)
			got, err := json.MarshalIndent(out, "", "  ")
			require.NoError(t, err)
			got = append(got, '\n')
			golden := filepath.Join("testdata", "edc_bsc."+format+".golden.json")
			if *update {
				require.NoError(t, os.WriteFile(golden, got, 0o600))
			}
			want, err := os.ReadFile(golden) // #nosec G304 -- fixed golden file
			require.NoError(t, err, "run with -update to create")
			require.Equal(t, string(want), string(got),
				"%s rendering differs from %s (run with -update if intended)", format, golden)
		})
	}
}

// TestParseELMIsDeterministic checks node ids are stable across runs for
// nodes without an id, so a re-sent document upserts the same records.
func TestParseELMIsDeterministic(t *testing.T) {
	doc := []byte(`{"credentialSubject": {"givenName": "A", "familyName": "B",
		"hasClaim": {"type": "LearningAchievement", "title": "Course X"}}}`)
	client := newClient(t)
	first, err := client.Parse(FormatELM, doc, "data")
	require.NoError(t, err)
	second, err := client.Parse(FormatELM, doc, "data")
	require.NoError(t, err)
	require.NotEmpty(t, first.Bundle.Persons)
	require.NotEmpty(t, second.Bundle.Persons)
	require.NotEmpty(t, first.Bundle.Courses)
	require.NotEmpty(t, second.Bundle.Courses)
	require.NotEmpty(t, first.Bundle.Results)
	assert.Equal(t, first.Bundle.Persons[0].SourcedID, second.Bundle.Persons[0].SourcedID, "generated sourcedIds differ between runs")
	assert.Equal(t, first.Bundle.Courses[0].SourcedID, second.Bundle.Courses[0].SourcedID, "generated sourcedIds differ between runs")
	assert.Equal(t, first.Bundle.Courses[0].SourcedID, first.Bundle.Results[0].CourseID, "result must reference the generated course")
}

func TestParseELMErrors(t *testing.T) {
	cases := []struct {
		name, doc, field string
	}{
		{"not a credential", `{"type": "Person"}`, "data.credentialSubject"},
		{"subject without name", `{"credentialSubject": {"id": "urn:p"}}`, "data.credentialSubject"},
		{"achievement without title", `{"credentialSubject": {"givenName": "A", "familyName": "B",
			"hasClaim": [{"type": "LearningAchievement"}]}}`, "data.credentialSubject.hasClaim[0]"},
	}
	client := newClient(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.Parse(FormatELM, []byte(tc.doc), "data")
			var ve *domain.ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Equal(t, tc.field, ve.Field)
		})
	}
	_, err := client.Parse(FormatELM, []byte(`{`), "data")
	assert.Error(t, err, "malformed JSON must fail")
	_, err = client.Parse("xml", []byte(`{}`), "data")
	assert.Error(t, err, "unknown format must fail")
}

// TestParseELMGrade checks a grade given as a Note (inline or referenced by
// id elsewhere in the document) yields its noteLiteral, and a plain grade
// value is used as-is.
func TestParseELMGrade(t *testing.T) {
	cases := map[string]string{
		"plain":      `"grade": "B"`,
		"inline":     `"grade": {"type": "Note", "noteLiteral": "B"}`,
		"referenced": `"grade": "urn:note:1"`,
	}
	for name, grade := range cases {
		t.Run(name, func(t *testing.T) {
			doc := `{"credentialSubject": {"givenName": "A", "familyName": "B",
				"hasClaim": {"type": "LearningAchievement", "title": "Course X",
					"wasDerivedFrom": {"type": "LearningAssessment", ` + grade + `}}},
				"notes": [{"id": "urn:note:1", "type": "Note", "noteLiteral": "B"}]}`
			parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
			require.NoError(t, err)
			require.NotEmpty(t, parsed.Bundle.Results)
			assert.Equal(t, "B", parsed.Bundle.Results[0].Grade)
		})
	}
}

// TestParseELMRejectsNonFiniteCredits checks credit points such as "NaN" or
// "+Inf" (which strconv.ParseFloat accepts) cannot reach the bundle: they
// are skipped as unusable, so the result carries no credits.
func TestParseELMRejectsNonFiniteCredits(t *testing.T) {
	for _, point := range []string{"NaN", "+Inf", "-Inf", "Infinity"} {
		t.Run(point, func(t *testing.T) {
			doc := `{"credentialSubject": {"givenName": "A", "familyName": "B",
				"hasClaim": {"type": "LearningAchievement", "title": "Course X",
					"creditReceived": {"point": "` + point + `"}}}}`
			parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
			require.NoError(t, err)
			require.NotEmpty(t, parsed.Bundle.Results)
			assert.Zero(t, parsed.Bundle.Results[0].Credits)
			_, err = json.Marshal(parsed.Bundle)
			assert.NoError(t, err, "bundle must stay JSON-encodable")
		})
	}
}

// TestParseELMOnlyMapsECTSCredits checks credits in a framework other than
// ECTS (or with none) are never published as ECTS: they are skipped with a
// warning, and an ECTS value on the specification is used instead.
func TestParseELMOnlyMapsECTSCredits(t *testing.T) {
	const local = `{"point": "9", "framework": {"id": "urn:local:hp", "prefLabel": "Local points"}}`
	const ects = `{"point": "7,5", "framework": {"id": "` + ectsConcept + `"}}`
	cases := []struct {
		name, props string
		want        float64
		warnings    int
	}{
		{"local only", `"creditReceived": [` + local + `]`, 0, 1},
		{"no framework", `"creditReceived": {"point": "9"}`, 0, 1},
		{"local received, ECTS specified", `"creditReceived": [` + local + `],
			"specifiedBy": {"id": "urn:spec:x", "title": "Course X", "creditPoint": ` + ects + `}`, 7.5, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := `{"credentialSubject": {"givenName": "A", "familyName": "B",
				"hasClaim": {"type": "LearningAchievement", "title": "Course X", ` + tc.props + `}}}`
			client := newClient(t)
			parsed, err := client.Parse(FormatELM, []byte(doc), "data")
			require.NoError(t, err)
			require.NotEmpty(t, parsed.Bundle.Results)
			assert.InDelta(t, tc.want, parsed.Bundle.Results[0].Credits, 0, "credits")
			assert.Len(t, parsed.Warnings, tc.warnings)
			out, err := client.Render("oeapi", parsed.Bundle)
			require.NoError(t, err)
			raw, err := json.Marshal(out)
			require.NoError(t, err)
			if tc.want == 0 {
				assert.NotContains(t, string(raw), `"ects"`, "non-ECTS credits published as ects")
			}
		})
	}
}

// TestParseELMTypedReference checks an organisation given as a typed
// reference ({"id", "type"}) is mapped from its definition elsewhere in the
// document, keeping its parent link, instead of being skipped for lacking a
// name.
func TestParseELMTypedReference(t *testing.T) {
	doc := `{"issuer": {"id": "urn:org:ifm", "type": "Organisation"},
		"credentialSubject": {"givenName": "A", "familyName": "B",
			"hasClaim": {"type": "LearningAchievement", "title": "Course X",
				"specifiedBy": {"id": "urn:spec:x", "type": "LearningAchievementSpecification"}}},
		"orgs": [{"id": "urn:org:ifm", "type": "Organisation", "legalName": "IFM", "parentOrganisation": {"id": "urn:org:liu", "type": "Organisation"}},
			{"id": "urn:org:liu", "type": "Organisation", "legalName": "LiU"}],
		"specs": [{"id": "urn:spec:x", "type": "LearningAchievementSpecification", "title": "Spec title",
			"identifier": {"notation": "X1"}}]}`
	parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
	require.NoError(t, err)
	bundle := parsed.Bundle
	orgs := map[string]*domain.Organization{}
	for _, org := range bundle.Organizations {
		orgs[org.SourcedID] = org
	}
	require.Len(t, orgs, 2, "warnings = %v", parsed.Warnings)
	require.NotNil(t, orgs["urn:org:ifm"], "warnings = %v", parsed.Warnings)
	assert.Equal(t, "IFM", orgs["urn:org:ifm"].Name)
	assert.Equal(t, "urn:org:liu", orgs["urn:org:ifm"].ParentID)
	require.Len(t, bundle.Courses, 1)
	assert.Equal(t, "Spec title", bundle.Courses[0].Title)
	assert.Equal(t, "X1", bundle.Courses[0].Code)
	assert.Equal(t, "urn:org:ifm", bundle.Courses[0].OrgID)
	assert.Empty(t, parsed.Warnings)
}

// TestParseELMOrganisationCycle checks a parent cycle terminates.
func TestParseELMOrganisationCycle(t *testing.T) {
	doc := `{"issuer": {"id": "urn:a", "legalName": "A", "parentOrganisation": {"id": "urn:b", "legalName": "B", "parentOrganisation": "urn:a"}},
		"credentialSubject": {"givenName": "A", "familyName": "B"}}`
	parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
	require.NoError(t, err)
	assert.Len(t, parsed.Bundle.Organizations, 2)
	assert.Len(t, parsed.Warnings, 1)
}
