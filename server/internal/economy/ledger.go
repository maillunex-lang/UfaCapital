package economy

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/maillunex-lang/UfaCapital/server/internal/domain"
	"github.com/maillunex-lang/UfaCapital/server/internal/money"
)

type posting struct {
	account  uuid.UUID
	ledger   int64
	reserved int64
}

func (s *Service) lockWorld(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(0x554641434150))
	return err
}

func (s *Service) move(ctx context.Context, tx pgx.Tx, operation uuid.UUID, kind string, meta any, posts []posting) error {
	agg := map[uuid.UUID]*posting{}
	var sum int64
	for _, p := range posts {
		if p.ledger == 0 && p.reserved == 0 {
			continue
		}
		cur := agg[p.account]
		if cur == nil {
			cur = &posting{account: p.account}
			agg[p.account] = cur
		}
		var err error
		if cur.ledger, err = money.Add(cur.ledger, p.ledger); err != nil {
			return err
		}
		if cur.reserved, err = money.Add(cur.reserved, p.reserved); err != nil {
			return err
		}
		sum += p.ledger
	}
	if sum != 0 {
		return domain.E(500, "LEDGER", "проводка не сходится")
	}
	ids := make([]uuid.UUID, 0, len(agg))
	for id := range agg {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	type acc struct {
		class     string
		available int64
		reserved  int64
		limit     *int64
	}
	state := map[uuid.UUID]*acc{}
	for _, id := range ids {
		a := &acc{}
		var lim *int64
		err := tx.QueryRow(ctx, `SELECT account_class, available, reserved, emission_limit FROM accounts WHERE id=$1 FOR UPDATE`, id).
			Scan(&a.class, &a.available, &a.reserved, &lim)
		if err != nil {
			return err
		}
		a.limit = lim
		state[id] = a
	}
	for id, d := range agg {
		a := state[id]
		a.reserved += d.reserved
		a.available += d.ledger - d.reserved
		if a.reserved < 0 {
			return domain.E(422, "INSUFFICIENT_FUNDS", "резерв больше остатка")
		}
		if a.class == "emission" {
			if a.reserved != 0 {
				return domain.E(422, "INSUFFICIENT_FUNDS", "эмиссия не резервируется")
			}
			if a.limit != nil && -a.available > *a.limit {
				return domain.E(422, "INSUFFICIENT_FUNDS", "лимит эмиссии исчерпан")
			}
			continue
		}
		if a.available < 0 {
			return domain.E(422, "INSUFFICIENT_FUNDS", "недостаточно средств")
		}
	}
	raw, _ := json.Marshal(meta)
	if raw == nil {
		raw = []byte(`{}`)
	}
	txID := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_transactions(id, operation_id, kind, metadata) VALUES ($1,$2,$3,$4)`,
		txID, operation, kind, raw); err != nil {
		return err
	}
	inserted := false
	for _, id := range ids {
		d := agg[id]
		a := state[id]
		if d.ledger != 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO ledger_entries(tx_id, account_id, amount) VALUES ($1,$2,$3)`, txID, id, d.ledger); err != nil {
				return err
			}
			inserted = true
		}
		if _, err := tx.Exec(ctx, `UPDATE accounts SET available=$2, reserved=$3 WHERE id=$1`, id, a.available, a.reserved); err != nil {
			return err
		}
	}
	if !inserted {
		if _, err := tx.Exec(ctx, `DELETE FROM ledger_transactions WHERE id=$1`, txID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) tickOf(ctx context.Context, tx pgx.Tx) (int64, error) {
	var tick int64
	err := tx.QueryRow(ctx, `SELECT tick FROM world_state WHERE id=1`).Scan(&tick)
	return tick, err
}

func (s *Service) cfg(ctx context.Context, tx pgx.Tx, key string) (int64, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT value FROM config_versions
		WHERE key=$1 AND effective_at <= now()
		ORDER BY version DESC LIMIT 1`, key).Scan(&raw)
	if err != nil {
		return 0, err
	}
	var rawStr string
	if err := json.Unmarshal(raw, &rawStr); err != nil {
		return 0, err
	}
	return money.Parse(rawStr)
}
