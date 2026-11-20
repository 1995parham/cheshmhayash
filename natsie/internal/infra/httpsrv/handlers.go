package httpsrv

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"gopkg.in/yaml.v3"

	"github.com/1995parham/natsie/internal/infra/store"
)

func (s *Server) health(c *echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) getManifest(c *echo.Context) error {
	id := c.Param("id")
	if !store.ValidID(id) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid manifest id"})
	}

	m, err := s.store.Get(c.Request().Context(), id)
	if err != nil {
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
