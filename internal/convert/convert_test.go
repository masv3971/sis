package convert

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/pkg/elm"
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

	require.Len(t, bundle.Entitlements, 1)
	entitlement := bundle.Entitlements[0]
	assert.Equal(t, "urn:epass:learningEntitlement:1", entitlement.SourcedID)
	assert.Equal(t, "Access to master studies", entitlement.Title)
	assert.Equal(t, person.SourcedID, entitlement.PersonID)
	assert.Equal(t, "urn:epass:org:liu-ifm", entitlement.OrgID, "issuer is the fallback awarding body")
	assert.Empty(t, entitlement.ResultID)

	assert.Empty(t, parsed.Warnings)
}

// TestParseELMv3Names checks the ELM v3 property names provenBy and
// influencedBy are read like wasDerivedFrom and wasInfluencedBy, and
// entitlesTo yields an entitlement linked to the result.
func TestParseELMv3Names(t *testing.T) {
	doc := `{"credentialSubject": {"id": "urn:p", "givenName": "A", "familyName": "B",
		"hasClaim": {"id": "urn:ach", "type": "LearningAchievement", "title": "Course X",
			"provenBy": {"type": "LearningAssessment", "grade": {"type": "Note", "noteLiteral": "Pass"}},
			"influencedBy": {"id": "urn:act", "type": "LearningActivity", "title": "Course X, spring",
				"temporal": {"startDate": "2026-01-10T00:00:00Z", "endDate": "2026-03-10T00:00:00Z"}},
			"entitlesTo": {"id": "urn:ent", "type": "LearningEntitlement", "title": "Access to Y",
				"expiryDate": "2030-01-01T00:00:00Z"}}}}`
	parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
	require.NoError(t, err)
	bundle := parsed.Bundle
	require.Len(t, bundle.Results, 1)
	assert.Equal(t, "Pass", bundle.Results[0].Grade)
	assert.Equal(t, "urn:act", bundle.Results[0].OfferingID)
	require.Len(t, bundle.Offerings, 1)
	require.Len(t, bundle.Enrollments, 1)
	require.Len(t, bundle.AcademicSessions, 1)
	require.Len(t, bundle.Entitlements, 1)
	assert.Equal(t, "urn:ach", bundle.Entitlements[0].ResultID)
	assert.Equal(t, "2030-01-01", bundle.Entitlements[0].ExpiryDate)
	assert.Empty(t, parsed.Warnings)
}

// TestParseELMEntitlementClaimOrder checks an entitlement is linked to its
// result whether its top-level claim comes before or after the achievement.
func TestParseELMEntitlementClaimOrder(t *testing.T) {
	entitlement := `{"id": "urn:ent", "type": "LearningEntitlement", "title": "Access to Y"}`
	achievement := `{"id": "urn:ach", "type": "LearningAchievement", "title": "Course X",
		"entitlesTo": {"id": "urn:ent", "type": "LearningEntitlement", "title": "Access to Y"}}`
	for name, claims := range map[string]string{
		"entitlement first": entitlement + "," + achievement,
		"achievement first": achievement + "," + entitlement,
	} {
		t.Run(name, func(t *testing.T) {
			doc := `{"credentialSubject": {"id": "urn:p", "givenName": "A", "familyName": "B", "hasClaim": [` + claims + `]}}`
			parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
			require.NoError(t, err)
			require.Len(t, parsed.Bundle.Entitlements, 1)
			assert.Equal(t, "urn:ach", parsed.Bundle.Entitlements[0].ResultID)
		})
	}
}

