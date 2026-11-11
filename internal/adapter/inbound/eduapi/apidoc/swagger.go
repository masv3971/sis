package apidoc

import _ "github.com/SUNET/sis/pkg/eduapi"

// This file holds the swag annotations for the Edu-API document (`make
// swagger`, instance "eduapi"). The generic List and Get handlers are mounted
// once per entity type, so one doc comment cannot describe them; every
// operation gets its own free-standing block here instead, which
// `swag init --parseFuncBody` picks up.
//
// Keep these blocks in step with the Edu-API routes in internal/httpserver.

//	@title			SIS Edu-API v1.0
//	@version		1.0
//	@description	Read-only publication of the SIS data in 1EdTech Edu-API v1.0 (Final) shapes.
//	@description
//	@description	Every endpoint needs a Bearer access token from one of the token issuers
//	@description	SIS trusts (sis.auth.issuers), with scope sis.read or the spec's
//	@description	http://purl.1edtech.org/spec/eduapi/v1p0/scope/core.readonly.
//	@description
//	@description	Lists are bare JSON arrays. X-Total-Count holds the number of stored
//	@description	records and Link the first, prev, next and last pages. A record is
//	@description	addressed by its sourcedId, and references between records are
//	@description	sourcedIds. Errors use the spec's Imsx_StatusInfo body. Lists that
//	@description	support sort and filter say so; the others answer 400.
//	@description
//	@description				SIS stores less than the spec requires, so a required field with no data
//	@description				is filled in one of three ways. Free text (title, description) falls back
//	@description				to the record's own title, then its course title or sourcedId. An
//	@description				enumeration carries the extensible term ext:unspecified, except
//	@description				registrationStatus, which is always closed. References and dates are
//	@description				omitted rather than invented, so a client can receive a record without them.
//	@license.name				BSD-2-Clause
//	@license.url				https://opensource.org/license/bsd-2-clause
//	@BasePath					/1edtech/eduapi/base/v1p0
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Access token prefixed with "Bearer ", e.g. "Bearer eyJhbGciOi...".
//	@tag.name					persons
//	@tag.description			People known to the institution
//	@tag.name					organizations
//	@tag.description			Institutions and organizational units
//	@tag.name					academicSessions
//	@tag.description			Terms, semesters and school years
//	@tag.name					courseTemplates
//	@tag.description			Time-independent descriptions of courses
//	@tag.name					collectionTemplates
//	@tag.description			Groupings of courses, such as programs
//	@tag.name					courseOfferings
//	@tag.description			Courses delivered in an academic session
//	@tag.name					enrollments
//	@tag.description			A person's participation in an offering, in a role

// -- persons ------------------------------------------------------------------

//	@Summary		List persons
//	@ID				list-persons
//	@Description	Lists persons. sort accepts sourcedId, legalName.familyName, legalName.givenName and dateOfBirth; filter accepts the same fields.
//	@Tags			persons
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int					false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int					false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string				false	"Field to sort by"
//	@Param			orderBy	query		string				false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string				false	"One or two terms joined by AND or OR, e.g. legalName.familyName~'son'"
//	@Success		200		{array}		eduapi.Person		"Success"
//	@Header			200		{integer}	X-Total-Count		"Number of stored records"
//	@Header			200		{string}	Link				"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo	"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/persons [get]

//	@Summary		Get person
//	@ID				get-person
//	@Description	Returns one person by sourcedId.
//	@Tags			persons
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string				true	"Person sourcedId"
//	@Success		200	{object}	eduapi.Person		"Success"
//	@Failure		401	{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403	{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		404	{object}	eduapi.StatusInfo	"Not Found"
//	@Failure		500	{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/persons/{id} [get]

//	@Summary		List students
//	@ID				list-students
//	@Description	Lists the persons with the student role. sort and filter as for persons.
//	@Tags			persons
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int					false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int					false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string				false	"Field to sort by"
//	@Param			orderBy	query		string				false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string				false	"One or two terms joined by AND or OR, e.g. legalName.familyName~'son'"
//	@Success		200		{array}		eduapi.Person		"Success"
//	@Header			200		{integer}	X-Total-Count		"Number of stored records"
//	@Header			200		{string}	Link				"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo	"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/students [get]

