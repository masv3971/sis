package restadmin

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/core/domain"
)

func writeErr(c *gin.Context, status int, code, msg string) {
	c.JSON(status, gin.H{"error": code, "message": msg})
}

// writeServiceErr maps domain/service errors onto HTTP status codes.
func writeServiceErr(c *gin.Context, err error) {
	var ve *domain.ValidationError
	switch {
	case errors.As(err, &ve):
		writeErr(c, http.StatusBadRequest, "validation", ve.Error())
	case errors.Is(err, domain.ErrNotFound):
		writeErr(c, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, domain.ErrConflict):
		writeErr(c, http.StatusConflict, "conflict", "resource already exists")
	default:
		writeErr(c, http.StatusInternalServerError, "internal", "internal server error")
	}
}
