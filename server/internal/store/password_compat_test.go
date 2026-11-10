package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openPasswordDB(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), Limits{
		PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 10, RatePerDay: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestUnknownEmailPasswordDoesNotCreateUser(t *testing.T) {
	st := openPasswordDB(t)
	err := st.CheckPassword("nobody@example.com", "not-a-real-1", "203.0.113.5", time.Now())
	if !errors.Is(err, ErrBadPassword) {
		t.Fatalf("err %v", err)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("users created: %d", n)
	}
}

func TestLegacyBcryptHashFromOriginalImplementation(t *testing.T) {
	st := openPasswordDB(t)
	user, _, _, err := st.Login("legacy@example.com", "1.0", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := hashPassword("legacy-pass-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(fixture, "$2a$") && !strings.HasPrefix(fixture, "$2b$") {
		t.Fatalf("original hash is not bcrypt: %s", fixture[:4])
	}
	stamp := time.Now().UTC().Format(time.RFC3339)
	if _, err := st.db.Exec(`UPDATE users SET password_hash=?, password_set_at=? WHERE id=?`, fixture, stamp, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckPassword("legacy@example.com", "legacy-pass-1", "", time.Now()); err != nil {
		t.Fatalf("legacy hash rejected: %v", err)
	}
	if err := st.CheckPassword("legacy@example.com", "wrong-pass-9", "", time.Now()); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("wrong password %v", err)
	}
	var stored string
	if err := st.db.QueryRow(`SELECT password_hash FROM users WHERE id=?`, user.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != fixture {
		t.Fatal("check rewrote the stored hash")
	}
}

func TestExpiredCodeIsRejectedAndConsumed(t *testing.T) {
	st := openPasswordDB(t)
	email := "expire@example.com"
	issued, err := st.IssueCode(email, "203.0.113.8", time.Now().Add(-11*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	err = st.CheckCode(email, issued.Code, time.Now())
	var input InputError
	if !errors.As(err, &input) || !strings.Contains(input.Message, "失效") {
		t.Fatalf("expired code %v", err)
	}
	if err := st.CheckCode(email, issued.Code, time.Now()); err == nil {
		t.Fatal("expired code still accepted")
	}
	var consumed int
	if err := st.db.QueryRow(`SELECT consumed FROM email_codes WHERE email=?`, email).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if consumed != 1 {
		t.Fatalf("consumed %d", consumed)
	}
}
