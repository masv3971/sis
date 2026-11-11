// Package eduapi holds Edu-API v1.0 (Final, Oct 2026) read DTOs and mappers
// from the canonical domain. Written from the spec's JSON schemas
// (https://purl.imsglobal.org/spec/eduapi/v1p0/schema/openapi/eduapi_v1p0_openapi3.json);
// no external dependency. The spec says null values and empty arrays MUST be
// omitted, so every optional field here is omitempty.
//
// SIS stores less than the spec requires. A required field SIS has no data for
// is filled as follows: free text from the record's own title, enumerations
// with the extensible term "ext:unspecified" (registrationStatus is a closed
// enum and gets "closed"), and references and dates are omitted rather than
// invented.
package eduapi

import (
	"cmp"
	"strings"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
)

// BasePath is the path every Edu-API v1.0 endpoint must start with.
const BasePath = "/1edtech/eduapi/base/v1p0"

// ScopeCoreReadonly is the OAuth scope the spec defines for reading the core
// resources.
const ScopeCoreReadonly = "http://purl.1edtech.org/spec/eduapi/v1p0/scope/core.readonly"

const unspecified = "ext:unspecified"

// LanguageTypedString is a string with its language.
type LanguageTypedString struct {
	RecordLanguage string `json:"recordLanguage"`
	Value          string `json:"value"`
}

// IdentifierEntry is a typed identifier.
type IdentifierEntry struct {
	Identifier     string `json:"identifier"`
	IdentifierType string `json:"identifierType"`
}

// PersonName holds the parts of a name.
type PersonName struct {
	FamilyName     string `json:"familyName,omitempty"`
	GivenName      string `json:"givenName,omitempty"`
	AdditionalName string `json:"additionalName,omitempty"`
}

// OptionallyTypedEmail is a primary email address.
type OptionallyTypedEmail struct {
	EmailType string `json:"emailType,omitempty"`
	Email     string `json:"email"`
}

// TypedEmail is an additional email address.
type TypedEmail struct {
	EmailType string `json:"emailType"`
	Email     string `json:"email"`
}

// OptionallyTypedPhone is a primary phone number.
type OptionallyTypedPhone struct {
	PhoneType string `json:"phoneType,omitempty"`
	Phone     string `json:"phone"`
}

// TypedPhone is an additional phone number.
type TypedPhone struct {
	PhoneType string `json:"phoneType"`
	Phone     string `json:"phone"`
}

// Person is an Edu-API person. Role is not part of a person: the spec puts
// it on enrollments and affiliations.
type Person struct {
	SourcedID        string                `json:"sourcedId"`
	OtherIdentifiers []IdentifierEntry     `json:"otherIdentifiers,omitempty"`
	RecordLanguage   string                `json:"recordLanguage"`
	LegalName        *PersonName           `json:"legalName,omitempty"`
	FormattedName    string                `json:"formattedName,omitempty"`
	DateOfBirth      string                `json:"dateOfBirth,omitempty"`
	PrimaryEmail     *OptionallyTypedEmail `json:"primaryEmail,omitempty"`
	OtherEmails      []TypedEmail          `json:"otherEmails,omitempty"`
	PrimaryPhone     *OptionallyTypedPhone `json:"primaryPhone,omitempty"`
	OtherPhones      []TypedPhone          `json:"otherPhones,omitempty"`
	DateLastModified string                `json:"dateLastModified,omitempty"`
	RecordStatus     string                `json:"recordStatus"`
}

// Organization is an administrative unit. References are sourcedId strings.
type Organization struct {
	SourcedID        string                `json:"sourcedId"`
	RecordLanguage   string                `json:"recordLanguage"`
	Name             []LanguageTypedString `json:"name"`
	OrganizationType string                `json:"organizationType"`
	Parent           string                `json:"parent,omitempty"`
	DateLastModified string                `json:"dateLastModified,omitempty"`
	RecordStatus     string                `json:"recordStatus"`
}

