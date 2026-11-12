package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maillunex-lang/UfaCapital/server/internal/domain"
	"github.com/maillunex-lang/UfaCapital/server/internal/economy"
	"github.com/maillunex-lang/UfaCapital/server/internal/identity"
	"github.com/maillunex-lang/UfaCapital/server/internal/money"
)

type Server struct {
	Auth      *identity.Service
	Econ      *economy.Service
	Pool      *pgxpool.Pool
	TestAdmin bool
	Secure    bool
	// AdminDir holds the built web admin. Empty means the panel is not served.
	AdminDir string
	// AdminLoopbackOnly hides everything under /admin from non-local clients.
	// It stays on until the panel is behind HTTPS.
	AdminLoopbackOnly bool
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /api/v1/auth/register", s.register)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/refresh", s.refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("GET /api/v1/auth/me", s.me)
	mux.HandleFunc("GET /api/v1/world/cities", s.cities)
	mux.HandleFunc("GET /api/v1/world/goods", s.goods)
	mux.HandleFunc("GET /api/v1/world/recipes", s.recipes)
	mux.HandleFunc("GET /api/v1/market", s.market)
	mux.HandleFunc("GET /api/v1/sync/snapshot", s.snapshot)
	mux.HandleFunc("GET /api/v1/sync/events", s.events)
	mux.HandleFunc("GET /api/v1/assets/manifest", s.manifest)
	mux.HandleFunc("POST /api/v1/companies", s.createCompany)
	mux.HandleFunc("GET /api/v1/companies/mine", s.myCompany)
	mux.HandleFunc("GET /api/v1/companies/board", s.board)
	mux.HandleFunc("POST /api/v1/enterprises", s.openEnterprise)
	mux.HandleFunc("POST /api/v1/enterprises/{id}/workforce", s.workforce)
	mux.HandleFunc("POST /api/v1/enterprises/{id}/policy", s.policy)
	mux.HandleFunc("POST /api/v1/enterprises/{id}/expand", s.expand)
	mux.HandleFunc("POST /api/v1/market/orders", s.placeOrder)
	mux.HandleFunc("POST /api/v1/market/orders/{id}/cancel", s.cancelOrder)
	mux.HandleFunc("POST /api/v1/market/orders/{id}/accept", s.acceptOrder)
	mux.HandleFunc("POST /api/v1/contracts", s.offerContract)
	mux.HandleFunc("POST /api/v1/contracts/{id}/accept", s.acceptContract)
	mux.HandleFunc("POST /api/v1/loans", s.borrow)
	mux.HandleFunc("POST /api/v1/loans/{id}/repay", s.repay)
	mux.HandleFunc("POST /api/v1/test-admin/grants", s.grant)
	mux.HandleFunc("POST /admin/api/v1/auth/login", s.adminLogin)
	mux.HandleFunc("POST /admin/api/v1/auth/totp", s.adminTOTP)
	mux.HandleFunc("POST /admin/api/v1/auth/logout", s.adminLogout)
	mux.HandleFunc("GET /admin/api/v1/me", s.adminMe)
	mux.HandleFunc("GET /admin/api/v1/players", s.adminPlayers)
	mux.HandleFunc("POST /admin/api/v1/players/{id}/roles", s.adminRoles)
	mux.HandleFunc("GET /admin/api/v1/audit", s.adminAudit)
	mux.HandleFunc("POST /admin/api/v1/recipes", s.adminRecipe)
	mux.HandleFunc("POST /admin/api/v1/config", s.adminConfig)
	mux.HandleFunc("POST /admin/api/v1/vip", s.adminVIP)
	mux.HandleFunc("GET /admin/api/v1/agents", s.adminAgents)
	mux.HandleFunc("POST /admin/api/v1/agents/{id}/pause", s.adminPause)
	mux.HandleFunc("GET /admin/api/v1/companies", s.adminCompanies)
	mux.HandleFunc("GET /admin/api/v1/config", s.adminConfigList)
	if s.AdminDir != "" {
		mux.Handle("GET /admin/", http.StripPrefix("/admin/", http.FileServer(http.Dir(s.AdminDir))))
	}
	return withCORS(s.guardAdmin(mux))
}

