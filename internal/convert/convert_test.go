package convert

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/pkg/logger"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden.json")

func newClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(t.Context(), nil, logger.NewWithWriter("test", false, "error", io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name)) // #nosec G304 -- fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestParseELMMapping checks the mapping rules on a credential with a
// qualification whose part is a graded course with an activity.
func TestParseELMMapping(t *testing.T) {
	parsed, err := newClient(t).Parse(FormatELM, readFixture(t, "edc_bsc.json"), "data")
	if err != nil {
		t.Fatal(err)
	}
	b := parsed.Bundle

	if len(b.Persons) != 1 {
		t.Fatalf("persons = %d", len(b.Persons))
	}
	p := b.Persons[0]
	if p.SourcedID != "urn:epass:person:liu-student-4711" || p.GivenName != "Ada" || p.FamilyName != "Lovelace" ||
		p.DateOfBirth != "2001-12-10" || len(p.Emails) != 1 || p.Emails[0].Value != "adalo123@student.liu.se" ||
		len(p.Identifiers) != 1 || p.Identifiers[0] != (domain.Identifier{Type: "LiU-ID", Value: "adalo123"}) {
		t.Errorf("person = %+v", p)
	}

	orgs := map[string]*domain.Organization{}
	for _, o := range b.Organizations {
		orgs[o.SourcedID] = o
	}
	if len(orgs) != 2 || orgs["urn:epass:org:liu-ifm"].ParentID != "urn:epass:org:liu" || orgs["urn:epass:org:liu"].Name != "Linköping University" {
		t.Errorf("organizations = %+v", b.Organizations)
	}

	if len(b.Programs) != 1 || b.Programs[0].Code != "NBIO" || b.Programs[0].Level != "6" || b.Programs[0].OrgID != "urn:epass:org:liu" {
		t.Errorf("programs = %+v", b.Programs)
	}
	if len(b.Courses) != 1 || b.Courses[0].SourcedID != "urn:epass:learningAchievementSpec:bi101" || b.Courses[0].Code != "BI101" ||
		b.Courses[0].Title != "Biology 101" || b.Courses[0].OrgID != "urn:epass:org:liu-ifm" {
		t.Errorf("courses = %+v", b.Courses)
	}
	if len(b.AcademicSessions) != 1 || b.AcademicSessions[0].Title != "Autumn 2025" || b.AcademicSessions[0].StartDate != "2025-09-01" {
		t.Errorf("sessions = %+v", b.AcademicSessions)
	}
	if len(b.Offerings) != 1 || b.Offerings[0].CourseID != b.Courses[0].SourcedID || b.Offerings[0].AcademicSessionID != "urn:epass:period:ht25" {
		t.Errorf("offerings = %+v", b.Offerings)
	}
	if len(b.Enrollments) != 1 || b.Enrollments[0].PersonID != p.SourcedID || b.Enrollments[0].OfferingID != b.Offerings[0].SourcedID {
		t.Errorf("enrollments = %+v", b.Enrollments)
	}

	results := map[string]*domain.Result{}
	for _, r := range b.Results {
		results[r.SourcedID] = r
	}
	bsc, bi := results["urn:epass:learningAchievement:bsc"], results["urn:epass:learningAchievement:bi101"]
	if bsc == nil || bsc.ProgramID != "urn:epass:qualification:bsc-bio" || bsc.Credits != 180 || bsc.ResultDate != "2026-01-31" {
		t.Errorf("program result = %+v", bsc)
	}
	if bi == nil || bi.CourseID != b.Courses[0].SourcedID || bi.Grade != "A" || bi.Credits != 7.5 ||
		bi.OfferingID != b.Offerings[0].SourcedID || bi.ResultDate != "2026-01-20" {
		t.Errorf("course result = %+v", bi)
	}

	if len(parsed.Warnings) != 1 {
		t.Errorf("warnings = %v; want one for the LearningEntitlement claim", parsed.Warnings)
	}
}

