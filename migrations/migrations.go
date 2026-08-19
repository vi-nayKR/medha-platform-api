package migrations

import (
	"embed"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var MigrationsFS embed.FS

// Run migrations against the provided database.
func Run(dialect string, dbURL string) error {
	goose.SetBaseFS(MigrationsFS)

	if err := goose.SetDialect(dialect); err != nil {
		return err
	}

	db, err := goose.OpenDBWithDriver(dialect, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := goose.Up(db, ".", goose.WithAllowMissing()); err != nil {
		return err
	}

	// Post-migration cleanup to deduplicate existing databases
	cleanupSQL := `
	DO $$
	BEGIN
		-- Delete duplicate festival records, keeping the oldest/first created one.
		DELETE FROM festivals
		WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY festival, date ORDER BY created_at ASC, id ASC) as row_num
				FROM festivals
			) t
			WHERE t.row_num > 1
		);

		-- Add unique constraint if it doesn't already exist.
		IF NOT EXISTS (
			SELECT 1 
			FROM pg_constraint 
			WHERE conname = 'uniq_festival_date'
		) THEN
			ALTER TABLE festivals ADD CONSTRAINT uniq_festival_date UNIQUE (festival, date);
		END IF;
	END $$;
	`
	if _, err := db.Exec(cleanupSQL); err != nil {
		return err
	}

	return nil
}
