// Package fileimport loads records from JSON files at startup
// (sis.file_import.files), e.g. fake students for development and demos.
//
// A file is one JSON object with an array per record type:
//
//	{
//	  "organizations":    [{"sourcedId": "liu", "name": "Linköping University", "type": "institution"}],
//	  "academicSessions": [{"sourcedId": "ht2026", "title": "Autumn 2026", "type": "semester"}],
//	  "courses":          [{"sourcedId": "TDDE01", "title": "Machine Learning", "orgId": "liu"}],
//	  "programs":         [...],
//	  "offerings":        [{"sourcedId": "TDDE01-ht2026", "courseId": "TDDE01", "academicSessionId": "ht2026"}],
//	  "persons":          [{"sourcedId": "anna", "givenName": "Anna", "familyName": "Andersson", "status": "active"}],
//	  "enrollments":      [{"sourcedId": "anna-TDDE01", "personId": "anna", "offeringId": "TDDE01-ht2026"}],
//	  "results":          [...]
//	}
//
// Records use the /api/v1 field names, with two differences: every record
// needs a sourcedId, and references (orgId, courseId, personId, ...) are
// sourcedIds, not ids. A reference must name a record in the same file or one
// already stored. Ids and timestamps are assigned by SIS.
//
// Every record is upserted by sourcedId and never expires, so importing the
// same file again changes nothing, and on every start the file wins over
// changes made to its records through the API.
package fileimport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// File is the import file format.
type File struct {
	Organizations    []*domain.Organization    `json:"organizations"`
	AcademicSessions []*domain.AcademicSession `json:"academicSessions"`
	Courses          []*domain.Course          `json:"courses"`
	Programs         []*domain.Program         `json:"programs"`
	Offerings        []*domain.Offering        `json:"offerings"`
	Persons          []domain.Person           `json:"persons"`
	Enrollments      []*domain.Enrollment      `json:"enrollments"`
	Results          []*domain.Result          `json:"results"`
}

// Run imports every file in cfg.SIS.FileImport.Files, in order. The first
// failure stops it and is returned; earlier files stay imported.
func Run(ctx context.Context, cfg *model.Cfg, catalog *readmodel.Client, log *logger.Log) error {
	log = log.New("fileimport")
	for _, path := range cfg.SIS.FileImport.Files {
		n, err := Import(ctx, catalog, path)
		if err != nil {
			return fmt.Errorf("file import %s: %w", path, err)
		}
		log.Info("imported file", "path", path, "records", n)
	}
	return nil
}

// Import loads one file and returns how many records it stored.
func Import(ctx context.Context, catalog *readmodel.Client, path string) (int, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from operator config
	if err != nil {
		return 0, err
	}
	b, err := Decode(data)
	if err != nil {
		return 0, err
	}
	items, err := catalog.Populate(ctx, b, nil)
	return len(items), err
}

// Decode parses an import file into a bundle ready for readmodel.Populate.
// Unknown fields are rejected, so a typo fails instead of being dropped.
func Decode(data []byte) (*domain.Bundle, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return nil, errors.New("parse: more than one JSON value")
	}
	// Ids are SIS's to assign; a file cannot pin them. (Populate already
	// clears entity ids, and the services set timestamps and expiry.)
	for i := range f.Persons {
		f.Persons[i].ID = ""
	}
	return &domain.Bundle{
		Persons:          f.Persons,
		Organizations:    f.Organizations,
		AcademicSessions: f.AcademicSessions,
		Courses:          f.Courses,
		Programs:         f.Programs,
		Offerings:        f.Offerings,
		Enrollments:      f.Enrollments,
		Results:          f.Results,
	}, nil
}
