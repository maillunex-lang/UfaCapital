package economy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maillunex-lang/UfaCapital/server/internal/domain"
	"github.com/maillunex-lang/UfaCapital/server/internal/identity"
)

const (
	accEmission = "10000000-0000-4000-8000-000000000001"
	accHuman    = "10000000-0000-4000-8000-000000000002"
	accAI       = "10000000-0000-4000-8000-000000000003"
	accCredit   = "10000000-0000-4000-8000-000000000004"
	accCity     = "10000000-0000-4000-8000-000000000005"
	accTest     = "10000000-0000-4000-8000-000000000006"
)

type Service struct {
	Pool      *pgxpool.Pool
	AssetsDir string
	TestAdmin bool
	Auth      *identity.Service
}

type Replay struct {
	Status int
	Body   []byte
}

func (s *Service) run(ctx context.Context, actor uuid.UUID, route, key string, raw []byte, fn func(tx pgx.Tx) (any, error)) (any, error) {
	if key == "" {
		return nil, domain.E(400, "IDEMPOTENCY_KEY", "нужен заголовок Idempotency-Key")
	}
	hash := sha256.Sum256(raw)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `INSERT INTO idempotency_keys(actor_id, route, idem_key, body_hash, status_code, response)
		VALUES ($1,$2,$3,$4,0,'null') ON CONFLICT DO NOTHING`, actor, route, key, hash[:])
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		var got []byte
		var status int
		var resp []byte
		err = tx.QueryRow(ctx, `SELECT body_hash, status_code, response FROM idempotency_keys
			WHERE actor_id=$1 AND route=$2 AND idem_key=$3 FOR UPDATE`, actor, route, key).Scan(&got, &status, &resp)
		if err != nil {
			return nil, err
		}
		if sha256Diff(got, hash[:]) {
			return nil, domain.E(409, "IDEMPOTENCY_CONFLICT", "ключ уже использован с другим телом")
		}
		if status == 0 {
			return nil, domain.E(409, "IDEMPOTENCY_CONFLICT", "запрос ещё выполняется")
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return Replay{Status: status, Body: resp}, nil
	}
	if err := s.lockWorld(ctx, tx); err != nil {
		return nil, err
	}
	data, err := fn(tx)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE idempotency_keys SET status_code=200, response=$4
		WHERE actor_id=$1 AND route=$2 AND idem_key=$3`, actor, route, key, body); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return data, nil
}

func sha256Diff(a, b []byte) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}

func (s *Service) Bootstrap(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM world_state`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return tx.Commit(ctx)
	}
	if err := s.lockWorld(ctx, tx); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO cities(code, title, lat, lon) VALUES ('ufa','Уфа',54.7388,55.9721)`)
	if err != nil {
		return err
	}
	for _, g := range [][3]string{{"grain", "Зерно", "мешок"}, {"flour", "Мука", "мешок"}, {"bread", "Хлеб", "буханка"}} {
		if _, err := tx.Exec(ctx, `INSERT INTO goods(code, title, unit) VALUES ($1,$2,$3)`, g[0], g[1], g[2]); err != nil {
			return err
		}
	}
	templates := []struct {
		code, title, recipe       string
		rent, expand, store, book int64
		workers                   int
	}{
		{"mill", "Мельница", "grain_to_flour", 8000, 12000, 400, 80000, 2},
		{"bakery", "Пекарня", "flour_to_bread", 9000, 14000, 300, 90000, 2},
		{"shop", "Магазин", "", 5000, 7000, 200, 40000, 1},
		{"warehouse", "Склад", "", 4000, 6000, 800, 30000, 0},
	}
	for _, t := range templates {
		var recipe any
		if t.recipe != "" {
			recipe = t.recipe
		}
		if _, err := tx.Exec(ctx, `INSERT INTO enterprise_templates(code, title, rent, expand_cost, min_workers, storage, book_value, recipe_code)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, t.code, t.title, t.rent, t.expand, t.workers, t.store, t.book, recipe); err != nil {
			return err
		}
	}
	if err := insertRecipe(ctx, tx, "grain_to_flour", 1, []Item{{"grain", "10"}}, []Item{{"flour", "8"}}, 100, 1); err != nil {
		return err
	}
	if err := insertRecipe(ctx, tx, "flour_to_bread", 1, []Item{{"flour", "10"}}, []Item{{"bread", "16"}}, 120, 1); err != nil {
		return err
	}
	defaults := map[string]string{
		"human_grant": "100000", "ai_grant": "100000", "labor_cap": "400",
		"population_daily_income": "20000", "population_max_bread_price": "90",
		"grain_daily_cap": "400", "grain_price": "40",
		"bread_spoil_ticks": "24", "flour_spoil_ticks": "168", "grain_spoil_ticks": "168",
		"loan_rate_bps": "500", "loan_period_ticks": "24", "loan_ltv_bps": "5000",
		"default_wage": "15",
	}
	for k, v := range defaults {
		raw, _ := json.Marshal(v)
		if _, err := tx.Exec(ctx, `INSERT INTO config_versions(id, key, version, value, effective_at) VALUES ($1,$2,1,$3,'2000-01-01')`,
			uuid.New(), k, raw); err != nil {
			return err
		}
	}
	limit := int64(100_000_000_000)
	funds := []struct {
		id, code, class string
		limit           *int64
	}{
		{accEmission, "emission", "emission", &limit},
		{accHuman, "human_grant_fund", "fund", nil},
		{accAI, "ai_startup_fund", "fund", nil},
		{accCredit, "credit_fund", "fund", nil},
		{accCity, "city_ufa", "normal", nil},
		{accTest, "test_grant_fund", "fund", nil},
	}
	for _, f := range funds {
		if _, err := tx.Exec(ctx, `INSERT INTO accounts(id, code, owner_kind, account_class, emission_limit) VALUES ($1,$2,'system',$3,$4)`,
			f.id, f.code, f.class, f.limit); err != nil {
			return err
		}
	}
	op := uuid.MustParse("30000000-0000-4000-8000-000000000001")
	if err := s.move(ctx, tx, op, "bootstrap_funds", map[string]string{"reason": "начальные фонды"}, []posting{
		{must(accEmission), -19_000_000, 0},
		{must(accHuman), 10_000_000, 0},
		{must(accAI), 2_000_000, 0},
		{must(accCredit), 5_000_000, 0},
		{must(accCity), 1_000_000, 0},
		{must(accTest), 1_000_000, 0},
	}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO world_state(id, tick) VALUES (1,0)`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO world_ticks(tick, operation_id) VALUES (0,$1)`, op); err != nil {
		return err
	}
	ext, err := s.ensureSystemUser(ctx, tx, "20000000-0000-4000-8000-000000000001", "external@system.ufacapital.invalid", "Внешний поставщик", false)
	if err != nil {
		return err
	}
	pop, err := s.ensureSystemUser(ctx, tx, "20000000-0000-4000-8000-000000000002", "population@system.ufacapital.invalid", "Население Уфы", false)
	if err != nil {
		return err
	}
	miller, err := s.ensureSystemUser(ctx, tx, "20000000-0000-4000-8000-000000000003", "miller@agents.ufacapital.invalid", "Алтын иген", true)
	if err != nil {
		return err
	}
	baker, err := s.ensureSystemUser(ctx, tx, "20000000-0000-4000-8000-000000000004", "baker@agents.ufacapital.invalid", "Икмәк урамы", true)
	if err != nil {
		return err
	}
	extCo, err := s.ensureCompany(ctx, tx, ext, "SYSTEM", "Внешний зерновой контур", false)
	if err != nil {
		return err
	}
	popCo, err := s.ensureCompany(ctx, tx, pop, "SYSTEM", "Население Уфы", false)
	if err != nil {
		return err
	}
	if _, err := s.ensureCompany(ctx, tx, miller, "AI", "Алтын иген", true); err != nil {
		return err
	}
	if _, err := s.ensureCompany(ctx, tx, baker, "AI", "Икмәк урамы", true); err != nil {
		return err
	}
	if err := s.ensureAgent(ctx, tx, miller, "cautious", "mill", 1101); err != nil {
		return err
	}
	if err := s.ensureAgent(ctx, tx, baker, "aggressive", "bakery", 2202); err != nil {
		return err
	}
	if err := s.ensureDepot(ctx, tx, extCo, "warehouse"); err != nil {
		return err
	}
	if err := s.ensureDepot(ctx, tx, popCo, "shop"); err != nil {
		return err
	}
	popAcc, err := companyAccount(ctx, tx, popCo)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO population_state(city_code, account_id, budget_remaining) VALUES ('ufa',$1,0)`, popAcc); err != nil {
		return err
	}
	if err := s.move(ctx, tx, uuid.MustParse("30000000-0000-4000-8000-000000000002"), "population_seed", map[string]string{"reason": "стартовый бюджет населения"}, []posting{
		{must(accEmission), -50000, 0},
		{popAcc, 50000, 0},
	}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE population_state SET budget_remaining=50000 WHERE city_code='ufa'`); err != nil {
		return err
	}
	if err := s.supplyGrain(ctx, tx, 0, 200); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func must(s string) uuid.UUID { return uuid.MustParse(s) }

