// Package restadmin is the inbound native /api/v1 REST adapter — the primary
// write contract for business systems and the SPA. It translates HTTP into
// service-layer calls; all ingress funnels through the same core.
package restadmin

import (
	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/core/service"
)

// Handler serves the native /api/v1 admin REST API.
type Handler struct {
	svc *service.Service
}

// New returns a Handler backed by the service layer.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// Guards are the per-operation authorization middlewares applied to routes.
type Guards struct {
	Read  gin.HandlerFunc
	Write gin.HandlerFunc
}

// Register attaches the person routes to the given router group (mounted at
// /api/v1), guarding reads and writes with the supplied middleware.
func (h *Handler) Register(rg *gin.RouterGroup, g Guards) {
	rg.GET("/persons", g.Read, h.listPersons)
	rg.POST("/persons", g.Write, h.createPerson)
	rg.GET("/persons/:id", g.Read, h.getPerson)
	rg.PUT("/persons/:id", g.Write, h.updatePerson)
	rg.DELETE("/persons/:id", g.Write, h.deletePerson)
}
