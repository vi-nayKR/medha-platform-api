package migrations_test

import (
	"strings"
	"testing"

	"github.com/medha/backend/migrations"
)

func TestPublicationAccountMigrationsAreVersionedNoOps(t *testing.T) {
	for _, name := range []string{
		"00021_dev_users_seed.sql",
		"00035_reset_dev_users.sql",
		"00048_seed_dev_pandit_profiles.sql",
		"00049_seed_reviewer_test_accounts.sql",
	} {
		t.Run(name, func(t *testing.T) {
			data, err := migrations.MigrationsFS.ReadFile(name)
			if err != nil {
				t.Fatalf("migration version must remain embedded: %v", err)
			}
			text := string(data)
			if strings.Count(text, "-- +goose Up") != 1 || strings.Count(text, "-- +goose Down") != 1 {
				t.Fatal("migration must retain both Goose directions")
			}
			var statements []string
			for _, line := range strings.Split(text, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "--") {
					statements = append(statements, line)
				}
			}
			if strings.Join(statements, "\n") != "SELECT 1;\nSELECT 1;" {
				t.Fatal("account fixtures must not insert, modify or delete user data")
			}
		})
	}
}