// TestParseELMEntitlementIssuedDate checks the awarding date is the issue
// date, with issuedDate as the fallback.
func TestParseELMEntitlementIssuedDate(t *testing.T) {
	for name, tc := range map[string]struct{ props, want string }{
		"awardingDate":        {`"awardedBy": {"awardingDate": "2026-02-03T00:00:00Z"}`, "2026-02-03"},
		"issuedDate fallback": {`"issuedDate": "2026-04-05T00:00:00Z"`, "2026-04-05"},
		"awardingDate wins":   {`"issuedDate": "2026-04-05T00:00:00Z", "awardedBy": {"awardingDate": "2026-02-03T00:00:00Z"}`, "2026-02-03"},
	} {
		t.Run(name, func(t *testing.T) {
			doc := `{"credentialSubject": {"id": "urn:p", "givenName": "A", "familyName": "B",
				"hasClaim": {"id": "urn:ent", "type": "LearningEntitlement", "title": "Access to Y", ` + tc.props + `}}}`
			parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
			require.NoError(t, err)
			require.Len(t, parsed.Bundle.Entitlements, 1)
			assert.Equal(t, tc.want, parsed.Bundle.Entitlements[0].IssuedDate)
		})
	}
}

// TestParseELMParticipation checks a LearningActivity claim (a certificate
// of participation) yields the course, offering and enrollment, but no
// result.
func TestParseELMParticipation(t *testing.T) {
	doc := `{"issuer": {"id": "urn:org:i", "legalName": "Issuer"},
		"credentialSubject": {"id": "urn:p", "givenName": "A", "familyName": "B",
		"hasClaim": {"id": "urn:act", "type": "LearningActivity", "title": "Webinar",
			"awardedBy": {"awardingBody": {"id": "urn:org:a", "legalName": "Awarder"}},
			"specifiedBy": {"id": "urn:spec", "type": "LearningActivitySpecification", "title": "EDC Webinar"},
			"temporal": {"id": "urn:period", "startDate": "2023-06-28T12:00:00Z", "endDate": "2023-06-28T14:00:00Z"}}}}`
	parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
	require.NoError(t, err)
	bundle := parsed.Bundle
	assert.Empty(t, bundle.Results)
	require.Len(t, bundle.Courses, 1)
	assert.Equal(t, "urn:spec", bundle.Courses[0].SourcedID)
	assert.Equal(t, "EDC Webinar", bundle.Courses[0].Title)
	assert.Equal(t, "urn:org:a", bundle.Courses[0].OrgID)
	require.Len(t, bundle.Offerings, 1)
	assert.Equal(t, "urn:act", bundle.Offerings[0].SourcedID)
	assert.Equal(t, "Webinar", bundle.Offerings[0].Title)
	assert.Equal(t, "urn:period", bundle.Offerings[0].AcademicSessionID)
	require.Len(t, bundle.Enrollments, 1)
	assert.Equal(t, "urn:p", bundle.Enrollments[0].PersonID)
	assert.Empty(t, parsed.Warnings)
}

// requireResultsMatchOfferings fails when a result is linked to an offering
// of a different course.
func requireResultsMatchOfferings(t *testing.T, bundle *domain.Bundle) {
	t.Helper()
	courseOf := map[string]string{}
	for _, offering := range bundle.Offerings {
		courseOf[offering.SourcedID] = offering.CourseID
	}
	for _, result := range bundle.Results {
		if result.OfferingID != "" {
			assert.Equal(t, courseOf[result.OfferingID], result.CourseID, "result %s and its offering are of different courses", result.SourcedID)
		}
	}
}

