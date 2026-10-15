package domain_test

import (
	"errors"
	"testing"

	"github.com/SUNET/sis/internal/core/domain"
)

func TestStatusValid(t *testing.T) {
	for _, s := range []domain.Status{domain.StatusActive, domain.StatusInactive, domain.StatusToBeDeleted} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []domain.Status{"", "archived", "Active"} {
		if s.Valid() {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestPersonRejectsUndeclaredStatus(t *testing.T) {
	p := domain.Person{GivenName: "Ada", FamilyName: "Lovelace", Status: "archived"}
	var verr *domain.ValidationError
	if err := p.Validate(); !errors.As(err, &verr) || verr.Field != "status" {
		t.Fatalf("expected status validation error, got %v", err)
	}
}

func TestEntityStatusOptionalButChecked(t *testing.T) {
	var verr *domain.ValidationError
	bad := &domain.Organization{Name: "X", Base: domain.Base{Status: "bogus"}}
	if err := bad.Validate(); !errors.As(err, &verr) || verr.Field != "status" {
		t.Fatalf("expected status validation error, got %v", err)
	}
	if err := (&domain.Organization{Name: "Y"}).Validate(); err != nil {
		t.Fatalf("empty entity status must stay optional: %v", err)
	}
}
