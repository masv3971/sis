package convert

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/pkg/elm"
)

// parseELM maps an ELM v3 credential (a European Digital Credential in
// JSON-LD) to canonical records:
//
//	credentialSubject                        -> Person (role student)
//	issuer, awardedBy.awardingBody           -> Organization (parentOrganisation -> ParentID)
//	hasClaim LearningAchievement             -> Result for the subject, plus what it is for:
//	  specifiedBy Qualification              ->   Program
//	  specifiedBy (any other specification)  ->   Course
//	  provenBy LearningAssessment            ->   the Result's grade
//	  influencedBy LearningActivity          ->   Offering (+ AcademicSession from temporal) and Enrollment;
//	                                              for a Program result, of the activity's own Course
//	  entitlesTo LearningEntitlement         ->   Entitlement linked to the Result
//	  hasPart                                ->   nested achievements, mapped the same way
//	hasClaim LearningActivity                -> Course (from specifiedBy), Offering and Enrollment;
//	                                            no Result (a certificate of participation)
//	hasClaim LearningEntitlement             -> Entitlement for the subject
//
// The pre-v3 names wasDerivedFrom and wasInfluencedBy are read as well. An
// achievement without a title (e.g. a credit-only claim) takes its
// specification's title, else a generated one.
//
// sourcedIds are the ELM node ids, so re-sending a document converges on the
// same records. A node without an id gets a deterministic one derived from
// the credential id (or a hash of the document) and the node's path.
//
// Parts that cannot be mapped (other claim types, organisations without a
// name) are skipped with a warning; records
// that are mapped but invalid (e.g. a subject without a name) fail the whole
// conversion with a domain.ValidationError naming the document path.
//
// The credential may come plain or in an EDCI envelope (uploadable or
// sealed, see unwrapELM); paths in errors and warnings then name its place
// inside the envelope (e.g. data.payload.credentialSubject).
func parseELM(data []byte, path string) (*Parsed, error) {
	data, path, envelopeWarnings, err := unwrapELM(data, path)
	if err != nil {
		return nil, err
	}
	doc, err := elm.Parse(data, path)
	if err != nil {
		return nil, err
	}
	root := doc.Root
	subjects := root.Nodes("credentialSubject")
	if len(subjects) == 0 {
		return nil, &domain.ValidationError{Field: path + ".credentialSubject", Msg: "required: data must be an ELM credential"}
	}

	b := &elmBuilder{base: root.ID(), memo: map[string]string{}, inProgress: map[string]bool{}, planned: map[string]string{}, planSeen: map[string]bool{}, warnings: envelopeWarnings}
	if b.base == "" {
		sum := sha256.Sum256(data)
		b.base = "elm:sha256:" + hex.EncodeToString(sum[:16])
	}
	b.rootPath = path

	issuer := b.org(root.Node("issuer"))
	for _, subject := range subjects {
		b.planActivities(subject.Nodes("hasClaim"))
	}
	for _, subject := range subjects {
		person, err := b.person(subject)
		if err != nil {
			return nil, err
		}
		for _, claim := range subject.Nodes("hasClaim") {
			if err := b.claim(claim, person, issuer); err != nil {
				return nil, err
			}
		}
	}
	return &Parsed{Bundle: &b.bundle, Warnings: b.warnings}, nil
}

type elmBuilder struct {
	base     string // prefix for generated sourcedIds
	rootPath string
	bundle   domain.Bundle
	// memo maps type|sourcedId to the sourcedId to reference ("" when the
	// node was skipped), so a node reached twice is mapped once.
	memo       map[string]string
	inProgress map[string]bool // organisations being mapped (cycle guard)
	// planned maps the sourcedId of an activity without a specification to
	// the course of the first achievement it influenced (see planActivities).
	planned  map[string]string
	planSeen map[string]bool // achievements planActivities has visited (cycle guard)
	warnings []string
}

// sid returns the node's sourcedId: its id, or one derived from its path.
func (b *elmBuilder) sid(node *elm.Node) string {
	if id := node.ID(); id != "" {
		return id
	}
	return b.base + "#" + strings.TrimPrefix(strings.TrimPrefix(node.Path, b.rootPath), ".")
}