// guardAdmin sets security headers on /admin and, while AdminLoopbackOnly is set,
// answers 404 to clients from outside the machine.
// The check uses the socket address, not a header, so a client cannot spoof it.
func (s *Server) guardAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/") {
			if s.AdminLoopbackOnly {
				host, _, err := net.SplitHostPort(r.RemoteAddr)
				if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
					writeErr(w, r, domain.E(404, "NOT_FOUND", "не найдено"))
					return
				}
			}
			h := w.Header()
			h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
			h.Set("X-Frame-Options", "DENY")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			if strings.HasPrefix(r.URL.Path, "/admin/api/") {
				h.Set("Cache-Control", "no-store")
			}
		}
		next.ServeHTTP(w, r)
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-CSRF-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Pool.Ping(ctx); err != nil {
		writeErr(w, r, domain.E(503, "UNAVAILABLE", "база недоступна"))
		return
	}
	writeOK(w, r, map[string]string{"status": "ok"})
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	sess, err := s.Auth.Register(r.Context(), in.Email, in.Password, in.DisplayName)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeOK(w, r, sessionJSON(sess))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	sess, _, err := s.Auth.Login(r.Context(), in.Email, in.Password, "api")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeOK(w, r, sessionJSON(sess))
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Refresh string `json:"refresh_token"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	sess, err := s.Auth.Refresh(r.Context(), in.Refresh)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeOK(w, r, sessionJSON(sess))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	u, err := s.bearer(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	_ = u
	tok := bearerToken(r)
	if err := s.Auth.Logout(r.Context(), tok); err != nil {
		writeErr(w, r, err)
		return
	}
	writeOK(w, r, map[string]string{"status": "logged_out"})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u, err := s.bearer(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeOK(w, r, userJSON(u))
}

func (s *Server) cities(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `SELECT code, title, lat, lon FROM cities ORDER BY code`)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var code, title string
		var lat, lon float64
		if err := rows.Scan(&code, &title, &lat, &lon); err != nil {
			writeErr(w, r, err)
			return
		}
		out = append(out, map[string]any{"id": code, "title": title, "lat": lat, "lon": lon})
	}
	writeOK(w, r, out)
}

func (s *Server) goods(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `SELECT code, title, unit FROM goods WHERE active ORDER BY code`)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var a, b, c string
		if err := rows.Scan(&a, &b, &c); err != nil {
			writeErr(w, r, err)
			return
		}
		out = append(out, map[string]string{"id": a, "title": b, "unit": c})
	}
	writeOK(w, r, out)
}

func (s *Server) recipes(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `SELECT DISTINCT ON (code) code, version, inputs, outputs, energy_cost::text
		FROM recipe_versions WHERE active AND effective_at<=now() ORDER BY code, version DESC`)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var code, energy string
		var version int
		var in, outb []byte
		if err := rows.Scan(&code, &version, &in, &outb, &energy); err != nil {
			writeErr(w, r, err)
			return
		}
		out = append(out, map[string]any{"code": code, "version": money.Format(int64(version)), "inputs": json.RawMessage(in), "outputs": json.RawMessage(outb), "energy_cost": energy})
	}
	writeOK(w, r, out)
}

func (s *Server) market(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `SELECT id::text, good_code, side, price::text, qty_open::text, company_id::text
		FROM market_orders WHERE status='open' AND city_code='ufa' ORDER BY good_code, side, price`)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var id, good, side, price, qty, company string
		if err := rows.Scan(&id, &good, &side, &price, &qty, &company); err != nil {
			writeErr(w, r, err)
			return
		}
		out = append(out, map[string]string{"order_id": id, "good": good, "side": side, "price": price, "qty_open": qty, "company_id": company})
	}
	writeOK(w, r, out)
}

func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) {
	u, err := s.bearer(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	var tick int64
	_ = s.Pool.QueryRow(r.Context(), `SELECT tick FROM world_state WHERE id=1`).Scan(&tick)
	company, _ := s.Econ.MyCompany(r.Context(), u.ID)
	writeOK(w, r, map[string]any{"tick": money.Format(tick), "user": userJSON(u), "company": company, "city": "ufa"})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if _, err := s.bearer(r); err != nil {
		writeErr(w, r, err)
		return
	}
	cursor := r.URL.Query().Get("cursor")
	if cursor == "" {
		cursor = "0"
	}
	c, err := money.Parse(cursor)
	if err != nil && cursor != "0" {
		writeErr(w, r, err)
		return
	}
	rows, err := s.Pool.Query(r.Context(), `SELECT id::text, event_id::text, topic, payload FROM outbox WHERE id>$1 ORDER BY id LIMIT 100`, c)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	var out []map[string]any
	var last = cursor
	for rows.Next() {
		var id, eid, topic string
		var payload []byte
		if err := rows.Scan(&id, &eid, &topic, &payload); err != nil {
			writeErr(w, r, err)
			return
		}
		last = id
		out = append(out, map[string]any{"cursor": id, "event_id": eid, "topic": topic, "payload": json.RawMessage(payload)})
	}
	writeOK(w, r, map[string]any{"events": out, "next_cursor": last})
}

func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	var doc []byte
	var version int
	err := s.Pool.QueryRow(r.Context(), `SELECT version, document FROM asset_manifests WHERE active ORDER BY version DESC LIMIT 1`).Scan(&version, &doc)
	if err != nil {
		writeOK(w, r, map[string]any{"version": "0", "assets": []any{}})
		return
	}
	writeOK(w, r, map[string]any{"version": money.Format(int64(version)), "assets": json.RawMessage(doc)})
}

func (s *Server) createCompany(w http.ResponseWriter, r *http.Request) {
	u, raw, key, ok := s.econReq(w, r)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректное тело"))
		return
	}
	out, err := s.Econ.CreateCompany(r.Context(), u.ID, key, raw, strings.TrimSpace(in.Name))
	s.writeCmd(w, r, out, err)
}

func (s *Server) myCompany(w http.ResponseWriter, r *http.Request) {
	u, err := s.bearer(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.Econ.MyCompany(r.Context(), u.ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeOK(w, r, v)
}

func (s *Server) board(w http.ResponseWriter, r *http.Request) {
	u, err := s.bearer(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	v, err := s.Econ.PlayerBoard(r.Context(), u.ID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeOK(w, r, v)
}

func (s *Server) openEnterprise(w http.ResponseWriter, r *http.Request) {
	u, raw, key, ok := s.econReq(w, r)
	if !ok {
		return
	}
	var in struct {
		Company  string `json:"company_id"`
		Template string `json:"template"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректное тело"))
		return
	}
	cid, err := uuid.Parse(in.Company)
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректная компания"))
		return
	}
	out, err := s.Econ.OpenEnterprise(r.Context(), u.ID, key, raw, cid, in.Template)
	s.writeCmd(w, r, out, err)
}

