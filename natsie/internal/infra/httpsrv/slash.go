package httpsrv

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
)

// slashResponse is the common shape Mattermost and Slack accept on the
// synchronous reply to a slash command.
type slashResponse struct {
	ResponseType string `json:"response_type"`
	Text         string `json:"text"`
}

const (
	responseEphemeral = "ephemeral"
)

func (s *Server) handleSlash(c *echo.Context) error {
	// Slash commands arrive as application/x-www-form-urlencoded.
	if err := c.Request().ParseForm(); err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse("bad form"))
	}
	form := c.Request().PostForm

	// Constant-time token check — both Mattermost and Slack send this in
	// the form body. (Slack also signs requests via X-Slack-Signature; we
	// add HMAC verification in the approval-flow commit.)
	token := form.Get("token")
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.slashToken)) != 1 {
		return c.JSON(http.StatusUnauthorized, errorResponse("invalid token"))
	}

	text := strings.TrimSpace(form.Get("text"))
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return c.JSON(http.StatusOK, slashResponse{
			ResponseType: responseEphemeral,
			Text:         slashHelp(),
		})
	}

	switch fields[0] {
	case "list":
		return s.slashList(c)
	case "show":
		if len(fields) < 2 {
			return c.JSON(http.StatusOK, slashResponse{ResponseType: responseEphemeral, Text: "usage: `/natsie show <manifest-id>`"})
		}
		return s.slashShow(c, fields[1])
	case "help":
		return c.JSON(http.StatusOK, slashResponse{ResponseType: responseEphemeral, Text: slashHelp()})
	default:
		return c.JSON(http.StatusOK, slashResponse{
			ResponseType: responseEphemeral,
			Text:         fmt.Sprintf("unknown subcommand `%s`\n\n%s", fields[0], slashHelp()),
		})
	}
}

func (s *Server) slashList(c *echo.Context) error {
	ids, err := s.store.List(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusOK, slashResponse{ResponseType: responseEphemeral, Text: "list failed: " + err.Error()})
	}
	if len(ids) == 0 {
		return c.JSON(http.StatusOK, slashResponse{ResponseType: responseEphemeral, Text: "no manifests in store"})
	}
	var b strings.Builder
	b.WriteString("Manifests:\n")
	for i, id := range ids {
		if i >= 20 {
			fmt.Fprintf(&b, "...and %d more\n", len(ids)-20)
			break
		}
		fmt.Fprintf(&b, "- `%s`\n", id)
	}
	return c.JSON(http.StatusOK, slashResponse{ResponseType: responseEphemeral, Text: b.String()})
}

func (s *Server) slashShow(c *echo.Context, id string) error {
	m, err := s.store.Get(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusOK, slashResponse{
			ResponseType: responseEphemeral,
			Text:         fmt.Sprintf("manifest `%s` not found: %v", id, err),
		})
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Manifest `%s` (%d entries, generated %s):\n", id, len(m.Entries), m.GeneratedAt.Format("2006-01-02T15:04:05Z"))
	for i, e := range m.Entries {
		if i >= 10 {
			fmt.Fprintf(&b, "...and %d more\n", len(m.Entries)-10)
			break
		}
		fmt.Fprintf(&b, "- `%s/%s` (pending=%d, idle=%s)\n", e.Stream, e.Consumer, e.NumPending, e.Idle)
	}
	return c.JSON(http.StatusOK, slashResponse{ResponseType: responseEphemeral, Text: b.String()})
}

func slashHelp() string {
	return "natsie slash commands:\n" +
		"- `/natsie list` — list stored manifest IDs\n" +
		"- `/natsie show <id>` — preview a manifest\n" +
		"- `/natsie help` — this message"
}

func errorResponse(text string) slashResponse {
	return slashResponse{ResponseType: responseEphemeral, Text: text}
}
