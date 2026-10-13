package restadmin

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

const defaultLimit = 50

func (h *Handler) listPersons(c *gin.Context) {
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
	page, err := h.svc.ListPersons(c.Request.Context(), params)
	if err != nil {
		writeServiceErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"persons": page.Items,
		"total":   page.Total,
		"limit":   params.Limit,
		"offset":  params.Offset,
	})
}

func (h *Handler) createPerson(c *gin.Context) {
	var p domain.Person
	if err := c.ShouldBindJSON(&p); err != nil {
		writeErr(c, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	created, err := h.svc.CreatePerson(c.Request.Context(), p)
	if err != nil {
		writeServiceErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *Handler) getPerson(c *gin.Context) {
	p, err := h.svc.GetPerson(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeServiceErr(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) updatePerson(c *gin.Context) {
	var p domain.Person
	if err := c.ShouldBindJSON(&p); err != nil {
		writeErr(c, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	p.ID = c.Param("id") // path is authoritative over any id in the body
	updated, err := h.svc.UpdatePerson(c.Request.Context(), p)
	if err != nil {
		writeServiceErr(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *Handler) deletePerson(c *gin.Context) {
	if err := h.svc.DeletePerson(c.Request.Context(), c.Param("id")); err != nil {
		writeServiceErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