func (b *elmBuilder) warn(path, format string, args ...any) {
	b.warnings = append(b.warnings, path+": "+fmt.Sprintf(format, args...))
}

// seen reports whether typ/sid was already mapped and, if so, the sourcedId
// to reference.
func (b *elmBuilder) seen(typ, sid string) (string, bool) {
	ref, ok := b.memo[typ+"|"+sid]
	return ref, ok
}

func (b *elmBuilder) mark(typ, sid, ref string) { b.memo[typ+"|"+sid] = ref }

func base(sid string) domain.Base {
	return domain.Base{SourcedID: sid, Status: domain.StatusActive}
}

// check validates a mapped record, reporting a failure against the document
// path it came from.
func check(path, typ string, record interface{ Validate() error }) error {
	err := record.Validate()
	if invalid, ok := errors.AsType[*domain.ValidationError](err); ok {
		return &domain.ValidationError{Field: path, Msg: fmt.Sprintf("cannot map to %s: %s %s", typ, invalid.Field, invalid.Msg)}
	}
	return err
}

// org maps an Organisation node and its parent chain, returning its
// sourcedId, or "" when node is nil or cannot be mapped.
func (b *elmBuilder) org(node *elm.Node) string {
	if node == nil {
		return ""
	}
	sid := b.sid(node)
	if ref, ok := b.seen("organization", sid); ok {
		return ref
	}
	if b.inProgress[sid] {
		b.warn(node.Path, "organisation %s is its own ancestor; parent link dropped", sid)
		return ""
	}
	name := firstNonEmpty(node.Text("legalName"), node.Text("altLabel"), node.Text("prefLabel"), node.Text("name"))
	if name == "" {
		b.warn(node.Path, "organisation %s has no legalName (or is not described in the document); skipped", sid)
		b.mark("organization", sid, "")
		return ""
	}
	b.inProgress[sid] = true
	parent := node.Node("parentOrganisation")
	if parent == nil {
		parent = node.Node("parentOrganization")
	}
	parentSID := b.org(parent)
	delete(b.inProgress, sid)

	b.bundle.Organizations = append(b.bundle.Organizations, &domain.Organization{Base: base(sid), Name: name, ParentID: parentSID})
	b.mark("organization", sid, sid)
	return sid
}

// person maps a credential subject.
func (b *elmBuilder) person(subject *elm.Node) (string, error) {
	sid := b.sid(subject)
	if ref, ok := b.seen("person", sid); ok {
		return ref, nil
	}
	given, family := subject.Text("givenName"), subject.Text("familyName")
	if given == "" && family == "" {
		given, family = splitName(subject.Text("fullName"))
	}
	person := domain.Person{
		SourcedID: sid, Status: domain.StatusActive,
		GivenName: given, FamilyName: family,
		Roles:       []domain.Role{domain.RoleStudent},
		DateOfBirth: b.date(subject.Path+".dateOfBirth", subject.String("dateOfBirth")),
	}
	for _, prop := range []string{"identifier", "nationalID", "nationalId"} {
		for _, id := range subject.Nodes(prop) {
			if value := id.String("notation"); value != "" {
				person.Identifiers = append(person.Identifiers, domain.Identifier{
					Type: firstNonEmpty(id.Text("schemeName"), id.String("schemeId"), id.String("schemeAgency"), prop), Value: value,
				})
			}
		}
	}
	for _, contact := range subject.Nodes("contactPoint") {
		for _, email := range contact.Nodes("emailAddress") {
			if value := strings.TrimPrefix(firstNonEmpty(email.ID(), email.String("value")), "mailto:"); value != "" {
				person.Emails = append(person.Emails, domain.Contact{Type: "email", Value: value})
			}
		}
		for _, phone := range contact.Nodes("phone") {
			if value := firstNonEmpty(phone.String("phoneNumber"), strings.TrimPrefix(phone.ID(), "tel:")); value != "" {
				person.Phones = append(person.Phones, domain.Contact{Type: "phone", Value: value})
			}
		}
	}
	if err := check(subject.Path, "person", person); err != nil {
		return "", err
	}
	b.bundle.Persons = append(b.bundle.Persons, person)
	b.mark("person", sid, sid)
	return sid, nil
}

