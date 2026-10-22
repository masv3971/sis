package authapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/pkg/model"
)

func TestConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	get := func(t *testing.T, cfg *model.Cfg) ConfigReply {
		t.Helper()
		c, err := New(t.Context(), cfg)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		gc, _ := gin.CreateTestContext(w)
		gc.Request = httptest.NewRequest(http.MethodGet, "/auth/config", nil)
		c.Config(gc)
		var out ConfigReply
		require.Equal(t, http.StatusOK, w.Code, "body %s", w.Body)
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), "body %s", w.Body)
		return out
	}

	assert.Equal(t, ConfigReply{}, get(t, &model.Cfg{}), "disabled")

	cfg := &model.Cfg{}
	cfg.SIS.Adapter.Inbound.AuthAPI = model.AuthAPI{Issuer: "https://idp.example/realms/sis", ClientID: "sis-web", Scope: "openid sis.read"}
	want := ConfigReply{Enabled: true, Issuer: "https://idp.example/realms/sis", ClientID: "sis-web", Scope: "openid sis.read"}
	assert.Equal(t, want, get(t, cfg), "enabled")
}
