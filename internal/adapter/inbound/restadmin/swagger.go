package restadmin

// This file holds the swag annotations that cannot sit on a handler's doc
// comment. It contains the general API info (swag init -g swagger.go) and the
// operation blocks for the generic entity routes. ResourceList & co. are each
// mounted once per entity type, so one doc comment cannot describe them; every
// entity gets its own free-standing block here instead, which
// `swag init --parseFuncBody` picks up (see `make swagger`).
//
// Keep these blocks in step with registerResource in internal/httpserver.

//	@title			SIS admin API
//	@version		1.0
//	@description	Native REST API of the Student Information System (/api/v1).
//	@description
//	@description				Every endpoint needs a Bearer token. Machine clients get one from
//	@description				POST /oauth/token (client_credentials grant, scopes sis.read and
//	@description				sis.write); humans get one from POST /auth/login. Reads accept
//	@description				scope sis.read or role admin, registrar or readonly; writes accept
//	@description				scope sis.write or role admin or registrar.
//	@license.name				BSD-2-Clause
//	@license.url				https://opensource.org/license/bsd-2-clause
//	@BasePath					/api/v1
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Access token prefixed with "Bearer ", e.g. "Bearer eyJhbGciOi...".
//	@tag.name					persons
//	@tag.description			People known to the institution
//	@tag.name					organizations
//	@tag.description			Institutions and organizational units
//	@tag.name					academic-sessions
//	@tag.description			Terms, semesters and school years
//	@tag.name					courses
//	@tag.description			Units of study offered by an organization
//	@tag.name					offerings
//	@tag.description			Courses scheduled in an academic session (classes)
//	@tag.name					enrollments
//	@tag.description			A person's participation in an offering, in a role
//	@tag.name					programs
//	@tag.description			Structured collections of courses leading to a qualification
//	@tag.name					results
//	@tag.description			A person's grade and credits for a course or program
//	@tag.name					convert
//	@tag.description			Convert external formats (ELM) to Edu-API/OEAPI, and load them

// -- organizations ------------------------------------------------------------

//	@Summary		List organizations
//	@ID				list-organizations
//	@Description	Lists organizations with limit/offset paging.
//	@Tags			organizations
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int											false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int											false	"Number of items to skip"											minimum(0)
//	@Success		200		{object}	restadmin.ListReply[domain.Organization]	"Success"
//	@Failure		400		{object}	helpers.ErrorResponse						"Invalid paging parameters"
//	@Failure		401		{object}	auth.ErrorResponse							"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse							"Insufficient scope or role"
//	@Failure		500		{object}	helpers.ErrorResponse						"Internal Server Error"
//	@Router			/organizations [get]

//	@Summary		Create organization
//	@ID				create-organization
//	@Description	Creates an organization. name is required; parentId, when set, must be the id (not sourcedId) of an existing organization. id is generated when empty, sourcedId defaults to id, and createdAt/updatedAt are set by the server.
//	@Tags			organizations
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			organization	body		domain.Organization		true	"Organization to create"
//	@Success		201				{object}	domain.Organization		"Created"
//	@Failure		400				{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401				{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403				{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		409				{object}	helpers.ErrorResponse	"id or sourcedId already in use"
//	@Failure		413				{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500				{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/organizations [post]

//	@Summary		Get organization
//	@ID				get-organization
//	@Description	Returns one organization by id.
//	@Tags			organizations
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Organization id"
//	@Success		200	{object}	domain.Organization		"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/organizations/{id} [get]

//	@Summary		Replace organization
//	@ID				update-organization
//	@Description	Replaces an organization. parentId, when set, must be the id of an existing organization. The path id wins over any id in the body; createdAt is preserved and an empty sourcedId keeps the stored one.
//	@Tags			organizations
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id				path		string					true	"Organization id"
//	@Param			organization	body		domain.Organization		true	"Replacement organization"
//	@Success		200				{object}	domain.Organization		"Success"
//	@Failure		400				{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401				{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403				{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404				{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		409				{object}	helpers.ErrorResponse	"sourcedId already in use"
//	@Failure		413				{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500				{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/organizations/{id} [put]

//	@Summary		Delete organization
//	@ID				delete-organization
//	@Description	Deletes one organization by id.
//	@Tags			organizations
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Organization id"
//	@Success		204	"No Content"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/organizations/{id} [delete]

// -- academic-sessions --------------------------------------------------------