// claim maps one claim of the subject. An untyped claim is taken to be an
// achievement; claim types other than achievement, activity and entitlement
// are skipped with a warning.
func (b *elmBuilder) claim(claim *elm.Node, person, org string) error {
	switch {
	case claim.Is("LearningActivity"):
		return b.activity(claim, person, org)
	case claim.Is("LearningEntitlement"):
		_, err := b.entitlement(claim, person, org, "")
		return err
	case claim.Is("LearningAchievement") || len(claim.Types()) == 0:
		return b.achievement(claim, person, org)
	}
	b.warn(claim.Path, "claim type %s is not mapped; skipped", strings.Join(claim.Types(), ","))
	return nil
}

// nodes returns the nodes of every given property, each node once.
func nodes(node *elm.Node, props ...string) []*elm.Node {
	var out []*elm.Node
	seen := map[string]bool{}
	for _, prop := range props {
		for _, n := range node.Nodes(prop) {
			if id := n.ID(); id != "" {
				if seen[id] {
					continue
				}
				seen[id] = true
			}
			out = append(out, n)
		}
	}
	return out
}

// awardingOrg maps the awarding bodies of node's awardedBy and returns the
// first one mapped, else fallback.
func (b *elmBuilder) awardingOrg(node *elm.Node, fallback string) string {
	awarded := node.Node("awardedBy")
	if awarded == nil {
		return fallback
	}
	org := ""
	for _, body := range awarded.Nodes("awardingBody") {
		if bodySID := b.org(body); bodySID != "" && org == "" {
			org = bodySID
		}
	}
	return firstNonEmpty(org, fallback)
}

// activity maps a LearningActivity claim (a certificate of participation)
// to the Course its specification describes, an Offering of it and the
// subject's Enrollment. Nothing was achieved, so no Result is made.
func (b *elmBuilder) activity(activity *elm.Node, person, org string) error {
	org = b.awardingOrg(activity, org)
	course, err := b.activityCourse(activity, org)
	if err != nil {
		return err
	}
	if course == "" {
		course = b.planned[b.sid(activity)]
	}
	if course == "" {
		// No specification and no achievement: the activity describes the course itself.
		if course, err = b.course(nil, activity, activity.Text("title"), org); err != nil {
			return err
		}
	}
	_, _, err = b.offering(activity, course, org, person)
	return err
}

// planActivities walks the claims before anything is mapped and records, for
// each activity without a specification, the course of the first achievement
// (in document order) it influenced. Without it the course of such an
// activity would depend on which claim reached it first.
func (b *elmBuilder) planActivities(claims []*elm.Node) {
	for _, claim := range claims {
		if !claim.Is("LearningAchievement") && len(claim.Types()) != 0 {
			continue
		}
		// Like achievement(), only the first visit counts; hasPart may point back.
		claimSID := b.sid(claim)
		if b.planSeen[claimSID] {
			continue
		}
		b.planSeen[claimSID] = true
		course := ""
		if spec := claim.Node("specifiedBy"); spec == nil {
			course = b.sid(claim) + "#specification"
		} else if !spec.Is("Qualification") {
			course = b.sid(spec)
		}
		if course != "" {
			for _, activity := range nodes(claim, "influencedBy", "wasInfluencedBy") {
				if sid := b.sid(activity); activity.Node("specifiedBy") == nil && b.planned[sid] == "" {
					b.planned[sid] = course
				}
			}
		}
		b.planActivities(claim.Nodes("hasPart"))
	}
}

// activityCourse maps the specification of activity to a Course, or returns
// "" when the activity has none.
func (b *elmBuilder) activityCourse(activity *elm.Node, org string) (string, error) {
	spec := activity.Node("specifiedBy")
	if spec == nil {
		return "", nil
	}
	return b.course(spec, activity, firstNonEmpty(spec.Text("title"), activity.Text("title")), org)
}

