package apidoc

import _ "github.com/SUNET/sis/pkg/oeapi"

// This file holds the swag annotations for the OEAPI document (`make
// swagger`, instance "oeapi"). The generic List and Get handlers are mounted
// once per entity type, so one doc comment cannot describe them; every
// operation gets its own free-standing block here instead, which
// `swag init --parseFuncBody` picks up.
//
// Keep these blocks in step with the /oeapi routes in internal/httpserver.

//	@title			SIS Open Education API (OEAPI) v6.0
//	@version		6.0
//	@description	Read-only publication of the SIS data in Open Education API v6.0 shapes (/oeapi).
//	@description
//	@description	Every endpoint needs a Bearer access token with scope sis.read from one of
//	@description	the token issuers SIS trusts (sis.auth.issuers).
//	@description
//	@description				Lists use the OEAPI page envelope and pageNumber/pageSize. A record
//	@description				is addressed by its sourcedId, and references between records are
//	@description				sourcedIds, so every relationship can be followed through this API.
//	@description				sort and filter are not supported and answer 400.
//	@license.name				BSD-2-Clause
//	@license.url				https://opensource.org/license/bsd-2-clause
//	@BasePath					/oeapi
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
//	@tag.description			Courses scheduled in an academic session
//	@tag.name					associations
//	@tag.description			A person's participation in an offering, with the person's result nested
//	@tag.name					programs
//	@tag.description			Structured collections of courses leading to a qualification
//	@tag.name					results
//	@tag.description			A person's grade and credits for an offering, course or program

// -- persons ------------------------------------------------------------------

//	@Summary		List persons
//	@ID				list-persons
//	@Description	Lists persons in the OEAPI page envelope.
//	@Tags			persons
//	@Produce		json
//	@Security		BearerAuth
//	@Param			pageNumber	query		int							false	"1-based page number"														minimum(1)
//	@Param			pageSize	query		int							false	"Page size; defaults to default_page_size and is clamped to max_page_size"	minimum(1)
//	@Success		200			{object}	oeapi.Page[oeapi.Person]	"Success"
//	@Failure		400			{object}	helpers.ErrorResponse		"Invalid paging parameters, or sort/filter given"
//	@Failure		401			{object}	auth.ErrorResponse			"Missing, invalid or untrusted token"
//	@Failure		403			{object}	auth.ErrorResponse			"Token lacks the required scope"
//	@Failure		500			{object}	helpers.ErrorResponse		"Internal Server Error"
//	@Router			/persons [get]

//	@Summary		Get person
//	@ID				get-person
//	@Description	Returns one person by sourcedId.
//	@Tags			persons
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Person sourcedId"
//	@Success		200	{object}	oeapi.Person			"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing, invalid or untrusted token"
//	@Failure		403	{object}	auth.ErrorResponse		"Token lacks the required scope"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/persons/{id} [get]

// -- organizations ------------------------------------------------------------

//	@Summary		List organizations
//	@ID				list-organizations
//	@Description	Lists organizations in the OEAPI page envelope.
//	@Tags			organizations
//	@Produce		json
//	@Security		BearerAuth
//	@Param			pageNumber	query		int								false	"1-based page number"														minimum(1)
//	@Param			pageSize	query		int								false	"Page size; defaults to default_page_size and is clamped to max_page_size"	minimum(1)
//	@Success		200			{object}	oeapi.Page[oeapi.Organization]	"Success"
//	@Failure		400			{object}	helpers.ErrorResponse			"Invalid paging parameters, or sort/filter given"
//	@Failure		401			{object}	auth.ErrorResponse				"Missing, invalid or untrusted token"
//	@Failure		403			{object}	auth.ErrorResponse				"Token lacks the required scope"
//	@Failure		500			{object}	helpers.ErrorResponse			"Internal Server Error"
//	@Router			/organizations [get]

//	@Summary		Get organization
//	@ID				get-organization
//	@Description	Returns one organization by sourcedId.
//	@Tags			organizations
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Organization sourcedId"
//	@Success		200	{object}	oeapi.Organization		"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing, invalid or untrusted token"
//	@Failure		403	{object}	auth.ErrorResponse		"Token lacks the required scope"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/organizations/{id} [get]

