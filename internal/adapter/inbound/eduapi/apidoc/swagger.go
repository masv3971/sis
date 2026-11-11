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
//	@tag.name					affiliations
//	@tag.description			A person's relationship to an organization, in a role
//	@tag.name					componentTemplates
//	@tag.description			Parts of a course, such as a lecture or a laboratory
//	@tag.name					collectionOfferings
//	@tag.description			Programs delivered in an academic session
//	@tag.name					componentOfferings
//	@tag.description			Components delivered in an academic session
//	@tag.name					educationTemplates
//	@tag.description			Course, collection and component templates in one list
//	@tag.name					educationOfferings
//	@tag.description			Course, collection and component offerings in one list

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

// -- affiliations -------------------------------------------------------------

//	@Summary		List affiliations
//	@ID				list-affiliations
//	@Description	Lists affiliations. sort and filter accept sourcedId.
//	@Tags			affiliations
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int					false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int					false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string				false	"Field to sort by"
//	@Param			orderBy	query		string				false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string				false	"One or two terms joined by AND or OR, e.g. sourcedId='x'"
//	@Success		200		{array}		eduapi.Affiliation	"Success"
//	@Header			200		{integer}	X-Total-Count		"Number of stored records"
//	@Header			200		{string}	Link				"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo	"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/affiliations [get]

//	@Summary		Get affiliation
//	@ID				get-affiliation
//	@Description	Returns one affiliation by sourcedId.
//	@Tags			affiliations
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string				true	"Affiliation sourcedId"
//	@Success		200	{object}	eduapi.Affiliation	"Success"
//	@Failure		401	{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403	{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		404	{object}	eduapi.StatusInfo	"Not Found"
//	@Failure		500	{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/affiliations/{id} [get]

// -- componentTemplates -------------------------------------------------------

//	@Summary		List component templates
//	@ID				list-component-templates
//	@Description	Lists component templates. sort and filter accept sourcedId.
//	@Tags			componentTemplates
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int							false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int							false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string						false	"Field to sort by"
//	@Param			orderBy	query		string						false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string						false	"One or two terms joined by AND or OR, e.g. sourcedId='x'"
//	@Success		200		{array}		eduapi.ComponentTemplate	"Success"
//	@Header			200		{integer}	X-Total-Count				"Number of stored records"
//	@Header			200		{string}	Link						"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo			"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo			"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo			"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo			"Internal Server Error"
//	@Router			/componentTemplates [get]

//	@Summary		Get component template
//	@ID				get-component-template
//	@Description	Returns one component template by sourcedId.
//	@Tags			componentTemplates
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string						true	"Component template sourcedId"
//	@Success		200	{object}	eduapi.ComponentTemplate	"Success"
//	@Failure		401	{object}	eduapi.StatusInfo			"Missing, invalid or untrusted token"
//	@Failure		403	{object}	eduapi.StatusInfo			"Token lacks the required scope"
//	@Failure		404	{object}	eduapi.StatusInfo			"Not Found"
//	@Failure		500	{object}	eduapi.StatusInfo			"Internal Server Error"
//	@Router			/componentTemplates/{id} [get]

// -- collectionOfferings ------------------------------------------------------

//	@Summary		List collection offerings
//	@ID				list-collection-offerings
//	@Description	Lists collection offerings (SIS collection offerings). sort and filter accept sourcedId.
//	@Tags			collectionOfferings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int							false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int							false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string						false	"Field to sort by"
//	@Param			orderBy	query		string						false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string						false	"One or two terms joined by AND or OR, e.g. sourcedId='x'"
//	@Success		200		{array}		eduapi.CollectionOffering	"Success"
//	@Header			200		{integer}	X-Total-Count				"Number of stored records"
//	@Header			200		{string}	Link						"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo			"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo			"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo			"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo			"Internal Server Error"
//	@Router			/collectionOfferings [get]

//	@Summary		Get collection offering
//	@ID				get-collection-offering
//	@Description	Returns one collection offering by sourcedId.
//	@Tags			collectionOfferings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string						true	"Collection offering sourcedId"
//	@Success		200	{object}	eduapi.CollectionOffering	"Success"
//	@Failure		401	{object}	eduapi.StatusInfo			"Missing, invalid or untrusted token"
//	@Failure		403	{object}	eduapi.StatusInfo			"Token lacks the required scope"
//	@Failure		404	{object}	eduapi.StatusInfo			"Not Found"
//	@Failure		500	{object}	eduapi.StatusInfo			"Internal Server Error"
//	@Router			/collectionOfferings/{id} [get]

//	@Summary		List the collection offerings of an academic session
//	@ID				list-session-collection-offerings
//	@Description	Lists the collection offerings in one academic session. sort and filter accept sourcedId.
//	@Tags			academicSessions
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string						true	"Academic session sourcedId"
//	@Param			limit	query		int							false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int							false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string						false	"Field to sort by"
//	@Param			orderBy	query		string						false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string						false	"One or two terms joined by AND or OR, e.g. sourcedId='x'"
//	@Success		200		{array}		eduapi.CollectionOffering	"Success"
//	@Header			200		{integer}	X-Total-Count				"Number of stored records"
//	@Header			200		{string}	Link						"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo			"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo			"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo			"Token lacks the required scope"
//	@Failure		404		{object}	eduapi.StatusInfo			"No such academic session"
//	@Failure		500		{object}	eduapi.StatusInfo			"Internal Server Error"
//	@Router			/academicSessions/{id}/collectionOfferings [get]

// -- componentOfferings -------------------------------------------------------