// entitlement maps a LearningEntitlement to an Entitlement for person,
// optionally linked to the result it came with.
func (b *elmBuilder) entitlement(node *elm.Node, person, org, result string) (string, error) {
	sid := b.sid(node)
	if ref, ok := b.seen("entitlement", sid); ok {
		if result != "" {
			for _, stored := range b.bundle.Entitlements {
				if stored.SourcedID == sid && stored.ResultID == "" {
					stored.ResultID = result
				}
			}
		}
		return ref, nil
	}
	b.mark("entitlement", sid, sid)
	org = b.awardingOrg(node, org)
	title := node.Text("title")
	if spec := node.Node("specifiedBy"); spec != nil {
		title = firstNonEmpty(spec.Text("title"), title)
	}
	issued := node.String("issuedDate")
	if awarded := node.Node("awardedBy"); awarded != nil {
		issued = firstNonEmpty(awarded.String("awardingDate"), issued)
	}
	entitlement := &domain.Entitlement{
		Base: base(sid), PersonID: person, ResultID: result, Title: title, OrgID: org,
		IssuedDate: b.date(node.Path+".issuedDate", issued), ExpiryDate: b.date(node.Path+".expiryDate", node.String("expiryDate")),
	}
	if err := check(node.Path, "entitlement", entitlement); err != nil {
		return "", err
	}
	b.bundle.Entitlements = append(b.bundle.Entitlements, entitlement)
	return sid, nil
}

// achievement maps a LearningAchievement to a Result for person, plus the
// Course or Program it is for and the activities it came out of. org is the
// awarding organisation to assume when the achievement names none.
func (b *elmBuilder) achievement(achievement *elm.Node, person, org string) error {
	sid := b.sid(achievement)
	if _, ok := b.seen("result", sid); ok {
		return nil
	}
	b.mark("result", sid, sid)

	awarded := achievement.Node("awardedBy")
	org = b.awardingOrg(achievement, org)
	spec := achievement.Node("specifiedBy")
	title := achievement.Text("title")
	if spec != nil {
		title = firstNonEmpty(spec.Text("title"), title)
	}
	if title == "" {
		title = untitledAchievement
		b.warn(achievement.Path, "achievement has no title; titled %q", title)
	}

	result := &domain.Result{Base: base(sid), PersonID: person, Credits: b.credits(achievement, spec)}
	var err error
	if spec != nil && spec.Is("Qualification") {
		result.ProgramID, err = b.program(spec, title, org)
	} else {
		result.CourseID, err = b.course(spec, achievement, title, org)
	}
	if err != nil {
		return err
	}
	if awarded != nil {
		result.ResultDate = b.date(awarded.Path+".awardingDate", awarded.String("awardingDate"))
	}
	for _, assessment := range nodes(achievement, "provenBy", "wasDerivedFrom") {
		if gradeText := grade(assessment); gradeText != "" {
			result.Grade = gradeText
			if result.ResultDate == "" {
				if assessmentAward := assessment.Node("awardedBy"); assessmentAward != nil {
					result.ResultDate = b.date(assessmentAward.Path+".awardingDate", assessmentAward.String("awardingDate"))
				}
			}
			break
		}
	}
	for _, activity := range nodes(achievement, "influencedBy", "wasInfluencedBy") {
		if result.CourseID == "" {
			// A qualification is no course: the activity offers the course its specification describes.
			if err := b.activity(activity, person, org); err != nil {
				return err
			}
			continue
		}
		offeringSID, offeringCourse, err := b.offering(activity, result.CourseID, org, person)
		if err != nil {
			return err
		}
		if offeringCourse != result.CourseID {
			b.warn(activity.Path, "activity is an offering of course %s, not of the achievement's course %s; result not linked to it", offeringCourse, result.CourseID)
			continue
		}
		if result.OfferingID == "" {
			result.OfferingID = offeringSID
		}
	}
	if err := check(achievement.Path, "result", result); err != nil {
		return err
	}
	b.bundle.Results = append(b.bundle.Results, result)

	for _, entitled := range achievement.Nodes("entitlesTo") {
		if _, err := b.entitlement(entitled, person, org, sid); err != nil {
			return err
		}
	}
	for _, part := range achievement.Nodes("hasPart") {
		if err := b.claim(part, person, org); err != nil {
			return err
		}
	}
	return nil
}

