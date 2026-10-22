# ELM v3 corpus

Public European Learning Model (ELM v3) credentials that SIS must ingest.
`TestELMCorpus` (`internal/httpserver/corpus_test.go`) sends every
`*.json` / `*.jsonld` file here, unchanged, through `POST /api/v1/parse`
(Edu-API and OEAPI) and `POST /api/v1/populate`, and reads the stored records
back. Files that are expected to fail are listed, with the reason, in
`knownFailures` in that test.

The files come in the three shapes EDCI produces, and SIS accepts all three:

| Shape | Top-level keys | Where it comes from |
|---|---|---|
| plain | `@context`, `credentialSubject`, ... | what an issuing system generates |
| uploadable | `credential` (+ `deliveryDetails`) | EDCI "upload for sealing" format |
| signed | `payload` (credential as JSON string), `signatures` | EDCI-sealed (JAdES) credential, e.g. a student's download |

SIS does not verify the seal of a signed credential (it trusts the caller's
token); it says so in a conversion warning.

## Sources

Licence: EUPL-1.2 (© European Union), see
<https://github.com/european-commission-empl/European-Learning-Model/blob/master/license>.
Files are unmodified apart from their names.

From <https://github.com/european-commission-empl/European-Learning-Model>
(archived), commit `9d7c5d22002237c3afeb1750b7038e6fe2cdd371`:

| File | Upstream path |
|---|---|
| `ec-certofpart-signed.jsonld` | `Credentials/JSON-LD Examples (ELM v3)/Sample-CertOfPart-signed.jsonld` |
| `ec-certofpart-unsigned.jsonld` | `Credentials/JSON-LD Examples (ELM v3)/Sample-CertOfPart-unsigned.jsonld` |
| `ec-certofpart-unsigned-uploadable.json` | `Credentials/JSON-LD Examples (ELM v3)/Sample-CertOfPart-unsigned-uploadable.json` |
| `ec-credential-sample.jsonld` | `rdf/samples/credential-sample.jsonld` |

Micro-credentials (Council Recommendation Annex I) live in their own corpus,
`testdata/microcredential-corpus/`. ELM v2 XML examples are not included:
SIS does not read ELM XML yet.

## More documents, not vendored

The EC's successor repository
<https://code.europa.eu/qualifications-courses-and-credentials/ELM-support>
(`credential examples/`) has more samples: joint degree, master's degree,
transcript of records, and community samples (SEPIE, University of Padova,
Web2Learn); its AA micro-credential (`AA-Annex1-MC-*`) belongs in the
micro-credential corpus (`SIS_MICROCREDENTIAL_CORPUS_DIR`). It carries no
licence, so the files are not committed here. To run the test against them as
well:

```bash
dir=/tmp/elm-extra; mkdir -p "$dir"
curl -fsSL 'https://code.europa.eu/api/v4/projects/627/repository/tree?recursive=true&per_page=100' |
  jq -r '.[] | select(.type=="blob") | .path | select(test("\\.(json|jsonld)$"))' |
  while read -r p; do
    curl -fsSL "https://code.europa.eu/api/v4/projects/627/repository/files/$(jq -rn --arg p "$p" '$p|@uri')/raw?ref=main" \
      -o "$dir/elmsupport-$(basename "$p" | tr 'A-Z' 'a-z')"
  done
SIS_ELM_CORPUS_DIR="$dir" go test ./internal/httpserver -run TestELMCorpus -v
```

## Refreshing

```bash
base='https://raw.githubusercontent.com/european-commission-empl/European-Learning-Model/<commit>'
curl -fsSL "$base/Credentials/JSON-LD%20Examples%20(ELM%20v3)/Sample-CertOfPart-signed.jsonld" -o ec-certofpart-signed.jsonld
# ... one line per file in the table above
```
