package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maillunex-lang/UfaCapital/server/internal/economy"
	"github.com/maillunex-lang/UfaCapital/server/internal/httpapi"
	"github.com/maillunex-lang/UfaCapital/server/internal/identity"
	"github.com/maillunex-lang/UfaCapital/server/internal/migrate"
)

func main() {
	addr := getenv("HTTP_ADDR", ":8088")
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := migrate.Up(ctx, pool, getenv("MIGRATIONS_DIR", "server/migrations")); err != nil {
		log.Fatal(err)
	}
	auth := &identity.Service{
		Pool:         pool,
		ArgonMemory:  uint32(getenvInt("ARGON_MEMORY", 19*1024)),
		ArgonTime:    uint32(getenvInt("ARGON_TIME", 2)),
		ArgonThreads: 1,
	}
	econ := &economy.Service{
		Pool:      pool,
		Auth:      auth,
		TestAdmin: os.Getenv("TEST_ADMIN_ENABLED") == "true",
		AssetsDir: getenv("ASSET_DIR", "data/assets"),
	}
	if err := econ.Bootstrap(ctx); err != nil {
		log.Fatal(err)
	}
	if os.Getenv("UC_DISABLE_SCHEDULER") != "1" {
		go func() {
			every := time.Duration(getenvInt("TICK_EVERY_MS", 2000)) * time.Millisecond
			t := time.NewTicker(every)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if err := econ.Tick(context.Background()); err != nil {
						log.Printf("tick: %v", err)
					}
				}
			}
		}()
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           (&httpapi.Server{Auth: auth, Econ: econ, Pool: pool, TestAdmin: econ.TestAdmin, Secure: os.Getenv("ENVIRONMENT") == "prod"}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sh)
	}()
	log.Printf("ufacapital listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	return n
}