//	@Summary		List staff
//	@ID				list-staff
//	@Description	Lists the persons with the staff or faculty role. sort and filter as for persons.
//	@Tags			persons
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int					false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int					false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string				false	"Field to sort by"
//	@Param			orderBy	query		string				false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string				false	"One or two terms joined by AND or OR, e.g. legalName.familyName~'son'"
//	@Success		200		{array}		eduapi.Person		"Success"
//	@Header			200		{integer}	X-Total-Count		"Number of stored records"
//	@Header			200		{string}	Link				"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo	"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/staff [get]

// -- organizations ------------------------------------------------------------

//	@Summary		List organizations
//	@ID				list-organizations
//	@Description	Lists organizations. sort and filter accept sourcedId.
//	@Tags			organizations
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int					false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int					false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string				false	"Field to sort by"
//	@Param			orderBy	query		string				false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string				false	"One or two terms joined by AND or OR, e.g. sourcedId='x'"
//	@Success		200		{array}		eduapi.Organization	"Success"
//	@Header			200		{integer}	X-Total-Count		"Number of stored records"
//	@Header			200		{string}	Link				"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo	"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/organizations [get]

//	@Summary		Get organization
//	@ID				get-organization
//	@Description	Returns one organization by sourcedId.
//	@Tags			organizations
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string				true	"Organization sourcedId"
//	@Success		200	{object}	eduapi.Organization	"Success"
//	@Failure		401	{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403	{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		404	{object}	eduapi.StatusInfo	"Not Found"
//	@Failure		500	{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/organizations/{id} [get]

// -- academicSessions ---------------------------------------------------------

//	@Summary		List academic sessions
//	@ID				list-academic-sessions
//	@Description	Lists academic sessions. sort and filter accept sourcedId.
//	@Tags			academicSessions
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int						false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int						false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string					false	"Field to sort by"
//	@Param			orderBy	query		string					false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string					false	"One or two terms joined by AND or OR, e.g. sourcedId='x'"
//	@Success		200		{array}		eduapi.AcademicSession	"Success"
//	@Header			200		{integer}	X-Total-Count			"Number of stored records"
//	@Header			200		{string}	Link					"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo		"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo		"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo		"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo		"Internal Server Error"
//	@Router			/academicSessions [get]

//	@Summary		Get academic session
//	@ID				get-academic-session
//	@Description	Returns one academic session by sourcedId.
//	@Tags			academicSessions
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Academic session sourcedId"
//	@Success		200	{object}	eduapi.AcademicSession	"Success"
//	@Failure		401	{object}	eduapi.StatusInfo		"Missing, invalid or untrusted token"
//	@Failure		403	{object}	eduapi.StatusInfo		"Token lacks the required scope"
//	@Failure		404	{object}	eduapi.StatusInfo		"Not Found"
//	@Failure		500	{object}	eduapi.StatusInfo		"Internal Server Error"
//	@Router			/academicSessions/{id} [get]

//	@Summary		List the course offerings of an academic session
//	@ID				list-session-course-offerings
//	@Description	Lists the course offerings in one academic session. sort and filter accept sourcedId.
//	@Tags			academicSessions
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Academic session sourcedId"
//	@Param			limit	query		int						false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int						false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string					false	"Field to sort by"
//	@Param			orderBy	query		string					false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string					false	"One or two terms joined by AND or OR, e.g. sourcedId='x'"
//	@Success		200		{array}		eduapi.CourseOffering	"Success"
//	@Header			200		{integer}	X-Total-Count			"Number of stored records"
//	@Header			200		{string}	Link					"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo		"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo		"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo		"Token lacks the required scope"
//	@Failure		404		{object}	eduapi.StatusInfo		"No such academic session"
//	@Failure		500		{object}	eduapi.StatusInfo		"Internal Server Error"
//	@Router			/academicSessions/{id}/courseOfferings [get]