//	@Summary		List academic sessions
//	@ID				list-academic-sessions
//	@Description	Lists academic sessions with limit/offset paging.
//	@Tags			academic-sessions
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int											false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int											false	"Number of items to skip"											minimum(0)
//	@Success		200		{object}	restadmin.ListReply[domain.AcademicSession]	"Success"
//	@Failure		400		{object}	helpers.ErrorResponse						"Invalid paging parameters"
//	@Failure		401		{object}	auth.ErrorResponse							"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse							"Insufficient scope or role"
//	@Failure		500		{object}	helpers.ErrorResponse						"Internal Server Error"
//	@Router			/academic-sessions [get]

//	@Summary		Create academic session
//	@ID				create-academic-session
//	@Description	Creates an academic session. title is required. id is generated when empty, sourcedId defaults to id, and createdAt/updatedAt are set by the server.
//	@Tags			academic-sessions
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			session	body		domain.AcademicSession	true	"Academic session to create"
//	@Success		201		{object}	domain.AcademicSession	"Created"
//	@Failure		400		{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		409		{object}	helpers.ErrorResponse	"id or sourcedId already in use"
//	@Failure		413		{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/academic-sessions [post]

//	@Summary		Get academic session
//	@ID				get-academic-session
//	@Description	Returns one academic session by id.
//	@Tags			academic-sessions
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Academic session id"
//	@Success		200	{object}	domain.AcademicSession	"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/academic-sessions/{id} [get]

//	@Summary		Replace academic session
//	@ID				update-academic-session
//	@Description	Replaces an academic session. The path id wins over any id in the body; createdAt is preserved and an empty sourcedId keeps the stored one.
//	@Tags			academic-sessions
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Academic session id"
//	@Param			session	body		domain.AcademicSession	true	"Replacement academic session"
//	@Success		200		{object}	domain.AcademicSession	"Success"
//	@Failure		400		{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404		{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		409		{object}	helpers.ErrorResponse	"sourcedId already in use"
//	@Failure		413		{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/academic-sessions/{id} [put]

//	@Summary		Delete academic session
//	@ID				delete-academic-session
//	@Description	Deletes one academic session by id.
//	@Tags			academic-sessions
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Academic session id"
//	@Success		204	"No Content"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/academic-sessions/{id} [delete]

// -- courses ------------------------------------------------------------------

//	@Summary		List courses
//	@ID				list-courses
//	@Description	Lists courses with limit/offset paging.
//	@Tags			courses
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int									false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int									false	"Number of items to skip"											minimum(0)
//	@Success		200		{object}	restadmin.ListReply[domain.Course]	"Success"
//	@Failure		400		{object}	helpers.ErrorResponse				"Invalid paging parameters"
//	@Failure		401		{object}	auth.ErrorResponse					"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse					"Insufficient scope or role"
//	@Failure		500		{object}	helpers.ErrorResponse				"Internal Server Error"
//	@Router			/courses [get]

//	@Summary		Create course
//	@ID				create-course
//	@Description	Creates a course. title is required; orgId, when set, must be the id (not sourcedId) of an existing organization. id is generated when empty, sourcedId defaults to id, and createdAt/updatedAt are set by the server.
//	@Tags			courses
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			course	body		domain.Course			true	"Course to create"
//	@Success		201		{object}	domain.Course			"Created"
//	@Failure		400		{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		409		{object}	helpers.ErrorResponse	"id or sourcedId already in use"
//	@Failure		413		{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/courses [post]

//	@Summary		Get course
//	@ID				get-course
//	@Description	Returns one course by id.
//	@Tags			courses
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Course id"
//	@Success		200	{object}	domain.Course			"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/courses/{id} [get]

//	@Summary		Replace course
//	@ID				update-course
//	@Description	Replaces a course. orgId, when set, must be the id of an existing organization. The path id wins over any id in the body; createdAt is preserved and an empty sourcedId keeps the stored one.
//	@Tags			courses
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Course id"
//	@Param			course	body		domain.Course			true	"Replacement course"
//	@Success		200		{object}	domain.Course			"Success"
//	@Failure		400		{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404		{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		409		{object}	helpers.ErrorResponse	"sourcedId already in use"
//	@Failure		413		{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/courses/{id} [put]

//	@Summary		Delete course
//	@ID				delete-course
//	@Description	Deletes one course by id.
//	@Tags			courses
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Course id"
//	@Success		204	"No Content"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/courses/{id} [delete]

// -- offerings ----------------------------------------------------------------

