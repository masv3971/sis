package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/core/domain"
)

func TestValidSourcedID(t *testing.T) {
	for _, ok := range []string{"", "bio202601", "liu:anna.andersson", "urn:epass:person:x", "a b", "550e8400-e29b-41d4-a716-446655440000"} {
		assert.True(t, domain.ValidSourcedID(ok), "%q", ok)
	}
	for _, bad := range []string{"åsa", "bio\x00", "tab\there", "del\x7f", "日本"} {
		assert.False(t, domain.ValidSourcedID(bad), "%q", bad)
	}
}

func TestNormalizeSourcedID(t *testing.T) {
	org := &domain.Organization{Base: domain.Base{SourcedID: "BIO202601"}}
	require.NoError(t, domain.NormalizeSourcedID(org))
	assert.Equal(t, "bio202601", org.SourcedID)

	empty := &domain.Organization{}
	require.NoError(t, domain.NormalizeSourcedID(empty), "an empty sourcedId is left for the caller to default or reject")
	assert.Empty(t, empty.SourcedID)

	bad := &domain.Organization{Base: domain.Base{SourcedID: "Åsa"}}
	err := domain.NormalizeSourcedID(bad)
	var ve *domain.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, "sourcedId", ve.Field)
	assert.Equal(t, "Åsa", bad.SourcedID, "a rejected value is not changed")
}