// AcademicSession is a term, semester or school year.
type AcademicSession struct {
	SourcedID        string                `json:"sourcedId"`
	RecordLanguage   string                `json:"recordLanguage"`
	SessionType      string                `json:"sessionType,omitempty"`
	Title            []LanguageTypedString `json:"title,omitempty"`
	StartDate        string                `json:"startDate,omitempty"`
	EndDate          string                `json:"endDate,omitempty"`
	DateLastModified string                `json:"dateLastModified,omitempty"`
	RecordStatus     string                `json:"recordStatus"`
}

// education holds the fields shared by every template and offering.
type education struct {
	SourcedID        string                `json:"sourcedId"`
	RecordLanguage   string                `json:"recordLanguage"`
	Title            []LanguageTypedString `json:"title,omitempty"`
	Description      []LanguageTypedString `json:"description,omitempty"`
	PrimaryCode      IdentifierEntry       `json:"primaryCode"`
	Organization     string                `json:"organization,omitempty"`
	DateLastModified string                `json:"dateLastModified,omitempty"`
	RecordStatus     string                `json:"recordStatus"`
}

// template holds the fields shared by every education template.
type template struct {
	education
	Level         string                `json:"level"`
	CreditType    string                `json:"creditType"`
	GradingScheme []LanguageTypedString `json:"gradingScheme"`
}

// delivery holds the fields shared by every offering that follow its
// education fields.
type delivery struct {
	AcademicSession    string `json:"academicSession,omitempty"`
	OfferingFormat     string `json:"offeringFormat"`
	RegistrationStatus string `json:"registrationStatus"`
	StartDate          string `json:"startDate,omitempty"`
	EndDate            string `json:"endDate,omitempty"`
}

// CourseTemplate is a time-independent description of a course: our Course.
type CourseTemplate struct {
	CourseType string `json:"courseType"`
	template
	// Parent holds the collection templates (programs) the course is part of.
	Parent []string `json:"parent,omitempty"`
}

// CollectionTemplate is a grouping of courses, e.g. a program: our Program.
type CollectionTemplate struct {
	CollectionType string `json:"collectionType"`
	template
}

// ComponentTemplate is a part of a course, such as a lecture or a laboratory:
// our ComponentTemplate.
type ComponentTemplate struct {
	ComponentType string `json:"componentType"`
	template
	// Parent holds the course templates the component is part of.
	Parent []string `json:"parent,omitempty"`
}

// Education is the type-neutral view of a course, collection or component
// template, as /educationTemplates lists them. The spec's Education schema
// has no type and no parent, so neither is published here.
type Education struct{ template }

// CourseOffering is a course delivered in an academic session: our Offering.
type CourseOffering struct {
	OfferingType string `json:"offeringType"`
	Course       string `json:"course"`
	education
	delivery
	// Parent holds the collection offerings the course offering is part of.
	Parent []string `json:"parent,omitempty"`
}

// CollectionOffering is a program delivered in an academic session: our
// CollectionOffering.
type CollectionOffering struct {
	OfferingType string `json:"offeringType"`
	Collection   string `json:"collection"`
	education
	delivery
}

// ComponentOffering is a component delivered in an academic session: our
// ComponentOffering.
type ComponentOffering struct {
	OfferingType string `json:"offeringType"`
	Component    string `json:"component"`
	education
	delivery
	// Parent holds the course offerings the component is delivered with.
	Parent []string `json:"parent,omitempty"`
}

// EducationOffering is the type-neutral view of a course, collection or
// component offering, as /educationOfferings lists them. Its offeringType is
// collection, course or component.
type EducationOffering struct {
	OfferingType string `json:"offeringType"`
	education
	delivery
}

// Education returns the type-neutral view of the template.
func (t CourseTemplate) Education() Education { return Education{t.template} }