func (s *Server) workforce(w http.ResponseWriter, r *http.Request) {
	u, raw, key, ok := s.econReq(w, r)
	if !ok {
		return
	}
	eid, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректный id"))
		return
	}
	var in struct {
		Headcount string `json:"headcount"`
		Wage      string `json:"wage"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректное тело"))
		return
	}
	h, err := money.Parse(in.Headcount)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	wage, err := money.Parse(in.Wage)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.Econ.SetWorkforce(r.Context(), u.ID, key, raw, eid, int(h), wage)
	s.writeCmd(w, r, out, err)
}

func (s *Server) policy(w http.ResponseWriter, r *http.Request) {
	u, raw, key, ok := s.econReq(w, r)
	if !ok {
		return
	}
	eid, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректный id"))
		return
	}
	var in struct {
		Running bool `json:"running"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректное тело"))
		return
	}
	out, err := s.Econ.SetPolicy(r.Context(), u.ID, key, raw, eid, in.Running)
	s.writeCmd(w, r, out, err)
}

func (s *Server) expand(w http.ResponseWriter, r *http.Request) {
	u, raw, key, ok := s.econReq(w, r)
	if !ok {
		return
	}
	eid, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректный id"))
		return
	}
	out, err := s.Econ.Expand(r.Context(), u.ID, key, raw, eid)
	s.writeCmd(w, r, out, err)
}

