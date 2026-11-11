package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Up applies pending SQL files in lexical order. Applied files are immutable.
func Up(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	if err := execScript(ctx, pool, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version text PRIMARY KEY,
		checksum text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	for _, name := range files {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		checksum := hex.EncodeToString(sum[:])
		var existing string
		err = pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=$1`, name).Scan(&existing)
		if err == nil {
			if existing != checksum {
				return fmt.Errorf("migration %s checksum changed after apply", name)
			}
			continue
		}
		script := "BEGIN;\n" + string(body) + "\nINSERT INTO schema_migrations(version, checksum) VALUES ('" + name + "','" + checksum + "');\nCOMMIT;\n"
		if err := execScript(ctx, pool, script); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func execScript(ctx context.Context, pool *pgxpool.Pool, sql string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	mrr := conn.Conn().PgConn().Exec(ctx, sql)
	for mrr.NextResult() {
		if _, err := mrr.ResultReader().Close(); err != nil {
			_ = mrr.Close()
			return err
		}
	}
	return mrr.Close()
}
