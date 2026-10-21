package elm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextForms(t *testing.T) {
	doc, err := Parse([]byte(`{
		"plain": " Ada ",
		"lang": {"sv": ["Kurs"], "en": ["Course"]},
		"langOne": {"sv": "Kurs"},
		"value": {"@value": "V", "@language": "en"},
		"arr": ["", "second"],
		"num": 12345678901234567890
	}`), "data")
	require.NoError(t, err)
	root := doc.Root
	cases := map[string]string{"plain": "Ada", "lang": "Course", "langOne": "Kurs", "value": "V", "arr": "second", "num": "12345678901234567890", "missing": ""}
	for prop, want := range cases {
		assert.Equal(t, want, root.Text(prop), "Text(%s)", prop)
	}
	doc.Languages = []string{"sv", "en"}
	assert.Equal(t, "Kurs", root.Text("lang"), "language preference not applied")
}

func TestNodesResolveReferences(t *testing.T) {
	doc, err := Parse([]byte(`{
		"issuer": "urn:org:1",
		"credentialSubject": {"hasClaim": {"id": "urn:a:1", "type": "LearningAchievement",
			"awardedBy": {"awardingBody": [{"id": "urn:org:1"}, "urn:org:missing"]}}},
		"defs": [{"id": "urn:org:1", "type": "Organisation", "legalName": {"en": "Uni"}}]
	}`), "data")
	require.NoError(t, err)
	issuer := doc.Root.Node("issuer")
	require.NotNil(t, issuer, "string reference not resolved")
	require.Equal(t, "Uni", issuer.Text("legalName"), "string reference not resolved")
	require.True(t, issuer.Is("Organisation"), "string reference not resolved: %+v", issuer)
	claims := doc.Root.Node("credentialSubject").Nodes("hasClaim")
	require.Len(t, claims, 1, "single value must be one node")
	require.Equal(t, "data.credentialSubject.hasClaim", claims[0].Path)
	bodies := claims[0].Node("awardedBy").Nodes("awardingBody")
	require.Len(t, bodies, 2, "stub/missing references")
	require.Equal(t, "Uni", bodies[0].Text("legalName"), "resolved reference")
	require.Equal(t, "urn:org:missing", bodies[1].ID(), "missing reference stub")
	assert.Equal(t, "data.credentialSubject.hasClaim.awardedBy.awardingBody[1]", bodies[1].Path)
}

// TestNodesCompleteTypedReferences checks an inline reference carrying more
// than its id (e.g. its type) is completed from the indexed definition,
// keeping its own inline properties, without altering the document.
func TestNodesCompleteTypedReferences(t *testing.T) {
	doc, err := Parse([]byte(`{
		"issuer": {"id": "urn:org:1", "type": "Organisation", "altLabel": "U"},
		"defs": [{"id": "urn:org:1", "type": "Organisation", "legalName": {"en": "Uni"}, "altLabel": "Full",
			"parentOrganisation": {"id": "urn:org:0", "type": "Organisation"}},
			{"id": "urn:org:0", "type": "Organisation", "legalName": "Root"}]
	}`), "data")
	require.NoError(t, err)
	issuer := doc.Root.Node("issuer")
	require.NotNil(t, issuer, "typed reference not completed")
	require.Equal(t, "Uni", issuer.Text("legalName"), "typed reference not completed")
	require.True(t, issuer.Is("Organisation"), "typed reference not completed: %+v", issuer)
	assert.Equal(t, "U", issuer.Text("altLabel"), "inline property must win over the indexed one")
	if parent := issuer.Node("parentOrganisation"); assert.NotNil(t, parent, "nested typed reference not completed") {
		assert.Equal(t, "Root", parent.Text("legalName"), "nested typed reference not completed")
	}
	defs := doc.Root.Nodes("defs")
	require.NotEmpty(t, defs)
	assert.Equal(t, "Full", defs[0].Text("altLabel"), "indexed definition was modified")
}

func TestParseRejectsNonObject(t *testing.T) {
	_, err := Parse([]byte(`[1]`), "data")
	assert.Error(t, err, "array root must be rejected")
	_, err = Parse([]byte(`{`), "data")
	assert.Error(t, err, "malformed JSON must be rejected")
}
