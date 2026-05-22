package httpsrv

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
	"gopkg.in/yaml.v3"
)

func (s *Server) health(c *echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) getManifest(c *echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
	}

	m, err := s.store.Get(c.Request().Context(), id)
	if err != nil {
		// Distinguish invalid-id (caller bug) from not-found (legitimate).
		// The store rejects bad ids with an "invalid manifest id" message.
		if strings.Contains(err.Error(), "invalid manifest id") {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusNotFound, map[string]string{"error": "manifest not found", "id": id})
	}

	body, err := yaml.Marshal(m)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "marshal: " + err.Error()})
	}
	c.Response().Header().Set(echo.HeaderContentType, "application/yaml")
	c.Response().WriteHeader(http.StatusOK)
	_, err = c.Response().Write(body)
	return err
}
