package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maillunex-lang/UfaCapital/server/internal/httpapi"
	"github.com/maillunex-lang/UfaCapital/server/internal/identity"
)

const adminPassword = "test-only-admin-password-1"

type adminClient struct {
	t    *testing.T
	base string
	http *http.Client
}

func (c *adminClient) do(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	for _, ck := range c.http.Jar.Cookies(req.URL) {
		if ck.Name == "uc_csrf" {
			req.Header.Set("X-CSRF-Token", ck.Value)
		}
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return res.StatusCode, out
}

func errCode(out map[string]any) string {
	e, _ := out["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

// loginAdmin bootstraps the first superadmin and walks password + TOTP.
func loginAdmin(t *testing.T, e *env) *adminClient {
	t.Helper()
	secret, err := e.econ.Auth.BootstrapAdmin(context.Background(), "root@example.test", adminPassword, "Админ")
	if err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	c := &adminClient{t: t, base: e.srv.URL, http: &http.Client{Jar: jar}}
	code, out := c.do("POST", "/admin/api/v1/auth/login", map[string]string{"email": "root@example.test", "password": adminPassword})
	if code != 200 {
		t.Fatalf("login: %d %#v", code, out)
	}
	mfa, _ := data(t, out)["mfa_token"].(string)
	if mfa == "" {
		t.Fatalf("no mfa_token: %#v", out)
	}
	otp, err := identity.DebugTOTP(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if code, out = c.do("POST", "/admin/api/v1/auth/totp", map[string]string{"mfa_token": mfa, "code": otp}); code != 200 {
		t.Fatalf("totp: %d %#v", code, out)
	}
	return c
}

func list(t *testing.T, out map[string]any) []any {
	t.Helper()
	l, ok := out["data"].([]any)
	if !ok {
		t.Fatalf("no list: %#v", out)
	}
	return l
}

func TestAdminBootstrapOnlyOnce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.econ.Auth.BootstrapAdmin(ctx, "a@example.test", "short", "A"); err == nil {
		t.Fatal("short password accepted")
	}
	if _, err := e.econ.Auth.BootstrapAdmin(ctx, "a@example.test", adminPassword, "A"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.econ.Auth.BootstrapAdmin(ctx, "b@example.test", adminPassword, "B"); err == nil {
		t.Fatal("second superadmin created")
	}
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='bootstrap_admin'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("bootstrap audit rows = %d, %v", n, err)
	}
}

func TestAdminConfigChangeIsAudited(t *testing.T) {
	e := newEnv(t)
	c := loginAdmin(t, e)

	code, out := c.do("GET", "/admin/api/v1/config", nil)
	if code != 200 {
		t.Fatalf("config list: %d %#v", code, out)
	}
	version := ""
	for _, it := range list(t, out) {
		m := it.(map[string]any)
		if m["key"] == "grain_price" {
			version, _ = m["version"].(string)
		}
	}
	if version == "" {
		t.Fatalf("grain_price not listed: %#v", out)
	}

	// Without the CSRF header the change must be refused.
	raw, _ := json.Marshal(map[string]string{"key": "grain_price", "value": "777", "expected_version": version})
	req, _ := http.NewRequest("POST", e.srv.URL+"/admin/api/v1/config", bytes.NewReader(raw))
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("no CSRF header: %d", res.StatusCode)
	}

	body := map[string]string{"key": "grain_price", "value": "777", "expected_version": version}
	if code, out = c.do("POST", "/admin/api/v1/config", body); code != 200 {
		t.Fatalf("set config: %d %#v", code, out)
	}
	// Same expected_version again is stale.
	if code, out = c.do("POST", "/admin/api/v1/config", body); code != 409 {
		t.Fatalf("stale write: %d %#v", code, out)
	}
	if code, out = c.do("POST", "/admin/api/v1/config", map[string]string{"key": "no_such_key", "value": "1", "expected_version": "0"}); code != 404 || errCode(out) != "UNKNOWN_KEY" {
		t.Fatalf("unknown key: %d %#v", code, out)
	}

	_, out = c.do("GET", "/admin/api/v1/config", nil)
	found := false
	for _, it := range list(t, out) {
		m := it.(map[string]any)
		if m["key"] == "grain_price" && m["value"] == "777" && m["version"] != version {
			found = true
		}
	}
	if !found {
		t.Fatalf("new value not listed: %#v", out)
	}

	code, out = c.do("GET", "/admin/api/v1/audit", nil)
	if code != 200 {
		t.Fatalf("audit: %d %#v", code, out)
	}
	var mine int
	for _, it := range list(t, out) {
		m := it.(map[string]any)
		if m["actor"] == "Админ" && m["after"] != nil {
			mine++
		}
	}
	if mine != 1 {
		t.Fatalf("want exactly one audit row by the admin, got %d: %#v", mine, out)
	}

	code, out = c.do("GET", "/admin/api/v1/players", nil)
	if code != 200 {
		t.Fatalf("players: %d %#v", code, out)
	}
	hasRoles := false
	for _, it := range list(t, out) {
		m := it.(map[string]any)
		if m["email"] == "root@example.test" {
			r, _ := m["roles"].([]any)
			hasRoles = len(r) == 2
		}
	}
	if !hasRoles {
		t.Fatalf("admin roles not listed: %#v", out)
	}
}

func TestAdminRefusesPlayersAndStrangers(t *testing.T) {
	e := newEnv(t)
	token := register(t, e, "p@example.test", "Игрок")

	// No cookie.
	res, err := http.Get(e.srv.URL + "/admin/api/v1/config")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 && res.StatusCode != 403 {
		t.Fatalf("anonymous config: %d", res.StatusCode)
	}
	// A player's game token is not an admin session.
	req, _ := http.NewRequest("GET", e.srv.URL+"/admin/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.AddCookie(&http.Cookie{Name: "uc_admin", Value: token})
	if res, err = http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 && res.StatusCode != 403 {
		t.Fatalf("player config: %d", res.StatusCode)
	}
	// A player without TOTP cannot get past admin login.
	jar, _ := cookiejar.New(nil)
	c := &adminClient{t: t, base: e.srv.URL, http: &http.Client{Jar: jar}}
	if code, out := c.do("POST", "/admin/api/v1/auth/login", map[string]string{"email": "p@example.test", "password": "correct-horse-battery"}); code == 200 {
		t.Fatalf("player passed admin login: %#v", out)
	}
}

func TestAdminLoopbackOnlyAndStatic(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<title>panel</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := (&httpapi.Server{Auth: e.econ.Auth, Econ: e.econ, Pool: e.pool, AdminDir: dir, AdminLoopbackOnly: true}).Handler()
	get := func(path, remote string, hdr map[string]string) int {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = remote
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if c := get("/admin/", "127.0.0.1:5000", nil); c != 200 {
		t.Fatalf("local panel: %d", c)
	}
	for _, p := range []string{"/admin/", "/admin", "/admin/api/v1/me", "/admin/index.html"} {
		if c := get(p, "203.0.113.9:5000", map[string]string{"X-Forwarded-For": "127.0.0.1"}); c != 404 {
			t.Fatalf("remote %s: %d", p, c)
		}
	}
	if c := get("/healthz", "203.0.113.9:5000", nil); c != 200 {
		t.Fatalf("remote healthz: %d", c)
	}
}
