package convert

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

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
//	  wasDerivedFrom LearningAssessment      ->   the Result's grade
//	  wasInfluencedBy LearningActivity       ->   Offering (+ AcademicSession from temporal) and Enrollment
//	  hasPart                                ->   nested achievements, mapped the same way
//
// sourcedIds are the ELM node ids, so re-sending a document converges on the
// same records. A node without an id gets a deterministic one derived from
// the credential id (or a hash of the document) and the node's path.
//
// Parts that cannot be mapped (other claim types, organisations without a
// name, activities of a qualification) are skipped with a warning; records
// that are mapped but invalid (e.g. a subject without a name) fail the whole
// conversion with a domain.ValidationError naming the document path.
func parseELM(data []byte, path string) (*Parsed, error) {
	doc, err := elm.Parse(data, path)
	if err != nil {
		return nil, err
	}
	root := doc.Root
	subjects := root.Nodes("credentialSubject")
	if len(subjects) == 0 {
		return nil, &domain.ValidationError{Field: path + ".credentialSubject", Msg: "required: data must be an ELM credential"}
	}

	x := &elmBuilder{base: root.ID(), memo: map[string]string{}, inProgress: map[string]bool{}}
	if x.base == "" {
		sum := sha256.Sum256(data)
		x.base = "elm:sha256:" + hex.EncodeToString(sum[:16])
	}
	x.rootPath = path

	issuer := x.org(root.Node("issuer"))
	for _, s := range subjects {
		person, err := x.person(s)
		if err != nil {
			return nil, err
		}
		for _, claim := range s.Nodes("hasClaim") {
			if err := x.claim(claim, person, issuer); err != nil {
				return nil, err
			}
		}
	}
	return &Parsed{Bundle: &x.b, Warnings: x.warnings}, nil
}

type elmBuilder struct {
	base     string // prefix for generated sourcedIds
	rootPath string
	b        domain.Bundle
	// memo maps type|sourcedId to the sourcedId to reference ("" when the
	// node was skipped), so a node reached twice is mapped once.
	memo       map[string]string
	inProgress map[string]bool // organisations being mapped (cycle guard)
	warnings   []string
}

// sid returns the node's sourcedId: its id, or one derived from its path.
func (x *elmBuilder) sid(n *elm.Node) string {
	if id := n.ID(); id != "" {
		return id
	}
	return x.base + "#" + strings.TrimPrefix(strings.TrimPrefix(n.Path, x.rootPath), ".")
}

func (x *elmBuilder) warn(path, format string, args ...any) {
	x.warnings = append(x.warnings, path+": "+fmt.Sprintf(format, args...))
}

// seen reports whether typ/sid was already mapped and, if so, the sourcedId
// to reference.
func (x *elmBuilder) seen(typ, sid string) (string, bool) {
	v, ok := x.memo[typ+"|"+sid]
	return v, ok
}

func (x *elmBuilder) mark(typ, sid, ref string) { x.memo[typ+"|"+sid] = ref }

func base(sid string) domain.Base {
	return domain.Base{SourcedID: sid, Status: domain.StatusActive}
}

// check validates a mapped record, reporting a failure against the document
// path it came from.
func check(path, typ string, e interface{ Validate() error }) error {
	err := e.Validate()
	if ve, ok := errors.AsType[*domain.ValidationError](err); ok {
		return &domain.ValidationError{Field: path, Msg: fmt.Sprintf("cannot map to %s: %s %s", typ, ve.Field, ve.Msg)}
	}
	return err
}

// org maps an Organisation node and its parent chain, returning its
// sourcedId, or "" when n is nil or cannot be mapped.
func (x *elmBuilder) org(n *elm.Node) string {
	if n == nil {
		return ""
	}
	sid := x.sid(n)
	if ref, ok := x.seen("organization", sid); ok {
		return ref
	}
	if x.inProgress[sid] {
		x.warn(n.Path, "organisation %s is its own ancestor; parent link dropped", sid)
		return ""
	}
	name := first(n.Text("legalName"), n.Text("altLabel"), n.Text("prefLabel"), n.Text("name"))
	if name == "" {
		x.warn(n.Path, "organisation %s has no legalName (or is not described in the document); skipped", sid)
		x.mark("organization", sid, "")
		return ""
	}
	x.inProgress[sid] = true
	parent := n.Node("parentOrganisation")
	if parent == nil {
		parent = n.Node("parentOrganization")
	}
	parentSID := x.org(parent)
	delete(x.inProgress, sid)

	x.b.Organizations = append(x.b.Organizations, &domain.Organization{Base: base(sid), Name: name, ParentID: parentSID})
	x.mark("organization", sid, sid)
	return sid
}

