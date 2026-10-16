package domain

import (
	"errors"
	"testing"
)

func TestProgramValidate(t *testing.T) {
	if err := (&Program{}).Validate(); !isField(err, "title") {
		t.Fatalf("missing title: %v", err)
	}
	if err := (&Program{Title: "BSc"}).Validate(); err != nil {
		t.Fatalf("valid program: %v", err)
	}
}

func TestResultValidate(t *testing.T) {
	cases := []struct {
		name  string
		r     Result
		field string // "" means valid
	}{
		{"no person", Result{CourseID: "c"}, "personId"},
		{"neither target", Result{PersonID: "p"}, "courseId"},
		{"both targets", Result{PersonID: "p", CourseID: "c", ProgramID: "g"}, "courseId"},
		{"negative credits", Result{PersonID: "p", CourseID: "c", Credits: -1}, "credits"},
		{"course", Result{PersonID: "p", CourseID: "c", Credits: 7.5}, ""},
		{"program", Result{PersonID: "p", ProgramID: "g"}, ""},
	}
	for _, c := range cases {
		err := c.r.Validate()
		if c.field == "" && err != nil {
			t.Errorf("%s: unexpected %v", c.name, err)
		}
		if c.field != "" && !isField(err, c.field) {
			t.Errorf("%s: want field %s, got %v", c.name, c.field, err)
		}
	}
}

func isField(err error, field string) bool {
	var ve *ValidationError
	return errors.As(err, &ve) && ve.Field == field
}
