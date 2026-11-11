package economy

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/maillunex-lang/UfaCapital/server/internal/domain"
	"github.com/maillunex-lang/UfaCapital/server/internal/money"
)

type CompanyView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"owner_kind"`
	Status   string `json:"status"`
	City     string `json:"city"`
	Cash     string `json:"cash"`
	Reserved string `json:"reserved"`
}

func (s *Service) CreateCompany(ctx context.Context, user uuid.UUID, key string, raw []byte, name string) (any, error) {
	return s.run(ctx, user, "POST /api/v1/companies", key, raw, func(tx pgx.Tx) (any, error) {
		var consumed bool
		var ai bool
		if err := tx.QueryRow(ctx, `SELECT grant_consumed, is_ai FROM users WHERE id=$1`, user).Scan(&consumed, &ai); err != nil {
			return nil, domain.E(403, "FORBIDDEN", "нет пользователя")
		}
		if ai {
			return nil, domain.E(403, "FORBIDDEN", "компанию ИИ создаёт сервер")
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM companies WHERE owner_user_id=$1 AND status<>'closed'`, user).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, domain.E(409, "COMPANY_EXISTS", "уже есть активная компания")
		}
		if name == "" || len(name) > 80 {
			return nil, domain.E(400, "BAD_NAME", "укажите название")
		}
		id := uuid.New()
		acc := uuid.New()
		kind := "HUMAN"
		var roles int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM user_roles WHERE user_id=$1 AND role='test_operator'`, user).Scan(&roles)
		if roles > 0 {
			kind = "TEST_ADMIN"
		}
		if _, err := tx.Exec(ctx, `INSERT INTO accounts(id, code, owner_kind, owner_id, account_class) VALUES ($1,$2,'company',$3,'normal')`,
			acc, "cash:"+id.String(), id); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO companies(id, owner_user_id, city_code, name, owner_kind, status, cash_account_id, exclude_from_rankings)
			VALUES ($1,$2,'ufa',$3,$4,'active',$5,$6)`, id, user, name, kind, acc, kind == "TEST_ADMIN"); err != nil {
			return nil, err
		}
		granted := "0"
		if !consumed && kind == "HUMAN" {
			amount, err := s.cfg(ctx, tx, "human_grant")
			if err != nil {
				return nil, err
			}
			op := uuid.NewSHA1(uuid.NameSpaceOID, []byte("human-grant:"+user.String()))
			if err := s.move(ctx, tx, op, "human_grant", map[string]string{"company": id.String()}, []posting{
				{must(accHuman), -amount, 0}, {acc, amount, 0},
			}); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE users SET grant_consumed=true WHERE id=$1`, user); err != nil {
				return nil, err
			}
			granted = money.Format(amount)
		}
		if err := s.ensureDepot(ctx, tx, id, "warehouse"); err != nil {
			return nil, err
		}
		return map[string]string{"company_id": id.String(), "grant": granted, "owner_kind": kind}, nil
	})
}

func (s *Service) OpenEnterprise(ctx context.Context, user uuid.UUID, key string, raw []byte, company uuid.UUID, template string) (any, error) {
	return s.run(ctx, user, "POST /api/v1/enterprises", key, raw, func(tx pgx.Tx) (any, error) {
		return s.openEnterprise(ctx, tx, user, company, template)
	})
}

func (s *Service) openEnterprise(ctx context.Context, tx pgx.Tx, user, company uuid.UUID, template string) (any, error) {
	acc, status, err := s.own(ctx, tx, user, company)
	if err != nil {
		return nil, err
	}
	if status != "active" {
		return nil, domain.E(422, "COMPANY_RESTRICTED", "компания не может открывать предприятия")
	}
	var rent, storage int64
	var title string
	err = tx.QueryRow(ctx, `SELECT rent, storage, title FROM enterprise_templates WHERE code=$1`, template).Scan(&rent, &storage, &title)
	if err != nil {
		return nil, domain.E(404, "NOT_FOUND", "нет такого предприятия")
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM enterprises WHERE company_id=$1 AND template_code=$2 AND status<>'closed'`, company, template).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, domain.E(409, "ENTERPRISE_EXISTS", "такое предприятие уже есть")
	}
	tick, _ := s.tickOf(ctx, tx)
	eid := uuid.New()
	op := uuid.New()
	if err := s.move(ctx, tx, op, "rent", map[string]string{"enterprise": eid.String()}, []posting{
		{acc, -rent, 0}, {must(accCity), rent, 0},
	}); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO enterprises(id, company_id, template_code, city_code, capacity, status, created_tick)
		VALUES ($1,$2,$3,'ufa',1,'stopped',$4)`, eid, company, template, tick); err != nil {
		return nil, err
	}
	wage, _ := s.cfg(ctx, tx, "default_wage")
	if _, err := tx.Exec(ctx, `INSERT INTO workforce(enterprise_id, headcount, wage) VALUES ($1,0,$2)`, eid, wage); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO warehouses(id, enterprise_id, capacity) VALUES ($1,$2,$3)`, uuid.New(), eid, storage); err != nil {
		return nil, err
	}
	return map[string]string{"enterprise_id": eid.String(), "template": template, "rent": money.Format(rent)}, nil
}

func (s *Service) own(ctx context.Context, tx pgx.Tx, user, company uuid.UUID) (uuid.UUID, string, error) {
	var acc uuid.UUID
	var status string
	var owner uuid.UUID
	err := tx.QueryRow(ctx, `SELECT owner_user_id, cash_account_id, status FROM companies WHERE id=$1`, company).Scan(&owner, &acc, &status)
	if err != nil || owner != user {
		return uuid.Nil, "", domain.E(404, "NOT_FOUND", "компания не найдена")
	}
	return acc, status, nil
}