// course maps the specification of an achievement to a Course. Without a
// specification the achievement itself describes the course.
func (b *elmBuilder) course(spec, achievement *elm.Node, title, org string) (string, error) {
	source, sid := spec, ""
	if spec != nil {
		sid = b.sid(spec)
	} else {
		source, sid = achievement, b.sid(achievement)+"#specification"
	}
	if ref, ok := b.seen("course", sid); ok {
		return ref, nil
	}
	course := &domain.Course{Base: base(sid), Code: notation(source), Title: title, OrgID: org}
	if err := check(source.Path, "course", course); err != nil {
		return "", err
	}
	b.bundle.Courses = append(b.bundle.Courses, course)
	b.mark("course", sid, sid)
	return sid, nil
}

// program maps a Qualification to a Program.
func (b *elmBuilder) program(spec *elm.Node, title, org string) (string, error) {
	sid := b.sid(spec)
	if ref, ok := b.seen("program", sid); ok {
		return ref, nil
	}
	program := &domain.Program{Base: base(sid), Code: notation(spec), Title: title, OrgID: org}
	if eqfLevel := spec.Node("eqfLevel"); eqfLevel != nil {
		program.Level = firstNonEmpty(lastSegment(eqfLevel.ID()), eqfLevel.Text("prefLabel"))
	}
	if err := check(spec.Path, "program", program); err != nil {
		return "", err
	}
	b.bundle.Programs = append(b.bundle.Programs, program)
	b.mark("program", sid, sid)
	return sid, nil
}

// offering maps a LearningActivity to an Offering, with the subject's
// Enrollment in it, and returns its sourcedId and the course it is of. That
// is the course the activity's own specification describes, else fallback,
// and is fixed by the first visit, so it never depends on claim order.
func (b *elmBuilder) offering(activity *elm.Node, fallback, org, person string) (sid, course string, err error) {
	sid = b.sid(activity)
	course, ok := b.seen("offering", sid)
	if !ok {
		org = b.awardingOrg(activity, org)
		own, err := b.activityCourse(activity, org)
		if err != nil {
			return "", "", err
		}
		course = firstNonEmpty(own, b.planned[sid], fallback)
		title := activity.Text("title")
		if spec := activity.Node("specifiedBy"); spec != nil {
			title = firstNonEmpty(title, spec.Text("title"))
		}
		offering := &domain.Offering{Base: base(sid), Title: title, CourseID: course, AcademicSessionID: b.session(activity), OrgID: org}
		if err := check(activity.Path, "offering", offering); err != nil {
			return "", "", err
		}
		b.bundle.Offerings = append(b.bundle.Offerings, offering)
		b.mark("offering", sid, course)
	}
	enrollmentSID := sid + "#enrollment:" + person
	if _, ok := b.seen("enrollment", enrollmentSID); !ok {
		b.bundle.Enrollments = append(b.bundle.Enrollments, &domain.Enrollment{Base: base(enrollmentSID), PersonID: person, OfferingID: sid, Role: domain.RoleStudent})
		b.mark("enrollment", enrollmentSID, enrollmentSID)
	}
	return sid, course, nil
}

// session maps an activity's temporal period to an AcademicSession. Periods
// without an id are keyed by their dates, so activities sharing a period
// share one session.
func (b *elmBuilder) session(activity *elm.Node) string {
	period := activity.Node("temporal")
	if period == nil {
		return ""
	}
	start := b.date(period.Path+".startDate", period.String("startDate"))
	end := b.date(period.Path+".endDate", period.String("endDate"))
	if start == "" && end == "" {
		return ""
	}
	sid := firstNonEmpty(period.ID(), "elm:period:"+start+"/"+end)
	if ref, ok := b.seen("academicSession", sid); ok {
		return ref
	}
	title := firstNonEmpty(period.Text("prefLabel"), strings.Trim(start+" – "+end, " –"))
	b.bundle.AcademicSessions = append(b.bundle.AcademicSessions, &domain.AcademicSession{
		Base: base(sid), Title: title, Type: "term", StartDate: start, EndDate: end,
	})
	b.mark("academicSession", sid, sid)
	return sid
}