// person maps a credential subject.
func (x *elmBuilder) person(n *elm.Node) (string, error) {
	sid := x.sid(n)
	if ref, ok := x.seen("person", sid); ok {
		return ref, nil
	}
	given, family := n.Text("givenName"), n.Text("familyName")
	if given == "" && family == "" {
		given, family = splitName(n.Text("fullName"))
	}
	p := domain.Person{
		SourcedID: sid, Status: domain.StatusActive,
		GivenName: given, FamilyName: family,
		Roles:       []domain.Role{domain.RoleStudent},
		DateOfBirth: isoDate(n.String("dateOfBirth")),
	}
	for _, prop := range []string{"identifier", "nationalID", "nationalId"} {
		for _, id := range n.Nodes(prop) {
			if v := id.String("notation"); v != "" {
				p.Identifiers = append(p.Identifiers, domain.Identifier{
					Type: first(id.Text("schemeName"), id.String("schemeId"), id.String("schemeAgency"), prop), Value: v,
				})
			}
		}
	}
	for _, cp := range n.Nodes("contactPoint") {
		for _, m := range cp.Nodes("emailAddress") {
			if v := strings.TrimPrefix(first(m.ID(), m.String("value")), "mailto:"); v != "" {
				p.Emails = append(p.Emails, domain.Contact{Type: "email", Value: v})
			}
		}
		for _, ph := range cp.Nodes("phone") {
			if v := first(ph.String("phoneNumber"), strings.TrimPrefix(ph.ID(), "tel:")); v != "" {
				p.Phones = append(p.Phones, domain.Contact{Type: "phone", Value: v})
			}
		}
	}
	if err := check(n.Path, "person", p); err != nil {
		return "", err
	}
	x.b.Persons = append(x.b.Persons, p)
	x.mark("person", sid, sid)
	return sid, nil
}

// claim maps one claim of the subject. Only learning achievements carry
// enough to map; other claim types are skipped with a warning.
func (x *elmBuilder) claim(n *elm.Node, person, org string) error {
	if types := n.Types(); len(types) > 0 && !n.Is("LearningAchievement") {
		x.warn(n.Path, "claim type %s is not mapped; skipped", strings.Join(types, ","))
		return nil
	}
	return x.achievement(n, person, org)
}

// achievement maps a LearningAchievement to a Result for person, plus the
// Course or Program it is for and the activities it came out of. org is the
// awarding organisation to assume when the achievement names none.
func (x *elmBuilder) achievement(n *elm.Node, person, org string) error {
	sid := x.sid(n)
	if _, ok := x.seen("result", sid); ok {
		return nil
	}
	x.mark("result", sid, sid)

	awarded := n.Node("awardedBy")
	if awarded != nil {
		for i, body := range awarded.Nodes("awardingBody") {
			if s := x.org(body); s != "" && i == 0 {
				org = s
			}
		}
	}
	spec := n.Node("specifiedBy")
	title := n.Text("title")
	if spec != nil {
		title = first(spec.Text("title"), title)
	}

	r := &domain.Result{Base: base(sid), PersonID: person, Credits: x.credits(n, spec)}
	var err error
	if spec != nil && spec.Is("Qualification") {
		r.ProgramID, err = x.program(spec, title, org)
	} else {
		r.CourseID, err = x.course(spec, n, title, org)
	}
	if err != nil {
		return err
	}
	if awarded != nil {
		r.ResultDate = isoDate(awarded.String("awardingDate"))
	}
	for _, a := range n.Nodes("wasDerivedFrom") {
		if g := grade(a); g != "" {
			r.Grade = g
			if r.ResultDate == "" {
				if aw := a.Node("awardedBy"); aw != nil {
					r.ResultDate = isoDate(aw.String("awardingDate"))
				}
			}
			break
		}
	}
	for _, act := range n.Nodes("wasInfluencedBy") {
		if r.CourseID == "" {
			x.warn(act.Path, "activity of a qualification is not mapped (an offering needs a course); skipped")
			continue
		}
		off, err := x.offering(act, r.CourseID, org, person)
		if err != nil {
			return err
		}
		if r.OfferingID == "" {
			r.OfferingID = off
		}
	}
	if err := check(n.Path, "result", r); err != nil {
		return err
	}
	x.b.Results = append(x.b.Results, r)

	for _, part := range n.Nodes("hasPart") {
		if err := x.claim(part, person, org); err != nil {
			return err
		}
	}
	return nil
}

// course maps the specification of an achievement to a Course. Without a
// specification the achievement itself describes the course.
func (x *elmBuilder) course(spec, ach *elm.Node, title, org string) (string, error) {
	n, sid := spec, ""
	if spec != nil {
		sid = x.sid(spec)
	} else {
		n, sid = ach, x.sid(ach)+"#specification"
	}
	if ref, ok := x.seen("course", sid); ok {
		return ref, nil
	}
	c := &domain.Course{Base: base(sid), Code: notation(n), Title: title, OrgID: org}
	if err := check(n.Path, "course", c); err != nil {
		return "", err
	}
	x.b.Courses = append(x.b.Courses, c)
	x.mark("course", sid, sid)
	return sid, nil
}

// program maps a Qualification to a Program.
func (x *elmBuilder) program(spec *elm.Node, title, org string) (string, error) {
	sid := x.sid(spec)
	if ref, ok := x.seen("program", sid); ok {
		return ref, nil
	}
	p := &domain.Program{Base: base(sid), Code: notation(spec), Title: title, OrgID: org}
	if lvl := spec.Node("eqfLevel"); lvl != nil {
		p.Level = first(lastSegment(lvl.ID()), lvl.Text("prefLabel"))
	}
	if err := check(spec.Path, "program", p); err != nil {
		return "", err
	}
	x.b.Programs = append(x.b.Programs, p)
	x.mark("program", sid, sid)
	return sid, nil
}

