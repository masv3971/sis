package elm

import (
	"testing"
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
	if err != nil {
		t.Fatal(err)
	}
	n := doc.Root
	cases := map[string]string{"plain": "Ada", "lang": "Course", "langOne": "Kurs", "value": "V", "arr": "second", "num": "12345678901234567890", "missing": ""}
	for prop, want := range cases {
		if got := n.Text(prop); got != want {
			t.Errorf("Text(%s) = %q, want %q", prop, got, want)
		}
	}
	doc.Languages = []string{"sv", "en"}
	if got := n.Text("lang"); got != "Kurs" {
		t.Errorf("language preference not applied: %q", got)
	}
}

func TestNodesResolveReferences(t *testing.T) {
	doc, err := Parse([]byte(`{
		"issuer": "urn:org:1",
		"credentialSubject": {"hasClaim": {"id": "urn:a:1", "type": "LearningAchievement",
			"awardedBy": {"awardingBody": [{"id": "urn:org:1"}, "urn:org:missing"]}}},
		"defs": [{"id": "urn:org:1", "type": "Organisation", "legalName": {"en": "Uni"}}]
	}`), "data")
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Root.Node("issuer"); got == nil || got.Text("legalName") != "Uni" || !got.Is("Organisation") {
		t.Fatalf("string reference not resolved: %+v", got)
	}
	claims := doc.Root.Node("credentialSubject").Nodes("hasClaim")
	if len(claims) != 1 || claims[0].Path != "data.credentialSubject.hasClaim" {
		t.Fatalf("single value must be one node: %+v", claims)
	}
	bodies := claims[0].Node("awardedBy").Nodes("awardingBody")
	if len(bodies) != 2 || bodies[0].Text("legalName") != "Uni" || bodies[1].ID() != "urn:org:missing" {
		t.Fatalf("stub/missing references: %+v", bodies)
	}
	if bodies[1].Path != "data.credentialSubject.hasClaim.awardedBy.awardingBody[1]" {
		t.Fatalf("path = %s", bodies[1].Path)
	}
}

func TestParseRejectsNonObject(t *testing.T) {
	if _, err := Parse([]byte(`[1]`), "data"); err == nil {
		t.Fatal("array root must be rejected")
	}
	if _, err := Parse([]byte(`{`), "data"); err == nil {
		t.Fatal("malformed JSON must be rejected")
	}
}