// credits returns the ECTS credits received for an achievement, else the
// ECTS credit points of its specification. Result.Credits is ECTS by
// definition (OEAPI publishes it as "ects"), so credit points in any other
// framework, or without one, are never used: when no ECTS value is found
// they are reported in a warning and the result carries no credits.
func (b *elmBuilder) credits(achievement, spec *elm.Node) float64 {
	var skipped []string
	pick := func(points []*elm.Node) (float64, bool) {
		for _, point := range points {
			// ParseFloat accepts "NaN" and "Inf", which are not credits
			// (and not JSON-encodable): skip them like negative values.
			value, err := strconv.ParseFloat(strings.ReplaceAll(point.String("point"), ",", "."), 64)
			if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				b.warn(point.Path, "credit point %q is not a non-negative number; skipped", point.String("point"))
				continue
			}
			if isECTS(point.Node("framework")) {
				return value, true
			}
			skipped = append(skipped, point.Path)
		}
		return 0, false
	}
	if value, ok := pick(achievement.Nodes("creditReceived")); ok {
		return value
	}
	if spec != nil {
		if value, ok := pick(spec.Nodes("creditPoint")); ok {
			return value
		}
	}
	for _, path := range skipped {
		b.warn(path, "credit point is not in ECTS (only ECTS credits are mapped); skipped")
	}
	return 0
}

// untitledAchievement titles an achievement that names nothing it is for.
const untitledAchievement = "Credits"

// ectsConcept is the EU education-credit vocabulary concept for ECTS.
const ectsConcept = "http://data.europa.eu/snb/education-credit/6fcec5c5af"

// isECTS reports whether a credit framework concept is ECTS.
func isECTS(framework *elm.Node) bool {
	if framework == nil {
		return false
	}
	if framework.ID() == ectsConcept {
		return true
	}
	label := strings.ToLower(framework.Text("prefLabel"))
	return label == "ects" || strings.Contains(label, "european credit transfer")
}

// grade returns an assessment's grade: the literal of its grade Note (inline
// or referenced by id elsewhere in the document), else a plain grade value.
// The Note is tried first because a string grade may be a reference, and
// returning it as-is would store the Note's id as the grade.
func grade(assessment *elm.Node) string {
	if note := assessment.Node("grade"); note != nil {
		if literal := note.Text("noteLiteral"); literal != "" {
			return literal
		}
	}
	return assessment.Literal("grade")
}

// notation returns the first identifier notation of node (e.g. a course
// code).
func notation(node *elm.Node) string {
	for _, id := range node.Nodes("identifier") {
		if value := id.String("notation"); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(candidates ...string) string {
	for _, candidate := range candidates {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

// date maps an xsd:date/dateTime value to an ISO 8601 calendar date. A value
// that is not one is dropped with a warning rather than failing the whole
// credential: the person and their results are still useful without it.
func (b *elmBuilder) date(path, value string) string {
	if value == "" {
		return ""
	}
	date := isoDate(value)
	if date == "" {
		b.warn(path, "%q is not an ISO 8601 date; skipped", value)
	}
	return date
}

// xsdDateLayouts are the forms of an xsd:date and xsd:dateTime. Go reads
// fractional seconds after the seconds field whatever the layout.
var xsdDateLayouts = []string{time.DateOnly, "2006-01-02Z07:00", "2006-01-02T15:04:05", time.RFC3339}

// isoDate returns the calendar date of a whole xsd:date or xsd:dateTime
// value (2026-01-15T00:00:00+01:00 -> 2026-01-15), or "" when value is
// neither, so trailing text or an impossible time is never half accepted.
func isoDate(value string) string {
	for _, layout := range xsdDateLayouts {
		if _, err := time.Parse(layout, value); err == nil {
			return value[:10]
		}
	}
	return ""
}

func lastSegment(uri string) string {
	if i := strings.LastIndexAny(uri, "/#"); i >= 0 {
		return uri[i+1:]
	}
	return uri
}

// splitName splits a full name at its last space into given and family name.
func splitName(full string) (given, family string) {
	full = strings.TrimSpace(full)
	if i := strings.LastIndex(full, " "); i > 0 {
		return strings.TrimSpace(full[:i]), full[i+1:]
	}
	return "", full
}
