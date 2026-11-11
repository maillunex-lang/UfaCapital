package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maillunex-lang/UfaCapital/server/internal/economy"
	"github.com/maillunex-lang/UfaCapital/server/internal/httpapi"
	"github.com/maillunex-lang/UfaCapital/server/internal/identity"
	"github.com/maillunex-lang/UfaCapital/server/internal/migrate"
)

func TestMain(m *testing.M) {
	if os.Getenv("DATABASE_URL") == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required for integration tests")
		os.Exit(2)
	}
	os.Exit(m.Run())
}

type env struct {
	srv  *httptest.Server
	econ *economy.Service
	pool *pgxpool.Pool
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	name := "uc_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	base := os.Getenv("DATABASE_URL")
	url := dbURL(base, name)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate.Up(ctx, pool, migrationsDir()); err != nil {
		t.Fatal(err)
	}
	auth := &identity.Service{Pool: pool, ArgonMemory: 64, ArgonTime: 1, ArgonThreads: 1, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour}
	econ := &economy.Service{Pool: pool, Auth: auth, TestAdmin: true}
	if err := econ.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	h := (&httpapi.Server{Auth: auth, Econ: econ, Pool: pool, TestAdmin: true}).Handler()
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		srv.Close()
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
		admin.Close()
	})
	return &env{srv: srv, econ: econ, pool: pool}
}

func (e *env) post(t *testing.T, path, token, key string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return res.StatusCode, out
}

func data(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	d, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("no data: %#v", out)
	}
	return d
}

func register(t *testing.T, e *env, email, name string) string {
	t.Helper()
	code, out := e.post(t, "/api/v1/auth/register", "", "", map[string]string{
		"email": email, "password": "correct-horse-battery", "display_name": name,
	})
	if code != 200 {
		t.Fatalf("register %s: %d %#v", email, code, out)
	}
	return data(t, out)["access_token"].(string)
}

