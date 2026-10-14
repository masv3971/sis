package restadmin

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/pkg/helpers"
)

// The generic resource handlers below could not be methods on *Client: Go
// does not allow type parameters on methods, so they are free functions that
// take *Client as the first argument. Each returns a gin.HandlerFunc the
// httpserver can mount directly. The _ parameter keeps the signature uniform
// in case we later want to log 5xx paths off *Client.

// ResourceList lists entities of type T with limit/offset paging.
func ResourceList[T any, PT domain.EntityPtr[T]](_ *Client, svc *service.Resource[T, PT]) gin.HandlerFunc {
	return func(gc *gin.Context) {
		params := listParams(gc)
		page, err := svc.List(gc.Request.Context(), params)
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		gc.JSON(http.StatusOK, gin.H{
			"items":  page.Items,
			"total":  page.Total,
			"limit":  params.Limit,
			"offset": params.Offset,
		})
	}
}

// ResourceCreate creates an entity of type T from the JSON body.
func ResourceCreate[T any, PT domain.EntityPtr[T]](_ *Client, svc *service.Resource[T, PT]) gin.HandlerFunc {
	return func(gc *gin.Context) {
		e := PT(new(T))
		if err := gc.ShouldBindJSON(e); err != nil {
			helpers.WriteError(gc, err)
			return
		}
		created, err := svc.Create(gc.Request.Context(), e)
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
		e, err := svc.Get(gc.Request.Context(), gc.Param("id"))
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		gc.JSON(http.StatusOK, e)
	}
}

// ResourceUpdate replaces an entity; the path id is authoritative over the body.
func ResourceUpdate[T any, PT domain.EntityPtr[T]](_ *Client, svc *service.Resource[T, PT]) gin.HandlerFunc {
	return func(gc *gin.Context) {
		e := PT(new(T))
		if err := gc.ShouldBindJSON(e); err != nil {
			helpers.WriteError(gc, err)
			return
		}
		e.SetID(gc.Param("id"))
		updated, err := svc.Update(gc.Request.Context(), e)
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		gc.JSON(http.StatusOK, updated)
	}
}

// ResourceDelete removes an entity of type T by id.
func ResourceDelete[T any, PT domain.EntityPtr[T]](_ *Client, svc *service.Resource[T, PT]) gin.HandlerFunc {
	return func(gc *gin.Context) {
		if err := svc.Delete(gc.Request.Context(), gc.Param("id")); err != nil {
			helpers.WriteError(gc, err)
			return
		}
		gc.Status(http.StatusNoContent)
	}
}
