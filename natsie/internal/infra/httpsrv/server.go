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
// needs (manifest store, logger).
type Server struct {
	e      *echo.Echo
	listen string
	store  store.Store
	log    *log.Logger
}

// New constructs the Server. Routes are registered immediately so callers
// can list them in startup logs.
func New(listen string, st store.Store, logger *log.Logger) *Server {
	e := echo.New()
	s := &Server{e: e, listen: listen, store: st, log: logger}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.e.GET("/health", s.health)
	s.e.GET("/manifest/:id", s.getManifest)
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