// -- academic-sessions --------------------------------------------------------

//	@Summary		List academic sessions
//	@ID				list-academic-sessions
//	@Description	Lists academic sessions in the OEAPI page envelope.
//	@Tags			academic-sessions
//	@Produce		json
//	@Security		BearerAuth
//	@Param			pageNumber	query		int									false	"1-based page number"														minimum(1)
//	@Param			pageSize	query		int									false	"Page size; defaults to default_page_size and is clamped to max_page_size"	minimum(1)
//	@Success		200			{object}	oeapi.Page[oeapi.AcademicSession]	"Success"
//	@Failure		400			{object}	helpers.ErrorResponse				"Invalid paging parameters, or sort/filter given"
//	@Failure		401			{object}	auth.ErrorResponse					"Missing, invalid or untrusted token"
//	@Failure		403			{object}	auth.ErrorResponse					"Token lacks the required scope"
//	@Failure		500			{object}	helpers.ErrorResponse				"Internal Server Error"
//	@Router			/academic-sessions [get]

//	@Summary		Get academic session
//	@ID				get-academic-session
//	@Description	Returns one academic session by sourcedId.
//	@Tags			academic-sessions
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Academic session sourcedId"
//	@Success		200	{object}	oeapi.AcademicSession	"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing, invalid or untrusted token"
//	@Failure		403	{object}	auth.ErrorResponse		"Token lacks the required scope"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/academic-sessions/{id} [get]

// -- courses ------------------------------------------------------------------

//	@Summary		List courses
//	@ID				list-courses
//	@Description	Lists courses in the OEAPI page envelope.
//	@Tags			courses
//	@Produce		json
//	@Security		BearerAuth
//	@Param			pageNumber	query		int							false	"1-based page number"														minimum(1)
//	@Param			pageSize	query		int							false	"Page size; defaults to default_page_size and is clamped to max_page_size"	minimum(1)
//	@Success		200			{object}	oeapi.Page[oeapi.Course]	"Success"
//	@Failure		400			{object}	helpers.ErrorResponse		"Invalid paging parameters, or sort/filter given"
//	@Failure		401			{object}	auth.ErrorResponse			"Missing, invalid or untrusted token"
//	@Failure		403			{object}	auth.ErrorResponse			"Token lacks the required scope"
//	@Failure		500			{object}	helpers.ErrorResponse		"Internal Server Error"
//	@Router			/courses [get]

//	@Summary		Get course
//	@ID				get-course
//	@Description	Returns one course by sourcedId.
//	@Tags			courses
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Course sourcedId"
//	@Success		200	{object}	oeapi.Course			"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing, invalid or untrusted token"
//	@Failure		403	{object}	auth.ErrorResponse		"Token lacks the required scope"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/courses/{id} [get]

// -- offerings ----------------------------------------------------------------

//	@Summary		List offerings
//	@ID				list-offerings
//	@Description	Lists offerings in the OEAPI page envelope.
//	@Tags			offerings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			pageNumber	query		int							false	"1-based page number"														minimum(1)
//	@Param			pageSize	query		int							false	"Page size; defaults to default_page_size and is clamped to max_page_size"	minimum(1)
//	@Success		200			{object}	oeapi.Page[oeapi.Offering]	"Success"
//	@Failure		400			{object}	helpers.ErrorResponse		"Invalid paging parameters, or sort/filter given"
//	@Failure		401			{object}	auth.ErrorResponse			"Missing, invalid or untrusted token"
//	@Failure		403			{object}	auth.ErrorResponse			"Token lacks the required scope"
//	@Failure		500			{object}	helpers.ErrorResponse		"Internal Server Error"
//	@Router			/offerings [get]

//	@Summary		Get offering
//	@ID				get-offering
//	@Description	Returns one offering by sourcedId.
//	@Tags			offerings
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Offering sourcedId"
//	@Success		200	{object}	oeapi.Offering			"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing, invalid or untrusted token"
//	@Failure		403	{object}	auth.ErrorResponse		"Token lacks the required scope"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/offerings/{id} [get]