// TestRenderGolden pins the full Edu-API and OEAPI renderings of the fixture.
// Run `go test ./internal/convert -update` after an intended change.
func TestRenderGolden(t *testing.T) {
	c := newClient(t)
	parsed, err := c.Parse(FormatELM, readFixture(t, "edc_bsc.json"), "data")
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range c.OutputFormats() {
		t.Run(format, func(t *testing.T) {
			out, err := c.Render(format, parsed.Bundle)
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			golden := filepath.Join("testdata", "edc_bsc."+format+".golden.json")
			if *update {
				if err := os.WriteFile(golden, got, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden) // #nosec G304 -- fixed golden file
			if err != nil {
				t.Fatalf("%v (run with -update to create)", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s rendering differs from %s (run with -update if intended):\n%s", format, golden, got)
			}
		})
	}
}

// TestParseELMIsDeterministic checks node ids are stable across runs for
// nodes without an id, so a re-sent document upserts the same records.
func TestParseELMIsDeterministic(t *testing.T) {
	doc := []byte(`{"credentialSubject": {"givenName": "A", "familyName": "B",
		"hasClaim": {"type": "LearningAchievement", "title": "Course X"}}}`)
	c := newClient(t)
	a, err := c.Parse(FormatELM, doc, "data")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Parse(FormatELM, doc, "data")
	if err != nil {
		t.Fatal(err)
	}
	if a.Bundle.Persons[0].SourcedID != b.Bundle.Persons[0].SourcedID || a.Bundle.Courses[0].SourcedID != b.Bundle.Courses[0].SourcedID {
		t.Fatal("generated sourcedIds differ between runs")
	}
	if a.Bundle.Results[0].CourseID != a.Bundle.Courses[0].SourcedID {
		t.Fatal("result must reference the generated course")
	}
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
	c := newClient(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Parse(FormatELM, []byte(tc.doc), "data")
			ve, ok := errors.AsType[*domain.ValidationError](err)
			if !ok || ve.Field != tc.field {
				t.Fatalf("want ValidationError at %s, got %v", tc.field, err)
			}
		})
	}
	if _, err := c.Parse(FormatELM, []byte(`{`), "data"); err == nil {
		t.Fatal("malformed JSON must fail")
	}
	if _, err := c.Parse("xml", []byte(`{}`), "data"); err == nil {
		t.Fatal("unknown format must fail")
	}
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
			if err != nil {
				t.Fatal(err)
			}
			if g := parsed.Bundle.Results[0].Grade; g != "B" {
				t.Fatalf("grade = %q, want B", g)
			}
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
			if err != nil {
				t.Fatal(err)
			}
			if c := parsed.Bundle.Results[0].Credits; c != 0 {
				t.Fatalf("credits = %v, want 0", c)
			}
			if _, err := json.Marshal(parsed.Bundle); err != nil {
				t.Fatalf("bundle must stay JSON-encodable: %v", err)
			}
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
			c := newClient(t)
			parsed, err := c.Parse(FormatELM, []byte(doc), "data")
			if err != nil {
				t.Fatal(err)
			}
			if got := parsed.Bundle.Results[0].Credits; got != tc.want {
				t.Fatalf("credits = %v, want %v", got, tc.want)
			}
			if len(parsed.Warnings) != tc.warnings {
				t.Fatalf("warnings = %v, want %d", parsed.Warnings, tc.warnings)
			}
			out, err := c.Render("oeapi", parsed.Bundle)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == 0 && bytes.Contains(raw, []byte(`"ects"`)) {
				t.Fatalf("non-ECTS credits published as ects: %s", raw)
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
	if err != nil {
		t.Fatal(err)
	}
	b := parsed.Bundle
	orgs := map[string]*domain.Organization{}
	for _, o := range b.Organizations {
		orgs[o.SourcedID] = o
	}
	if len(orgs) != 2 || orgs["urn:org:ifm"] == nil || orgs["urn:org:ifm"].Name != "IFM" || orgs["urn:org:ifm"].ParentID != "urn:org:liu" {
		t.Fatalf("organizations = %+v, warnings = %v", b.Organizations, parsed.Warnings)
	}
	if len(b.Courses) != 1 || b.Courses[0].Title != "Spec title" || b.Courses[0].Code != "X1" || b.Courses[0].OrgID != "urn:org:ifm" {
		t.Fatalf("courses = %+v", b.Courses)
	}
	if len(parsed.Warnings) != 0 {
		t.Fatalf("warnings = %v", parsed.Warnings)
	}
}

// TestParseELMOrganisationCycle checks a parent cycle terminates.
func TestParseELMOrganisationCycle(t *testing.T) {
	doc := `{"issuer": {"id": "urn:a", "legalName": "A", "parentOrganisation": {"id": "urn:b", "legalName": "B", "parentOrganisation": "urn:a"}},
		"credentialSubject": {"givenName": "A", "familyName": "B"}}`
	parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Bundle.Organizations) != 2 || len(parsed.Warnings) != 1 {
		t.Fatalf("orgs=%+v warnings=%v", parsed.Bundle.Organizations, parsed.Warnings)
	}
}