func (s *Service) SetWorkforce(ctx context.Context, user uuid.UUID, key string, raw []byte, enterprise uuid.UUID, headcount int, wage int64) (any, error) {
	return s.run(ctx, user, "POST /api/v1/enterprises/"+enterprise.String()+"/workforce", key, raw, func(tx pgx.Tx) (any, error) {
		return s.setWorkforce(ctx, tx, user, enterprise, headcount, wage)
	})
}

func (s *Service) setWorkforce(ctx context.Context, tx pgx.Tx, user, enterprise uuid.UUID, headcount int, wage int64) (any, error) {
	if headcount < 0 || wage < 0 {
		return nil, domain.E(400, "BAD_AMOUNT", "штат не может быть отрицательным")
	}
	var company uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT company_id FROM enterprises WHERE id=$1 AND status<>'closed'`, enterprise).Scan(&company); err != nil {
		return nil, domain.E(404, "NOT_FOUND", "предприятие не найдено")
	}
	if _, _, err := s.own(ctx, tx, user, company); err != nil {
		return nil, err
	}
	capLabor, err := s.cfg(ctx, tx, "labor_cap")
	if err != nil {
		return nil, err
	}
	var used int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(w.headcount),0) FROM workforce w
		JOIN enterprises e ON e.id=w.enterprise_id WHERE e.city_code='ufa' AND e.id<>$1 AND e.status<>'closed'`, enterprise).Scan(&used); err != nil {
		return nil, err
	}
	if int64(used+headcount) > capLabor {
		return nil, domain.E(422, "CAPACITY_EXCEEDED", "в городе нет свободных работников")
	}
	if _, err := tx.Exec(ctx, `UPDATE workforce SET headcount=$2, wage=$3 WHERE enterprise_id=$1`, enterprise, headcount, wage); err != nil {
		return nil, err
	}
	return map[string]string{"enterprise_id": enterprise.String(), "headcount": money.Format(int64(headcount)), "wage": money.Format(wage)}, nil
}

