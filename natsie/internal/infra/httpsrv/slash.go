package httpsrv

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/1995parham/natsie/internal/chatops"
)

// slashResponse is the common shape Mattermost and Slack accept on the
// synchronous reply to a slash command.
type slashResponse struct {
	ResponseType string `json:"response_type"`
	Text         string `json:"text"`
}

const (
	responseEphemeral = "ephemeral"
	slashTrigger      = "/natsie"
)

func (s *Server) handleSlash(c *echo.Context) error {
	// Slash commands arrive as application/x-www-form-urlencoded.
	if err := c.Request().ParseForm(); err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse("bad form"))
	}

	form := c.Request().PostForm

	// Constant-time token check — both Mattermost and Slack send this in
	// the form body.
	token := form.Get("token")
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.signingKey)) != 1 {
		return c.JSON(http.StatusUnauthorized, errorResponse("invalid token"))
	}

	argv := strings.Fields(strings.TrimSpace(form.Get("text")))
	reply := chatops.Dispatch(c.Request().Context(), chatops.Deps{
		Store:      s.store,
		Audit:      s.audit,
		BaseURL:    s.baseURL,
		SigningKey: s.signingKey,
	}, slashTrigger, argv)

	return c.JSON(http.StatusOK, slashResponse{
		ResponseType: responseEphemeral,
		Text:         reply,
	})
}

func errorResponse(text string) slashResponse {
	return slashResponse{ResponseType: responseEphemeral, Text: text}
}
