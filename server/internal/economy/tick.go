package economy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/maillunex-lang/UfaCapital/server/internal/money"
)

func (s *Service) Tick(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := s.lockWorld(ctx, tx); err != nil {
		return err
	}
	tick, err := s.tickOf(ctx, tx)
	if err != nil {
		return err
	}
	tick++
	op := uuid.New()
	steps := []struct {
		name string
		fn   func() error
	}{
		{"complete", func() error { return s.completeJobs(ctx, tx, tick, op) }},
		{"spoil", func() error { return s.spoil(ctx, tx, tick, op) }},
		{"deliver", func() error { return s.deliver(ctx, tx, tick) }},
		{"loans", func() error { return s.loans(ctx, tx, tick) }},
	}
	for _, st := range steps {
		if err := st.fn(); err != nil {
			return fmt.Errorf("%s tick %d: %w", st.name, tick, err)
		}
	}
	if tick%24 == 0 {
		if err := s.populationIncome(ctx, tx, tick); err != nil {
			return fmt.Errorf("population income: %w", err)
		}
		if err := s.supplyGrain(ctx, tx, tick/24, 0); err != nil {
			return fmt.Errorf("grain supply: %w", err)
		}
	}
	if err := s.populationBuy(ctx, tx, tick); err != nil {
		return fmt.Errorf("population buy tick %d: %w", tick, err)
	}
	if tick%3 == 0 {
		if err := s.decideAll(ctx, tx, tick); err != nil {
			return fmt.Errorf("agents tick %d: %w", tick, err)
		}
	}
	if err := s.startJobs(ctx, tx, tick); err != nil {
		return fmt.Errorf("start jobs tick %d: %w", tick, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE world_state SET tick=$1, updated_at=now() WHERE id=1`, tick); err != nil {
		return fmt.Errorf("world tick %d: %w", tick, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO world_ticks(tick, operation_id) VALUES ($1,$2)`, tick, op); err != nil {
		return fmt.Errorf("world_ticks %d: %w", tick, err)
	}
	if err := s.emit(ctx, tx, "tick", map[string]string{"tick": money.Format(tick)}); err != nil {
		return fmt.Errorf("emit tick %d: %w", tick, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tick %d: %w", tick, err)
	}
	return nil
}

func (s *Service) completeJobs(ctx context.Context, tx pgx.Tx, tick int64, op uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT id, enterprise_id, recipe_version_id, input_qty, output_qty, wage_reserved, energy_reserved
		FROM production_jobs WHERE status='running' AND complete_tick<=$1 FOR UPDATE`, tick)
	if err != nil {
		return err
	}
	defer rows.Close()
	type job struct {
		id, ent, recipe uuid.UUID
		in, out         []byte
		wage, energy    int64
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.ent, &j.recipe, &j.in, &j.out, &j.wage, &j.energy); err != nil {
			return err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("jobs scan: %w", err)
	}
	for _, j := range jobs {
		var company uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT company_id FROM enterprises WHERE id=$1`, j.ent).Scan(&company); err != nil {
			return fmt.Errorf("company: %w", err)
		}
		acc, err := companyAccount(ctx, tx, company)
		if err != nil {
			return fmt.Errorf("account: %w", err)
		}
		if err := s.consumeReserved(ctx, tx, j.id, company, tick, op); err != nil {
			return fmt.Errorf("consume: %w", err)
		}
		var pop uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT account_id FROM population_state WHERE city_code='ufa'`).Scan(&pop); err != nil {
			return fmt.Errorf("pop: %w", err)
		}
		pay := uuid.New()
		posts := []posting{}
		if j.wage > 0 {
			posts = append(posts, posting{acc, -j.wage, -j.wage}, posting{pop, j.wage, 0})
		}
		if j.energy > 0 {
			posts = append(posts, posting{acc, -j.energy, -j.energy}, posting{must(accCity), j.energy, 0})
		}
		if len(posts) > 0 {
			if err := s.settleJobMoney(ctx, tx, j.id, posts, pay); err != nil {
				return fmt.Errorf("settle: %w", err)
			}
		}
		var outs []Item
		if err := json.Unmarshal(j.out, &outs); err != nil {
			return fmt.Errorf("out json: %w", err)
		}
		var ins []Item
		_ = json.Unmarshal(j.in, &ins)
		for _, it := range outs {
			q, err := money.Parse(it.Qty)
			if err != nil {
				return err
			}
			spoil, err := s.cfg(ctx, tx, it.Good+"_spoil_ticks")
			if err != nil {
				return fmt.Errorf("spoil cfg: %w", err)
			}
			unit := int64(0)
			if q > 0 {
				unit = (j.wage + j.energy) / q
			}
			if err := s.addLot(ctx, tx, company, it.Good, q, unit, tick, spoil); err != nil {
				return fmt.Errorf("add lot: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE production_jobs SET status='done' WHERE id=$1`, j.id); err != nil {
			return fmt.Errorf("done: %w", err)
		}
	}
	return nil
}