func (s *Service) SetPolicy(ctx context.Context, user uuid.UUID, key string, raw []byte, enterprise uuid.UUID, running bool) (any, error) {
	return s.run(ctx, user, "POST /api/v1/enterprises/"+enterprise.String()+"/policy", key, raw, func(tx pgx.Tx) (any, error) {
		var company uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT company_id FROM enterprises WHERE id=$1`, enterprise).Scan(&company); err != nil {
			return nil, domain.E(404, "NOT_FOUND", "предприятие не найдено")
		}
		if _, _, err := s.own(ctx, tx, user, company); err != nil {
			return nil, err
		}
		status := "stopped"
		if running {
			status = "running"
		}
		if _, err := tx.Exec(ctx, `UPDATE enterprises SET status=$2 WHERE id=$1 AND status<>'closed'`, enterprise, status); err != nil {
			return nil, err
		}
		return map[string]string{"enterprise_id": enterprise.String(), "status": status}, nil
	})
}

func (s *Service) Expand(ctx context.Context, user uuid.UUID, key string, raw []byte, enterprise uuid.UUID) (any, error) {
	return s.run(ctx, user, "POST /api/v1/enterprises/"+enterprise.String()+"/expand", key, raw, func(tx pgx.Tx) (any, error) {
		return s.expand(ctx, tx, user, enterprise)
	})
}

func (s *Service) expand(ctx context.Context, tx pgx.Tx, user, enterprise uuid.UUID) (any, error) {
	var company uuid.UUID
	var template string
	var capacity int
	var pledged bool
	err := tx.QueryRow(ctx, `SELECT company_id, template_code, capacity, pledged FROM enterprises WHERE id=$1 AND status<>'closed'`, enterprise).
		Scan(&company, &template, &capacity, &pledged)
	if err != nil {
		return nil, domain.E(404, "NOT_FOUND", "предприятие не найдено")
	}
	acc, status, err := s.own(ctx, tx, user, company)
	if err != nil {
		return nil, err
	}
	if status != "active" || pledged {
		return nil, domain.E(422, "COMPANY_RESTRICTED", "расширение недоступно")
	}
	var cost, storage int64
	if err := tx.QueryRow(ctx, `SELECT expand_cost, storage FROM enterprise_templates WHERE code=$1`, template).Scan(&cost, &storage); err != nil {
		return nil, err
	}
	op := uuid.New()
	if err := s.move(ctx, tx, op, "expand", map[string]string{"enterprise": enterprise.String()}, []posting{
		{acc, -cost, 0}, {must(accCity), cost, 0},
	}); err != nil {
		return nil, err
	}
	capacity++
	if _, err := tx.Exec(ctx, `UPDATE enterprises SET capacity=$2 WHERE id=$1`, enterprise, capacity); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE warehouses SET capacity=$2 WHERE enterprise_id=$1`, enterprise, storage*int64(capacity)); err != nil {
		return nil, err
	}
	return map[string]string{"enterprise_id": enterprise.String(), "capacity": money.Format(int64(capacity)), "cost": money.Format(cost)}, nil
}

func (s *Service) PlaceOrder(ctx context.Context, user uuid.UUID, key string, raw []byte, company uuid.UUID, good, side string, price, qty int64) (any, error) {
	return s.run(ctx, user, "POST /api/v1/market/orders", key, raw, func(tx pgx.Tx) (any, error) {
		return s.placeOrder(ctx, tx, user, company, good, side, price, qty)
	})
}

func (s *Service) placeOrder(ctx context.Context, tx pgx.Tx, user, company uuid.UUID, good, side string, price, qty int64) (any, error) {
	if price <= 0 || qty <= 0 || (side != "buy" && side != "sell") {
		return nil, domain.E(400, "BAD_AMOUNT", "некорректная заявка")
	}
	if _, err := money.Mul(price, qty); err != nil {
		return nil, err
	}
	acc, status, err := s.own(ctx, tx, user, company)
	if err != nil {
		return nil, err
	}
	if status != "active" {
		return nil, domain.E(422, "COMPANY_RESTRICTED", "компания не торгует")
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT active FROM goods WHERE code=$1`, good).Scan(&active); err != nil || !active {
		return nil, domain.E(404, "NOT_FOUND", "нет такого товара")
	}
	tick, _ := s.tickOf(ctx, tx)
	oid := uuid.New()
	total, err := money.Mul(price, qty)
	if err != nil {
		return nil, err
	}
	if side == "buy" {
		if err := s.reserveMoney(ctx, tx, acc, total, "order", oid); err != nil {
			return nil, err
		}
	} else {
		if err := s.reserveGood(ctx, tx, company, good, qty, oid); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO market_orders(id, company_id, city_code, good_code, side, price, qty_total, qty_open, status, created_tick)
		VALUES ($1,$2,'ufa',$3,$4,$5,$6,$6,'open',$7)`, oid, company, good, side, price, qty, tick); err != nil {
		return nil, err
	}
	return map[string]string{"order_id": oid.String(), "side": side, "good": good, "price": money.Format(price), "qty": money.Format(qty)}, nil
}

func (s *Service) reserveMoney(ctx context.Context, tx pgx.Tx, account uuid.UUID, amount int64, reason string, ref uuid.UUID) error {
	if amount <= 0 {
		return domain.E(400, "BAD_AMOUNT", "некорректная сумма")
	}
	var class string
	var available int64
	err := tx.QueryRow(ctx, `SELECT account_class, available FROM accounts WHERE id=$1 FOR UPDATE`, account).Scan(&class, &available)
	if err != nil {
		return err
	}
	if available < amount {
		return domain.E(422, "INSUFFICIENT_FUNDS", "недостаточно средств")
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET available=available-$2, reserved=reserved+$2 WHERE id=$1`, account, amount); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO money_reservations(id, account_id, amount, reason, ref_id) VALUES ($1,$2,$3,$4,$5)`,
		uuid.New(), account, amount, reason, ref)
	return err
}

func (s *Service) releaseMoney(ctx context.Context, tx pgx.Tx, ref uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT id, account_id, amount FROM money_reservations WHERE ref_id=$1 AND active FOR UPDATE`, ref)
	if err != nil {
		return err
	}
	defer rows.Close()
	type r struct {
		id, acc uuid.UUID
		amt     int64
	}
	var list []r
	for rows.Next() {
		var x r
		if err := rows.Scan(&x.id, &x.acc, &x.amt); err != nil {
			return err
		}
		list = append(list, x)
	}
	rows.Close()
	for _, x := range list {
		if _, err := tx.Exec(ctx, `UPDATE accounts SET available=available+$2, reserved=reserved-$2 WHERE id=$1`, x.acc, x.amt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE money_reservations SET active=false WHERE id=$1`, x.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) reserveGood(ctx context.Context, tx pgx.Tx, company uuid.UUID, good string, qty int64, ref uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT id, qty, reserved FROM inventory_lots
		WHERE company_id=$1 AND good_code=$2 AND qty>reserved
		ORDER BY expires_tick NULLS LAST, created_tick, id FOR UPDATE`, company, good)
	if err != nil {
		return err
	}
	defer rows.Close()
	type lot struct {
		id       uuid.UUID
		qty, res int64
	}
	var lots []lot
	for rows.Next() {
		var l lot
		if err := rows.Scan(&l.id, &l.qty, &l.res); err != nil {
			return err
		}
		lots = append(lots, l)
	}
	rows.Close()
	need := qty
	for _, l := range lots {
		if need == 0 {
			break
		}
		free := l.qty - l.res
		take := free
		if take > need {
			take = need
		}
		if _, err := tx.Exec(ctx, `UPDATE inventory_lots SET reserved=reserved+$2 WHERE id=$1`, l.id, take); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO lot_reservations(id, lot_id, qty, ref_id) VALUES ($1,$2,$3,$4)`, uuid.New(), l.id, take, ref); err != nil {
			return err
		}
		need -= take
	}
	if need > 0 {
		return domain.E(422, "STOCK_UNAVAILABLE", "недостаточно товара")
	}
	return nil
}

func (s *Service) releaseGood(ctx context.Context, tx pgx.Tx, ref uuid.UUID) error {
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
		if _, err := tx.Exec(ctx, `UPDATE inventory_lots SET reserved=reserved-$2 WHERE id=$1`, x.lot, x.qty); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE lot_reservations SET active=false WHERE id=$1`, x.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) CancelOrder(ctx context.Context, user uuid.UUID, key string, raw []byte, order uuid.UUID) (any, error) {
	return s.run(ctx, user, "POST /api/v1/market/orders/"+order.String()+"/cancel", key, raw, func(tx pgx.Tx) (any, error) {
		var company uuid.UUID
		var status string
		var open int64
		err := tx.QueryRow(ctx, `SELECT company_id, status, qty_open FROM market_orders WHERE id=$1 FOR UPDATE`, order).Scan(&company, &status, &open)
		if err != nil {
			return nil, domain.E(404, "NOT_FOUND", "заявка не найдена")
		}
		if _, _, err := s.own(ctx, tx, user, company); err != nil {
			return nil, err
		}
		if status != "open" {
			return nil, domain.E(409, "STATE", "заявка уже закрыта")
		}
		if err := s.releaseMoney(ctx, tx, order); err != nil {
			return nil, err
		}
		if err := s.releaseGood(ctx, tx, order); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE market_orders SET status='cancelled', qty_open=0 WHERE id=$1`, order); err != nil {
			return nil, err
		}
		return map[string]string{"order_id": order.String(), "status": "cancelled"}, nil
	})
}

