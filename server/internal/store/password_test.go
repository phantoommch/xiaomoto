package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestLoginWithPasswordDoesNotCreateUser(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), Limits{
		PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 10, RatePerDay: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	_, _, _, err = st.LoginWithPassword("ghost@example.com", "correct-horse", time.Now())
	if !errors.Is(err, ErrPassword) {
		t.Fatalf("missing user err %v", err)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("users %d", n)
	}
}