type Item struct {
	Good string `json:"good"`
	Qty  string `json:"qty"`
}

func insertRecipe(ctx context.Context, tx pgx.Tx, code string, version int, in, out []Item, energy int64, dur int) error {
	inb, _ := json.Marshal(in)
	outb, _ := json.Marshal(out)
	_, err := tx.Exec(ctx, `INSERT INTO recipe_versions(id, code, version, inputs, outputs, energy_cost, duration_ticks, effective_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'2000-01-01')`, uuid.New(), code, version, inb, outb, energy, dur)
	return err
}

func (s *Service) ensureSystemUser(ctx context.Context, tx pgx.Tx, id, email, name string, ai bool) (uuid.UUID, error) {
	uid := uuid.MustParse(id)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT true FROM users WHERE id=$1`, uid).Scan(&exists); err == nil && exists {
		return uid, nil
	}
	pw, err := identity.RandomPassword()
	if err != nil {
		return uuid.Nil, err
	}
	hash, err := identity.HashPassword(pw, 64, 1, 1)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO users(id, email, password_hash, display_name, is_ai) VALUES ($1,$2,$3,$4,$5)`,
		uid, email, hash, name, ai)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO user_roles(user_id, role) VALUES ($1,'player')`, uid)
	return uid, err
}

func (s *Service) ensureCompany(ctx context.Context, tx pgx.Tx, user uuid.UUID, kind, name string, grant bool) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM companies WHERE owner_user_id=$1 AND status<>'closed'`, user).Scan(&id)
	if err == nil {
		return id, nil
	}
	id = uuid.New()
	acc := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO accounts(id, code, owner_kind, owner_id, account_class) VALUES ($1,$2,'company',$3,'normal')`,
		acc, "cash:"+id.String(), id); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO companies(id, owner_user_id, city_code, name, owner_kind, status, cash_account_id, exclude_from_rankings)
		VALUES ($1,$2,'ufa',$3,$4,'active',$5,$6)`, id, user, name, kind, acc, kind != "HUMAN"); err != nil {
		return uuid.Nil, err
	}
	if grant {
		amount, err := s.cfg(ctx, tx, "ai_grant")
		if err != nil {
			return uuid.Nil, err
		}
		op := uuid.NewSHA1(uuid.NameSpaceOID, []byte("ai-grant:"+user.String()))
		if err := s.move(ctx, tx, op, "ai_grant", map[string]string{"company": id.String()}, []posting{
			{must(accAI), -amount, 0}, {acc, amount, 0},
		}); err != nil {
			return uuid.Nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET grant_consumed=true WHERE id=$1`, user); err != nil {
			return uuid.Nil, err
		}
	}
	return id, nil
}

func (s *Service) ensureAgent(ctx context.Context, tx pgx.Tx, user uuid.UUID, profile, spec string, seed int64) error {
	var company uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM companies WHERE owner_user_id=$1`, user).Scan(&company); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO agents(id, user_id, company_id, profile, specialization, decision_seed, strategy)
		VALUES ($1,$2,$3,$4,$5,$6,$4) ON CONFLICT (user_id) DO NOTHING`, uuid.New(), user, company, profile, spec, seed)
	return err
}

func (s *Service) ensureDepot(ctx context.Context, tx pgx.Tx, company uuid.UUID, template string) error {
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM enterprises WHERE company_id=$1 AND status<>'closed'`, company).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var storage int64
	if err := tx.QueryRow(ctx, `SELECT storage FROM enterprise_templates WHERE code=$1`, template).Scan(&storage); err != nil {
		return err
	}
	eid := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO enterprises(id, company_id, template_code, city_code, capacity, status, created_tick)
		VALUES ($1,$2,$3,'ufa',1,'stopped',0)`, eid, company, template); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workforce(enterprise_id, headcount, wage) VALUES ($1,0,0)`, eid); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO warehouses(id, enterprise_id, capacity) VALUES ($1,$2,$3)`, uuid.New(), eid, storage)
	return err
}

func companyAccount(ctx context.Context, tx pgx.Tx, company uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT cash_account_id FROM companies WHERE id=$1`, company).Scan(&id)
	return id, err
}

func (s *Service) audit(ctx context.Context, tx pgx.Tx, actor *uuid.UUID, action, reason string, before, after any) error {
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	if b == nil {
		b = []byte(`{}`)
	}
	if a == nil {
		a = []byte(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_log(id, actor_id, action, reason, before_state, after_state, correlation_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, uuid.New(), actor, action, reason, b, a, uuid.New())
	return err
}

func (s *Service) emit(ctx context.Context, tx pgx.Tx, topic string, payload any) error {
	raw, _ := json.Marshal(payload)
	_, err := tx.Exec(ctx, `INSERT INTO outbox(event_id, topic, payload) VALUES ($1,$2,$3)`, uuid.New(), topic, raw)
	return err
}

var errDone = errors.New("done")

func nowUTC() time.Time { return time.Now().UTC() }