func (s *Service) AcceptOrder(ctx context.Context, user uuid.UUID, key string, raw []byte, order uuid.UUID, qty, maxTotal int64) (any, error) {
	return s.run(ctx, user, "POST /api/v1/market/orders/"+order.String()+"/accept", key, raw, func(tx pgx.Tx) (any, error) {
		var buyer uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM companies WHERE owner_user_id=$1 AND status='active'`, user).Scan(&buyer)
		if err != nil {
			return nil, domain.E(404, "NOT_FOUND", "нет активной компании")
		}
		return s.acceptOrder(ctx, tx, buyer, order, qty, maxTotal)
	})
}

func (s *Service) acceptOrder(ctx context.Context, tx pgx.Tx, actorCompany, order uuid.UUID, qty, maxTotal int64) (any, error) {
	if qty <= 0 || maxTotal < 0 {
		return nil, domain.E(400, "BAD_AMOUNT", "некорректный объём")
	}
	var owner uuid.UUID
	var good, side, status string
	var price, open int64
	err := tx.QueryRow(ctx, `SELECT company_id, good_code, side, price, qty_open, status FROM market_orders WHERE id=$1 FOR UPDATE`, order).
		Scan(&owner, &good, &side, &price, &open, &status)
	if err != nil {
		return nil, domain.E(404, "NOT_FOUND", "заявка не найдена")
	}
	if status != "open" || open <= 0 {
		return nil, domain.E(409, "STATE", "заявка недоступна")
	}
	if qty > open {
		qty = open
	}
	total, err := money.Mul(price, qty)
	if err != nil {
		return nil, err
	}
	if total > maxTotal {
		return nil, domain.E(422, "STALE_VERSION", "цена заявки выше допустимой суммы")
	}
	var buyer, seller uuid.UUID
	if side == "sell" {
		seller, buyer = owner, actorCompany
	} else {
		buyer, seller = owner, actorCompany
	}
	if buyer == seller {
		return nil, domain.E(422, "STATE", "нельзя исполнить свою заявку")
	}
	buyerAcc, err := companyAccount(ctx, tx, buyer)
	if err != nil {
		return nil, err
	}
	sellerAcc, err := companyAccount(ctx, tx, seller)
	if err != nil {
		return nil, err
	}
	op := uuid.New()
	tick, _ := s.tickOf(ctx, tx)
	if side == "sell" {
		if err := s.move(ctx, tx, op, "trade", map[string]string{"order": order.String()}, []posting{
			{buyerAcc, -total, 0}, {sellerAcc, total, 0},
		}); err != nil {
			return nil, err
		}
		if err := s.takeReservedGoods(ctx, tx, order, seller, buyer, good, qty, op, tick); err != nil {
			return nil, err
		}
	} else {
		if err := s.reserveGood(ctx, tx, seller, good, qty, op); err != nil {
			return nil, err
		}
		if err := s.captureReservedMoney(ctx, tx, order, buyerAcc, sellerAcc, total, op); err != nil {
			return nil, err
		}
		if err := s.takeReservedGoods(ctx, tx, op, seller, buyer, good, qty, op, tick); err != nil {
			return nil, err
		}
	}
	open -= qty
	st := "open"
	if open == 0 {
		st = "filled"
		if err := s.releaseMoney(ctx, tx, order); err != nil {
			return nil, err
		}
		if err := s.releaseGood(ctx, tx, order); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE market_orders SET qty_open=$2, status=$3 WHERE id=$1`, order, open, st); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trades(id, order_id, buyer_company_id, seller_company_id, good_code, qty, price, total, tick, operation_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, uuid.New(), order, buyer, seller, good, qty, price, total, tick, op); err != nil {
		return nil, err
	}
	_ = s.emit(ctx, tx, "trade", map[string]string{"good": good, "qty": money.Format(qty), "price": money.Format(price)})
	return map[string]string{"trade_qty": money.Format(qty), "total": money.Format(total), "price": money.Format(price), "good": good}, nil
}

func (s *Service) reduceMoneyReserve(ctx context.Context, tx pgx.Tx, ref uuid.UUID, amount int64) error {
	var id, acc uuid.UUID
	var have int64
	err := tx.QueryRow(ctx, `SELECT id, account_id, amount FROM money_reservations WHERE ref_id=$1 AND active FOR UPDATE`, ref).Scan(&id, &acc, &have)
	if err != nil {
		return err
	}
	if amount > have {
		return domain.E(422, "INSUFFICIENT_FUNDS", "резерв меньше сделки")
	}
	if _, err := tx.Exec(ctx, `UPDATE money_reservations SET amount=amount-$2 WHERE id=$1`, id, amount); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET reserved=reserved-$2 WHERE id=$1`, acc, amount); err != nil {
		return err
	}
	return nil
}

func (s *Service) captureReservedMoney(ctx context.Context, tx pgx.Tx, ref, from, to uuid.UUID, amount int64, op uuid.UUID) error {
	var id uuid.UUID
	var have int64
	err := tx.QueryRow(ctx, `SELECT id, amount FROM money_reservations WHERE ref_id=$1 AND active FOR UPDATE`, ref).Scan(&id, &have)
	if err != nil {
		return domain.E(422, "INSUFFICIENT_FUNDS", "нет денежного резерва")
	}
	if amount > have {
		return domain.E(422, "INSUFFICIENT_FUNDS", "резерв меньше сделки")
	}
	if amount == have {
		if _, err := tx.Exec(ctx, `UPDATE money_reservations SET active=false WHERE id=$1`, id); err != nil {
			return err
		}
	} else if _, err := tx.Exec(ctx, `UPDATE money_reservations SET amount=amount-$2 WHERE id=$1`, id, amount); err != nil {
		return err
	}
	return s.move(ctx, tx, op, "trade_reserved", map[string]string{"ref": ref.String()}, []posting{
		{from, -amount, -amount},
		{to, amount, 0},
	})
}

func (s *Service) takeReservedGoods(ctx context.Context, tx pgx.Tx, ref, seller, buyer uuid.UUID, good string, qty int64, op uuid.UUID, tick int64) error {
	rows, err := tx.Query(ctx, `SELECT id, lot_id, qty FROM lot_reservations WHERE ref_id=$1 AND active ORDER BY id FOR UPDATE`, ref)
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
	need := qty
	var costNum, costDen int64
	for _, x := range list {
		if need == 0 {
			break
		}
		take := x.qty
		if take > need {
			take = need
		}
		var unit int64
		if err := tx.QueryRow(ctx, `SELECT unit_cost FROM inventory_lots WHERE id=$1 FOR UPDATE`, x.lot).Scan(&unit); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE inventory_lots SET qty=qty-$2, reserved=reserved-$2 WHERE id=$1`, x.lot, take); err != nil {
			return err
		}
		if take == x.qty {
			if _, err := tx.Exec(ctx, `UPDATE lot_reservations SET active=false WHERE id=$1`, x.id); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, `UPDATE lot_reservations SET qty=qty-$2 WHERE id=$1`, x.id, take); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO inventory_moves(id, operation_id, lot_id, good_code, qty, kind, company_id, created_tick)
			VALUES ($1,$2,$3,$4,$5,'trade_out',$6,$7)`, uuid.New(), op, x.lot, good, -take, seller, tick); err != nil {
			return err
		}
		part, err := money.Mul(unit, take)
		if err != nil {
			return err
		}
		costNum += part
		costDen += take
		need -= take
	}
	if need > 0 {
		return domain.E(422, "STOCK_UNAVAILABLE", "резерв товара меньше сделки")
	}
	unitOut := int64(0)
	if costDen > 0 {
		unitOut = costNum / costDen
	}
	if err := s.addLot(ctx, tx, buyer, good, qty, unitOut, tick, 0); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO inventory_moves(id, operation_id, good_code, qty, kind, company_id, created_tick)
		VALUES ($1,$2,$3,$4,'trade_in',$5,$6)`, uuid.New(), op, good, qty, buyer, tick)
	return err
}

