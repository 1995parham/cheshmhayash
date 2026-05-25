package httpsrv

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/1995parham/natsie/internal/audit"
	"github.com/1995parham/natsie/internal/infra/store"
)

// SignApprovalToken returns the HMAC-SHA256 token (URL-safe base64) that
// proves the holder is authorised to apply the named manifest. Exported
// so the bot can build approve URLs before sending them to chat.
func SignApprovalToken(key, manifestID string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(manifestID))

	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// signApproval is the unexported alias used by handlers in this package.
func signApproval(key, manifestID string) string {
	return SignApprovalToken(key, manifestID)
}

func (s *Server) checkApprovalToken(id, presented string) bool {
	want := signApproval(s.signingKey, id)

	return subtle.ConstantTimeCompare([]byte(presented), []byte(want)) == 1
}

func (s *Server) previewApproval(c *echo.Context) error {
	id := c.Param("id")
	if !store.ValidID(id) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid manifest id"})
	}

	token := c.QueryParam("token")
	if !s.checkApprovalToken(id, token) {
		_ = s.audit.Log(audit.Event{
			Kind: "approve.preview", Manifest: id, Source: c.RealIP(),
			Error: "invalid token",
		})

		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
	}

	m, err := s.store.Get(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "manifest not found", "id": id})
	}

	_ = s.audit.Log(audit.Event{
		Kind: "approve.preview", Manifest: id, Source: c.RealIP(),
		Entries: len(m.Entries),
	})

	// id has already passed store.ValidID (alphanumerics plus ._-, no
	// HTML-significant characters) and the response is text/plain, so this is
	// not exploitable; escape it anyway as defense-in-depth and to keep the
	// reflected-XSS dataflow provably sanitised at the sink.
	var body strings.Builder
	fmt.Fprintf(&body, "Confirm cleanup for manifest %s (%d entries):\n\n", html.EscapeString(id), len(m.Entries))

	for i, e := range m.Entries {
		if i >= 20 {
			fmt.Fprintf(&body, "...and %d more\n", len(m.Entries)-20)

			break
		}

		fmt.Fprintf(&body, "  %s/%s (pending=%d)\n", e.Stream, e.Consumer, e.NumPending)
	}

	body.WriteString("\nPOST the same URL to confirm. Re-verification runs at apply time;\n")
	body.WriteString("any consumer that's become active since the manifest was generated\n")
	body.WriteString("will be preserved automatically.\n")

	c.Response().Header().Set(echo.HeaderContentType, "text/plain; charset=utf-8")
	c.Response().WriteHeader(http.StatusOK)
	_, err = c.Response().Write([]byte(body.String()))

	return err
}

func (s *Server) doApproval(c *echo.Context) error {
	id := c.Param("id")
	if !store.ValidID(id) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid manifest id"})
	}

	token := c.QueryParam("token")
	if !s.checkApprovalToken(id, token) {
		_ = s.audit.Log(audit.Event{
			Kind: "approve.apply", Manifest: id, Source: c.RealIP(),
			Error: "invalid token",
		})

		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
	}

	m, err := s.store.Get(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "manifest not found", "id": id})
	}

	result, err := applyManifest(c.Request().Context(), m, s.connect)
	if err != nil {
		_ = s.audit.Log(audit.Event{
			Kind: "approve.apply", Manifest: id, Source: c.RealIP(),
			Entries: len(m.Entries), Result: result.Summary(), Error: err.Error(),
		})

		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error":   err.Error(),
			"summary": result.Summary(),
		})
	}

	_ = s.audit.Log(audit.Event{
		Kind: "approve.apply", Manifest: id, Source: c.RealIP(),
		Entries: len(m.Entries), Result: result.Summary(),
	})

	return c.JSON(http.StatusOK, map[string]any{
		"id":      id,
		"summary": result.Summary(),
		"events":  result.Events,
	})
}