// Education returns the type-neutral view of the template.
func (t CollectionTemplate) Education() Education { return Education{t.template} }

// Education returns the type-neutral view of the template.
func (t ComponentTemplate) Education() Education { return Education{t.template} }

// Education returns the type-neutral view of the offering.
func (o CourseOffering) Education() EducationOffering {
	return EducationOffering{"course", o.education, o.delivery}
}

// Education returns the type-neutral view of the offering.
func (o CollectionOffering) Education() EducationOffering {
	return EducationOffering{"collection", o.education, o.delivery}
}

// Education returns the type-neutral view of the offering.
func (o ComponentOffering) Education() EducationOffering {
	return EducationOffering{"component", o.education, o.delivery}
}

// Affiliation relates a person to an organization in a role. The spec's
// schema has no recordLanguage.
type Affiliation struct {
	SourcedID         string `json:"sourcedId"`
	Person            string `json:"person"`
	Organization      string `json:"organization"`
	Role              string `json:"role"`
	AffiliationStatus string `json:"affiliationStatus"`
	StartDate         string `json:"startDate,omitempty"`
	EndDate           string `json:"endDate,omitempty"`
	DateLastModified  string `json:"dateLastModified,omitempty"`
	RecordStatus      string `json:"recordStatus"`
}

// Enrollment joins a person and an offering in a role.
type Enrollment struct {
	SourcedID         string `json:"sourcedId"`
	RecordLanguage    string `json:"recordLanguage"`
	IsActive          bool   `json:"isActive"`
	EnrollmentStatus  string `json:"enrollmentStatus"`
	Person            string `json:"person"`
	Role              string `json:"role"`
	EducationOffering string `json:"educationOffering"`
	OfferingType      string `json:"offeringType"`
	DateLastModified  string `json:"dateLastModified,omitempty"`
	RecordStatus      string `json:"recordStatus"`
}

// StatusInfo is the error body (Imsx_StatusInfo).
type StatusInfo struct {
	CodeMajor   string     `json:"imsx_codeMajor"`
	Severity    string     `json:"imsx_severity"`
	Description string     `json:"imsx_description,omitempty"`
	CodeMinor   *CodeMinor `json:"imsx_codeMinor,omitempty"`
}

// CodeMinor holds the minor status codes.
type CodeMinor struct {
	Fields []CodeMinorField `json:"imsx_codeMinorField"`
}

// CodeMinorField is one minor status code.
type CodeMinorField struct {
	Name  string `json:"imsx_codeMinorFieldName"`
	Value string `json:"imsx_codeMinorFieldValue"`
}

// MinorInvalidFilterField is the spec's code minor for a filter on a field
// the resource lacks.
const MinorInvalidFilterField = "invalid_filter_field"

// WithMinor returns s with its code minor replaced, for the cases where the
// spec names a more specific code than the HTTP status implies.
func (s StatusInfo) WithMinor(minor string) StatusInfo {
	s.CodeMinor = &CodeMinor{Fields: []CodeMinorField{{Name: "sis", Value: minor}}}
	return s
}

// NewStatusInfo builds the error body for an HTTP status.
func NewStatusInfo(status int, description string) StatusInfo {
	minor, severity := "unknown", "error"
	switch status {
	case 400:
		minor = "invalid_query_parameter"
	case 401:
		minor = "unauthorizedrequest"
	case 403:
		minor = "forbidden"
	case 404:
		minor, severity = "not_found", "status"
	case 422:
		minor = "invalid_data"
	case 429:
		minor = "server_busy"
	case 500:
		minor = "internal_server_error"
	case 503:
		// The spec has no 503; this is its "retry later" code.
		minor = "server_busy"
	}
	return StatusInfo{
		CodeMajor: "failure", Severity: severity, Description: description,
		CodeMinor: &CodeMinor{Fields: []CodeMinorField{{Name: "sis", Value: minor}}},
	}
}

