package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
)

// TestNewEntitiesREST drives the four new resources through /api/v1: create,
// read, replace and delete, with references that must be canonical ids and a
// list reference checked entry by entry.
func TestNewEntitiesREST(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{
		Persons:             service.NewResource(memory.NewDocRepo[domain.Person](), nil, nil, log),
		Organizations:       service.NewResource(memory.NewDocRepo[domain.Organization](), nil, nil, log),
		AcademicSessions:    service.NewResource(memory.NewDocRepo[domain.AcademicSession](), nil, nil, log),
		Courses:             service.NewResource(memory.NewDocRepo[domain.Course](), nil, nil, log),
		Offerings:           service.NewResource(memory.NewDocRepo[domain.Offering](), nil, nil, log),
		Programs:            service.NewResource(memory.NewDocRepo[domain.Program](), nil, nil, log),
		Affiliations:        service.NewResource(memory.NewDocRepo[domain.Affiliation](), nil, nil, log),
		CollectionOfferings: service.NewResource(memory.NewDocRepo[domain.CollectionOffering](), nil, nil, log),
		ComponentTemplates:  service.NewResource(memory.NewDocRepo[domain.ComponentTemplate](), nil, nil, log),
		ComponentOfferings:  service.NewResource(memory.NewDocRepo[domain.ComponentOffering](), nil, nil, log),
	}
	cfg := testCfg("127.0.0.1:0")
	cfg.SIS.Adapter.Inbound.RestAdmin.DefaultLimit, cfg.SIS.Adapter.Inbound.RestAdmin.MaxLimit = 10, 10
	srv, iss := startWithIssuer(t, cfg, catalog)
	token := iss.Token(t, "writer", "sis.read sis.write")
	base := "http://" + srv.Addr().String()

	do := func(method, path, body string, want int) map[string]any {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, _ := http.NewRequestWithContext(t.Context(), method, base+path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err, "%s %s", method, path)
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		require.Equal(t, want, resp.StatusCode, "%s %s: body=%s", method, path, raw)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return out
	}
	api := "/api/v1/"

	do(http.MethodPost, api+"persons", `{"id":"p1","sourcedId":"stu","givenName":"A","familyName":"B","status":"active"}`, http.StatusCreated)
	do(http.MethodPost, api+"organizations", `{"id":"o1","name":"U"}`, http.StatusCreated)
	do(http.MethodPost, api+"programs", `{"id":"g1","title":"BSc"}`, http.StatusCreated)
	do(http.MethodPost, api+"courses", `{"id":"c1","title":"C","programIds":["g1"]}`, http.StatusCreated)
	do(http.MethodPost, api+"courses", `{"id":"c2","title":"C","programIds":["g1","nope"]}`, http.StatusBadRequest)
	do(http.MethodPost, api+"courses", `{"id":"c3","title":"C","programIds":[""]}`, http.StatusBadRequest)
	do(http.MethodPost, api+"component-templates", `{"id":"t-keep","title":"Keep"}`, http.StatusCreated)

	tests := []struct {
		path, create, replace, field, want string
		bad                                []string
	}{
		{"affiliations", `{"id":"a1","personId":"p1","orgId":"o1","role":"staff"}`, `{"personId":"p1","orgId":"o1","role":"student"}`, "role", "student",
			[]string{`{"id":"a2","personId":"p1"}`, `{"id":"a3","personId":"stu","orgId":"o1"}`, `{"id":"a4","personId":"p1","orgId":"o1","shoe":1}`}},
		{"collection-offerings", `{"id":"k1","programId":"g1","title":"BSc HT26"}`, `{"programId":"g1","title":"renamed"}`, "title", "renamed",
			[]string{`{"id":"k2"}`, `{"id":"k3","programId":"BSc"}`}},
		{"component-templates", `{"id":"t1","title":"Lab","type":"laboratory","courseIds":["c1"]}`, `{"title":"Lab 2","courseIds":["c1"]}`, "title", "Lab 2",
			[]string{`{"id":"t2"}`, `{"id":"t3","title":"T","courseIds":["c1","nope"]}`}},
		{"component-offerings", `{"id":"to1","componentId":"t-keep"}`, `{"componentId":"t-keep","title":"renamed"}`, "title", "renamed",
			[]string{`{"id":"to2"}`, `{"id":"to3","componentId":"nope"}`}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			id := do(http.MethodPost, api+tt.path, tt.create, http.StatusCreated)["id"].(string)
			assert.Equal(t, id, do(http.MethodGet, api+tt.path+"/"+id, "", http.StatusOK)["id"])
			list := do(http.MethodGet, api+tt.path, "", http.StatusOK)
			assert.NotEmpty(t, list["items"], "list")
			do(http.MethodPost, api+tt.path, tt.create, http.StatusConflict)
			for _, bad := range tt.bad {
				do(http.MethodPost, api+tt.path, bad, http.StatusBadRequest)
			}
			got := do(http.MethodPut, api+tt.path+"/"+id, tt.replace, http.StatusOK)
			assert.Equal(t, tt.want, got[tt.field])
			do(http.MethodDelete, api+tt.path+"/"+id, "", http.StatusNoContent)
			do(http.MethodGet, api+tt.path+"/"+id, "", http.StatusNotFound)
		})
	}

	t.Run("list references round trip", func(t *testing.T) {
		got := do(http.MethodGet, api+"courses/c1", "", http.StatusOK)
		assert.Equal(t, []any{"g1"}, got["programIds"])
		got = do(http.MethodPut, api+"courses/c1", `{"title":"C","programIds":[]}`, http.StatusOK)
		assert.Empty(t, got["programIds"], "an empty list clears the references")
	})

	t.Run("requires scope", func(t *testing.T) {
		readOnly := iss.Token(t, "reader", "sis.read")
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, base+api+"affiliations", strings.NewReader(`{"personId":"p1","orgId":"o1"}`))
		req.Header.Set("Authorization", "Bearer "+readOnly)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}
