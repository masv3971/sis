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
