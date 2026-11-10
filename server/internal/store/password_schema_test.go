package store

import (
	"path/filepath"
	"testing"
)

// The open-source users table has no password material. This locks that fact
// until a migration is added from the production schema, not from a guess.
func TestUsersTableHasNoPasswordColumns(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), Limits{
		PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 10, RatePerDay: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rows, err := st.db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"id", "email", "created_at", "last_login_at", "terms_version_accepted", "terms_accepted_at", "disabled"} {
		if !cols[name] {
			t.Fatalf("users missing %s", name)
		}
	}
	for _, name := range []string{"password", "password_hash", "password_salt"} {
		if cols[name] {
			t.Fatalf("users unexpectedly has %s; do not invent a production password column", name)
		}
	}
}