//	@Summary		List component offerings
//	@ID				list-component-offerings
//	@Description	Lists component offerings. sort and filter accept sourcedId.
//	@Tags			componentOfferings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int							false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int							false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string						false	"Field to sort by"
//	@Param			orderBy	query		string						false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string						false	"One or two terms joined by AND or OR, e.g. sourcedId='x'"
//	@Success		200		{array}		eduapi.ComponentOffering	"Success"
//	@Header			200		{integer}	X-Total-Count				"Number of stored records"
//	@Header			200		{string}	Link						"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo			"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo			"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo			"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo			"Internal Server Error"
//	@Router			/componentOfferings [get]

//	@Summary		Get component offering
//	@ID				get-component-offering
//	@Description	Returns one component offering by sourcedId.
//	@Tags			componentOfferings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string						true	"Component offering sourcedId"
//	@Success		200	{object}	eduapi.ComponentOffering	"Success"
//	@Failure		401	{object}	eduapi.StatusInfo			"Missing, invalid or untrusted token"
//	@Failure		403	{object}	eduapi.StatusInfo			"Token lacks the required scope"
//	@Failure		404	{object}	eduapi.StatusInfo			"Not Found"
//	@Failure		500	{object}	eduapi.StatusInfo			"Internal Server Error"
//	@Router			/componentOfferings/{id} [get]

//	@Summary		List the component offerings of an academic session
//	@ID				list-session-component-offerings
//	@Description	Lists the component offerings in one academic session. sort and filter accept sourcedId.
//	@Tags			academicSessions
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string						true	"Academic session sourcedId"
//	@Param			limit	query		int							false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int							false	"Number of items to skip"											minimum(0)
//	@Param			sort	query		string						false	"Field to sort by"
//	@Param			orderBy	query		string						false	"Sort direction"	Enums(asc, desc)
//	@Param			filter	query		string						false	"One or two terms joined by AND or OR, e.g. sourcedId='x'"
//	@Success		200		{array}		eduapi.ComponentOffering	"Success"
//	@Header			200		{integer}	X-Total-Count				"Number of stored records"
//	@Header			200		{string}	Link						"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo			"Invalid query parameter or filter field"
//	@Failure		401		{object}	eduapi.StatusInfo			"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo			"Token lacks the required scope"
//	@Failure		404		{object}	eduapi.StatusInfo			"No such academic session"
//	@Failure		500		{object}	eduapi.StatusInfo			"Internal Server Error"
//	@Router			/academicSessions/{id}/componentOfferings [get]

// -- educationTemplates -------------------------------------------------------

//	@Summary		List education templates
//	@ID				list-education-templates
//	@Description	Lists course, collection and component templates in one list, ordered by sourcedId. Paged in memory: sort and filter answer 400. A template has no type and no parent here; use the typed resources for those.
//	@Tags			educationTemplates
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int					false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int					false	"Number of items to skip"											minimum(0)
//	@Success		200		{array}		eduapi.Education	"Success"
//	@Header			200		{integer}	X-Total-Count		"Number of stored records"
//	@Header			200		{string}	Link				"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo	"Invalid query parameter, or sort/filter given"
//	@Failure		401		{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/educationTemplates [get]

//	@Summary		Get education template
//	@ID				get-education-template
//	@Description	Returns one template by sourcedId, whether it is a course, collection or component template.
//	@Tags			educationTemplates
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string				true	"Template sourcedId"
//	@Success		200	{object}	eduapi.Education	"Success"
//	@Failure		401	{object}	eduapi.StatusInfo	"Missing, invalid or untrusted token"
//	@Failure		403	{object}	eduapi.StatusInfo	"Token lacks the required scope"
//	@Failure		404	{object}	eduapi.StatusInfo	"Not Found"
//	@Failure		500	{object}	eduapi.StatusInfo	"Internal Server Error"
//	@Router			/educationTemplates/{id} [get]

// -- educationOfferings -------------------------------------------------------

//	@Summary		List education offerings
//	@ID				list-education-offerings
//	@Description	Lists course, collection and component offerings in one list, ordered by sourcedId. offeringType is collection, course or component. Paged in memory: sort and filter answer 400.
//	@Tags			educationOfferings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int							false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int							false	"Number of items to skip"											minimum(0)
//	@Success		200		{array}		eduapi.EducationOffering	"Success"
//	@Header			200		{integer}	X-Total-Count				"Number of stored records"
//	@Header			200		{string}	Link						"RFC 8288 first, prev, next and last links"
//	@Failure		400		{object}	eduapi.StatusInfo			"Invalid query parameter, or sort/filter given"
//	@Failure		401		{object}	eduapi.StatusInfo			"Missing, invalid or untrusted token"
//	@Failure		403		{object}	eduapi.StatusInfo			"Token lacks the required scope"
//	@Failure		500		{object}	eduapi.StatusInfo			"Internal Server Error"
//	@Router			/educationOfferings [get]

//	@Summary		Get education offering
//	@ID				get-education-offering
//	@Description	Returns one offering by sourcedId, whether it is a course, collection or component offering.
//	@Tags			educationOfferings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string						true	"Offering sourcedId"
//	@Success		200	{object}	eduapi.EducationOffering	"Success"
//	@Failure		401	{object}	eduapi.StatusInfo			"Missing, invalid or untrusted token"
//	@Failure		403	{object}	eduapi.StatusInfo			"Token lacks the required scope"
//	@Failure		404	{object}	eduapi.StatusInfo			"Not Found"
//	@Failure		500	{object}	eduapi.StatusInfo			"Internal Server Error"
//	@Router			/educationOfferings/{id} [get]
