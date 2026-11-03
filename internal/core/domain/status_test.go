package domain_test

import (
	"testing"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusValid(t *testing.T) {
	for _, status := range []domain.Status{domain.StatusActive, domain.StatusInactive, domain.StatusToBeDeleted} {
		assert.True(t, status.Valid(), "%q should be valid", status)
	}
	for _, status := range []domain.Status{"", "archived", "Active"} {
		assert.False(t, status.Valid(), "%q should be invalid", status)
	}
}

func TestPersonRejectsUndeclaredStatus(t *testing.T) {
	person := domain.Person{GivenName: "Ada", FamilyName: "Lovelace", Status: "archived"}
	var verr *domain.ValidationError
	require.ErrorAs(t, person.Validate(), &verr, "expected status validation error")
	assert.Equal(t, "status", verr.Field)
}

func TestEntityStatusOptionalButChecked(t *testing.T) {
	var verr *domain.ValidationError
	bad := &domain.Organization{Name: "X", Status: "bogus"}
	if assert.ErrorAs(t, bad.Validate(), &verr, "expected status validation error") {
		assert.Equal(t, "status", verr.Field)
	}
	assert.NoError(t, (&domain.Organization{Name: "Y"}).Validate(), "empty entity status must stay optional")
}