// Mapper converts canonical records to Edu-API shapes in one record language.
type Mapper struct{ lang string }

// NewMapper returns a Mapper that tags every record and text with language.
func NewMapper(language string) Mapper { return Mapper{lang: language} }

// The relationship mappers below copy reference fields (ParentID, OrgID,
// CourseID, AcademicSessionID, PersonID, OfferingID) verbatim as references,
// which are sourcedIds. Callers must first rewrite those fields to the
// referenced records' sourcedIds (readmodel.Client.PublishRefs).

var (
	emailTerms = []string{"home", "homeEmail", "work", "workEmail", "ice", "iceEmail"}
	phoneTerms = []string{"home", "homePhone", "work", "workPhone", "ice", "icePhone"}
)

// Person maps a canonical person.
func (m Mapper) Person(person domain.Person) Person {
	out := Person{
		SourcedID: person.SourcedID, RecordLanguage: m.lang, DateOfBirth: person.DateOfBirth,
		DateLastModified: rfc3339(person.UpdatedAt), RecordStatus: recordStatus(person.Status),
		FormattedName: strings.Join(nonEmpty(person.GivenName, person.MiddleName, person.FamilyName), " "),
		LegalName:     &PersonName{FamilyName: person.FamilyName, GivenName: person.GivenName, AdditionalName: person.MiddleName},
	}
	for _, id := range person.Identifiers {
		out.OtherIdentifiers = append(out.OtherIdentifiers, IdentifierEntry{Identifier: id.Value, IdentifierType: identifierType(id.Type)})
	}
	for i, email := range person.Emails {
		kind := contactType(email.Type, "email", emailTerms)
		if i == 0 {
			out.PrimaryEmail = &OptionallyTypedEmail{EmailType: kind, Email: email.Value}
			continue
		}
		out.OtherEmails = append(out.OtherEmails, TypedEmail{EmailType: cmpOr(kind, "ext:other"), Email: email.Value})
	}
	for i, phone := range person.Phones {
		kind := contactType(phone.Type, "phone", phoneTerms)
		if i == 0 {
			out.PrimaryPhone = &OptionallyTypedPhone{PhoneType: kind, Phone: phone.Value}
			continue
		}
		out.OtherPhones = append(out.OtherPhones, TypedPhone{PhoneType: cmpOr(kind, "ext:other"), Phone: phone.Value})
	}
	return out
}

// contactType maps a contact's type onto the spec's terms. The generic
// type (what the ELM converter sets) says nothing, so it maps to none.
func contactType(t, generic string, terms []string) string {
	if strings.EqualFold(t, generic) {
		return ""
	}
	return termOr(t, "", terms...)
}

func cmpOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// Organization maps a canonical organization.
func (m Mapper) Organization(org *domain.Organization) Organization {
	orgType := org.Type
	if orgType == "" {
		orgType = "organization"
	}
	return Organization{
		SourcedID: org.SourcedID, RecordLanguage: m.lang, Name: m.text(org.Name), OrganizationType: orgType,
		Parent: org.ParentID, DateLastModified: rfc3339(org.UpdatedAt), RecordStatus: recordStatus(org.Status),
	}
}

// AcademicSession maps a canonical academic session.
func (m Mapper) AcademicSession(session *domain.AcademicSession) AcademicSession {
	return AcademicSession{
		SourcedID: session.SourcedID, RecordLanguage: m.lang, Title: m.text(session.Title),
		SessionType: enum(session.Type, "gradingPeriod", "semester", "schoolYear", "term", "studyPeriod"),
		StartDate:   dateTimeZ(session.StartDate), EndDate: dateTimeZ(session.EndDate),
		DateLastModified: rfc3339(session.UpdatedAt), RecordStatus: recordStatus(session.Status),
	}
}