func (s *Service) settleJobMoney(ctx context.Context, tx pgx.Tx, ref uuid.UUID, posts []posting, op uuid.UUID) error {
	// Reservation rows were stored against the job. Deactivate the spent amount and let move adjust reserved.
	var reserved int64
	for _, p := range posts {
		if p.reserved < 0 {
			reserved += -p.reserved
		}
	}
	rows, err := tx.Query(ctx, `SELECT id, amount FROM money_reservations WHERE ref_id=$1 AND active ORDER BY id FOR UPDATE`, ref)
	if err != nil {
		return err
	}
	type hold struct {
		id  uuid.UUID
		amt int64
	}
	var holds []hold
	for rows.Next() {
		var h hold
		if err := rows.Scan(&h.id, &h.amt); err != nil {
			rows.Close()
			return err
		}
		holds = append(holds, h)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	left := reserved
	for _, h := range holds {
		if left <= 0 {
			break
		}
		if h.amt <= left {
			if _, err := tx.Exec(ctx, `UPDATE money_reservations SET active=false WHERE id=$1`, h.id); err != nil {
				return err
			}
			left -= h.amt
		} else {
			if _, err := tx.Exec(ctx, `UPDATE money_reservations SET amount=amount-$2 WHERE id=$1`, h.id, left); err != nil {
				return err
			}
			left = 0
		}
	}
	return s.move(ctx, tx, op, "production_pay", map[string]string{"job": ref.String()}, posts)
}

func (s *Service) consumeReserved(ctx context.Context, tx pgx.Tx, ref, company uuid.UUID, tick int64, op uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT id, lot_id, qty FROM lot_reservations WHERE ref_id=$1 AND active FOR UPDATE`, ref)
	if err != nil {
		return err
	}
	defer rows.Close()
	type r struct {
		id, lot uuid.UUID
		qty     int64
	}
	var list []r
	for rows.Next() {
		var x r
		if err := rows.Scan(&x.id, &x.lot, &x.qty); err != nil {
			return err
		}
		list = append(list, x)
	}
	rows.Close()
	for _, x := range list {
		var good string
		if err := tx.QueryRow(ctx, `SELECT good_code FROM inventory_lots WHERE id=$1`, x.lot).Scan(&good); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE inventory_lots SET qty=qty-$2, reserved=reserved-$2 WHERE id=$1`, x.lot, x.qty); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE lot_reservations SET active=false WHERE id=$1`, x.id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO inventory_moves(id, operation_id, lot_id, good_code, qty, kind, company_id, created_tick)
			VALUES ($1,$2,$3,$4,$5,'produce_in',$6,$7)`, uuid.New(), op, x.lot, good, -x.qty, company, tick); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) startJobs(ctx context.Context, tx pgx.Tx, tick int64) error {
	rows, err := tx.Query(ctx, `SELECT e.id, e.company_id, e.capacity, t.recipe_code, w.headcount, w.wage, t.min_workers
		FROM enterprises e
		JOIN enterprise_templates t ON t.code=e.template_code
		JOIN workforce w ON w.enterprise_id=e.id
		WHERE e.status='running' AND t.recipe_code IS NOT NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type ent struct {
		id, company     uuid.UUID
		cap, heads, min int
		recipe          string
		wage            int64
	}
	var list []ent
	for rows.Next() {
		var e ent
		if err := rows.Scan(&e.id, &e.company, &e.cap, &e.recipe, &e.heads, &e.wage, &e.min); err != nil {
			return err
		}
		list = append(list, e)
	}
	rows.Close()
	for _, e := range list {
		if e.heads < e.min {
			continue
		}
		var running int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM production_jobs WHERE enterprise_id=$1 AND status='running'`, e.id).Scan(&running); err != nil {
			return err
		}
		if running >= e.cap {
			continue
		}
		if _, err := tx.Exec(ctx, `SAVEPOINT start_job`); err != nil {
			return err
		}
		if err := s.tryStartJob(ctx, tx, tick, e.id, e.company, e.recipe, e.heads, e.wage); err != nil {
			if _, rerr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT start_job`); rerr != nil {
				return rerr
			}
			continue
		}
		if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT start_job`); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) tryStartJob(ctx context.Context, tx pgx.Tx, tick int64, enterprise, company uuid.UUID, recipe string, heads int, wage int64) error {
	var rid uuid.UUID
	var inb, outb []byte
	var energy int64
	var dur int
	err := tx.QueryRow(ctx, `SELECT id, inputs, outputs, energy_cost, duration_ticks FROM recipe_versions
		WHERE code=$1 AND active AND effective_at<=now() ORDER BY version DESC LIMIT 1`, recipe).Scan(&rid, &inb, &outb, &energy, &dur)
	if err != nil {
		return err
	}
	var ins []Item
	if err := json.Unmarshal(inb, &ins); err != nil {
		return err
	}
	job := uuid.New()
	for _, it := range ins {
		q, err := money.Parse(it.Qty)
		if err != nil {
			return err
		}
		if err := s.reserveGood(ctx, tx, company, it.Good, q, job); err != nil {
			return err
		}
	}
	pay := wage*int64(heads) + energy
	acc, err := companyAccount(ctx, tx, company)
	if err != nil {
		return err
	}
	if pay > 0 {
		if err := s.reserveMoney(ctx, tx, acc, pay, "job", job); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO production_jobs(id, enterprise_id, recipe_version_id, start_tick, complete_tick, status, input_qty, output_qty, wage_reserved, energy_reserved)
		VALUES ($1,$2,$3,$4,$5,'running',$6,$7,$8,$9)`, job, enterprise, rid, tick, tick+int64(dur), inb, outb, wage*int64(heads), energy)
	return err
}

func (s *Service) spoil(ctx context.Context, tx pgx.Tx, tick int64, op uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT id, company_id, good_code, qty FROM inventory_lots
		WHERE expires_tick IS NOT NULL AND expires_tick<=$1 AND qty>0 FOR UPDATE`, tick)
	if err != nil {
		return err
	}
	defer rows.Close()
	type lot struct {
		id, company uuid.UUID
		good        string
		qty         int64
	}
	var lots []lot
	for rows.Next() {
		var l lot
		if err := rows.Scan(&l.id, &l.company, &l.good, &l.qty); err != nil {
			return err
		}
		lots = append(lots, l)
	}
	rows.Close()
	for _, l := range lots {
		if _, err := tx.Exec(ctx, `UPDATE lot_reservations SET active=false WHERE lot_id=$1 AND active`, l.id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE inventory_lots SET qty=0, reserved=0 WHERE id=$1`, l.id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO inventory_moves(id, operation_id, lot_id, good_code, qty, kind, company_id, created_tick)
			VALUES ($1,$2,$3,$4,$5,'spoil',$6,$7)`, uuid.New(), op, l.id, l.good, -l.qty, l.company, tick); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) deliver(ctx context.Context, tx pgx.Tx, tick int64) error {
	rows, err := tx.Query(ctx, `SELECT d.id, d.contract_id FROM deliveries d
		JOIN contracts c ON c.id=d.contract_id
		WHERE d.status='transit' AND d.arrive_tick<=$1 FOR UPDATE OF d`, tick)
	if err != nil {
		return err
	}
	defer rows.Close()
	type d struct{ id, contract uuid.UUID }
	var list []d
	for rows.Next() {
		var x d
		if err := rows.Scan(&x.id, &x.contract); err != nil {
			return err
		}
		list = append(list, x)
	}
	rows.Close()
	for _, x := range list {
		var seller, buyer uuid.UUID
		var good string
		var qty, price int64
		var status string
		err := tx.QueryRow(ctx, `SELECT seller_company_id, buyer_company_id, good_code, qty, price, status FROM contracts WHERE id=$1 FOR UPDATE`, x.contract).
			Scan(&seller, &buyer, &good, &qty, &price, &status)
		if err != nil {
			return err
		}
		if status != "accepted" {
			continue
		}
		total, err := money.Mul(price, qty)
		if err != nil {
			return err
		}
		sellerAcc, err := companyAccount(ctx, tx, seller)
		if err != nil {
			return err
		}
		buyerAcc, err := companyAccount(ctx, tx, buyer)
		if err != nil {
			return err
		}
		op := uuid.New()
		if err := s.captureReservedMoney(ctx, tx, x.contract, buyerAcc, sellerAcc, total, op); err != nil {
			return err
		}
		if err := s.takeReservedGoods(ctx, tx, x.contract, seller, buyer, good, qty, op, tick); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE contracts SET status='delivered' WHERE id=$1`, x.contract); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE deliveries SET status='done' WHERE id=$1`, x.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) loans(ctx context.Context, tx pgx.Tx, tick int64) error {
	rows, err := tx.Query(ctx, `SELECT id, company_id, outstanding, rate_bps, collateral_enterprise_id, next_interest_tick, due_tick, status, overdue_since_tick
		FROM loans WHERE status IN ('active','overdue') FOR UPDATE`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type ln struct {
		id, company, ent uuid.UUID
		out, next, due   int64
		rate             int
		status           string
		overdue          *int64
	}
	var list []ln
	for rows.Next() {
		var l ln
		if err := rows.Scan(&l.id, &l.company, &l.out, &l.rate, &l.ent, &l.next, &l.due, &l.status, &l.overdue); err != nil {
			return err
		}
		list = append(list, l)
	}
	rows.Close()
	for _, l := range list {
		acc, err := companyAccount(ctx, tx, l.company)
		if err != nil {
			return err
		}
		if l.status == "active" && tick >= l.next && l.out > 0 {
			interest := l.out * int64(l.rate) / 10000
			if interest > 0 {
				var available int64
				if err := tx.QueryRow(ctx, `SELECT available FROM accounts WHERE id=$1`, acc).Scan(&available); err != nil {
					return err
				}
				if available >= interest {
					op := uuid.New()
					if err := s.move(ctx, tx, op, "loan_interest", map[string]string{"loan": l.id.String()}, []posting{
						{acc, -interest, 0}, {must(accCredit), interest, 0},
					}); err != nil {
						return err
					}
				} else {
					l.status = "overdue"
					if _, err := tx.Exec(ctx, `UPDATE loans SET status='overdue', overdue_since_tick=$2 WHERE id=$1`, l.id, tick); err != nil {
						return err
					}
					if _, err := tx.Exec(ctx, `UPDATE companies SET status='restricted' WHERE id=$1 AND status='active'`, l.company); err != nil {
						return err
					}
				}
			}
			period, _ := s.cfg(ctx, tx, "loan_period_ticks")
			if _, err := tx.Exec(ctx, `UPDATE loans SET next_interest_tick=$2 WHERE id=$1`, l.id, tick+period); err != nil {
				return err
			}
		}
		if tick >= l.due && l.out > 0 && l.status == "active" {
			var available int64
			_ = tx.QueryRow(ctx, `SELECT available FROM accounts WHERE id=$1`, acc).Scan(&available)
			if available >= l.out {
				op := uuid.New()
				if err := s.move(ctx, tx, op, "loan_due", map[string]string{"loan": l.id.String()}, []posting{
					{acc, -l.out, 0}, {must(accCredit), l.out, 0},
				}); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE loans SET outstanding=0, status='repaid' WHERE id=$1`, l.id); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE enterprises SET pledged=false WHERE id=$1`, l.ent); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.Exec(ctx, `UPDATE loans SET status='overdue', overdue_since_tick=COALESCE(overdue_since_tick,$2) WHERE id=$1`, l.id, tick); err != nil {
				return err
			}
			_, _ = tx.Exec(ctx, `UPDATE companies SET status='restricted' WHERE id=$1 AND status='active'`, l.company)
			l.status = "overdue"
			if l.overdue == nil {
				v := tick
				l.overdue = &v
			}
		}
		if l.status == "overdue" && l.overdue != nil && tick >= *l.overdue+48 {
			if err := s.liquidate(ctx, tx, l.company, l.ent, l.id, l.out); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) liquidate(ctx context.Context, tx pgx.Tx, company, enterprise, loan uuid.UUID, outstanding int64) error {
	var template string
	if err := tx.QueryRow(ctx, `SELECT template_code FROM enterprises WHERE id=$1`, enterprise).Scan(&template); err != nil {
		return err
	}
	var book int64
	_ = tx.QueryRow(ctx, `SELECT book_value FROM enterprise_templates WHERE code=$1`, template).Scan(&book)
	pay := book / 2
	var cityAvail int64
	_ = tx.QueryRow(ctx, `SELECT available FROM accounts WHERE id=$1`, must(accCity)).Scan(&cityAvail)
	if pay > cityAvail {
		pay = cityAvail
	}
	acc, err := companyAccount(ctx, tx, company)
	if err != nil {
		return err
	}
	if pay > 0 {
		op := uuid.New()
		if err := s.move(ctx, tx, op, "liquidation_sale", map[string]string{"enterprise": enterprise.String()}, []posting{
			{must(accCity), -pay, 0}, {acc, pay, 0},
		}); err != nil {
			return err
		}
	}
	var available int64
	_ = tx.QueryRow(ctx, `SELECT available FROM accounts WHERE id=$1`, acc).Scan(&available)
	repay := available
	if repay > outstanding {
		repay = outstanding
	}
	if repay > 0 {
		op := uuid.New()
		if err := s.move(ctx, tx, op, "liquidation_repay", map[string]string{"loan": loan.String()}, []posting{
			{acc, -repay, 0}, {must(accCredit), repay, 0},
		}); err != nil {
			return err
		}
		outstanding -= repay
	}
	st := "defaulted"
	if outstanding == 0 {
		st = "repaid"
	}
	if _, err := tx.Exec(ctx, `UPDATE loans SET outstanding=$2, status=$3 WHERE id=$1`, loan, outstanding, st); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE enterprises SET status='closed', pledged=false WHERE id=$1`, enterprise); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE companies SET status='closed' WHERE id=$1`, company)
	return err
}

func (s *Service) populationIncome(ctx context.Context, tx pgx.Tx, tick int64) error {
	income, err := s.cfg(ctx, tx, "population_daily_income")
	if err != nil {
		return err
	}
	var acc uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT account_id FROM population_state WHERE city_code='ufa'`).Scan(&acc); err != nil {
		return err
	}
	op := uuid.NewSHA1(uuid.NameSpaceOID, []byte("pop-income:"+money.Format(tick)))
	if err := s.move(ctx, tx, op, "population_income", map[string]string{"tick": money.Format(tick)}, []posting{
		{must(accEmission), -income, 0}, {acc, income, 0},
	}); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE population_state SET budget_remaining = budget_remaining + $1 WHERE city_code='ufa'`, income)
	return err
}

func (s *Service) supplyGrain(ctx context.Context, tx pgx.Tx, day, qty int64) error {
	capQty, err := s.cfg(ctx, tx, "grain_daily_cap")
	if err != nil {
		return err
	}
	if qty <= 0 || qty > capQty {
		qty = capQty
	}
	var supplied int64
	err = tx.QueryRow(ctx, `SELECT supplied FROM external_limits WHERE city_code='ufa' AND good_code='grain' AND day_index=$1`, day).Scan(&supplied)
	if err == nil && supplied >= capQty {
		return nil
	}
	room := capQty - supplied
	if qty > room {
		qty = room
	}
	if qty <= 0 {
		return nil
	}
	var company uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT c.id FROM companies c JOIN users u ON u.id=c.owner_user_id WHERE u.email='external@system.ufacapital.invalid'`).Scan(&company); err != nil {
		return err
	}
	var user uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT owner_user_id FROM companies WHERE id=$1`, company).Scan(&user); err != nil {
		return err
	}
	var cap, used int64
	err = tx.QueryRow(ctx, `SELECT w.capacity, COALESCE((SELECT SUM(l.qty) FROM inventory_lots l WHERE l.warehouse_id=w.id),0)
		FROM warehouses w
		JOIN enterprises e ON e.id=w.enterprise_id
		WHERE e.company_id=$1 AND e.status<>'closed'
		ORDER BY w.capacity DESC LIMIT 1`, company).Scan(&cap, &used)
	if err != nil {
		return err
	}
	free := cap - used
	if free < qty {
		qty = free
	}
	if qty <= 0 {
		return nil
	}
	tick, _ := s.tickOf(ctx, tx)
	spoil, _ := s.cfg(ctx, tx, "grain_spoil_ticks")
	if err := s.addLot(ctx, tx, company, "grain", qty, 0, tick, spoil); err != nil {
		return err
	}
	price, err := s.cfg(ctx, tx, "grain_price")
	if err != nil {
		return err
	}
	if _, err := s.placeOrder(ctx, tx, user, company, "grain", "sell", price, qty); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO external_limits(city_code, good_code, day_index, supplied) VALUES ('ufa','grain',$1,$2)
		ON CONFLICT (city_code, good_code, day_index) DO UPDATE SET supplied=external_limits.supplied+$2`, day, qty)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO inventory_moves(id, operation_id, good_code, qty, kind, company_id, created_tick)
		VALUES ($1,$2,'grain',$3,'external_supply',$4,$5)`, uuid.New(), uuid.New(), qty, company, tick)
	return err
}

func (s *Service) populationBuy(ctx context.Context, tx pgx.Tx, tick int64) error {
	maxPrice, err := s.cfg(ctx, tx, "population_max_bread_price")
	if err != nil {
		return err
	}
	var company uuid.UUID
	var acc uuid.UUID
	var budget int64
	err = tx.QueryRow(ctx, `SELECT c.id, c.cash_account_id, a.available
		FROM companies c JOIN users u ON u.id=c.owner_user_id JOIN accounts a ON a.id=c.cash_account_id
		WHERE u.email='population@system.ufacapital.invalid'`).Scan(&company, &acc, &budget)
	if err != nil {
		return err
	}
	if budget <= 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT id, price, qty_open FROM market_orders
		WHERE city_code='ufa' AND good_code='bread' AND side='sell' AND status='open' AND price<=$1
		ORDER BY price, created_tick, seq FOR UPDATE`, maxPrice)
	if err != nil {
		return err
	}
	defer rows.Close()
	type o struct {
		id         uuid.UUID
		price, qty int64
	}
	var orders []o
	for rows.Next() {
		var x o
		if err := rows.Scan(&x.id, &x.price, &x.qty); err != nil {
			return err
		}
		orders = append(orders, x)
	}
	rows.Close()
	left := budget
	for _, o := range orders {
		if left < o.price {
			break
		}
		qty := left / o.price
		if qty > o.qty {
			qty = o.qty
		}
		if qty <= 0 {
			continue
		}
		total, err := money.Mul(o.price, qty)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SAVEPOINT pop_buy`); err != nil {
			return err
		}
		if _, err := s.acceptOrder(ctx, tx, company, o.id, qty, total); err != nil {
			if _, rerr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT pop_buy`); rerr != nil {
				return rerr
			}
			continue
		}
		if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT pop_buy`); err != nil {
			return err
		}
		left -= total
	}
	_, err = tx.Exec(ctx, `UPDATE population_state SET budget_remaining=$1 WHERE city_code='ufa'`, left)
	_ = tick
	return err
}
