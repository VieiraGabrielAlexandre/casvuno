// Command casvuno is the single Casvuno backend binary.
//
//	casvuno serve              start the HTTP API
//	casvuno migrate up         apply pending migrations
//	casvuno migrate down [N]   revert N migrations (default 1)
package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"casvuno/api/internal/app"
	"casvuno/api/internal/platform/config"
	"casvuno/api/internal/platform/postgres"
)

const usage = `usage:
  casvuno serve
  casvuno migrate up
  casvuno migrate down [N]`

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
