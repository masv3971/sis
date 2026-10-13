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

// Register attaches the person routes to the given router group (mounted at /api/v1).
func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/persons", h.listPersons)
	rg.POST("/persons", h.createPerson)
	rg.GET("/persons/:id", h.getPerson)
	rg.PUT("/persons/:id", h.updatePerson)
	rg.DELETE("/persons/:id", h.deletePerson)
}