//	@Summary		List the enrollments of an academic session
//	@ID				list-session-enrollments
//	@Description	Lists the enrollments on the course offerings of one academic session. Paged in memory: sort and filter answer 400.
//	@Tags			academicSessions
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string				true	"Academic session sourcedId"
//	@Param			limit	query		int					false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int					false	"Number of items to skip"											minimum(0)
//	@Success		200		{array}		eduapi.Enrollment	"Success"
//	@Header			200		{integer}	X-Total-Count		"Number of stored records"
//	@Header			200		{string}	Link				"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo	"Invalid query parameter, or sort/filter given"
//	@Failure		401		{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		404		{object}	eduapi.StatusInfo	"No such academic session"
//	@Failure		500		{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/academicSessions/{id}/enrollments [get]

// -- courseTemplates ----------------------------------------------------------

//	@Summary		List course templates
//	@ID				list-course-templates
//	@Description	Lists course templates (SIS courses). sort and filter accept sourcedId.
//	@Tags			courseTemplates
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int						false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int						false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string					false	"Field to sort by"
//	@Param			orderBy	query		string					false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string					false	"One or two terms joined by AND or OR, e.g. sourcedId='x'"
//	@Success		200		{array}		eduapi.CourseTemplate	"Success"
//	@Header			200		{integer}	X-Total-Count			"Number of stored records"
//	@Header			200		{string}	Link					"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo		"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo		"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo		"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo		"Internal Server Error"
//	@Router			/courseTemplates [get]

//	@Summary		Get course template
//	@ID				get-course-template
//	@Description	Returns one course template by sourcedId.
//	@Tags			courseTemplates
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Course template sourcedId"
//	@Success		200	{object}	eduapi.CourseTemplate	"Success"
//	@Failure		401	{object}	eduapi.StatusInfo		"Missing, invalid or untrusted token"
//	@Failure		403	{object}	eduapi.StatusInfo		"Token lacks the required scope"
//	@Failure		404	{object}	eduapi.StatusInfo		"Not Found"
//	@Failure		500	{object}	eduapi.StatusInfo		"Internal Server Error"
//	@Router			/courseTemplates/{id} [get]

// -- collectionTemplates ------------------------------------------------------

//	@Summary		List collection templates
//	@ID				list-collection-templates
//	@Description	Lists collection templates (SIS programs). sort and filter accept sourcedId.
//	@Tags			collectionTemplates
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int							false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int							false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string						false	"Field to sort by"
//	@Param			orderBy	query		string						false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string						false	"One or two terms joined by AND or OR, e.g. sourcedId='x'"
//	@Success		200		{array}		eduapi.CollectionTemplate	"Success"
//	@Header			200		{integer}	X-Total-Count				"Number of stored records"
//	@Header			200		{string}	Link						"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo			"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo			"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo			"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo			"Internal Server Error"
//	@Router			/collectionTemplates [get]

//	@Summary		Get collection template
//	@ID				get-collection-template
//	@Description	Returns one collection template by sourcedId.
//	@Tags			collectionTemplates
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string						true	"Collection template sourcedId"
//	@Success		200	{object}	eduapi.CollectionTemplate	"Success"
//	@Failure		401	{object}	eduapi.StatusInfo			"Missing, invalid or untrusted token"
//	@Failure		403	{object}	eduapi.StatusInfo			"Token lacks the required scope"
//	@Failure		404	{object}	eduapi.StatusInfo			"Not Found"
//	@Failure		500	{object}	eduapi.StatusInfo			"Internal Server Error"
//	@Router			/collectionTemplates/{id} [get]

// -- courseOfferings ----------------------------------------------------------

//	@Summary		List course offerings
//	@ID				list-course-offerings
//	@Description	Lists course offerings (SIS offerings). sort and filter accept sourcedId.
//	@Tags			courseOfferings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int						false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int						false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string					false	"Field to sort by"
//	@Param			orderBy	query		string					false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string					false	"One or two terms joined by AND or OR, e.g. sourcedId='x'"
//	@Success		200		{array}		eduapi.CourseOffering	"Success"
//	@Header			200		{integer}	X-Total-Count			"Number of stored records"
//	@Header			200		{string}	Link					"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo		"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo		"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo		"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo		"Internal Server Error"
//	@Router			/courseOfferings [get]

