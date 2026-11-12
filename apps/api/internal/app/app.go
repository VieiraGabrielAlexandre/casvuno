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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/accounts"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/categories"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/households"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/identity"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/ledger"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/audit"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/clock"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/config"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/idempotency"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/logging"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
)

// Module wires the platform and the business modules. Tests may replace the clock and the
// password parameters with fx.Decorate.
var Module = fx.Options(
	fx.Provide(
		logging.New,
		newPool,
		postgres.NewTxManager,
		audit.NewRecorder,
		idempotency.New, // used by financial routes from F2.3 on (ADR 0007)
		func() clock.Clock { return clock.System{} },
		func() identity.Argon2Params { return identity.DefaultArgon2Params },
		newRouter,
		newHTTPServer,
	),
	identityModule,
	householdsModule,
	categoriesModule,
	accountsModule,
	ledgerModule,
	fx.Invoke(func(*http.Server) {}), // the server is the root of the object graph
)

var accountsModule = fx.Module("accounts",
	fx.Provide(
		accounts.NewService,
		accounts.NewHandler,
		// households implements the consumer-defined accounts.MemberChecker.
		func(s *households.Service) accounts.MemberChecker { return s },
	),
)

var ledgerModule = fx.Module("ledger",
	fx.Provide(
		ledger.NewService,
		ledger.NewHandler,
		// Consumer-defined interfaces of the ledger, implemented by other modules.
		func(s *categories.Service) ledger.CategoryChecker { return s },
		func(s *households.Service) ledger.MemberChecker { return s },
		func(s *accounts.Service) ledger.Cash { return ledgerCash{s} },
	),
)

// ledgerCash adapts the accounts module to ledger.Cash (direct payments and receipts of
// obligations), keeping the two modules unaware of each other.
type ledgerCash struct{ accounts *accounts.Service }

func (c ledgerCash) PostCash(ctx context.Context, householdID uuid.UUID, mv ledger.CashMovement) (uuid.UUID, error) {
	in := accounts.PostInput{
		AccountID: mv.AccountID, Direction: accounts.DirectionOut, Kind: accounts.KindDirectPayment,
		AmountCents: mv.AmountCents, PostedOn: mv.PostedOn, Description: mv.Description, Field: mv.Field,
	}
	if mv.Incoming {
		in.Direction, in.Kind = accounts.DirectionIn, accounts.KindDirectReceipt
	}
	return c.accounts.PostMovement(ctx, householdID, in)
}

func (c ledgerCash) ReverseCash(ctx context.Context, householdID uuid.UUID, movementIDs []uuid.UUID, on time.Time, description string) error {
	return c.accounts.ReverseMovements(ctx, householdID, movementIDs, on, description)
}

func (c ledgerCash) RequireActiveAccount(ctx context.Context, householdID, accountID uuid.UUID, field string) error {
	return c.accounts.RequireActive(ctx, householdID, accountID, field)
}

var categoriesModule = fx.Module("categories",
	fx.Provide(
		categories.NewService,
		categories.NewHandler,
		// categories implements the consumer-defined households.CategorySeeder.
		func(s *categories.Service) households.CategorySeeder { return s },
	),
)

var identityModule = fx.Module("identity",
	fx.Provide(
		identity.NewPasswordHasher,
		func(tx *postgres.TxManager, clk clock.Clock, h *identity.PasswordHasher, inv identity.InviteChecker, cfg config.Config) *identity.Service {
			return identity.NewService(tx, clk, h, inv, cfg.RegistrationEnabled)
		},
		func(svc *identity.Service, clk clock.Clock, log *slog.Logger, cfg config.Config) *identity.Handler {
			return identity.NewHandler(svc, clk, log, cfg.SecureCookies())
		},
	),
)

var householdsModule = fx.Module("households",
	fx.Provide(
		func(tx *postgres.TxManager, clk clock.Clock, rec *audit.Recorder, seeder households.CategorySeeder, cfg config.Config) *households.Service {
			return households.NewService(tx, clk, rec, seeder, cfg.AppOrigin)
		},
		households.NewHandler,
		// households implements the consumer-defined identity.InviteChecker.
		func(s *households.Service) identity.InviteChecker { return s },
	),
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
