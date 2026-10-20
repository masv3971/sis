package authapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/pkg/model"
)

func TestConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	get := func(t *testing.T, cfg *model.Cfg) ConfigReply {
		t.Helper()
		c, err := New(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		gc, _ := gin.CreateTestContext(w)
		gc.Request = httptest.NewRequest(http.MethodGet, "/auth/config", nil)
		c.Config(gc)
		var out ConfigReply
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("status %d body %s", w.Code, w.Body)
		}
		return out
	}

	if got := get(t, &model.Cfg{}); got != (ConfigReply{}) {
		t.Fatalf("disabled: %+v", got)
	}

	cfg := &model.Cfg{}
	cfg.SIS.Adapter.Inbound.AuthAPI = model.AuthAPI{Issuer: "https://idp.example/realms/sis", ClientID: "sis-web", Scope: "openid sis.read"}
	want := ConfigReply{Enabled: true, Issuer: "https://idp.example/realms/sis", ClientID: "sis-web", Scope: "openid sis.read"}
	if got := get(t, cfg); got != want {
		t.Fatalf("enabled: %+v, want %+v", got, want)
	}
}