func (s *Server) placeOrder(w http.ResponseWriter, r *http.Request) {
	u, raw, key, ok := s.econReq(w, r)
	if !ok {
		return
	}
	var in struct {
		Company, Good, Side, Price, Qty string
	}
	dec := struct {
		Company string `json:"company_id"`
		Good    string `json:"good"`
		Side    string `json:"side"`
		Price   string `json:"price"`
		Qty     string `json:"qty"`
	}{}
	if err := json.Unmarshal(raw, &dec); err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректное тело"))
		return
	}
	_ = in
	cid, err := uuid.Parse(dec.Company)
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректная компания"))
		return
	}
	price, err := money.Parse(dec.Price)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	qty, err := money.Parse(dec.Qty)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.Econ.PlaceOrder(r.Context(), u.ID, key, raw, cid, dec.Good, dec.Side, price, qty)
	s.writeCmd(w, r, out, err)
}

func (s *Server) cancelOrder(w http.ResponseWriter, r *http.Request) {
	u, raw, key, ok := s.econReq(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректный id"))
		return
	}
	out, err := s.Econ.CancelOrder(r.Context(), u.ID, key, raw, id)
	s.writeCmd(w, r, out, err)
}

func (s *Server) acceptOrder(w http.ResponseWriter, r *http.Request) {
	u, raw, key, ok := s.econReq(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректный id"))
		return
	}
	var in struct {
		Qty      string `json:"qty"`
		MaxTotal string `json:"max_total"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректное тело"))
		return
	}
	qty, err := money.Parse(in.Qty)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	maxTotal, err := money.Parse(in.MaxTotal)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.Econ.AcceptOrder(r.Context(), u.ID, key, raw, id, qty, maxTotal)
	s.writeCmd(w, r, out, err)
}

func (s *Server) offerContract(w http.ResponseWriter, r *http.Request) {
	u, raw, key, ok := s.econReq(w, r)
	if !ok {
		return
	}
	var in struct {
		Company  string `json:"company_id"`
		Good     string `json:"good"`
		Qty      string `json:"qty"`
		Price    string `json:"price"`
		Deadline string `json:"deadline_tick"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректное тело"))
		return
	}
	cid, err := uuid.Parse(in.Company)
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректная компания"))
		return
	}
	qty, err := money.Parse(in.Qty)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	price, err := money.Parse(in.Price)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	deadline, err := money.Parse(in.Deadline)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.Econ.OfferContract(r.Context(), u.ID, key, raw, cid, in.Good, qty, price, deadline)
	s.writeCmd(w, r, out, err)
}

func (s *Server) acceptContract(w http.ResponseWriter, r *http.Request) {
	u, raw, key, ok := s.econReq(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректный id"))
		return
	}
	out, err := s.Econ.AcceptContract(r.Context(), u.ID, key, raw, id)
	s.writeCmd(w, r, out, err)
}

