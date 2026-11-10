// Package app is the composition root: the only place that knows about Uber Fx.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/config"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/logging"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
)

// Module wires the platform. Business modules are added here as they are implemented.
var Module = fx.Options(
	fx.Provide(
		logging.New,
		newPool,
		newRouter,
		newHTTPServer,
	),
	fx.Invoke(func(*http.Server) {}), // the server is the root of the object graph
)

// New builds the application for the given configuration.
func New(cfg config.Config, extra ...fx.Option) *fx.App {
	opts := []fx.Option{
		fx.Supply(cfg),
		Module,
		fx.StopTimeout(cfg.ShutdownTimeout),
		fx.WithLogger(func(log *slog.Logger) fxevent.Logger {
			l := &fxevent.SlogLogger{Logger: log.With(slog.String("component", "fx"))}
			l.UseLogLevel(slog.LevelDebug)
			return l
		}),
	}
	return fx.New(append(opts, extra...)...)
}

const startupPingTimeout = 5 * time.Second

func newPool(lc fx.Lifecycle, cfg config.Config) (*pgxpool.Pool, error) {
	pool, err := postgres.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{
		// Fail fast at startup when the database is unreachable.
		OnStart: func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, startupPingTimeout)
			defer cancel()
			if err := pool.Ping(ctx); err != nil {
				return fmt.Errorf("database unreachable: %w", err)
			}
			return nil
		},
		OnStop: func(context.Context) error {
			pool.Close()
			return nil
		},
	})
	return pool, nil
}

func newHTTPServer(lc fx.Lifecycle, sd fx.Shutdowner, cfg config.Config, handler http.Handler, log *slog.Logger) *http.Server {
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
	}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", srv.Addr)
			if err != nil {
				return fmt.Errorf("listen on %s: %w", srv.Addr, err)
			}
			log.Info("http server listening", slog.String("addr", ln.Addr().String()))
			go func() {
				if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Error("http server stopped unexpectedly", slog.Any("error", err))
					_ = sd.Shutdown(fx.ExitCode(1))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("http server shutting down")
			return srv.Shutdown(ctx)
		},
	})
	return srv
}