// TestParseELMSharedActivityCourse checks an activity shared by a
// participation claim and an achievement of another course is an offering
// of its own specification's course in either claim order, and the result
// is not linked to an offering of a different course.
func TestParseELMSharedActivityCourse(t *testing.T) {
	activity := `{"id": "urn:act", "type": "LearningActivity", "title": "Webinar",
		"specifiedBy": {"id": "urn:specX", "type": "LearningActivitySpecification", "title": "X"}}`
	achievement := func(spec string) string {
		return `{"id": "urn:ach", "type": "LearningAchievement", "title": "Y",
			"specifiedBy": {"id": "` + spec + `", "type": "LearningAchievementSpecification", "title": "Y"},
			"influencedBy": {"id": "urn:act", "type": "LearningActivity"}}`
	}
	for name, tc := range map[string]struct {
		claims     string
		linked     bool
		wantWarned int
	}{
		"activity first":                        {activity + "," + achievement("urn:specY"), false, 1},
		"achievement first":                     {achievement("urn:specY") + "," + activity, false, 1},
		"same specification, activity first":    {activity + "," + achievement("urn:specX"), true, 0},
		"same specification, achievement first": {achievement("urn:specX") + "," + activity, true, 0},
	} {
		t.Run(name, func(t *testing.T) {
			doc := `{"credentialSubject": {"id": "urn:p", "givenName": "A", "familyName": "B", "hasClaim": [` + tc.claims + `]}}`
			parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
			require.NoError(t, err)
			bundle := parsed.Bundle
			require.Len(t, bundle.Offerings, 1)
			assert.Equal(t, "urn:specX", bundle.Offerings[0].CourseID, "the activity's own specification")
			require.Len(t, bundle.Results, 1)
			if tc.linked {
				assert.Equal(t, "urn:act", bundle.Results[0].OfferingID)
			} else {
				assert.Empty(t, bundle.Results[0].OfferingID)
			}
			assert.Len(t, parsed.Warnings, tc.wantWarned)
			requireResultsMatchOfferings(t, bundle)
		})
	}
}

// TestParseELMSharedActivityParentChild checks a child achievement sharing
// the activity of its qualification, which names no course, decides the
// course of the activity's offering and is linked to it.
func TestParseELMSharedActivityParentChild(t *testing.T) {
	doc := `{"credentialSubject": {"id": "urn:p", "givenName": "A", "familyName": "B",
		"hasClaim": {"id": "urn:parent", "type": "LearningAchievement", "title": "BSc",
			"specifiedBy": {"id": "urn:qual", "type": "Qualification", "title": "BSc"},
			"influencedBy": {"id": "urn:act", "type": "LearningActivity", "title": "Study"},
			"hasPart": {"id": "urn:child", "type": "LearningAchievement", "title": "Course",
				"specifiedBy": {"id": "urn:course", "type": "LearningAchievementSpecification", "title": "Course"},
				"influencedBy": {"id": "urn:act", "type": "LearningActivity"}}}}}`
	parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
	require.NoError(t, err)
	bundle := parsed.Bundle
	require.Len(t, bundle.Offerings, 1)
	assert.Equal(t, "urn:course", bundle.Offerings[0].CourseID)
	require.Len(t, bundle.Results, 2)
	for _, result := range bundle.Results {
		if result.SourcedID == "urn:child" {
			assert.Equal(t, "urn:act", result.OfferingID)
		}
	}
	assert.Empty(t, parsed.Warnings)
	requireResultsMatchOfferings(t, bundle)
}

// TestParseELMSpeclessActivityOrder checks an activity without a
// specification, shared by a participation claim and achievements, is an
// offering of the first achievement's course in any claim order, and only
// that achievement's result is linked to it.
func TestParseELMSpeclessActivityOrder(t *testing.T) {
	activity := `{"id": "urn:act", "type": "LearningActivity", "title": "Webinar"}`
	achievement := func(id, spec string) string {
		return `{"id": "` + id + `", "type": "LearningAchievement", "title": "` + id + `",
			"specifiedBy": {"id": "` + spec + `", "type": "LearningAchievementSpecification", "title": "` + id + `"},
			"influencedBy": {"id": "urn:act", "type": "LearningActivity"}}`
	}
	one, two := achievement("urn:ach1", "urn:spec1"), achievement("urn:ach2", "urn:spec2")
	for name, claims := range map[string]string{
		"activity first":   activity + "," + one + "," + two,
		"activity between": one + "," + activity + "," + two,
		"activity last":    one + "," + two + "," + activity,
	} {
		t.Run(name, func(t *testing.T) {
			doc := `{"credentialSubject": {"id": "urn:p", "givenName": "A", "familyName": "B", "hasClaim": [` + claims + `]}}`
			parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
			require.NoError(t, err)
			bundle := parsed.Bundle
			require.Len(t, bundle.Offerings, 1)
			assert.Equal(t, "urn:spec1", bundle.Offerings[0].CourseID, "the first achievement's course")
			for _, course := range bundle.Courses {
				assert.NotContains(t, course.SourcedID, "#specification", "no synthetic course for the activity")
			}
			linked := map[string]string{}
			for _, result := range bundle.Results {
				linked[result.SourcedID] = result.OfferingID
			}
			assert.Equal(t, map[string]string{"urn:ach1": "urn:act", "urn:ach2": ""}, linked)
			assert.Len(t, parsed.Warnings, 1, "the second achievement is not linked")
			requireResultsMatchOfferings(t, bundle)
		})
	}
}

