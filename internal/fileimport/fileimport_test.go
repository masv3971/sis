package fileimport

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
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
	ctx := context.Background()
	c := newCatalog()
	n, err := Import(ctx, c, fakeStudents)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	// 4 organizations, 2 sessions, 5 courses, 2 programs, 5 offerings,
	// 10 persons, 9 enrollments, 8 results.
	if n != 45 {
		t.Fatalf("records = %d, want 45", n)
	}

	persons, err := c.Persons.ListPersons(ctx, port.ListParams{})
	if err != nil || persons.Total != 10 {
		t.Fatalf("persons = %d, %v", persons.Total, err)
	}
	anna, err := c.Persons.GetPersonBySourcedID(ctx, "liu:anna.andersson")
	if err != nil || anna.ID == "" || anna.ID == anna.SourcedID || len(anna.Identifiers) != 2 {
		t.Fatalf("anna = %+v, %v", anna, err)
	}

	// References are stored as canonical ids and publish back as sourcedIds.
	r, err := c.Results.GetBySourcedID(ctx, "liu:anna.andersson:TDDE01")
	if err != nil {
		t.Fatal(err)
	}
	if r.PersonID != anna.ID {
		t.Fatalf("result personId = %q, want anna's id %q", r.PersonID, anna.ID)
	}
	if err := c.CheckRefs(ctx, r); err != nil {
		t.Fatalf("CheckRefs: %v", err)
	}
	if _, err := c.PublishRefs(ctx, r); err != nil || r.PersonID != "liu:anna.andersson" || r.CourseID != "liu:TDDE01" {
		t.Fatalf("published result = %+v, %v", r, err)
	}
	dept, err := c.Organizations.GetBySourcedID(ctx, "liu-ida")
	if err != nil || dept.ParentID == "" {
		t.Fatalf("dept = %+v, %v", dept, err)
	}

	// Imported records are permanent.
	if anna.ExpiresAt != nil || r.ExpiresAt != nil {
		t.Fatal("imported records must not expire")
	}

	// Importing again converges on the same records.
	if _, err := Import(ctx, c, fakeStudents); err != nil {
		t.Fatalf("second Import: %v", err)
	}
	again, _ := c.Persons.GetPersonBySourcedID(ctx, "liu:anna.andersson")
	if again.ID != anna.ID {
		t.Fatalf("id changed on re-import: %q -> %q", anna.ID, again.ID)
	}
	if persons, _ := c.Persons.ListPersons(ctx, port.ListParams{}); persons.Total != 10 {
		t.Fatalf("persons after re-import = %d", persons.Total)
	}
}

func TestDecodeRejects(t *testing.T) {
	cases := map[string]string{
		"unknown field":   `{"students": []}`,
		"unknown in item": `{"persons": [{"sourcedId": "a", "givenName": "A", "familyName": "B", "status": "active", "shoeSize": 42}]}`,
		"two values":      `{} {}`,
		"not json":        `persons:`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(body)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// TestImportStoresNothingOnBadReference checks a file with a reference to a
// record that exists nowhere fails before anything is stored.
func TestImportStoresNothingOnBadReference(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bad.json")
	body := `{
	  "persons": [{"sourcedId": "a", "givenName": "A", "familyName": "B", "status": "active"}],
	  "enrollments": [{"sourcedId": "e", "personId": "a", "offeringId": "nowhere"}]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newCatalog()
	_, err := Import(ctx, c, path)
	if err == nil || !strings.Contains(err.Error(), "nowhere") {
		t.Fatalf("expected an unknown-reference error, got %v", err)
	}
	if p, _ := c.Persons.ListPersons(ctx, port.ListParams{}); p.Total != 0 {
		t.Fatalf("stored %d persons from a rejected file", p.Total)
	}
}

func TestRunNamesFailingFile(t *testing.T) {
	cfg := &model.Cfg{}
	cfg.SIS.FileImport.Files = []string{fakeStudents, "/no/such/file.json"}
	err := Run(context.Background(), cfg, newCatalog(), logger.NewWithWriter("test", false, "error", io.Discard))
	if err == nil || !strings.Contains(err.Error(), "/no/such/file.json") {
		t.Fatalf("expected error naming the missing file, got %v", err)
	}
}
