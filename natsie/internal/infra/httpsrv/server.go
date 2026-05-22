// Package httpsrv hosts the bot's HTTP listener: a read-only manifest
// viewer, a health endpoint, and (in follow-up commits) the slash-command
// handler plus signed approval URLs.
//
// The package name avoids collision with the stdlib net/http import that
// callers also need.
package httpsrv

import (
	"context"
	"log"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/1995parham/natsie/internal/audit"
	"github.com/1995parham/natsie/internal/cleanup"
	"github.com/1995parham/natsie/internal/infra/store"
)

const defaultGracefulTimeout = 10 * time.Second

// Server wraps an Echo instance with the bot-specific dependencies it
// needs (manifest store, signing key, NATS connector, audit log, logger).
type Server struct {
	e          *echo.Echo
	listen     string
	store      store.Store
	log        *log.Logger
	audit      *audit.Logger
	signingKey string
	connect    cleanup.Connector
}

// Options groups optional Server inputs that have grown beyond a sensible
// positional argument list. SigningKey and Connector are optional;
// supplying neither restricts the listener to the read-only endpoints
// (/health and /manifest/:id). Audit may be nil; the audit logger
// already treats a nil receiver as a no-op.
type Options struct {
	SigningKey string
	Connector  cleanup.Connector
	Audit      *audit.Logger
}

// New constructs the Server. Routes are registered immediately so callers
// can list them in startup logs. SigningKey gates the /slash and
// /approve endpoints; Connector additionally gates /approve (we don't
// expose the deletion path without a way to actually delete).
func New(listen string, st store.Store, opts Options, logger *log.Logger) *Server {
	e := echo.New()
	s := &Server{
		e:          e,
		listen:     listen,
		store:      st,
		log:        logger,
		audit:      opts.Audit,
		signingKey: opts.SigningKey,
		connect:    opts.Connector,
	}
	s.routes()

	return s
}

func (s *Server) routes() {
	s.e.GET("/healthz", s.health)
	s.e.GET("/manifest/:id", s.getManifest)

	if s.signingKey != "" {
		s.e.POST("/slash", s.handleSlash)
	}

	if s.signingKey != "" && s.connect != nil {
		s.e.GET("/approve/:id", s.previewApproval)
		s.e.POST("/approve/:id", s.doApproval)
	}
}

// SignApproval returns the HMAC-SHA256 token (base64url) that authorises
// the approval URL for the given manifest ID. Exported so the bot can
// build the link before sending it to chat.
func (s *Server) SignApproval(manifestID string) string {
	return signApproval(s.signingKey, manifestID)
}

// Start launches the HTTP listener and blocks until ctx is canceled.
// StartConfig.Start handles the graceful shutdown internally.
func (s *Server) Start(ctx context.Context) error {
	sc := echo.StartConfig{
		Address:         s.listen,
		HideBanner:      true,
		HidePort:        true,
		GracefulTimeout: defaultGracefulTimeout,
	}

	return sc.Start(ctx, s.e)
}
