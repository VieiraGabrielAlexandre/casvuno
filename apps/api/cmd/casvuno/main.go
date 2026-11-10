// Command casvuno is the single Casvuno backend binary.
//
//	casvuno serve                                  start the HTTP API
//	casvuno migrate up                             apply pending migrations
//	casvuno migrate down [N]                       revert N migrations (default 1)
//	casvuno admin reset-password --email E         set a new password (read from stdin) and revoke sessions
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	_ "time/tzdata" // household time zones must resolve even on minimal images

	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/app"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/identity"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/clock"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/config"
	"github.com/VieiraGabrielAlexandre/casvuno/apps/api/internal/platform/postgres"
)

const usage = `usage:
  casvuno serve
  casvuno migrate up
  casvuno migrate down [N]
  casvuno admin reset-password --email user@example.com   (new password read from stdin)`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	switch args[0] {
	case "serve":
		app.New(cfg).Run() // blocks until SIGINT/SIGTERM, exits non-zero on startup failure
		return nil
	case "migrate":
		return migrate(cfg, args[1:])
	case "admin":
		return admin(cfg, args[1:], os.Stdin)
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usage)
	}
}

func migrate(cfg config.Config, args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "up":
		if err := postgres.MigrateUp(cfg.DatabaseURL); err != nil {
			return err
		}
		fmt.Println("migrations applied")
		return nil
	case "down":
		steps := 1
		if len(args) > 1 {
			n, err := strconv.Atoi(args[1])
			if err != nil || n <= 0 {
				return fmt.Errorf("invalid number of steps %q", args[1])
			}
			steps = n
		}
		if err := postgres.MigrateDown(cfg.DatabaseURL, steps); err != nil {
			return err
		}
		fmt.Printf("reverted %d migration(s)\n", steps)
		return nil
	default:
		return fmt.Errorf("unknown migrate command %q\n%s", args[0], usage)
	}
}

// admin runs operator commands. Password recovery has no e-mail flow in V1 (I-12).
func admin(cfg config.Config, args []string, stdin io.Reader) error {
	if len(args) == 0 || args[0] != "reset-password" {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	email := fs.String("email", "", "e-mail of the user")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("--email is required")
	}

	fmt.Fprintln(os.Stderr, "New password (single line on stdin):")
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read password: %w", err)
	}
	password := strings.TrimRight(line, "\r\n")

	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	svc := identity.NewService(postgres.NewTxManager(pool), clock.System{},
		identity.NewPasswordHasher(identity.DefaultArgon2Params), nil, cfg.RegistrationEnabled)
	if err := svc.ResetPassword(ctx, *email, password); err != nil {
		return err
	}
	fmt.Println("password updated; all sessions of the user were revoked")
	return nil
}
