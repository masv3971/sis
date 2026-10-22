package restadmin

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/command"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/pkg/helpers"
)

// The generic resource handlers below could not be methods on *Client: Go
// does not allow type parameters on methods, so they are free functions that
// take *Client as the first argument. Each returns a gin.HandlerFunc the
// httpserver can mount directly. Handlers that do not need the Client discard
// it with _ but keep the signature uniform.

// ListReply is the response envelope of the generic entity list endpoints.
type ListReply[T any] struct {
	Items []T `json:"items"`
	// Total is the number of entities across all pages.
	Total int `json:"total"`
	// Limit is the effective page size after defaulting and clamping.
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// ResourceList lists entities of type T with limit/offset paging, bounded by
// the Client's configured max_limit.
//
// The swag annotations for these generic handlers live per entity in
// swagger.go, because one handler is mounted once per entity type.
func ResourceList[T any, PT domain.EntityPtr[T]](c *Client, svc *service.Resource[T, PT]) gin.HandlerFunc {
	return func(gc *gin.Context) {
		params, err := c.listParams(gc)
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		page, err := svc.List(gc.Request.Context(), params)
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		gc.JSON(http.StatusOK, ListReply[PT]{
			Items:  page.Items,
			Total:  page.Total,
			Limit:  params.Limit,
			Offset: params.Offset,
		})
	}
}

// ResourceCreate creates an entity of type T from the JSON body. References
// to other records must be their canonical ids (see readmodel.CheckRefs).
func ResourceCreate[T any, PT domain.EntityPtr[T]](c *Client, svc *service.Resource[T, PT]) gin.HandlerFunc {
	return func(gc *gin.Context) {
		entity := PT(new(T))
		if err := helpers.BindJSON(gc, entity); err != nil {
			helpers.WriteError(gc, err)
			return
		}
		created, err := command.Create(gc.Request.Context(), c.commands, svc, entity)
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		gc.JSON(http.StatusCreated, created)
	}
}

// ResourceGet returns a single entity of type T by id.
func ResourceGet[T any, PT domain.EntityPtr[T]](_ *Client, svc *service.Resource[T, PT]) gin.HandlerFunc {
	return func(gc *gin.Context) {
		entity, err := svc.Get(gc.Request.Context(), gc.Param("id"))
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		gc.JSON(http.StatusOK, entity)
	}
}

// ResourceUpdate replaces an entity; the path id is authoritative over the
// body. References are checked as in ResourceCreate.
func ResourceUpdate[T any, PT domain.EntityPtr[T]](c *Client, svc *service.Resource[T, PT]) gin.HandlerFunc {
	return func(gc *gin.Context) {
		entity := PT(new(T))
		if err := helpers.BindJSON(gc, entity); err != nil {
			helpers.WriteError(gc, err)
			return
		}
		updated, err := command.Update(gc.Request.Context(), c.commands, svc, gc.Param("id"), entity)
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		gc.JSON(http.StatusOK, updated)
	}
}

// ResourceDelete removes an entity of type T by id.
func ResourceDelete[T any, PT domain.EntityPtr[T]](c *Client, svc *service.Resource[T, PT]) gin.HandlerFunc {
	return func(gc *gin.Context) {
		if err := command.Delete(gc.Request.Context(), c.commands, svc, gc.Param("id")); err != nil {
			helpers.WriteError(gc, err)
			return
		}
		gc.Status(http.StatusNoContent)
	}
}