// cyclicELMClaims are claims whose hasPart refers back to an ancestor or to
// themselves, each with the achievements expected from Parse.
var cyclicELMClaims = map[string]struct {
	claims  string
	results []string
}{
	"self reference": {
		`{"id": "urn:a", "type": "LearningAchievement", "title": "A", "hasPart": {"id": "urn:a"}}`,
		[]string{"urn:a"},
	},
	"two-node cycle": {
		`{"id": "urn:a", "type": "LearningAchievement", "title": "A", "hasPart": {"id": "urn:b"}},
		 {"id": "urn:b", "type": "LearningAchievement", "title": "B", "hasPart": {"id": "urn:a"}}`,
		[]string{"urn:a", "urn:b"},
	},
	"cycle through a third node": {
		`{"id": "urn:a", "type": "LearningAchievement", "title": "A", "hasPart": {"id": "urn:b", "type": "LearningAchievement", "title": "B",
			"hasPart": {"id": "urn:c", "type": "LearningAchievement", "title": "C", "hasPart": {"id": "urn:a"}}}}`,
		[]string{"urn:a", "urn:b", "urn:c"},
	},
}

// withinTime fails the test when fn does not return in time, so a runaway
// recursion is reported instead of hanging the run.
func withinTime(t *testing.T, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("did not return: claims are walked without a cycle guard")
	}
}

// TestParseELMCyclicParts checks claims that contain themselves through
// hasPart are mapped once each and neither recurse forever in the planning
// pass nor in achievement().
func TestParseELMCyclicParts(t *testing.T) {
	for name, tc := range cyclicELMClaims {
		t.Run(name, func(t *testing.T) {
			doc := `{"credentialSubject": {"id": "urn:p", "givenName": "A", "familyName": "B", "hasClaim": [` + tc.claims + `]}}`
			var parsed *Parsed
			var err error
			withinTime(t, func() { parsed, err = newClient(t).Parse(FormatELM, []byte(doc), "data") })
			require.NoError(t, err)
			var got []string
			for _, result := range parsed.Bundle.Results {
				got = append(got, result.SourcedID)
			}
			assert.ElementsMatch(t, tc.results, got, "each achievement once")
			assert.Empty(t, parsed.Warnings)
		})
	}
}

// TestPlanActivitiesCyclicParts checks the planning pass alone ends on the
// same documents and still plans the activity of a claim inside the cycle.
func TestPlanActivitiesCyclicParts(t *testing.T) {
	for name, tc := range cyclicELMClaims {
		t.Run(name, func(t *testing.T) {
			// urn:act has no specification; urn:a, the first claim, names it.
			claims := strings.Replace(tc.claims, `"title": "A"`, `"title": "A", "specifiedBy": {"id": "urn:specA"}, "influencedBy": {"id": "urn:act", "type": "LearningActivity"}`, 1)
			raw := `{"credentialSubject": {"id": "urn:p", "hasClaim": [` + claims + `]}}`
			doc, err := elm.Parse([]byte(raw), "data")
			require.NoError(t, err)
			b := &elmBuilder{base: "b", rootPath: "data", planned: map[string]string{}, planSeen: map[string]bool{}}
			subject := doc.Root.Nodes("credentialSubject")[0]
			withinTime(t, func() { b.planActivities(subject.Nodes("hasClaim")) })
			assert.Equal(t, map[string]string{"urn:act": "urn:specA"}, b.planned)
		})
	}
}

