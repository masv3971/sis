package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
)

// corpus is a folder of vendored public documents (see its README) that
// SIS must ingest.
type corpus struct {
	name, dir string
	// min guards against the folder silently going missing.
	min int
	// env names an extra directory of documents to run as well, e.g. the
	// unlicensed EC samples the README tells how to fetch.
	env string
}

var corpora = []corpus{
	{name: "elm", dir: "../../testdata/elm-corpus", min: 4, env: "SIS_ELM_CORPUS_DIR"},
	{name: "microcredential", dir: "../../testdata/microcredential-corpus", min: 3, env: "SIS_MICROCREDENTIAL_CORPUS_DIR"},
}

// knownFailures lists corpus files that SIS cannot ingest yet, keyed by file
// name, with a fragment of the error they must fail with. An entry whose
// file starts to pass fails the test, so the list stays accurate.
var knownFailures = map[string]string{}

// eduPaths and oePaths map a populate item type to the provider path that
// serves it.
var (
	eduPaths = map[string]string{
		"person": "persons", "organization": "orgs", "academicSession": "academicSessions", "course": "courses",
		"offering": "classes", "enrollment": "enrollments", "program": "programs", "result": "results",
	}
	oePaths = map[string]string{
		"person": "persons", "organization": "organizations", "academicSession": "academic-sessions", "course": "courses",
		"offering": "offerings", "enrollment": "associations", "program": "programs", "result": "results",
	}
)

// TestELMCorpus sends every public document of each corpus (ELM credentials,
// micro-credentials), unchanged, through POST /api/v1/parse (Edu-API and
// OEAPI) and /api/v1/populate, as a customer would, and reads every stored
// record back from both providers.
func TestELMCorpus(t *testing.T) {
	var all []string
	for _, c := range corpora {
		files := corpusFiles(t, c.dir)
		require.GreaterOrEqual(t, len(files), c.min, "corpus %s is incomplete", c.dir)
		if extra := os.Getenv(c.env); extra != "" {
			files = append(files, corpusFiles(t, extra)...)
		}
		all = append(all, files...)
		t.Run(c.name, func(t *testing.T) { runCorpus(t, files) })
	}
	for name := range knownFailures {
		assert.True(t, slices.ContainsFunc(all, func(file string) bool { return filepath.Base(file) == name }),
			"knownFailures lists %s, which is in no corpus", name)
	}
}

// runCorpus checks each file in its own subtest.
func runCorpus(t *testing.T, files []string) {
	for _, file := range files {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			doc, err := os.ReadFile(file) // #nosec G304 -- corpus file
			require.NoError(t, err)
			srv := newCorpusServer(t)

			if reason, known := knownFailures[name]; known {
				status, body := srv.post(t, srv.reader, "/api/v1/parse", envelopeFor(doc, "eduapi", ""))
				require.NotEqual(t, http.StatusOK, status, "%s now parses: remove it from knownFailures", name)
				assert.Contains(t, string(body), reason, "fails for another reason than knownFailures says")
				return
			}

			records := map[string]int{}
			for _, format := range []string{"eduapi", "oeapi"} {
				status, body := srv.post(t, srv.reader, "/api/v1/parse", envelopeFor(doc, format, ""))
				require.Equal(t, http.StatusOK, status, "parse %s: %s", format, body)
				var reply struct {
					Data     map[string]json.RawMessage `json:"data"`
					Warnings []string                   `json:"warnings"`
				}
				require.NoError(t, json.Unmarshal(body, &reply))
				var persons []json.RawMessage
				require.NoError(t, json.Unmarshal(reply.Data["persons"], &persons))
				require.NotEmpty(t, persons, "parse %s: no person", format)
				if format == "eduapi" {
					for key, raw := range reply.Data {
						var list []json.RawMessage
						require.NoError(t, json.Unmarshal(raw, &list), "eduapi %s", key)
						records[key] = len(list)
					}
					for _, warning := range reply.Warnings {
						t.Log("warning:", warning)
					}
				}
			}

			status, body := srv.post(t, srv.writer, "/api/v1/populate", envelopeFor(doc, "", `"ttl":"1h"`))
			require.Equal(t, http.StatusOK, status, "populate: %s", body)
			var reply struct {
				Items []readmodel.PopulateItem `json:"items"`
			}
			require.NoError(t, json.Unmarshal(body, &reply))
			total := 0
			for _, count := range records {
				total += count
			}
			require.Len(t, reply.Items, total, "populate stored a different number of records than parse returned (%v)", records)

			for _, item := range reply.Items {
				id := url.PathEscape(item.SourcedID)
				require.Contains(t, eduPaths, item.Type, "unknown item type")
				srv.get(t, "/eduapi/"+eduPaths[item.Type]+"/"+id)
				srv.get(t, "/oeapi/"+oePaths[item.Type]+"/"+id)
			}
			t.Logf("%d records %v", total, records)
		})
	}
}

// corpusFiles lists the ELM documents (*.json, *.jsonld) in dir.
func corpusFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "corpus dir")
	var files []string
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if !entry.IsDir() && (ext == ".json" || ext == ".jsonld") {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	return files
}

// envelopeFor wraps doc, unchanged, in a parse (format set) or populate
// (format empty) request body; extra is added as is, e.g. a ttl.
func envelopeFor(doc []byte, format, extra string) string {
	meta := `{"input_format":"elm"}`
	if format != "" {
		meta = `{"input_format":"elm","output_format":"` + format + `"}`
	}
	if extra != "" {
		extra = "," + extra
	}
	return `{"metadata":` + meta + extra + `,"data":` + string(doc) + `}`
}

// corpusServer is a server over an empty in-memory catalog, configured like
// production for document endpoints.
type corpusServer struct {
	base, reader, writer string
}

func newCorpusServer(t *testing.T) *corpusServer {
	t.Helper()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.HTTPServer.ReadTimeout = 10 * time.Second
	cfg.SIS.Adapter.Inbound.ConvertAPI.MaxBodyBytes = 5 << 20 // the shipped default
	cfg.SIS.Adapter.Inbound.ConvertAPI.WriteTimeout = 30 * time.Second
	cfg.SIS.Adapter.Inbound.OEAPI.DefaultPageSize, cfg.SIS.Adapter.Inbound.OEAPI.MaxPageSize = 10, 10
	cfg.SIS.Adapter.Inbound.EduAPI.DefaultLimit, cfg.SIS.Adapter.Inbound.EduAPI.MaxLimit = 10, 10
	srv, iss := startWithIssuer(t, cfg, memCatalog(time.Now, log))
	return &corpusServer{
		base:   "http://" + srv.Addr().String(),
		reader: iss.Token(t, "svc", "sis.read"),
		writer: iss.Token(t, "svc", "sis.read sis.write"),
	}
}

func (s *corpusServer) post(t *testing.T, token, path, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.base+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return s.do(t, req)
}

// get requires path to answer 200 to a reader.
func (s *corpusServer) get(t *testing.T, path string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.base+path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+s.reader)
	status, body := s.do(t, req)
	assert.Equal(t, http.StatusOK, status, "GET %s: %s", path, body)
}

func (s *corpusServer) do(t *testing.T, req *http.Request) (int, []byte) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "%s %s", req.Method, req.URL.Path)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}