func (s *Service) addLot(ctx context.Context, tx pgx.Tx, company uuid.UUID, good string, qty, unit, tick, spoilTicks int64) error {
	if qty <= 0 {
		return nil
	}
	var wh uuid.UUID
	var cap int64
	err := tx.QueryRow(ctx, `SELECT w.id, w.capacity FROM warehouses w
		JOIN enterprises e ON e.id=w.enterprise_id
		WHERE e.company_id=$1 AND e.status<>'closed'
		ORDER BY w.capacity DESC LIMIT 1`, company).Scan(&wh, &cap)
	if err != nil {
		return domain.E(422, "CAPACITY_EXCEEDED", "нет склада")
	}
	var used int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(qty),0) FROM inventory_lots WHERE warehouse_id=$1`, wh).Scan(&used); err != nil {
		return err
	}
	if used+qty > cap {
		return domain.E(422, "CAPACITY_EXCEEDED", "склад переполнен")
	}
	var exp any
	if spoilTicks > 0 {
		exp = tick + spoilTicks
	}
	_, err = tx.Exec(ctx, `INSERT INTO inventory_lots(id, warehouse_id, company_id, good_code, qty, unit_cost, created_tick, expires_tick)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, uuid.New(), wh, company, good, qty, unit, tick, exp)
	return err
}

func (s *Service) OfferContract(ctx context.Context, user uuid.UUID, key string, raw []byte, company uuid.UUID, good string, qty, price, deadline int64) (any, error) {
	return s.run(ctx, user, "POST /api/v1/contracts", key, raw, func(tx pgx.Tx) (any, error) {
		return s.offerContract(ctx, tx, user, company, good, qty, price, deadline)
	})
}

func (s *Service) offerContract(ctx context.Context, tx pgx.Tx, user, company uuid.UUID, good string, qty, price, deadline int64) (any, error) {
	if _, _, err := s.own(ctx, tx, user, company); err != nil {
		return nil, err
	}
	tick, _ := s.tickOf(ctx, tx)
	if deadline <= tick || qty <= 0 || price <= 0 {
		return nil, domain.E(400, "BAD_AMOUNT", "некорректный контракт")
	}
	id := uuid.New()
	if err := s.reserveGood(ctx, tx, company, good, qty, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO contracts(id, seller_company_id, city_code, good_code, qty, price, deadline_tick, status, created_tick)
		VALUES ($1,$2,'ufa',$3,$4,$5,$6,'offered',$7)`, id, company, good, qty, price, deadline, tick); err != nil {
		return nil, err
	}
	return map[string]string{"contract_id": id.String(), "status": "offered"}, nil
}

func (s *Service) AcceptContract(ctx context.Context, user uuid.UUID, key string, raw []byte, contract uuid.UUID) (any, error) {
	return s.run(ctx, user, "POST /api/v1/contracts/"+contract.String()+"/accept", key, raw, func(tx pgx.Tx) (any, error) {
		var buyer uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM companies WHERE owner_user_id=$1 AND status='active'`, user).Scan(&buyer); err != nil {
			return nil, domain.E(404, "NOT_FOUND", "нет активной компании")
		}
		return s.acceptContract(ctx, tx, buyer, contract)
	})
}