// TestParseELMSpeclessActivityAlone checks an activity without a
// specification that no achievement names keeps its own course.
func TestParseELMSpeclessActivityAlone(t *testing.T) {
	doc := `{"credentialSubject": {"id": "urn:p", "givenName": "A", "familyName": "B",
		"hasClaim": {"id": "urn:act", "type": "LearningActivity", "title": "Webinar"}}}`
	parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
	require.NoError(t, err)
	require.Len(t, parsed.Bundle.Offerings, 1)
	assert.Equal(t, "urn:act#specification", parsed.Bundle.Offerings[0].CourseID)
}

// TestParseELMActivityAwardingOrg checks the offering and its course are of
// the activity's own awarding body, in either claim order.
func TestParseELMActivityAwardingOrg(t *testing.T) {
	activity := `{"id": "urn:act", "type": "LearningActivity", "title": "Webinar",
		"awardedBy": {"awardingBody": {"id": "urn:org:b", "legalName": "B"}},
		"specifiedBy": {"id": "urn:spec", "type": "LearningActivitySpecification", "title": "X"}}`
	achievement := `{"id": "urn:ach", "type": "LearningAchievement", "title": "X",
		"awardedBy": {"awardingBody": {"id": "urn:org:a", "legalName": "A"}},
		"specifiedBy": {"id": "urn:achSpec", "type": "LearningAchievementSpecification", "title": "X"},
		"influencedBy": {"id": "urn:act", "type": "LearningActivity"}}`
	for name, claims := range map[string]string{
		"activity first":    activity + "," + achievement,
		"achievement first": achievement + "," + activity,
	} {
		t.Run(name, func(t *testing.T) {
			doc := `{"credentialSubject": {"id": "urn:p", "givenName": "A", "familyName": "B", "hasClaim": [` + claims + `]}}`
			parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
			require.NoError(t, err)
			require.Len(t, parsed.Bundle.Offerings, 1)
			assert.Equal(t, "urn:org:b", parsed.Bundle.Offerings[0].OrgID)
			orgs := map[string]string{}
			for _, course := range parsed.Bundle.Courses {
				orgs[course.SourcedID] = course.OrgID
			}
			assert.Equal(t, map[string]string{"urn:spec": "urn:org:b", "urn:achSpec": "urn:org:a"}, orgs)
		})
	}
}

// TestParseELMCreditOnly checks an achievement naming nothing but credits
// is kept as a result with a generated title, and a warning.
func TestParseELMCreditOnly(t *testing.T) {
	doc := `{"credentialSubject": {"givenName": "A", "familyName": "B",
		"hasClaim": {"type": "LearningAchievement",
			"creditReceived": {"point": "5", "framework": {"id": "` + ectsConcept + `"}}}}}`
	parsed, err := newClient(t).Parse(FormatELM, []byte(doc), "data")
	require.NoError(t, err)
	require.Len(t, parsed.Bundle.Results, 1)
	assert.InDelta(t, 5, parsed.Bundle.Results[0].Credits, 0)
	require.Len(t, parsed.Bundle.Courses, 1)
	assert.Equal(t, untitledAchievement, parsed.Bundle.Courses[0].Title)
	assert.Len(t, parsed.Warnings, 1)
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
		{"entitlement without title", `{"credentialSubject": {"givenName": "A", "familyName": "B",
			"hasClaim": [{"type": "LearningEntitlement"}]}}`, "data.credentialSubject.hasClaim[0]"},
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