//	@Summary		Get course offering
//	@ID				get-course-offering
//	@Description	Returns one course offering by sourcedId.
//	@Tags			courseOfferings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Course offering sourcedId"
//	@Success		200	{object}	eduapi.CourseOffering	"Success"
//	@Failure		401	{object}	eduapi.StatusInfo		"Missing, invalid or untrusted token"
//	@Failure		403	{object}	eduapi.StatusInfo		"Token lacks the required scope"
//	@Failure		404	{object}	eduapi.StatusInfo		"Not Found"
//	@Failure		500	{object}	eduapi.StatusInfo		"Internal Server Error"
//	@Router			/courseOfferings/{id} [get]

//	@Summary		List the enrollments of a course offering
//	@ID				list-offering-enrollments
//	@Description	Lists the enrollments on one course offering. sort and filter accept sourcedId.
//	@Tags			courseOfferings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string				true	"Course offering sourcedId"
//	@Param			limit	query		int					false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int					false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string				false	"Field to sort by"
//	@Param			orderBy	query		string				false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string				false	"One or two terms joined by AND or OR, e.g. sourcedId='x'"
//	@Success		200		{array}		eduapi.Enrollment	"Success"
//	@Header			200		{integer}	X-Total-Count		"Number of stored records"
//	@Header			200		{string}	Link				"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo	"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		404		{object}	eduapi.StatusInfo	"No such course offering"
//	@Failure		500		{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/courseOfferings/{id}/enrollments [get]

//	@Summary		List the students of a course offering
//	@ID				list-offering-students
//	@Description	Lists the persons enrolled on one course offering as students. Paged in memory: sort and filter answer 400.
//	@Tags			courseOfferings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string				true	"Course offering sourcedId"
//	@Param			limit	query		int					false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int					false	"Number of items to skip"											minimum(0)
//	@Success		200		{array}		eduapi.Person		"Success"
//	@Header			200		{integer}	X-Total-Count		"Number of stored records"
//	@Header			200		{string}	Link				"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo	"Invalid query parameter, or sort/filter given"
//	@Failure		401		{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		404		{object}	eduapi.StatusInfo	"No such course offering"
//	@Failure		500		{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/courseOfferings/{id}/students [get]

//	@Summary		List the staff of a course offering
//	@ID				list-offering-staff
//	@Description	Lists the persons enrolled on one course offering as staff. Paged in memory: sort and filter answer 400.
//	@Tags			courseOfferings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string				true	"Course offering sourcedId"
//	@Param			limit	query		int					false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int					false	"Number of items to skip"											minimum(0)
//	@Success		200		{array}		eduapi.Person		"Success"
//	@Header			200		{integer}	X-Total-Count		"Number of stored records"
//	@Header			200		{string}	Link				"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo	"Invalid query parameter, or sort/filter given"
//	@Failure		401		{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		404		{object}	eduapi.StatusInfo	"No such course offering"
//	@Failure		500		{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/courseOfferings/{id}/staff [get]

// -- enrollments --------------------------------------------------------------

//	@Summary		List enrollments
//	@ID				list-enrollments
//	@Description	Lists enrollments. sort and filter accept sourcedId.
//	@Tags			enrollments
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int					false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int					false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string				false	"Field to sort by"
//	@Param			orderBy	query		string				false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string				false	"One or two terms joined by AND or OR, e.g. sourcedId='x'"
//	@Success		200		{array}		eduapi.Enrollment	"Success"
//	@Header			200		{integer}	X-Total-Count		"Number of stored records"
//	@Header			200		{string}	Link				"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo	"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/enrollments [get]

//	@Summary		Get enrollment
//	@ID				get-enrollment
//	@Description	Returns one enrollment by sourcedId.
//	@Tags			enrollments
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string				true	"Enrollment sourcedId"
//	@Success		200	{object}	eduapi.Enrollment	"Success"
//	@Failure		401	{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403	{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		404	{object}	eduapi.StatusInfo	"Not Found"
//	@Failure		500	{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/enrollments/{id} [get]
