package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/core/domain"
)

// TestPersonDateOfBirth checks dateOfBirth is optional but, when given, an
// ISO 8601 calendar date that exists.
func TestPersonDateOfBirth(t *testing.T) {
	valid := []string{"", "2001-03-14", "2000-02-29", "1900-01-01"}
	invalid := []string{
		"14/03/2001", "2001-3-14", "20010314", "2001-03-14T00:00:00Z",
		"2001-02-29", "2001-13-01", "2001-04-31", " 2001-03-14", "yesterday",
	}
	person := func(dob string) domain.Person {
		return domain.Person{GivenName: "Ada", FamilyName: "Lovelace", Status: domain.StatusActive, DateOfBirth: dob}
	}
	for _, dob := range valid {
		assert.NoError(t, person(dob).Validate(), "%q", dob)
	}
	for _, dob := range invalid {
		var verr *domain.ValidationError
		require.ErrorAs(t, person(dob).Validate(), &verr, "%q", dob)
		assert.Equal(t, "dateOfBirth", verr.Field, "%q", dob)
		assert.Equal(t, domain.MsgISODate, verr.Msg, "%q", dob)
	}
}
