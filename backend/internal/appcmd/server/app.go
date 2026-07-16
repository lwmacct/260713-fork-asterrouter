package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/buildinfo"
	"github.com/astercloud/asterrouter/backend/internal/config"
)

type App struct {
	cfg *config.Server
}

func NewApp(cfg *config.Server) *App {
	return &App{cfg: cfg}
}

func (app *App) Run(ctx context.Context) error {
	if app == nil || app.cfg == nil {
		return config.ErrInvalidHTTP
	}
	validated, err := config.Validate(*app.cfg, buildinfo.BuildType)
	if err != nil {
		return err
	}
	if validated.Security.SecretKey == "" {
		validated.Security.SecretKey, err = ephemeralSecret()
		if err != nil {
			return fmt.Errorf("generate ephemeral development secret: %w", err)
		}
		slog.Warn("using an ephemeral development secret; sessions and encrypted data will not survive restart")
	}
	*app.cfg = validated

	rt, err := newRuntime(ctx, app.cfg)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close(context.Background()) }()

	srv := newHTTPServer(app.cfg, rt)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", srv.Addr, err)
	}
	defer func() { _ = listener.Close() }()

	rt.Start(ctx)
	serveErr := make(chan error, 1)
	go func() {
		slog.Info("AsterRouter service starting", "listen", srv.Addr, "storage", rt.storageMode)
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	select {
	case <-ctx.Done():
	case received := <-signalCh:
		slog.Info("received shutdown signal", "signal", received.String())
	case err := <-serveErr:
		return errors.Join(fmt.Errorf("serve HTTP: %w", err), shutdown(ctx, srv, rt))
	case err := <-rt.Errors():
		return errors.Join(fmt.Errorf("runtime stopped: %w", err), shutdown(ctx, srv, rt))
	}
	return shutdown(ctx, srv, rt)
}

func shutdown(ctx context.Context, srv *http.Server, rt *runtime) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var errs []error
	if srv != nil {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, err)
		}
	}
	if rt != nil {
		if err := rt.Close(shutdownCtx); err != nil {
			errs = append(errs, err)
		}
	}
	slog.Info("AsterRouter service stopped")
	return errors.Join(errs...)
}

func ephemeralSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