func (s *Server) borrow(w http.ResponseWriter, r *http.Request) {
	u, raw, key, ok := s.econReq(w, r)
	if !ok {
		return
	}
	var in struct {
		Company    string `json:"company_id"`
		Enterprise string `json:"enterprise_id"`
		Amount     string `json:"amount"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректное тело"))
		return
	}
	cid, err1 := uuid.Parse(in.Company)
	eid, err2 := uuid.Parse(in.Enterprise)
	amt, err3 := money.Parse(in.Amount)
	if err1 != nil || err2 != nil || err3 != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректное тело"))
		return
	}
	out, err := s.Econ.Borrow(r.Context(), u.ID, key, raw, cid, eid, amt)
	s.writeCmd(w, r, out, err)
}

func (s *Server) repay(w http.ResponseWriter, r *http.Request) {
	u, raw, key, ok := s.econReq(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректный id"))
		return
	}
	var in struct {
		Amount string `json:"amount"`
	}
	_ = json.Unmarshal(raw, &in)
	amt, err := money.Parse(in.Amount)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.Econ.Repay(r.Context(), u.ID, key, raw, id, amt)
	s.writeCmd(w, r, out, err)
}

func (s *Server) grant(w http.ResponseWriter, r *http.Request) {
	u, raw, key, ok := s.econReq(w, r)
	if !ok {
		return
	}
	var in struct {
		Company string `json:"company_id"`
		Cash    string `json:"cash"`
		Good    string `json:"good"`
		Qty     string `json:"qty"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректное тело"))
		return
	}
	cid, err := uuid.Parse(in.Company)
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректная компания"))
		return
	}
	cash := int64(0)
	if in.Cash != "" {
		cash, err = money.Parse(in.Cash)
		if err != nil {
			writeErr(w, r, err)
			return
		}
	}
	qty := int64(0)
	if in.Qty != "" {
		qty, err = money.Parse(in.Qty)
		if err != nil {
			writeErr(w, r, err)
			return
		}
	}
	out, err := s.Econ.Grant(r.Context(), u.ID, key, raw, cid, cash, qty, in.Good, in.Reason)
	s.writeCmd(w, r, out, err)
}

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	sess, mfa, err := s.Auth.Login(r.Context(), in.Email, in.Password, "admin")
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if mfa {
		writeOK(w, r, map[string]any{"mfa_required": true, "mfa_token": sess.Access})
		return
	}
	writeErr(w, r, domain.E(403, "MFA_ENROLL", "для админки нужен второй фактор"))
}