// -- associations -------------------------------------------------------------

//	@Summary		List associations
//	@ID				list-associations
//	@Description	Lists enrollments as associations, each with the person's result in the offering nested.
//	@Tags			associations
//	@Produce		json
//	@Security		BearerAuth
//	@Param			pageNumber	query		int								false	"1-based page number"														minimum(1)
//	@Param			pageSize	query		int								false	"Page size; defaults to default_page_size and is clamped to max_page_size"	minimum(1)
//	@Success		200			{object}	oeapi.Page[oeapi.Association]	"Success"
//	@Failure		400			{object}	helpers.ErrorResponse			"Invalid paging parameters, or sort/filter given"
//	@Failure		401			{object}	auth.ErrorResponse				"Missing, invalid or untrusted token"
//	@Failure		403			{object}	auth.ErrorResponse				"Token lacks the required scope"
//	@Failure		500			{object}	helpers.ErrorResponse			"Internal Server Error"
//	@Router			/associations [get]

//	@Summary		Get association
//	@ID				get-association
//	@Description	Returns one enrollment by sourcedId as an association, with its result nested.
//	@Tags			associations
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Association (enrollment) sourcedId"
//	@Success		200	{object}	oeapi.Association		"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing, invalid or untrusted token"
//	@Failure		403	{object}	auth.ErrorResponse		"Token lacks the required scope"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/associations/{id} [get]

// -- programs -----------------------------------------------------------------

//	@Summary		List programs
//	@ID				list-programs
//	@Description	Lists programs in the OEAPI page envelope.
//	@Tags			programs
//	@Produce		json
//	@Security		BearerAuth
//	@Param			pageNumber	query		int							false	"1-based page number"														minimum(1)
//	@Param			pageSize	query		int							false	"Page size; defaults to default_page_size and is clamped to max_page_size"	minimum(1)
//	@Success		200			{object}	oeapi.Page[oeapi.Program]	"Success"
//	@Failure		400			{object}	helpers.ErrorResponse		"Invalid paging parameters, or sort/filter given"
//	@Failure		401			{object}	auth.ErrorResponse			"Missing, invalid or untrusted token"
//	@Failure		403			{object}	auth.ErrorResponse			"Token lacks the required scope"
//	@Failure		500			{object}	helpers.ErrorResponse		"Internal Server Error"
//	@Router			/programs [get]

//	@Summary		Get program
//	@ID				get-program
//	@Description	Returns one program by sourcedId.
//	@Tags			programs
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Program sourcedId"
//	@Success		200	{object}	oeapi.Program			"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing, invalid or untrusted token"
//	@Failure		403	{object}	auth.ErrorResponse		"Token lacks the required scope"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/programs/{id} [get]

// -- results ------------------------------------------------------------------

//	@Summary		List results
//	@ID				list-results
//	@Description	Lists every result as its own resource, including program results, which have no association.
//	@Tags			results
//	@Produce		json
//	@Security		BearerAuth
//	@Param			pageNumber	query		int							false	"1-based page number"														minimum(1)
//	@Param			pageSize	query		int							false	"Page size; defaults to default_page_size and is clamped to max_page_size"	minimum(1)
//	@Success		200			{object}	oeapi.Page[oeapi.Result]	"Success"
//	@Failure		400			{object}	helpers.ErrorResponse		"Invalid paging parameters, or sort/filter given"
//	@Failure		401			{object}	auth.ErrorResponse			"Missing, invalid or untrusted token"
//	@Failure		403			{object}	auth.ErrorResponse			"Token lacks the required scope"
//	@Failure		500			{object}	helpers.ErrorResponse		"Internal Server Error"
//	@Router			/results [get]

//	@Summary		Get result
//	@ID				get-result
//	@Description	Returns one result by sourcedId.
//	@Tags			results
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Result sourcedId"
//	@Success		200	{object}	oeapi.Result			"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing, invalid or untrusted token"
//	@Failure		403	{object}	auth.ErrorResponse		"Token lacks the required scope"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/results/{id} [get]
