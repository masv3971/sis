# Micro-credential corpus

Public European micro-credentials that SIS must ingest. These are ELM v3
credentials carrying the elements of the Council Recommendation on
micro-credentials (2022/C 243/02), Annex I. They are kept apart from
`testdata/elm-corpus/` so they can be tested and extended as their own set.

`TestELMCorpus` (`internal/httpserver/corpus_test.go`, subtest
`microcredential`) sends every `*.json` / `*.jsonld` file here, unchanged,
through `POST /api/v1/parse` (Edu-API and OEAPI) and `POST /api/v1/populate`,
and reads the stored records back. The shapes (plain, `{"credential": ...}`
uploadable, sealed `{"payload", "signatures"}`) are described in
`testdata/elm-corpus/README.md`.

## Sources

Licence: EUPL-1.2 (© European Union), see
<https://github.com/european-commission-empl/European-Learning-Model/blob/master/license>.
Files are unmodified apart from their names.

From <https://github.com/european-commission-empl/European-Learning-Model>
(archived), commit `9d7c5d22002237c3afeb1750b7038e6fe2cdd371`:

| File | Upstream path |
|---|---|
| `ec-mc-annex1-signed.jsonld` | `Credentials/JSON-LD Examples (ELM v3)/Sample-MC-Annex1-signed.jsonld` |
| `ec-mc-annex1-unsigned.jsonld` | `Credentials/JSON-LD Examples (ELM v3)/Sample-MC-Annex1-unsigned.jsonld` |
| `ec-mc-annex1-unsigned-uploadable.json` | `Credentials/JSON-LD Examples (ELM v3)/Sample-MC-Annex1-unsigned-uploadable.json` |

## More documents, not vendored

<https://code.europa.eu/qualifications-courses-and-credentials/ELM-support>
has `credential examples/JSON-LD Examples (ELM v3)/AA-Annex1-MC-signed.jsonld`
and `AA-Annex1-MC-unsigned.json`. The repository carries no licence, so they
are not committed. Fetch them (see the recipe in
`testdata/elm-corpus/README.md`) into a directory and run:

```bash
SIS_MICROCREDENTIAL_CORPUS_DIR=/tmp/mc-extra go test ./internal/httpserver -run TestELMCorpus -v
```