//	@Summary		List offerings
//	@ID				list-offerings
//	@Description	Lists offerings with limit/offset paging.
//	@Tags			offerings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int										false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int										false	"Number of items to skip"											minimum(0)
//	@Success		200		{object}	restadmin.ListReply[domain.Offering]	"Success"
//	@Failure		400		{object}	helpers.ErrorResponse					"Invalid paging parameters"
//	@Failure		401		{object}	auth.ErrorResponse						"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse						"Insufficient scope or role"
//	@Failure		500		{object}	helpers.ErrorResponse					"Internal Server Error"
//	@Router			/offerings [get]

//	@Summary		Create offering
//	@ID				create-offering
//	@Description	Creates an offering. courseId is required; courseId, academicSessionId and orgId must be ids (not sourcedIds) of existing records. id is generated when empty, sourcedId defaults to id, and createdAt/updatedAt are set by the server.
//	@Tags			offerings
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			offering	body		domain.Offering			true	"Offering to create"
//	@Success		201			{object}	domain.Offering			"Created"
//	@Failure		400			{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401			{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403			{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		409			{object}	helpers.ErrorResponse	"id or sourcedId already in use"
//	@Failure		413			{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500			{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/offerings [post]

//	@Summary		Get offering
//	@ID				get-offering
//	@Description	Returns one offering by id.
//	@Tags			offerings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Offering id"
//	@Success		200	{object}	domain.Offering			"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/offerings/{id} [get]

//	@Summary		Replace offering
//	@ID				update-offering
//	@Description	Replaces an offering. courseId, academicSessionId and orgId must be ids of existing records. The path id wins over any id in the body; createdAt is preserved and an empty sourcedId keeps the stored one.
//	@Tags			offerings
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id			path		string					true	"Offering id"
//	@Param			offering	body		domain.Offering			true	"Replacement offering"
//	@Success		200			{object}	domain.Offering			"Success"
//	@Failure		400			{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401			{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403			{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404			{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		409			{object}	helpers.ErrorResponse	"sourcedId already in use"
//	@Failure		413			{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500			{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/offerings/{id} [put]

//	@Summary		Delete offering
//	@ID				delete-offering
//	@Description	Deletes one offering by id.
//	@Tags			offerings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Offering id"
//	@Success		204	"No Content"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/offerings/{id} [delete]

// -- enrollments --------------------------------------------------------------

//	@Summary		List enrollments
//	@ID				list-enrollments
//	@Description	Lists enrollments with limit/offset paging.
//	@Tags			enrollments
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int										false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int										false	"Number of items to skip"											minimum(0)
//	@Success		200		{object}	restadmin.ListReply[domain.Enrollment]	"Success"
//	@Failure		400		{object}	helpers.ErrorResponse					"Invalid paging parameters"
//	@Failure		401		{object}	auth.ErrorResponse						"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse						"Insufficient scope or role"
//	@Failure		500		{object}	helpers.ErrorResponse					"Internal Server Error"
//	@Router			/enrollments [get]

//	@Summary		Create enrollment
//	@ID				create-enrollment
//	@Description	Creates an enrollment. personId and offeringId are required and must be the ids (not sourcedIds) of an existing person and offering. id is generated when empty, sourcedId defaults to id, and createdAt/updatedAt are set by the server.
//	@Tags			enrollments
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			enrollment	body		domain.Enrollment		true	"Enrollment to create"
//	@Success		201			{object}	domain.Enrollment		"Created"
//	@Failure		400			{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401			{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403			{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		409			{object}	helpers.ErrorResponse	"id or sourcedId already in use"
//	@Failure		413			{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500			{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/enrollments [post]

//	@Summary		Get enrollment
//	@ID				get-enrollment
//	@Description	Returns one enrollment by id.
//	@Tags			enrollments
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Enrollment id"
//	@Success		200	{object}	domain.Enrollment		"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/enrollments/{id} [get]

//	@Summary		Replace enrollment
//	@ID				update-enrollment
//	@Description	Replaces an enrollment. personId and offeringId must be the ids of an existing person and offering. The path id wins over any id in the body; createdAt is preserved and an empty sourcedId keeps the stored one.
//	@Tags			enrollments
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id			path		string					true	"Enrollment id"
//	@Param			enrollment	body		domain.Enrollment		true	"Replacement enrollment"
//	@Success		200			{object}	domain.Enrollment		"Success"
//	@Failure		400			{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401			{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403			{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404			{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		409			{object}	helpers.ErrorResponse	"sourcedId already in use"
//	@Failure		413			{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500			{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/enrollments/{id} [put]