// CourseTemplate maps a canonical course.
func (m Mapper) CourseTemplate(course *domain.Course) CourseTemplate {
	return CourseTemplate{
		CourseType: "standard",
		template:   m.template(&course.Base, course.Title, course.Code, course.OrgID, course.Level),
		Parent:     course.ProgramIDs,
	}
}

// componentTypes are the spec's component types, shared by templates and
// offerings.
var componentTypes = []string{
	"individualizedInstruction", "integratedLectureLab", "laboratory", "lecture", "practicum",
	"recitation", "research", "seminar", "independentStudy", "fieldExperience", "exam",
}

// ComponentTemplate maps a canonical component template.
func (m Mapper) ComponentTemplate(component *domain.ComponentTemplate) ComponentTemplate {
	return ComponentTemplate{
		ComponentType: cmp.Or(enum(component.Type, componentTypes...), unspecified),
		template:      m.template(&component.Base, component.Title, component.Code, component.OrgID, ""),
		Parent:        component.CourseIDs,
	}
}

// CollectionTemplate maps a canonical program.
func (m Mapper) CollectionTemplate(program *domain.Program) CollectionTemplate {
	return CollectionTemplate{
		CollectionType: "program",
		template:       m.template(&program.Base, program.Title, program.Code, program.OrgID, program.Level),
	}
}

// CourseOffering maps a canonical offering. session is the offering's
// academic session, which supplies the dates the spec requires; nil leaves
// them out. An offering may have no title but the spec requires one, so
// courseTitle (the offering's course) stands in, then the sourcedId.
func (m Mapper) CourseOffering(offering *domain.Offering, session *domain.AcademicSession, courseTitle string) CourseOffering {
	base, when := m.offering(&offering.Base, cmp.Or(offering.Title, courseTitle, offering.SourcedID), offering.OrgID, offering.AcademicSessionID, session)
	return CourseOffering{
		OfferingType: "standard", Course: offering.CourseID,
		education: base, delivery: when, Parent: offering.CollectionOfferingIDs,
	}
}

// CollectionOffering maps a canonical collection offering. session and
// programTitle are as for CourseOffering.
func (m Mapper) CollectionOffering(offering *domain.CollectionOffering, session *domain.AcademicSession, programTitle string) CollectionOffering {
	base, when := m.offering(&offering.Base, cmp.Or(offering.Title, programTitle, offering.SourcedID), offering.OrgID, offering.AcademicSessionID, session)
	return CollectionOffering{OfferingType: "program", Collection: offering.ProgramID, education: base, delivery: when}
}

// ComponentOffering maps a canonical component offering. session is as for
// CourseOffering. component is the offering's template, which supplies the
// title of an untitled offering and the offering type; nil leaves both to
// the fallbacks.
func (m Mapper) ComponentOffering(offering *domain.ComponentOffering, session *domain.AcademicSession, component *domain.ComponentTemplate) ComponentOffering {
	var componentTitle, componentType string
	if component != nil {
		componentTitle, componentType = component.Title, component.Type
	}
	base, when := m.offering(&offering.Base, cmp.Or(offering.Title, componentTitle, offering.SourcedID), offering.OrgID, offering.AcademicSessionID, session)
	return ComponentOffering{
		OfferingType: cmp.Or(enum(componentType, componentTypes...), unspecified), Component: offering.ComponentID,
		education: base, delivery: when, Parent: offering.CourseOfferingIDs,
	}
}

// Affiliation maps a canonical affiliation.
func (m Mapper) Affiliation(affiliation *domain.Affiliation) Affiliation {
	status := "active"
	if affiliation.Status == domain.StatusInactive {
		status = "inactive"
	}
	return Affiliation{
		SourcedID: affiliation.SourcedID, Person: affiliation.PersonID, Organization: affiliation.OrgID,
		Role: role(affiliation.Role), AffiliationStatus: status,
		StartDate: dateTimeZ(affiliation.StartDate), EndDate: dateTimeZ(affiliation.EndDate),
		DateLastModified: rfc3339(affiliation.UpdatedAt), RecordStatus: recordStatus(affiliation.Status),
	}
}

