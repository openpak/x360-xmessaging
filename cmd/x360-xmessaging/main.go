// Command x360-xmessaging serves the Xbox Live message inbox, Live service 18 (see internal/xmessaging).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openpak/x360-common/wire"
	"github.com/openpak/x360-common/xuacsapi"
	"github.com/openpak/x360-xmessaging/internal/xmessaging"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 30*time.Second)
	// X360_XMESSAGING_STATE is only the old inbox.json, imported once when the database is empty.
	store, err := xmessaging.Open(dbCtx, os.Getenv("X360_XMESSAGING_DATABASE_URL"), xmessaging.Schema,
		os.Getenv("X360_XMESSAGING_STATE"))
	dbCancel()
	if err != nil {
		log.Error("state: X360_XMESSAGING_DATABASE_URL is required and must be reachable", "err", err)
		os.Exit(2)
	}
	defer store.Close()
	log.Info("state in Postgres", "schema", xmessaging.Schema)
	if os.Getenv("X360_XUACS_INTERNAL_URL") == "" || os.Getenv("X360_XUACS_INTERNAL_KEY") == "" {
		log.Error("X360_XUACS_INTERNAL_URL and X360_XUACS_INTERNAL_KEY are required")
		os.Exit(2)
	}
	h := &xmessaging.Handler{Store: store, Log: log,
		IDs: &xuacsapi.Client{URL: os.Getenv("X360_XUACS_INTERNAL_URL"), Key: os.Getenv("X360_XUACS_INTERNAL_KEY")}}

	srvs := []*http.Server{{Addr: env("X360_XMESSAGING_ADDR", ":10018"), Handler: wire.Guard(os.Getenv("X360_GATEWAY_KEY"), h), ReadHeaderTimeout: 10 * time.Second}}
	if k := os.Getenv("X360_XMESSAGING_INTERNAL_KEY"); k != "" {
		srvs = append(srvs, &http.Server{Addr: env("X360_XMESSAGING_INTERNAL_ADDR", ":10118"), Handler: h.Internal(k), ReadHeaderTimeout: 10 * time.Second})
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, len(srvs))
	for _, s := range srvs {
		log.Info("x360-xmessaging listening", "addr", s.Addr)
		go func() { errc <- s.ListenAndServe() }()
	}
	select {
	case <-ctx.Done():
		sc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, s := range srvs {
			s.Shutdown(sc)
		}
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
	}
}
