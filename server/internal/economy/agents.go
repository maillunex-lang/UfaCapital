package economy

import (
	"context"
	"encoding/json"
	"math/rand"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type candidate struct {
	Action string `json:"action"`
	Score  int64  `json:"score"`
	Reason string `json:"reason"`
}

func (s *Service) decideAll(ctx context.Context, tx pgx.Tx, tick int64) error {
	rows, err := tx.Query(ctx, `SELECT id, user_id, company_id, profile, specialization, decision_seed, paused, strategy, cooldown_until_tick, loss_streak
		FROM agents ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type ag struct {
		id, user, company       uuid.UUID
		profile, spec, strategy string
		seed, cooldown          int64
		paused                  bool
		losses                  int
	}
	var list []ag
	for rows.Next() {
		var a ag
		if err := rows.Scan(&a.id, &a.user, &a.company, &a.profile, &a.spec, &a.seed, &a.paused, &a.strategy, &a.cooldown, &a.losses); err != nil {
			return err
		}
		list = append(list, a)
	}
	rows.Close()
	for _, a := range list {
		if a.paused || tick < a.cooldown {
			continue
		}
		if err := s.decideOne(ctx, tx, tick, a.id, a.user, a.company, a.profile, a.spec, a.strategy, a.seed, a.losses); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) decideOne(ctx context.Context, tx pgx.Tx, tick int64, agent, user, company uuid.UUID, profile, spec, strategy string, seed int64, losses int) error {
	var cash int64
	if err := tx.QueryRow(ctx, `SELECT a.available FROM accounts a JOIN companies c ON c.cash_account_id=a.id WHERE c.id=$1`, company).Scan(&cash); err != nil {
		return err
	}
	var last int64
	var haveLast bool
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT value FROM agent_memory WHERE agent_id=$1 AND key='last_cash'`, agent).Scan(&raw)
	if err == nil {
		_ = json.Unmarshal(raw, &last)
		haveLast = true
	}
	if haveLast && cash < last {
		losses++
	} else if cash > last {
		losses = 0
	}
	if losses >= 3 && strategy == profile {
		strategy = "defensive"
		if _, err := tx.Exec(ctx, `UPDATE agents SET strategy='defensive', cooldown_until_tick=$2, loss_streak=$3 WHERE id=$1`, agent, tick+3, losses); err != nil {
			return err
		}
	} else if _, err := tx.Exec(ctx, `UPDATE agents SET loss_streak=$2 WHERE id=$1`, agent, losses); err != nil {
		return err
	}
	mem, _ := s.memoryInt(ctx, tx, agent, "ewma_price")
	bestFlour := s.bestAsk(ctx, tx, "flour")
	bestGrain := s.bestAsk(ctx, tx, "grain")
	bestBread := s.bestBid(ctx, tx, "bread")
	if bestFlour > 0 {
		if mem == 0 {
			mem = bestFlour
		} else {
			mem = (mem*8 + bestFlour*2) / 10
		}
		_ = s.setMemory(ctx, tx, agent, "ewma_price", mem, tick)
	}
	rng := rand.New(rand.NewSource(seed + tick))
	var cands []candidate
	add := func(action string, score int64, reason string) {
		cands = append(cands, candidate{action, score, reason})
	}
	var ent uuid.UUID
	_ = tx.QueryRow(ctx, `SELECT id FROM enterprises WHERE company_id=$1 AND template_code=$2 AND status<>'closed'`, company, spec).Scan(&ent)
	if ent == uuid.Nil {
		add("open:"+spec, 100, "нет предприятия выбранной отрасли")
	} else {
		add("open:"+spec, -10, "предприятие уже есть")
		var heads int
		_ = tx.QueryRow(ctx, `SELECT headcount FROM workforce WHERE enterprise_id=$1`, ent).Scan(&heads)
		if heads < 2 && spec != "shop" {
			add("hire", 80, "не хватает штата")
		} else {
			add("hire", -5, "штат достаточен")
		}
		add("run", 70, "держать линию включённой")
	}
	stockGrain := s.stock(ctx, tx, company, "grain")
	stockFlour := s.stock(ctx, tx, company, "flour")
	stockBread := s.stock(ctx, tx, company, "bread")
	if spec == "mill" {
		if stockGrain < 30 {
			limit := mem
			if profile == "cautious" {
				limit = bestGrain
			}
			if bestGrain > 0 && (limit == 0 || bestGrain <= limit+5) {
				add("buy:grain", 60, "запас зерна низкий")
			} else {
				add("buy:grain", -20, "зерно дорого относительно памяти")
			}
		}
		if stockFlour >= 8 {
			add("sell:flour", 50, "есть мука на продажу")
		}
	}
	if spec == "bakery" {
		if stockFlour < 20 && bestFlour > 0 {
			if profile == "aggressive" || mem == 0 || bestFlour <= mem+10 {
				add("buy:flour", 75, "пекарне нужна мука")
			} else {
				add("buy:flour", -30, "мука дороже ожидаемой")
			}
		}
		if stockBread >= 8 {
			add("sell:bread", 65, "есть хлеб")
		}
		if cash < 5000 {
			add("borrow", 20, "мало кассы, можно заложить пекарню")
		} else {
			add("borrow", -40, "кредит не нужен")
		}
	}
	if spec == "mill" && stockFlour >= 10 {
		add("contract:flour", 40, "предложить муку пекарне")
	} else {
		add("contract:flour", -15, "нет партии для контракта")
	}
	add("expand", int64(20-losses*5), "расширение только при устойчивой кассе")
	// Noise so logs show a rejected alternative.
	add("wait", int64(rng.Intn(10)), "подождать и не трогать рынок")
	selected := cands[0]
	for _, c := range cands {
		if c.Score > selected.Score {
			selected = c
		}
	}
	result := map[string]string{"ok": "true"}
	if _, err := tx.Exec(ctx, `SAVEPOINT agent_action`); err != nil {
		return err
	}
	var actErr error
	switch selected.Action {
	case "open:" + spec:
		_, actErr = s.openEnterprise(ctx, tx, user, company, spec)
	case "hire":
		wage, _ := s.cfg(ctx, tx, "default_wage")
		_, actErr = s.setWorkforce(ctx, tx, user, ent, 2, wage)
	case "run":
		_, actErr = tx.Exec(ctx, `UPDATE enterprises SET status='running' WHERE id=$1`, ent)
	case "buy:grain":
		id := s.bestAskID(ctx, tx, "grain")
		if id != uuid.Nil {
			_, actErr = s.acceptOrder(ctx, tx, company, id, 10, bestGrain*10)
		}
	case "buy:flour":
		id := s.bestAskID(ctx, tx, "flour")
		qty := int64(10)
		if profile == "aggressive" {
			qty = 20
		}
		if id != uuid.Nil {
			_, actErr = s.acceptOrder(ctx, tx, company, id, qty, bestFlour*qty)
		}
	case "sell:flour":
		price := mem
		if price < 50 {
			price = 70
		}
		_, actErr = s.placeOrder(ctx, tx, user, company, "flour", "sell", price, 8)
	case "sell:bread":
		price := int64(55)
		if bestBread > 0 && price > bestBread {
			price = bestBread
		}
		if profile == "aggressive" {
			price = 48
		}
		_, actErr = s.placeOrder(ctx, tx, user, company, "bread", "sell", price, 8)
	case "borrow":
		if ent != uuid.Nil {
			_, actErr = s.borrow(ctx, tx, user, company, ent, 10000)
		}
	case "contract:flour":
		var baker uuid.UUID
		_ = tx.QueryRow(ctx, `SELECT company_id FROM agents WHERE specialization='bakery' AND id<>$1`, agent).Scan(&baker)
		if baker != uuid.Nil {
			_, actErr = s.offerContract(ctx, tx, user, company, "flour", 10, 70, tick+12)
		}
	case "expand":
		if ent != uuid.Nil && cash > 30000 && losses == 0 {
			_, actErr = s.expand(ctx, tx, user, ent)
		}
	default:
		result["ok"] = "skip"
	}
	if actErr != nil {
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT agent_action`); err != nil {
			return err
		}
		result["ok"] = "false"
		result["error"] = actErr.Error()
	} else if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT agent_action`); err != nil {
		return err
	}
	b, _ := json.Marshal(cands)
	r, _ := json.Marshal(result)
	if _, err := tx.Exec(ctx, `INSERT INTO decision_log(id, agent_id, tick, candidates, selected, reason, result)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, uuid.New(), agent, tick, b, selected.Action, selected.Reason, r); err != nil {
		return err
	}
	_ = s.setMemory(ctx, tx, agent, "last_cash", cash, tick)
	// Baker accepts an open flour contract if it has the role of buyer.
	if spec == "bakery" {
		var cid uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM contracts WHERE status='offered' AND good_code='flour' AND deadline_tick>$1 ORDER BY created_tick LIMIT 1`, tick).Scan(&cid)
		if err == nil {
			if _, err := tx.Exec(ctx, `SAVEPOINT agent_contract`); err == nil {
				if _, aerr := s.acceptContract(ctx, tx, company, cid); aerr != nil {
					_, _ = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT agent_contract`)
				} else {
					_, _ = tx.Exec(ctx, `RELEASE SAVEPOINT agent_contract`)
				}
			}
		}
	}
	return nil
}

func (s *Service) memoryInt(ctx context.Context, tx pgx.Tx, agent uuid.UUID, key string) (int64, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT value FROM agent_memory WHERE agent_id=$1 AND key=$2`, agent, key).Scan(&raw); err != nil {
		return 0, err
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Service) setMemory(ctx context.Context, tx pgx.Tx, agent uuid.UUID, key string, value int64, tick int64) error {
	raw, _ := json.Marshal(value)
	_, err := tx.Exec(ctx, `INSERT INTO agent_memory(agent_id, key, value, updated_tick) VALUES ($1,$2,$3,$4)
		ON CONFLICT (agent_id, key) DO UPDATE SET value=EXCLUDED.value, updated_tick=EXCLUDED.updated_tick`, agent, key, raw, tick)
	return err
}

func (s *Service) stock(ctx context.Context, tx pgx.Tx, company uuid.UUID, good string) int64 {
	var n int64
	_ = tx.QueryRow(ctx, `SELECT COALESCE(SUM(qty-reserved),0) FROM inventory_lots WHERE company_id=$1 AND good_code=$2`, company, good).Scan(&n)
	return n
}

func (s *Service) bestAsk(ctx context.Context, tx pgx.Tx, good string) int64 {
	var p int64
	_ = tx.QueryRow(ctx, `SELECT price FROM market_orders WHERE good_code=$1 AND side='sell' AND status='open' AND city_code='ufa' ORDER BY price, created_tick LIMIT 1`, good).Scan(&p)
	return p
}

func (s *Service) bestAskID(ctx context.Context, tx pgx.Tx, good string) uuid.UUID {
	var id uuid.UUID
	_ = tx.QueryRow(ctx, `SELECT id FROM market_orders WHERE good_code=$1 AND side='sell' AND status='open' AND city_code='ufa' ORDER BY price, created_tick LIMIT 1`, good).Scan(&id)
	return id
}

func (s *Service) bestBid(ctx context.Context, tx pgx.Tx, good string) int64 {
	var p int64
	_ = tx.QueryRow(ctx, `SELECT price FROM market_orders WHERE good_code=$1 AND side='buy' AND status='open' AND city_code='ufa' ORDER BY price DESC LIMIT 1`, good).Scan(&p)
	return p
}

func (s *Service) PauseAgent(ctx context.Context, actor, agent uuid.UUID, paused bool, reason string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE agents SET paused=$2 WHERE id=$1`, agent, paused); err != nil {
		return err
	}
	if err := s.audit(ctx, tx, &actor, "agent_pause", reason, nil, map[string]any{"agent": agent.String(), "paused": paused}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