// offering builds the fields every offering carries. title is already the
// final text; session, when set, supplies the dates.
func (m Mapper) offering(base *domain.Base, title, orgID, sessionID string, session *domain.AcademicSession) (education, delivery) {
	out := delivery{
		AcademicSession: sessionID,
		OfferingFormat:  unspecified,
		// SIS publishes records of past and current study, not a catalog to
		// enrol in.
		RegistrationStatus: "closed",
	}
	if session != nil {
		out.StartDate, out.EndDate = dateTimeZ(session.StartDate), dateTimeZ(session.EndDate)
	}
	return education{
		SourcedID: base.SourcedID, RecordLanguage: m.lang,
		Title: m.text(title), Description: m.text(title),
		PrimaryCode:  IdentifierEntry{Identifier: base.SourcedID, IdentifierType: "systemId"},
		Organization: orgID, DateLastModified: rfc3339(base.UpdatedAt), RecordStatus: recordStatus(base.Status),
	}, out
}

// Enrollment maps a canonical enrollment. SIS enrolls people only in
// offerings of courses, so offeringType is always "course".
func (m Mapper) Enrollment(enrollment *domain.Enrollment) Enrollment {
	active := enrollment.Status == domain.StatusActive || enrollment.Status == ""
	status := "enrolled"
	if !active {
		status = "finished"
	}
	return Enrollment{
		SourcedID: enrollment.SourcedID, RecordLanguage: m.lang, IsActive: active, EnrollmentStatus: status,
		Person: enrollment.PersonID, Role: role(enrollment.Role),
		EducationOffering: enrollment.OfferingID, OfferingType: "course",
		DateLastModified: rfc3339(enrollment.UpdatedAt), RecordStatus: recordStatus(enrollment.Status),
	}
}

func (m Mapper) template(base *domain.Base, title, code, orgID, level string) template {
	primary := IdentifierEntry{Identifier: code, IdentifierType: "identifier"}
	if code == "" {
		primary = IdentifierEntry{Identifier: base.SourcedID, IdentifierType: "systemId"}
	}
	return template{
		education: education{
			SourcedID: base.SourcedID, RecordLanguage: m.lang, Title: m.text(title), Description: m.text(title),
			PrimaryCode: primary, Organization: orgID,
			DateLastModified: rfc3339(base.UpdatedAt), RecordStatus: recordStatus(base.Status),
		},
		Level:         educationLevel(level),
		CreditType:    unspecified,
		GradingScheme: m.text("unspecified"),
	}
}

func (m Mapper) text(value string) []LanguageTypedString {
	if value == "" {
		return nil
	}
	return []LanguageTypedString{{RecordLanguage: m.lang, Value: value}}
}

// Bundle is a set of converted records in Edu-API shape, one array per
// resource, keyed like the endpoint paths. Results and entitlements are not
// part of Edu-API (results belong to the Edu-API Gradebook spec).
type Bundle struct {
	Persons             []Person             `json:"persons"`
	Organizations       []Organization       `json:"organizations"`
	AcademicSessions    []AcademicSession    `json:"academicSessions"`
	CourseTemplates     []CourseTemplate     `json:"courseTemplates"`
	CollectionTemplates []CollectionTemplate `json:"collectionTemplates"`
	CourseOfferings     []CourseOffering     `json:"courseOfferings"`
	Enrollments         []Enrollment         `json:"enrollments"`
}

