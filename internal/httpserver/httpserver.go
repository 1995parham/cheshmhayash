// Package httpserver runs an http.Server until its context ends and then
// shuts it down gracefully. The serve and mcp commands share it.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// New returns an http.Server for addr and h with the timeouts every
// cheshmhayash listener uses.
func New(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
	}
}

// Run serves srv until ctx ends or the listener fails. On ctx end it gives
// in-flight requests shutdownTimeout to complete. A listener failure is
// returned as an error.
func Run(ctx context.Context, srv *http.Server, log *slog.Logger) error {
	errc := make(chan error, 1)
	go func() {
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("http server on %s: %w", srv.Addr, err)
	case <-ctx.Done():
	}

	log.Info("shutting down", "address", srv.Addr)
	// The parent ctx is already done, so derive the shutdown deadline from
	// a context that keeps its values but not its cancellation.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("shutdown", "err", err)
	}
	return nil
}