// offering maps a LearningActivity to an Offering of course, with the
// subject's Enrollment in it.
func (x *elmBuilder) offering(act *elm.Node, course, org, person string) (string, error) {
	sid := x.sid(act)
	if _, ok := x.seen("offering", sid); !ok {
		title := act.Text("title")
		if spec := act.Node("specifiedBy"); spec != nil {
			title = first(title, spec.Text("title"))
		}
		o := &domain.Offering{Base: base(sid), Title: title, CourseID: course, AcademicSessionID: x.session(act), OrgID: org}
		if err := check(act.Path, "offering", o); err != nil {
			return "", err
		}
		x.b.Offerings = append(x.b.Offerings, o)
		x.mark("offering", sid, sid)
	}
	esid := sid + "#enrollment:" + person
	if _, ok := x.seen("enrollment", esid); !ok {
		x.b.Enrollments = append(x.b.Enrollments, &domain.Enrollment{Base: base(esid), PersonID: person, OfferingID: sid, Role: domain.RoleStudent})
		x.mark("enrollment", esid, esid)
	}
	return sid, nil
}

// session maps an activity's temporal period to an AcademicSession. Periods
// without an id are keyed by their dates, so activities sharing a period
// share one session.
func (x *elmBuilder) session(act *elm.Node) string {
	t := act.Node("temporal")
	if t == nil {
		return ""
	}
	start, end := isoDate(t.String("startDate")), isoDate(t.String("endDate"))
	if start == "" && end == "" {
		return ""
	}
	sid := first(t.ID(), "elm:period:"+start+"/"+end)
	if ref, ok := x.seen("academicSession", sid); ok {
		return ref
	}
	title := first(t.Text("prefLabel"), strings.Trim(start+" – "+end, " –"))
	x.b.AcademicSessions = append(x.b.AcademicSessions, &domain.AcademicSession{
		Base: base(sid), Title: title, Type: "term", StartDate: start, EndDate: end,
	})
	x.mark("academicSession", sid, sid)
	return sid
}

// credits returns the ECTS credits received for an achievement, else the
// ECTS credit points of its specification. Result.Credits is ECTS by
// definition (OEAPI publishes it as "ects"), so credit points in any other
// framework, or without one, are never used: when no ECTS value is found
// they are reported in a warning and the result carries no credits.
func (x *elmBuilder) credits(ach, spec *elm.Node) float64 {
	var skipped []string
	pick := func(ns []*elm.Node) (float64, bool) {
		for _, c := range ns {
			// ParseFloat accepts "NaN" and "Inf", which are not credits
			// (and not JSON-encodable): skip them like negative values.
			v, err := strconv.ParseFloat(strings.ReplaceAll(c.String("point"), ",", "."), 64)
			if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
				x.warn(c.Path, "credit point %q is not a non-negative number; skipped", c.String("point"))
				continue
			}
			if isECTS(c.Node("framework")) {
				return v, true
			}
			skipped = append(skipped, c.Path)
		}
		return 0, false
	}
	if v, ok := pick(ach.Nodes("creditReceived")); ok {
		return v
	}
	if spec != nil {
		if v, ok := pick(spec.Nodes("creditPoint")); ok {
			return v
		}
	}
	for _, p := range skipped {
		x.warn(p, "credit point is not in ECTS (only ECTS credits are mapped); skipped")
	}
	return 0
}

// ectsConcept is the EU education-credit vocabulary concept for ECTS.
const ectsConcept = "http://data.europa.eu/snb/education-credit/6fcec5c5af"

// isECTS reports whether a credit framework concept is ECTS.
func isECTS(fw *elm.Node) bool {
	if fw == nil {
		return false
	}
	if fw.ID() == ectsConcept {
		return true
	}
	label := strings.ToLower(fw.Text("prefLabel"))
	return label == "ects" || strings.Contains(label, "european credit transfer")
}

// grade returns an assessment's grade: the literal of its grade Note (inline
// or referenced by id elsewhere in the document), else a plain grade value.
// The Note is tried first because a string grade may be a reference, and
// returning it as-is would store the Note's id as the grade.
func grade(a *elm.Node) string {
	if g := a.Node("grade"); g != nil {
		if v := g.Text("noteLiteral"); v != "" {
			return v
		}
	}
	return a.Literal("grade")
}

// notation returns the first identifier notation of n (e.g. a course code).
func notation(n *elm.Node) string {
	for _, id := range n.Nodes("identifier") {
		if v := id.String("notation"); v != "" {
			return v
		}
	}
	return ""
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// isoDate trims an xsd:dateTime to its date part (2026-01-15T00:00:00+01:00
// -> 2026-01-15); anything else is returned as is.
func isoDate(s string) string {
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		return s[:10]
	}
	return s
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