func (s *Server) adminTOTP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"mfa_token"`
		Code  string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	sess, err := s.Auth.ConfirmTOTP(r.Context(), in.Token, in.Code)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	s.setAdminCookies(w, sess)
	writeOK(w, r, map[string]any{"user": userJSON(sess.User)})
}

func (s *Server) adminLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("uc_admin")
	if err == nil {
		_ = s.Auth.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "uc_admin", Value: "", Path: "/admin", MaxAge: -1, HttpOnly: true})
	writeOK(w, r, map[string]string{"status": "logged_out"})
}

func (s *Server) adminMe(w http.ResponseWriter, r *http.Request) {
	u, err := s.adminUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeOK(w, r, userJSON(u))
}

func (s *Server) adminPlayers(w http.ResponseWriter, r *http.Request) {
	u, err := s.adminUser(r)
	if err != nil || !s.Auth.Has(u, "admin_read", "moderator", "superadmin") {
		writeErr(w, r, domain.E(403, "FORBIDDEN", "нет прав"))
		return
	}
	rows, err := s.Pool.Query(r.Context(), `SELECT u.id::text, u.email, u.display_name, u.is_ai, u.blocked_at IS NOT NULL,
		COALESCE((SELECT array_agg(role ORDER BY role) FROM user_roles WHERE user_id=u.id), '{}')
		FROM users u ORDER BY u.created_at DESC LIMIT 100`)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, email, name string
		var ai, blocked bool
		var roles []string
		if err := rows.Scan(&id, &email, &name, &ai, &blocked, &roles); err != nil {
			writeErr(w, r, err)
			return
		}
		out = append(out, map[string]any{"id": id, "email": email, "display_name": name, "is_ai": ai, "blocked": blocked, "roles": roles})
	}
	writeOK(w, r, out)
}

func (s *Server) adminRoles(w http.ResponseWriter, r *http.Request) {
	u, err := s.adminUser(r)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if !s.csrf(r, w) {
		return
	}
	target, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректный id"))
		return
	}
	var in struct {
		Roles  []string `json:"roles"`
		Reason string   `json:"reason"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	tx, err := s.Pool.Begin(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if err := s.Auth.SetRoles(r.Context(), tx, u, target, in.Roles, in.Reason); err != nil {
		writeErr(w, r, err)
		return
	}
	actor := u.ID
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_log(id, actor_id, action, reason, correlation_id) VALUES ($1,$2,'set_roles',$3,$4)`,
		uuid.New(), actor, in.Reason, uuid.New()); err != nil {
		writeErr(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, r, err)
		return
	}
	writeOK(w, r, map[string]any{"user_id": target.String(), "roles": in.Roles})
}

func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request) {
	u, err := s.adminUser(r)
	if err != nil || !s.Auth.Has(u, "admin_read") {
		writeErr(w, r, domain.E(403, "FORBIDDEN", "нет прав"))
		return
	}
	rows, err := s.Pool.Query(r.Context(), `SELECT l.id::text, l.action, l.reason, l.created_at, COALESCE(u.display_name, 'система'), l.before_state, l.after_state
		FROM audit_log l LEFT JOIN users u ON u.id=l.actor_id ORDER BY l.created_at DESC, l.id LIMIT 200`)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, action, reason, actor string
		var before, after json.RawMessage
		var at time.Time
		if err := rows.Scan(&id, &action, &reason, &at, &actor, &before, &after); err != nil {
			writeErr(w, r, err)
			return
		}
		out = append(out, map[string]any{"id": id, "action": action, "reason": reason, "at": at.UTC().Format(time.RFC3339), "actor": actor, "before": before, "after": after})
	}
	writeOK(w, r, out)
}

func (s *Server) adminRecipe(w http.ResponseWriter, r *http.Request) {
	u, err := s.adminUser(r)
	if err != nil || !s.Auth.Has(u, "catalog_editor") {
		writeErr(w, r, errOr(err, domain.E(403, "FORBIDDEN", "нет прав")))
		return
	}
	if !s.csrf(r, w) {
		return
	}
	var in struct {
		Code            string         `json:"code"`
		Inputs          []economy.Item `json:"inputs"`
		Outputs         []economy.Item `json:"outputs"`
		Energy          string         `json:"energy_cost"`
		Duration        string         `json:"duration_ticks"`
		ExpectedVersion string         `json:"expected_version"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	energy, err := money.Parse(in.Energy)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	dur, err := money.Parse(in.Duration)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	exp, err := money.Parse(in.ExpectedVersion)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.Econ.AddRecipe(r.Context(), u.ID, in.Code, toItems(in.Inputs), toItems(in.Outputs), energy, int(dur), int(exp))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeOK(w, r, out)
}

func (s *Server) adminConfig(w http.ResponseWriter, r *http.Request) {
	u, err := s.adminUser(r)
	if err != nil || !s.Auth.Has(u, "economy_admin") {
		writeErr(w, r, errOr(err, domain.E(403, "FORBIDDEN", "нет прав")))
		return
	}
	if !s.csrf(r, w) {
		return
	}
	var in struct {
		Key             string `json:"key"`
		Value           string `json:"value"`
		ExpectedVersion string `json:"expected_version"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	exp, err := money.Parse(in.ExpectedVersion)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.Econ.SetConfig(r.Context(), u.ID, in.Key, in.Value, int(exp))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeOK(w, r, out)
}

func (s *Server) adminConfigList(w http.ResponseWriter, r *http.Request) {
	u, err := s.adminUser(r)
	if err != nil || !s.Auth.Has(u, "admin_read", "economy_admin") {
		writeErr(w, r, errOr(err, domain.E(403, "FORBIDDEN", "нет прав")))
		return
	}
	rows, err := s.Pool.Query(r.Context(), `SELECT DISTINCT ON (key) key, version, value #>> '{}', effective_at
		FROM config_versions ORDER BY key, version DESC`)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var key, value string
		var version int
		var at time.Time
		if err := rows.Scan(&key, &version, &value, &at); err != nil {
			writeErr(w, r, err)
			return
		}
		out = append(out, map[string]string{"key": key, "version": money.Format(int64(version)), "value": value, "effective_at": at.UTC().Format(time.RFC3339)})
	}
	writeOK(w, r, out)
}

func (s *Server) adminVIP(w http.ResponseWriter, r *http.Request) {
	u, err := s.adminUser(r)
	if err != nil || !s.Auth.Has(u, "moderator", "superadmin") {
		writeErr(w, r, errOr(err, domain.E(403, "FORBIDDEN", "нет прав")))
		return
	}
	if !s.csrf(r, w) {
		return
	}
	var in struct {
		User   string `json:"user_id"`
		Hours  string `json:"hours"`
		Reason string `json:"reason"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	uid, err := uuid.Parse(in.User)
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректный пользователь"))
		return
	}
	hours, err := money.Parse(in.Hours)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	out, err := s.Econ.SetVIP(r.Context(), u.ID, uid, int(hours), in.Reason)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	writeOK(w, r, out)
}