func (s *Service) acceptContract(ctx context.Context, tx pgx.Tx, buyer, contract uuid.UUID) (any, error) {
	var seller uuid.UUID
	var qty, price, deadline int64
	var status string
	err := tx.QueryRow(ctx, `SELECT seller_company_id, qty, price, deadline_tick, status FROM contracts WHERE id=$1 FOR UPDATE`, contract).
		Scan(&seller, &qty, &price, &deadline, &status)
	if err != nil {
		return nil, domain.E(404, "NOT_FOUND", "контракт не найден")
	}
	tick, _ := s.tickOf(ctx, tx)
	if status != "offered" || deadline < tick {
		return nil, domain.E(422, "CONTRACT_EXPIRED", "контракт недоступен")
	}
	if seller == buyer {
		return nil, domain.E(422, "STATE", "нельзя принять свой контракт")
	}
	total, err := money.Mul(price, qty)
	if err != nil {
		return nil, err
	}
	acc, err := companyAccount(ctx, tx, buyer)
	if err != nil {
		return nil, err
	}
	if err := s.reserveMoney(ctx, tx, acc, total, "contract", contract); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE contracts SET status='accepted', buyer_company_id=$2 WHERE id=$1`, contract, buyer); err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO deliveries(id, contract_id, arrive_tick, status) VALUES ($1,$2,$3,'transit')`, uuid.New(), contract, tick+1)
	if err != nil {
		return nil, err
	}
	return map[string]string{"contract_id": contract.String(), "status": "accepted", "total": money.Format(total)}, nil
}

func (s *Service) Borrow(ctx context.Context, user uuid.UUID, key string, raw []byte, company, enterprise uuid.UUID, amount int64) (any, error) {
	return s.run(ctx, user, "POST /api/v1/loans", key, raw, func(tx pgx.Tx) (any, error) {
		return s.borrow(ctx, tx, user, company, enterprise, amount)
	})
}

func (s *Service) borrow(ctx context.Context, tx pgx.Tx, user, company, enterprise uuid.UUID, amount int64) (any, error) {
	acc, status, err := s.own(ctx, tx, user, company)
	if err != nil {
		return nil, err
	}
	if status != "active" {
		return nil, domain.E(422, "COMPANY_RESTRICTED", "кредит недоступен")
	}
	if amount <= 0 {
		return nil, domain.E(400, "BAD_AMOUNT", "некорректная сумма")
	}
	var owner uuid.UUID
	var pledged bool
	var template string
	err = tx.QueryRow(ctx, `SELECT company_id, pledged, template_code FROM enterprises WHERE id=$1 AND status<>'closed'`, enterprise).Scan(&owner, &pledged, &template)
	if err != nil || owner != company || pledged {
		return nil, domain.E(422, "STATE", "залог недоступен")
	}
	var book int64
	if err := tx.QueryRow(ctx, `SELECT book_value FROM enterprise_templates WHERE code=$1`, template).Scan(&book); err != nil {
		return nil, err
	}
	ltv, _ := s.cfg(ctx, tx, "loan_ltv_bps")
	maxLoan := book * ltv / 10000
	if amount > maxLoan {
		return nil, domain.E(422, "CAPACITY_EXCEEDED", "сумма выше обеспечения")
	}
	rate, _ := s.cfg(ctx, tx, "loan_rate_bps")
	period, _ := s.cfg(ctx, tx, "loan_period_ticks")
	tick, _ := s.tickOf(ctx, tx)
	op := uuid.New()
	if err := s.move(ctx, tx, op, "loan_draw", map[string]string{"company": company.String()}, []posting{
		{must(accCredit), -amount, 0}, {acc, amount, 0},
	}); err != nil {
		return nil, err
	}
	id := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO loans(id, company_id, principal, outstanding, rate_bps, collateral_enterprise_id, next_interest_tick, due_tick, status)
		VALUES ($1,$2,$3,$3,$4,$5,$6,$7,'active')`, id, company, amount, rate, enterprise, tick+period, tick+period*4); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE enterprises SET pledged=true WHERE id=$1`, enterprise); err != nil {
		return nil, err
	}
	return map[string]string{"loan_id": id.String(), "amount": money.Format(amount)}, nil
}

