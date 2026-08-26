// Command brewops is an MCP server holding a speciality coffee shop's own
// knowledge: its catalogue, the recipes it has settled on for each lot, the
// roast profiles behind them, and what happened at the counter.
//
// It exists so a model does not have to invent numbers. Asked to scale a recipe
// or to explain why a brew ran fast, a language model produces figures that look
// right; a shop that needs the same cup twice cannot use figures that look
// right. Every tool here computes from the shop's records instead.
//
// It speaks MCP over stdio, as a local server launched by a host.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Jonialen/brewops-mcp/internal/server"
	"github.com/Jonialen/brewops-mcp/internal/store"
	"github.com/Jonialen/brewops-mcp/internal/tools"
)

const (
	serverName    = "brewops"
	serverVersion = "1.0.0"
)

func main() {
	dbPath := flag.String("db", defaultDatabasePath(),
		"path to the shop's database; it is created and seeded if absent")
	flag.Parse()

	// Logs go to stderr. On stdio, stdout carries protocol frames and nothing
	// else: one stray line there corrupts the stream for the client.
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags)

	if err := run(*dbPath); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("%s: %v", serverName, err)
	}
}

func run(dbPath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if dir := filepath.Dir(dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	shop, err := store.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer shop.Close()

	srv := server.New(serverName, serverVersion)
	tools.New(shop, nil).Register(srv)

	log.Printf("%s %s serving on stdio, records at %s", serverName, serverVersion, dbPath)
	return srv.ServeStdio(ctx, os.Stdin, os.Stdout)
}

// defaultDatabasePath keeps the shop's records with the user's other
// application data rather than in whatever directory the host happened to
// launch this server from.
func defaultDatabasePath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "brewops", "brewops.db")
	}
	return "brewops.db"
}
