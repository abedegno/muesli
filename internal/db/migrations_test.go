package db

import "testing"

func TestValidateMigrationNames(t *testing.T) {
	good := []string{
		"0001_init.up.sql", "0001_init.down.sql",
		"20260701143000_webhook.up.sql", "20260701143000_webhook.down.sql",
	}
	if err := validateMigrationNames(good); err != nil {
		t.Fatalf("valid set rejected: %v", err)
	}

	dupVersion := []string{
		"0016_a.up.sql", "0016_a.down.sql",
		"0016_b.up.sql", "0016_b.down.sql", // same version 0016 as _a -> two ups
	}
	if err := validateMigrationNames(dupVersion); err == nil {
		t.Fatal("expected duplicate version 0016 to be rejected")
	}

	missingDown := []string{"0002_x.up.sql"}
	if err := validateMigrationNames(missingDown); err == nil {
		t.Fatal("expected missing .down.sql to be rejected")
	}

	nonDigit := []string{"abc_x.up.sql", "abc_x.down.sql"}
	if err := validateMigrationNames(nonDigit); err == nil {
		t.Fatal("expected non-numeric version prefix to be rejected")
	}
}

// TestEmbeddedMigrationsValid runs the invariant against what actually ships.
func TestEmbeddedMigrationsValid(t *testing.T) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if err := validateMigrationNames(names); err != nil {
		t.Fatalf("embedded migrations invalid: %v", err)
	}
}