func TestEconomyFlow(t *testing.T) {
	e := newEnv(t)
	a := register(t, e, "player1@example.test", "Игрок Один")
	b := register(t, e, "player2@example.test", "Игрок Два")
	body := map[string]string{"name": "Мельница Юлдаш"}
	raw, _ := json.Marshal(body)
	code, out := e.post(t, "/api/v1/companies", a, "company-a", body)
	if code != 200 {
		t.Fatalf("company: %d %#v", code, out)
	}
	ca := data(t, out)
	if ca["grant"] != "100000" {
		t.Fatalf("grant %v", ca)
	}
	code, out = e.post(t, "/api/v1/companies", a, "company-a", body)
	if code != 200 || data(t, out)["company_id"] != ca["company_id"] {
		t.Fatalf("replay: %d %#v", code, out)
	}
	code, out = e.post(t, "/api/v1/companies", a, "company-a", map[string]string{"name": "Другое"})
	if code != 409 {
		t.Fatalf("idempotency conflict: %d %#v", code, out)
	}
	code, out = e.post(t, "/api/v1/companies", b, "company-b", map[string]string{"name": "Пекарня Сабантуй"})
	if code != 200 {
		t.Fatal(out)
	}
	cb := data(t, out)["company_id"].(string)
	code, out = e.post(t, "/api/v1/market/orders", a, "bad-qty", map[string]string{
		"company_id": ca["company_id"].(string), "good": "flour", "side": "buy", "price": "10", "qty": "-5",
	})
	if code != 400 {
		t.Fatalf("negative: %d %#v", code, out)
	}
	code, out = e.post(t, "/api/v1/market/orders", a, "overflow", map[string]string{
		"company_id": ca["company_id"].(string), "good": "flour", "side": "buy", "price": "9223372036854775807", "qty": "10",
	})
	if code != 400 {
		t.Fatalf("overflow: %d %#v", code, out)
	}
	var grainOrder string
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/api/v1/market", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var market map[string]any
	_ = json.NewDecoder(res.Body).Decode(&market)
	res.Body.Close()
	rows := market["data"].([]any)
	for _, row := range rows {
		m := row.(map[string]any)
		if m["good"] == "grain" && m["side"] == "sell" {
			grainOrder = m["order_id"].(string)
		}
	}
	if grainOrder == "" {
		t.Fatal("no grain offer")
	}
	code, out = e.post(t, "/api/v1/market/orders/"+grainOrder+"/accept", a, "buy-grain", map[string]string{"qty": "20", "max_total": "800"})
	if code != 200 {
		t.Fatalf("buy grain: %d %#v", code, out)
	}
	code, out = e.post(t, "/api/v1/enterprises", a, "open-mill", map[string]string{"company_id": ca["company_id"].(string), "template": "mill"})
	if code != 200 {
		t.Fatalf("mill: %d %#v", code, out)
	}
	mill := data(t, out)["enterprise_id"].(string)
	code, out = e.post(t, "/api/v1/enterprises/"+mill+"/workforce", a, "hire", map[string]string{"headcount": "2", "wage": "15"})
	if code != 200 {
		t.Fatalf("hire: %d %#v", code, out)
	}
	code, out = e.post(t, "/api/v1/enterprises/"+mill+"/policy", a, "run", map[string]bool{"running": true})
	if code != 200 {
		t.Fatal(out)
	}
	if err := e.econ.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.econ.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	var flour int64
	if err := e.pool.QueryRow(context.Background(), `SELECT COALESCE(SUM(qty),0) FROM inventory_lots WHERE company_id=$1 AND good_code='flour'`, ca["company_id"]).Scan(&flour); err != nil {
		t.Fatal(err)
	}
	if flour < 8 {
		t.Fatalf("flour %d", flour)
	}
	if err := e.econ.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(context.Background(), `SELECT COALESCE(SUM(qty),0) FROM inventory_lots WHERE company_id=$1 AND good_code='flour'`, ca["company_id"]).Scan(&flour); err != nil {
		t.Fatal(err)
	}
	if flour < 16 {
		t.Fatalf("flour after second batch %d", flour)
	}
	code, out = e.post(t, "/api/v1/market/orders", a, "sell-flour", map[string]string{
		"company_id": ca["company_id"].(string), "good": "flour", "side": "sell", "price": "70", "qty": "10",
	})
	if code != 200 {
		t.Fatalf("sell: %d %#v", code, out)
	}
	flourOrder := data(t, out)["order_id"].(string)
	code, out = e.post(t, "/api/v1/market/orders/"+flourOrder+"/accept", b, "buy-flour", map[string]string{"qty": "10", "max_total": "700"})
	if code != 200 {
		t.Fatalf("accept flour: %d %#v", code, out)
	}
	code, out = e.post(t, "/api/v1/enterprises", b, "open-bakery", map[string]string{"company_id": cb, "template": "bakery"})
	if code != 200 {
		t.Fatal(out)
	}
	bakery := data(t, out)["enterprise_id"].(string)
	code, _ = e.post(t, "/api/v1/enterprises/"+bakery+"/workforce", b, "hire-b", map[string]string{"headcount": "2", "wage": "15"})
	if code != 200 {
		t.Fatal(code)
	}
	_, _ = e.post(t, "/api/v1/enterprises/"+bakery+"/policy", b, "run-b", map[string]bool{"running": true})
	if err := e.econ.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.econ.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	var bread int64
	_ = e.pool.QueryRow(context.Background(), `SELECT COALESCE(SUM(qty),0) FROM inventory_lots WHERE company_id=$1 AND good_code='bread'`, cb).Scan(&bread)
	if bread < 16 {
		t.Fatalf("bread %d", bread)
	}
	assertBalanced(t, e.pool)
	// Restart: a new pool sees the same world and a repeated tick does not mint goods.
	pool2, err := pgxpool.New(context.Background(), e.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool2.Close()
	var tickBefore int64
	_ = pool2.QueryRow(context.Background(), `SELECT tick FROM world_state WHERE id=1`).Scan(&tickBefore)
	var jobsBefore int
	_ = pool2.QueryRow(context.Background(), `SELECT count(*) FROM production_jobs WHERE status='done'`).Scan(&jobsBefore)
	if err := e.econ.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	var jobsAfter int
	_ = pool2.QueryRow(context.Background(), `SELECT count(*) FROM production_jobs WHERE status='done'`).Scan(&jobsAfter)
	if jobsAfter < jobsBefore {
		t.Fatal("jobs lost")
	}
	assertBalanced(t, pool2)
	_ = raw
	// Race on a fresh small offer.
	code, out = e.post(t, "/api/v1/market/orders", a, "race-sell", map[string]string{
		"company_id": ca["company_id"].(string), "good": "flour", "side": "sell", "price": "10", "qty": "1",
	})
	if code != 200 {
		t.Fatal(out)
	}
	raceID := data(t, out)["order_id"].(string)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			c, _ := e.post(t, "/api/v1/market/orders/"+raceID+"/accept", b, fmt.Sprintf("race-%d", i), map[string]string{"qty": "1", "max_total": "10"})
			codes[i] = c
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, c := range codes {
		if c == 200 {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("race wins %d codes %v", wins, codes)
	}
	assertBalanced(t, e.pool)
}

func TestTickAdvancesWhenGrainWarehouseIsFull(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for i := 1; i <= 60; i++ {
		if err := e.econ.Tick(ctx); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}
	var tick int64
	if err := e.pool.QueryRow(ctx, `SELECT tick FROM world_state WHERE id=1`).Scan(&tick); err != nil {
		t.Fatal(err)
	}
	if tick != 60 {
		t.Fatalf("tick stopped at %d", tick)
	}
}

func TestAuthAndAdmin(t *testing.T) {
	e := newEnv(t)
	token := register(t, e, "adminish@example.test", "Админ")
	code, out := e.post(t, "/api/v1/auth/logout", token, "", map[string]string{})
	if code != 200 {
		t.Fatal(out)
	}
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("revoked session %d", res.StatusCode)
	}
	// Rate limit.
	for i := 0; i < 8; i++ {
		e.post(t, "/api/v1/auth/login", "", "", map[string]string{"email": "missing@example.test", "password": "wrong-password-here"})
	}
	code, out = e.post(t, "/api/v1/auth/login", "", "", map[string]string{"email": "missing@example.test", "password": "wrong-password-here"})
	if code != 429 {
		t.Fatalf("rate %d %#v", code, out)
	}
	code, _ = e.post(t, "/api/v1/test-admin/grants", token, "nope", map[string]string{"company_id": uuid.NewString(), "cash": "10", "reason": "не должен пройти"})
	if code != 401 && code != 403 {
		t.Fatalf("player grant %d", code)
	}
}

func TestMigrationUpgrade(t *testing.T) {
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := "uc_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
	url := dbURL(os.Getenv("DATABASE_URL"), name)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dir := t.TempDir()
	b, err := os.ReadFile(filepath.Join(migrationsDir(), "0001_identity.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/0001_identity.sql", b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := migrate.Up(ctx, pool, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users(id, email, password_hash, display_name) VALUES ($1,'up@example.test','x','Up')`, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if err := migrate.Up(ctx, pool, migrationsDir()); err != nil {
		t.Fatal(err)
	}
	var email string
	if err := pool.QueryRow(ctx, `SELECT email FROM users WHERE email='up@example.test'`).Scan(&email); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
}

func dbURL(base, name string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	u.Path = "/" + name
	return u.String()
}

func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}

func assertBalanced(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var ledger, proj int64
	if err := pool.QueryRow(context.Background(), `SELECT COALESCE(SUM(amount),0) FROM ledger_entries`).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT COALESCE(SUM(available+reserved),0) FROM accounts`).Scan(&proj); err != nil {
		t.Fatal(err)
	}
	if ledger != 0 || proj != 0 {
		t.Fatalf("imbalance ledger %d projection %d", ledger, proj)
	}
}
