package fileimport

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fakeStudents = "../../testdata/fake_students.json"

func newCatalog() *readmodel.Client {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	return &readmodel.Client{
		Persons:          service.New(memory.NewPersonRepository(), nil, nil, log),
		Organizations:    service.NewResource(memory.NewDocRepo[domain.Organization](), nil, nil, log),
		AcademicSessions: service.NewResource(memory.NewDocRepo[domain.AcademicSession](), nil, nil, log),
		Courses:          service.NewResource(memory.NewDocRepo[domain.Course](), nil, nil, log),
		Offerings:        service.NewResource(memory.NewDocRepo[domain.Offering](), nil, nil, log),
		Enrollments:      service.NewResource(memory.NewDocRepo[domain.Enrollment](), nil, nil, log),
		Programs:         service.NewResource(memory.NewDocRepo[domain.Program](), nil, nil, log),
		Results:          service.NewResource(memory.NewDocRepo[domain.Result](), nil, nil, log),
	}
}

// TestFakeStudentsImport loads the shipped fake-students file, checks the
// records and their references, and that a second import changes nothing.
func TestFakeStudentsImport(t *testing.T) {
	ctx := t.Context()
	catalog := newCatalog()
	count, err := Import(ctx, catalog, fakeStudents)
	require.NoError(t, err, "Import")
	// 4 organizations, 2 sessions, 5 courses, 2 programs, 5 offerings,
	// 10 persons, 9 enrollments, 8 results.
	require.Equal(t, 45, count, "records")

	persons, err := catalog.Persons.ListPersons(ctx, port.ListParams{})
	require.NoError(t, err)
	require.Equal(t, 10, persons.Total, "persons")
	anna, err := catalog.Persons.GetPersonBySourcedID(ctx, "liu:anna.andersson")
	require.NoError(t, err)
	require.NotEmpty(t, anna.ID, "anna id")
	require.NotEqual(t, anna.SourcedID, anna.ID, "anna id must differ from sourcedId")
	require.Len(t, anna.Identifiers, 2, "anna identifiers")

	// References are stored as canonical ids and publish back as sourcedIds.
	result, err := catalog.Results.GetBySourcedID(ctx, "liu:anna.andersson:TDDE01")
	require.NoError(t, err)
	require.Equal(t, anna.ID, result.PersonID, "result personId must be anna's id")
	require.NoError(t, catalog.CheckRefs(ctx, result), "CheckRefs")
	_, err = catalog.PublishRefs(ctx, result)
	require.NoError(t, err, "PublishRefs")
	require.Equal(t, "liu:anna.andersson", result.PersonID, "published personId")
	require.Equal(t, "liu:TDDE01", result.CourseID, "published courseId")
	dept, err := catalog.Organizations.GetBySourcedID(ctx, "liu-ida")
	require.NoError(t, err)
	require.NotEmpty(t, dept.ParentID, "dept parentId")

	// Imported records are permanent.
	require.Nil(t, anna.ExpiresAt, "imported records must not expire")
	require.Nil(t, result.ExpiresAt, "imported records must not expire")

	// Importing again converges on the same records.
	_, err = Import(ctx, catalog, fakeStudents)
	require.NoError(t, err, "second Import")
	again, _ := catalog.Persons.GetPersonBySourcedID(ctx, "liu:anna.andersson")
	assert.Equal(t, anna.ID, again.ID, "id changed on re-import")
	persons, _ = catalog.Persons.ListPersons(ctx, port.ListParams{})
	assert.Equal(t, 10, persons.Total, "persons after re-import")
}

func TestDecodeRejects(t *testing.T) {
	cases := map[string]string{
		"unknown field":   `{"students": []}`,
		"unknown in item": `{"persons": [{"sourcedId": "a", "givenName": "A", "familyName": "B", "status": "active", "shoeSize": 42}]}`,
		"two values":      `{} {}`,
		"not json":        `persons:`,
		"null org":        `{"organizations": [null]}`,
		"null course":     `{"courses": [null]}`,
		"null result":     `{"results": [{"sourcedId": "r"}, null]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(body))
			assert.Error(t, err)
		})
	}
}

// TestImportStoresNothingOnBadReference checks a file with a reference to a
// record that exists nowhere fails before anything is stored.
func TestImportStoresNothingOnBadReference(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "bad.json")
	body := `{
	  "persons": [{"sourcedId": "a", "givenName": "A", "familyName": "B", "status": "active"}],
	  "enrollments": [{"sourcedId": "e", "personId": "a", "offeringId": "nowhere"}]
	}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	catalog := newCatalog()
	_, err := Import(ctx, catalog, path)
	require.ErrorContains(t, err, "nowhere", "expected an unknown-reference error")
	page, _ := catalog.Persons.ListPersons(ctx, port.ListParams{})
	assert.Zero(t, page.Total, "stored persons from a rejected file")
}

func TestRunNamesFailingFile(t *testing.T) {
	cfg := &model.Cfg{}
	cfg.SIS.FileImport.Files = []string{fakeStudents, "/no/such/file.json"}
	err := Run(t.Context(), cfg, newCatalog(), logger.NewWithWriter("test", false, "error", io.Discard))
	assert.ErrorContains(t, err, "/no/such/file.json", "expected error naming the missing file")
}
