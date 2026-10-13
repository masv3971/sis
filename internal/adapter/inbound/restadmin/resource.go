package restadmin

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
)

// Registrar attaches one resource's routes to the API group under the guards.
type Registrar func(rg *gin.RouterGroup, g Guards)

// Resource builds a Registrar exposing generic CRUD for entity type T at /path.
func Resource[T any, PT domain.EntityPtr[T]](path string, svc *service.Resource[T, PT]) Registrar {
	h := &resourceHandler[T, PT]{svc: svc}
	return func(rg *gin.RouterGroup, g Guards) {
		rg.GET("/"+path, g.Read, h.list)
		rg.POST("/"+path, g.Write, h.create)
		rg.GET("/"+path+"/:id", g.Read, h.get)
		rg.PUT("/"+path+"/:id", g.Write, h.update)
		rg.DELETE("/"+path+"/:id", g.Write, h.delete)
	}
}

type resourceHandler[T any, PT domain.EntityPtr[T]] struct {
	svc *service.Resource[T, PT]
}

func (h *resourceHandler[T, PT]) list(c *gin.Context) {
	params := port.ListParams{Limit: defaultLimit, Sort: c.Query("sort"), Filter: c.Query("filter")}
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			params.Limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			params.Offset = n
		}
	}
	page, err := h.svc.List(c.Request.Context(), params)
	if err != nil {
		writeServiceErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":  page.Items,
		"total":  page.Total,
		"limit":  params.Limit,
		"offset": params.Offset,
	})
}

func (h *resourceHandler[T, PT]) create(c *gin.Context) {
	e := PT(new(T))
	if err := c.ShouldBindJSON(e); err != nil {
		writeErr(c, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	created, err := h.svc.Create(c.Request.Context(), e)
	if err != nil {
		writeServiceErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *resourceHandler[T, PT]) get(c *gin.Context) {
	e, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeServiceErr(c, err)
		return
	}
	c.JSON(http.StatusOK, e)
}

func (h *resourceHandler[T, PT]) update(c *gin.Context) {
	e := PT(new(T))
	if err := c.ShouldBindJSON(e); err != nil {
		writeErr(c, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	e.SetID(c.Param("id")) // path is authoritative over any id in the body
	updated, err := h.svc.Update(c.Request.Context(), e)
	if err != nil {
		writeServiceErr(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *resourceHandler[T, PT]) delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		writeServiceErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