func (s *Service) Repay(ctx context.Context, user uuid.UUID, key string, raw []byte, loan uuid.UUID, amount int64) (any, error) {
	return s.run(ctx, user, "POST /api/v1/loans/"+loan.String()+"/repay", key, raw, func(tx pgx.Tx) (any, error) {
		var company uuid.UUID
		var outstanding int64
		var ent uuid.UUID
		var status string
		err := tx.QueryRow(ctx, `SELECT company_id, outstanding, collateral_enterprise_id, status FROM loans WHERE id=$1 FOR UPDATE`, loan).
			Scan(&company, &outstanding, &ent, &status)
		if err != nil {
			return nil, domain.E(404, "NOT_FOUND", "кредит не найден")
		}
		acc, _, err := s.own(ctx, tx, user, company)
		if err != nil {
			return nil, err
		}
		if amount <= 0 || amount > outstanding {
			amount = outstanding
		}
		op := uuid.New()
		if err := s.move(ctx, tx, op, "loan_repay", map[string]string{"loan": loan.String()}, []posting{
			{acc, -amount, 0}, {must(accCredit), amount, 0},
		}); err != nil {
			return nil, err
		}
		outstanding -= amount
		st := status
		if outstanding == 0 {
			st = "repaid"
			if _, err := tx.Exec(ctx, `UPDATE enterprises SET pledged=false WHERE id=$1`, ent); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE loans SET outstanding=$2, status=$3 WHERE id=$1`, loan, outstanding, st); err != nil {
			return nil, err
		}
		return map[string]string{"loan_id": loan.String(), "outstanding": money.Format(outstanding), "status": st}, nil
	})
}

func (s *Service) Grant(ctx context.Context, user uuid.UUID, key string, raw []byte, company uuid.UUID, cash, qty int64, good, reason string) (any, error) {
	if !s.TestAdmin {
		return nil, domain.E(403, "FORBIDDEN", "тестовые команды выключены")
	}
	return s.run(ctx, user, "POST /api/v1/test-admin/grants", key, raw, func(tx pgx.Tx) (any, error) {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles WHERE user_id=$1 AND role IN ('test_operator','superadmin'))`, user).Scan(&ok); err != nil || !ok {
			return nil, domain.E(403, "FORBIDDEN", "нет роли тестового оператора")
		}
		if len(reason) < 8 {
			return nil, domain.E(400, "REASON_REQUIRED", "нужна причина")
		}
		var owner uuid.UUID
		var kind, status string
		var acc uuid.UUID
		err := tx.QueryRow(ctx, `SELECT owner_user_id, owner_kind, status, cash_account_id FROM companies WHERE id=$1`, company).Scan(&owner, &kind, &status, &acc)
		if err != nil || owner != user || kind != "TEST_ADMIN" || status == "closed" {
			return nil, domain.E(403, "FORBIDDEN", "грант только своей тестовой компании")
		}
		op := uuid.New()
		if cash > 0 {
			if err := s.move(ctx, tx, op, "test_grant", map[string]string{"reason": reason}, []posting{
				{must(accTest), -cash, 0}, {acc, cash, 0},
			}); err != nil {
				return nil, err
			}
		}
		tick, _ := s.tickOf(ctx, tx)
		if qty > 0 {
			if good == "" {
				return nil, domain.E(400, "BAD_AMOUNT", "укажите товар")
			}
			spoil, _ := s.cfg(ctx, tx, good+"_spoil_ticks")
			if err := s.addLot(ctx, tx, company, good, qty, 0, tick, spoil); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO inventory_moves(id, operation_id, good_code, qty, kind, company_id, created_tick)
				VALUES ($1,$2,$3,$4,'test_grant',$5,$6)`, uuid.New(), op, good, qty, company, tick); err != nil {
				return nil, err
			}
		}
		actor := user
		if err := s.audit(ctx, tx, &actor, "test_grant", reason, nil, map[string]string{"company": company.String(), "cash": money.Format(cash), "good": good, "qty": money.Format(qty)}); err != nil {
			return nil, err
		}
		return map[string]string{"operation_id": op.String(), "cash": money.Format(cash)}, nil
	})
}

func (s *Service) AddRecipe(ctx context.Context, actor uuid.UUID, code string, in, out []Item, energy int64, dur int, expected int) (any, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := s.lockWorld(ctx, tx); err != nil {
		return nil, err
	}
	var current int
	err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM recipe_versions WHERE code=$1`, code).Scan(&current)
	if err != nil {
		return nil, err
	}
	if expected != current {
		return nil, domain.E(409, "STALE_VERSION", "рецепт уже изменён")
	}
	if dur < 1 || energy < 0 || len(in) == 0 || len(out) == 0 {
		return nil, domain.E(400, "BAD_AMOUNT", "некорректный рецепт")
	}
	version := current + 1
	if err := insertRecipe(ctx, tx, code, version, in, out, energy, dur); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, tx, &actor, "recipe_version", "новая версия рецепта", map[string]int{"version": current}, map[string]any{"version": version, "code": code}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]string{"code": code, "version": money.Format(int64(version))}, nil
}

func (s *Service) SetConfig(ctx context.Context, actor uuid.UUID, key, value string, expected int) (any, error) {
	if _, err := money.Parse(value); err != nil {
		return nil, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var current int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM config_versions WHERE key=$1`, key).Scan(&current); err != nil {
		return nil, err
	}
	if expected != current {
		return nil, domain.E(409, "STALE_VERSION", "настройка уже изменена")
	}
	raw, _ := json.Marshal(value)
	version := current + 1
	if _, err := tx.Exec(ctx, `INSERT INTO config_versions(id, key, version, value, effective_at, created_by) VALUES ($1,$2,$3,$4,now(),$5)`,
		uuid.New(), key, version, raw, actor); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, tx, &actor, "config_version", key, map[string]int{"version": current}, map[string]string{"value": value, "version": money.Format(int64(version))}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]string{"key": key, "version": money.Format(int64(version)), "value": value}, nil
}

func (s *Service) SetVIP(ctx context.Context, actor, target uuid.UUID, hours int, reason string) (any, error) {
	if hours <= 0 || len(reason) < 4 {
		return nil, domain.E(400, "REASON_REQUIRED", "укажите срок и причину")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	id := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO vip_entitlements(id, user_id, starts_at, expires_at, source, created_by)
		VALUES ($1,$2,now(), now() + make_interval(hours => $3), 'admin', $4)`, id, target, hours, actor); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, tx, &actor, "vip_grant", reason, nil, map[string]any{"user": target.String(), "hours": hours}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]string{"entitlement_id": id.String()}, nil
}