//	@Summary		Delete enrollment
//	@ID				delete-enrollment
//	@Description	Deletes one enrollment by id.
//	@Tags			enrollments
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Enrollment id"
//	@Success		204	"No Content"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/enrollments/{id} [delete]

// -- programs -----------------------------------------------------------------

//	@Summary		List programs
//	@ID				list-programs
//	@Description	Lists programs with limit/offset paging.
//	@Tags			programs
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int									false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int									false	"Number of items to skip"											minimum(0)
//	@Success		200		{object}	restadmin.ListReply[domain.Program]	"Success"
//	@Failure		400		{object}	helpers.ErrorResponse				"Invalid paging parameters"
//	@Failure		401		{object}	auth.ErrorResponse					"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse					"Insufficient scope or role"
//	@Failure		500		{object}	helpers.ErrorResponse				"Internal Server Error"
//	@Router			/programs [get]

//	@Summary		Create program
//	@ID				create-program
//	@Description	Creates a program. title is required; orgId, when set, must be the id (not sourcedId) of an existing organization. id is generated when empty, sourcedId defaults to id, and createdAt/updatedAt are set by the server.
//	@Tags			programs
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			program	body		domain.Program			true	"Program to create"
//	@Success		201		{object}	domain.Program			"Created"
//	@Failure		400		{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		409		{object}	helpers.ErrorResponse	"id or sourcedId already in use"
//	@Failure		413		{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/programs [post]

//	@Summary		Get program
//	@ID				get-program
//	@Description	Returns one program by id.
//	@Tags			programs
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Program id"
//	@Success		200	{object}	domain.Program			"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/programs/{id} [get]

//	@Summary		Replace program
//	@ID				update-program
//	@Description	Replaces a program. orgId, when set, must be the id of an existing organization. The path id wins over any id in the body; createdAt is preserved and an empty sourcedId keeps the stored one.
//	@Tags			programs
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Program id"
//	@Param			program	body		domain.Program			true	"Replacement program"
//	@Success		200		{object}	domain.Program			"Success"
//	@Failure		400		{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404		{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		409		{object}	helpers.ErrorResponse	"sourcedId already in use"
//	@Failure		413		{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/programs/{id} [put]

//	@Summary		Delete program
//	@ID				delete-program
//	@Description	Deletes one program by id.
//	@Tags			programs
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Program id"
//	@Success		204	"No Content"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/programs/{id} [delete]

// -- results ------------------------------------------------------------------

//	@Summary		List results
//	@ID				list-results
//	@Description	Lists results with limit/offset paging.
//	@Tags			results
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int									false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int									false	"Number of items to skip"											minimum(0)
//	@Success		200		{object}	restadmin.ListReply[domain.Result]	"Success"
//	@Failure		400		{object}	helpers.ErrorResponse				"Invalid paging parameters"
//	@Failure		401		{object}	auth.ErrorResponse					"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse					"Insufficient scope or role"
//	@Failure		500		{object}	helpers.ErrorResponse				"Internal Server Error"
//	@Router			/results [get]

//	@Summary		Create result
//	@ID				create-result
//	@Description	Creates a result. personId is required, and exactly one of courseId and programId; offeringId is optional. All must be the ids (not sourcedIds) of existing records. id is generated when empty, sourcedId defaults to id, and createdAt/updatedAt are set by the server.
//	@Tags			results
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			result	body		domain.Result			true	"Result to create"
//	@Success		201		{object}	domain.Result			"Created"
//	@Failure		400		{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		409		{object}	helpers.ErrorResponse	"id or sourcedId already in use"
//	@Failure		413		{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/results [post]

//	@Summary		Get result
//	@ID				get-result
//	@Description	Returns one result by id.
//	@Tags			results
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Result id"
//	@Success		200	{object}	domain.Result			"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/results/{id} [get]

//	@Summary		Replace result
//	@ID				update-result
//	@Description	Replaces a result. References must be the ids of existing records. The path id wins over any id in the body; createdAt is preserved and an empty sourcedId keeps the stored one.
//	@Tags			results
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Result id"
//	@Param			result	body		domain.Result			true	"Replacement result"
//	@Success		200		{object}	domain.Result			"Success"
//	@Failure		400		{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404		{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		409		{object}	helpers.ErrorResponse	"sourcedId already in use"
//	@Failure		413		{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/results/{id} [put]

//	@Summary		Delete result
//	@ID				delete-result
//	@Description	Deletes one result by id.
//	@Tags			results
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Result id"
//	@Success		204	"No Content"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/results/{id} [delete]