// Bundle maps a converted domain.Bundle, whose references are already
// sourcedIds, to Edu-API shape.
func (m Mapper) Bundle(b *domain.Bundle) Bundle {
	sessions := make(map[string]*domain.AcademicSession, len(b.AcademicSessions))
	for _, session := range b.AcademicSessions {
		sessions[session.SourcedID] = session
	}
	courseTitles := make(map[string]string, len(b.Courses))
	for _, course := range b.Courses {
		courseTitles[course.SourcedID] = course.Title
	}
	out := Bundle{
		Persons:             make([]Person, 0, len(b.Persons)),
		Organizations:       mapAll(b.Organizations, m.Organization),
		AcademicSessions:    mapAll(b.AcademicSessions, m.AcademicSession),
		CourseTemplates:     mapAll(b.Courses, m.CourseTemplate),
		CollectionTemplates: mapAll(b.Programs, m.CollectionTemplate),
		CourseOfferings:     make([]CourseOffering, 0, len(b.Offerings)),
		Enrollments:         mapAll(b.Enrollments, m.Enrollment),
	}
	for _, person := range b.Persons {
		out.Persons = append(out.Persons, m.Person(person))
	}
	for _, offering := range b.Offerings {
		out.CourseOfferings = append(out.CourseOfferings, m.CourseOffering(offering, sessions[offering.AcademicSessionID], courseTitles[offering.CourseID]))
	}
	return out
}

func mapAll[E, D any](in []E, fn func(E) D) []D {
	out := make([]D, 0, len(in))
	for _, e := range in {
		out = append(out, fn(e))
	}
	return out
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// recordStatus maps the lifecycle status. An empty status (optional on
// entities) is an active record.
func recordStatus(status domain.Status) string {
	switch status {
	case domain.StatusInactive:
		return "inactive"
	case domain.StatusToBeDeleted:
		return "deleted"
	}
	return "active"
}

// role maps a person's role in an enrollment onto the spec's role terms.
func role(r domain.Role) string {
	switch r {
	case domain.RoleStudent:
		return "student"
	case domain.RoleStaff:
		return "staff"
	case domain.RoleFaculty:
		return "teacher"
	case domain.RoleGuest, "":
		return "member"
	}
	return extTerm(string(r))
}

// educationLevel maps a qualification level onto the spec's levels. EQF 6, 7
// and 8 match the bachelor's, post-bachelor's and doctoral definitions; any
// other level, including EQF 5 (below a bachelor's), is an extension term.
func educationLevel(level string) string {
	switch level {
	case "":
		return unspecified
	case "6":
		return "undergraduate"
	case "7":
		return "graduate"
	case "8":
		return "doctoral"
	}
	return enum(level, "graduate", "undergraduate", "doctoral")
}

var identifierTypes = []string{
	"systemId", "productId", "userName", "accountId", "emailAddress", "nationalIdentityNumber",
	"isbn", "issn", "lisSourcedId", "oneRosterSourcedId", "sisSourcedId", "identifier",
}

func identifierType(t string) string {
	if t == "" {
		return "identifier"
	}
	return enum(t, identifierTypes...)
}

// termOr maps t through pairs of (domain term, spec term); a term not listed
// is an extension. An empty t gives fallback.
func termOr(t, fallback string, pairs ...string) string {
	if t == "" {
		return fallback
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		if strings.EqualFold(t, pairs[i]) || strings.EqualFold(t, pairs[i+1]) {
			return pairs[i+1]
		}
	}
	return extTerm(t)
}

// enum returns s when it is one of allowed, otherwise an extension term.
// Empty stays empty.
func enum(s string, allowed ...string) string {
	if s == "" {
		return ""
	}
	for _, a := range allowed {
		if s == a {
			return s
		}
	}
	return extTerm(s)
}

// extTerm builds an extension term, "ext:" plus s limited to the characters
// the spec allows.
func extTerm(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return unspecified
	}
	return "ext:" + b.String()
}

// dateTimeZ turns a date or date-time into the spec's DateTimeZ. A value
// without a zone is taken as UTC; one it cannot read is left out.
func dateTimeZ(s string) string {
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Format(time.RFC3339)
	}
	for _, layout := range []string{"2006-01-02T15:04:05", time.DateOnly} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format(time.RFC3339)
		}
	}
	return ""
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