func (s *Server) adminAgents(w http.ResponseWriter, r *http.Request) {
	u, err := s.adminUser(r)
	if err != nil || !s.Auth.Has(u, "admin_read") {
		writeErr(w, r, domain.E(403, "FORBIDDEN", "нет прав"))
		return
	}
	rows, err := s.Pool.Query(r.Context(), `SELECT a.id::text, u.display_name, a.profile, a.specialization, a.paused, a.strategy, a.loss_streak
		FROM agents a JOIN users u ON u.id=a.user_id ORDER BY u.display_name`)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, name, profile, spec, strategy string
		var paused bool
		var losses int
		if err := rows.Scan(&id, &name, &profile, &spec, &paused, &strategy, &losses); err != nil {
			writeErr(w, r, err)
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "profile": profile, "specialization": spec, "paused": paused, "strategy": strategy, "loss_streak": losses, "is_ai": true})
	}
	writeOK(w, r, out)
}

func (s *Server) adminPause(w http.ResponseWriter, r *http.Request) {
	u, err := s.adminUser(r)
	if err != nil || !s.Auth.Has(u, "economy_admin") {
		writeErr(w, r, errOr(err, domain.E(403, "FORBIDDEN", "нет прав")))
		return
	}
	if !s.csrf(r, w) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректный id"))
		return
	}
	var in struct {
		Paused bool   `json:"paused"`
		Reason string `json:"reason"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.Econ.PauseAgent(r.Context(), u.ID, id, in.Paused, in.Reason); err != nil {
		writeErr(w, r, err)
		return
	}
	writeOK(w, r, map[string]any{"id": id.String(), "paused": in.Paused})
}

func (s *Server) adminCompanies(w http.ResponseWriter, r *http.Request) {
	u, err := s.adminUser(r)
	if err != nil || !s.Auth.Has(u, "admin_read") {
		writeErr(w, r, domain.E(403, "FORBIDDEN", "нет прав"))
		return
	}
	rows, err := s.Pool.Query(r.Context(), `SELECT c.id::text, c.name, c.owner_kind, c.status, a.available::text, a.reserved::text
		FROM companies c JOIN accounts a ON a.id=c.cash_account_id ORDER BY c.created_at`)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var id, name, kind, status, cash, reserved string
		if err := rows.Scan(&id, &name, &kind, &status, &cash, &reserved); err != nil {
			writeErr(w, r, err)
			return
		}
		out = append(out, map[string]string{"id": id, "name": name, "owner_kind": kind, "status": status, "cash": cash, "reserved": reserved})
	}
	writeOK(w, r, out)
}

func (s *Server) econReq(w http.ResponseWriter, r *http.Request) (identity.User, []byte, string, bool) {
	u, err := s.bearer(r)
	if err != nil {
		writeErr(w, r, err)
		return identity.User{}, nil, "", false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "слишком большое тело"))
		return identity.User{}, nil, "", false
	}
	return u, raw, r.Header.Get("Idempotency-Key"), true
}

func (s *Server) writeCmd(w http.ResponseWriter, r *http.Request, out any, err error) {
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if rep, ok := out.(economy.Replay); ok {
		var payload any
		if err := json.Unmarshal(rep.Body, &payload); err != nil {
			writeErr(w, r, err)
			return
		}
		writeJSON(w, rep.Status, map[string]any{
			"request_id":  requestID(),
			"server_time": time.Now().UTC().Format(time.RFC3339),
			"data":        payload,
		})
		return
	}
	writeOK(w, r, out)
}

func (s *Server) bearer(r *http.Request) (identity.User, error) {
	tok := bearerToken(r)
	if tok == "" {
		return identity.User{}, domain.E(401, "NO_SESSION", "нужна сессия")
	}
	u, _, _, err := s.Auth.Authenticate(r.Context(), tok, "api")
	return u, err
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

func (s *Server) adminUser(r *http.Request) (identity.User, error) {
	c, err := r.Cookie("uc_admin")
	if err != nil {
		return identity.User{}, domain.E(401, "NO_SESSION", "нужна сессия")
	}
	u, _, _, err := s.Auth.Authenticate(r.Context(), c.Value, "admin")
	return u, err
}

func (s *Server) csrf(r *http.Request, w http.ResponseWriter) bool {
	c, err := r.Cookie("uc_csrf")
	if err != nil || c.Value == "" || c.Value != r.Header.Get("X-CSRF-Token") {
		writeErr(w, r, domain.E(403, "CSRF", "не совпал CSRF-токен"))
		return false
	}
	return true
}

func (s *Server) setAdminCookies(w http.ResponseWriter, sess identity.Session) {
	http.SetCookie(w, &http.Cookie{
		Name: "uc_admin", Value: sess.Access, Path: "/admin", HttpOnly: true,
		Secure: s.Secure, SameSite: http.SameSiteLaxMode, Expires: sess.ExpiresAt,
	})
	http.SetCookie(w, &http.Cookie{
		Name: "uc_csrf", Value: sess.CSRF, Path: "/admin",
		Secure: s.Secure, SameSite: http.SameSiteLaxMode, Expires: sess.ExpiresAt,
	})
}

func readJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		writeErr(w, r, domain.E(400, "BAD_JSON", "некорректное тело"))
		return false
	}
	return true
}

func writeOK(w http.ResponseWriter, r *http.Request, data any) {
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id":  requestID(),
		"server_time": time.Now().UTC().Format(time.RFC3339),
		"data":        data,
	})
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	status := 500
	code := "INTERNAL"
	msg := "внутренняя ошибка"
	var d *domain.Error
	if errors.As(err, &d) {
		status = d.Status
		code = d.Code
		msg = d.Msg
	}
	writeJSON(w, status, map[string]any{
		"request_id":  requestID(),
		"server_time": time.Now().UTC().Format(time.RFC3339),
		"error":       map[string]string{"code": code, "message": msg},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func requestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return uuid.NewSHA1(uuid.NameSpaceOID, b).String()
}

func sessionJSON(sess identity.Session) map[string]any {
	return map[string]any{
		"access_token":      sess.Access,
		"refresh_token":     sess.Refresh,
		"access_expires_at": sess.ExpiresAt.UTC().Format(time.RFC3339),
		"user":              userJSON(sess.User),
	}
}

func userJSON(u identity.User) map[string]any {
	roles := u.Roles
	if roles == nil {
		roles = []string{}
	}
	var vip any
	if u.VIPUntil != nil {
		vip = u.VIPUntil.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"id": u.ID.String(), "email": u.Email, "display_name": u.DisplayName,
		"is_ai": u.IsAI, "roles": roles, "vip_until": vip,
	}
}

func errOr(err error, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

func toItems(in []economy.Item) []economy.Item { return in }
