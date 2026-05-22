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

	"github.com/1995parham/natsie/internal/infra/store"
)

const defaultGracefulTimeout = 10 * time.Second

// Server wraps an Echo instance with the bot-specific dependencies it
// needs (manifest store, logger, shared slash-command verification token).
type Server struct {
	e          *echo.Echo
	listen     string
	store      store.Store
	log        *log.Logger
	slashToken string
}

// New constructs the Server. Routes are registered immediately so callers
// can list them in startup logs. slashToken is the shared verification
// token configured in the Mattermost/Slack slash-command integration;
// empty disables the /slash endpoint.
func New(listen string, st store.Store, slashToken string, logger *log.Logger) *Server {
	e := echo.New()
	s := &Server{e: e, listen: listen, store: st, slashToken: slashToken, log: logger}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.e.GET("/health", s.health)
	s.e.GET("/manifest/:id", s.getManifest)
	if s.slashToken != "" {
		s.e.POST("/slash", s.handleSlash)
	}
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