func (s *Service) MyCompany(ctx context.Context, user uuid.UUID) (CompanyView, error) {
	var v CompanyView
	var cash, reserved int64
	err := s.Pool.QueryRow(ctx, `SELECT c.id, c.name, c.owner_kind, c.status, c.city_code, a.available, a.reserved
		FROM companies c JOIN accounts a ON a.id=c.cash_account_id
		WHERE c.owner_user_id=$1 AND c.status<>'closed'`, user).Scan(&v.ID, &v.Name, &v.Kind, &v.Status, &v.City, &cash, &reserved)
	if err != nil {
		return CompanyView{}, domain.E(404, "NOT_FOUND", "компания не найдена")
	}
	v.Cash = money.Format(cash)
	v.Reserved = money.Format(reserved)
	return v, nil
}

// PlayerBoard is the read model for a human client. Missing company is not an error.
func (s *Service) PlayerBoard(ctx context.Context, user uuid.UUID) (map[string]any, error) {
	var tick int64
	if err := s.Pool.QueryRow(ctx, `SELECT tick FROM world_state WHERE id=1`).Scan(&tick); err != nil {
		return nil, err
	}
	peers, err := s.peerList(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"tick":        money.Format(tick),
		"company":     nil,
		"enterprises": []any{},
		"stock":       []any{},
		"orders":      []any{},
		"contracts":   []any{},
		"offers":      []any{},
		"loans":       []any{},
		"peers":       peers,
	}
	v, err := s.MyCompany(ctx, user)
	if err != nil {
		var d *domain.Error
		if errors.As(err, &d) && d.Code == "NOT_FOUND" {
			return out, nil
		}
		return nil, err
	}
	out["company"] = v
	cid := uuid.MustParse(v.ID)
	ents, err := queryMaps(ctx, s.Pool, `SELECT e.id::text AS id, e.template_code AS template, t.title AS title,
		e.status AS status, e.capacity::text AS capacity, e.pledged AS pledged,
		w.headcount::text AS headcount, w.wage::text AS wage
		FROM enterprises e
		JOIN enterprise_templates t ON t.code=e.template_code
		JOIN workforce w ON w.enterprise_id=e.id
		WHERE e.company_id=$1 AND e.status<>'closed'
		ORDER BY e.created_tick, e.id`, cid)
	if err != nil {
		return nil, err
	}
	out["enterprises"] = ents
	stock, err := queryMaps(ctx, s.Pool, `SELECT good_code AS good, SUM(qty)::text AS qty, SUM(reserved)::text AS reserved
		FROM inventory_lots WHERE company_id=$1 AND qty>0 GROUP BY good_code ORDER BY good_code`, cid)
	if err != nil {
		return nil, err
	}
	out["stock"] = stock
	orders, err := queryMaps(ctx, s.Pool, `SELECT id::text AS id, good_code AS good, side, price::text AS price, qty_open::text AS qty_open
		FROM market_orders WHERE company_id=$1 AND status='open' ORDER BY created_tick DESC`, cid)
	if err != nil {
		return nil, err
	}
	out["orders"] = orders
	contracts, err := queryMaps(ctx, s.Pool, `SELECT id::text AS id, seller_company_id::text AS seller, COALESCE(buyer_company_id::text,'') AS buyer,
		good_code AS good, qty::text AS qty, price::text AS price, status, deadline_tick::text AS deadline_tick
		FROM contracts WHERE seller_company_id=$1 OR buyer_company_id=$1
		ORDER BY created_tick DESC LIMIT 20`, cid)
	if err != nil {
		return nil, err
	}
	out["contracts"] = contracts
	offers, err := queryMaps(ctx, s.Pool, `SELECT c.id::text AS id, c.seller_company_id::text AS seller, s.name AS seller_name,
		c.good_code AS good, c.qty::text AS qty, c.price::text AS price, c.deadline_tick::text AS deadline_tick
		FROM contracts c JOIN companies s ON s.id=c.seller_company_id
		WHERE c.status='offered' AND c.city_code='ufa' AND c.seller_company_id<>$1
		ORDER BY c.created_tick LIMIT 20`, cid)
	if err != nil {
		return nil, err
	}
	out["offers"] = offers
	loans, err := queryMaps(ctx, s.Pool, `SELECT id::text AS id, outstanding::text AS outstanding, principal::text AS principal,
		status, collateral_enterprise_id::text AS enterprise_id
		FROM loans WHERE company_id=$1 AND status IN ('active','overdue')`, cid)
	if err != nil {
		return nil, err
	}
	out["loans"] = loans
	return out, nil
}

func (s *Service) peerList(ctx context.Context) ([]any, error) {
	return queryMaps(ctx, s.Pool, `SELECT c.id::text AS id, c.name AS name, c.owner_kind AS owner_kind, c.status AS status, u.is_ai AS is_ai
		FROM companies c JOIN users u ON u.id=c.owner_user_id
		WHERE c.city_code='ufa' AND c.status<>'closed'
		ORDER BY c.owner_kind, c.name`)
}

func queryMaps(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, sql string, args ...any) ([]any, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []any
	fields := rows.FieldDescriptions()
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, f := range fields {
			switch v := vals[i].(type) {
			case [16]byte:
				m[string(f.Name)] = uuid.UUID(v).String()
			default:
				m[string(f.Name)] = v
			}
		}
		out = append(out, m)
	}
	if out == nil {
		out = []any{}
	}
	return out, rows.Err()
}
