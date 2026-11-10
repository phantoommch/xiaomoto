package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var ErrDisabled = errors.New("disabled")

const (
	codeTTL         = 10 * time.Minute
	maxCodeAttempts = 5
	emailMinGap     = 60 * time.Second
	emailPerHour    = 10
	ipPerMinute     = 5
	ipPerHour       = 30
	sessionTTL      = 180 * 24 * time.Hour
)

type User struct {
	ID                   string
	Email                string
	TermsVersionAccepted string
	Disabled             bool
}

type IssuedCode struct {
	Code string
}

func (s *Store) IssueCode(email, ip string, now time.Time) (IssuedCode, error) {
	email = normalizeEmail(email)
	if !validEmail(email) {
		return IssuedCode{}, InputError{Message: "邮箱格式不正确"}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return IssuedCode{}, err
	}
	defer tx.Rollback()
	if err := allowAuth(tx, "email", email, now, emailMinGap, emailPerHour); err != nil {
		return IssuedCode{}, err
	}
	if ip != "" {
		if err := allowAuth(tx, "ip", ip, now, 0, ipPerHour); err != nil {
			return IssuedCode{}, err
		}
		if err := countLimit(tx, "ip", ip, now.Add(-time.Minute), ipPerMinute); err != nil {
			return IssuedCode{}, err
		}
	}
	code, err := randomDigits(6)
	if err != nil {
		return IssuedCode{}, err
	}
	salt, err := randomHex(16)
	if err != nil {
		return IssuedCode{}, err
	}
	stamp := now.UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`UPDATE email_codes SET consumed=1 WHERE email=? AND consumed=0`, email); err != nil {
		return IssuedCode{}, err
	}
	if _, err := tx.Exec(`
INSERT INTO email_codes (email, code_hash, salt, expires_at, attempts, consumed, created_at, ip)
VALUES (?, ?, ?, ?, 0, 0, ?, ?)`,
		email, hashSecret(salt, code), salt, now.UTC().Add(codeTTL).Format(time.RFC3339), stamp, ip,
	); err != nil {
		return IssuedCode{}, err
	}
	if _, err := tx.Exec(`INSERT INTO auth_limits (scope, key, created_at) VALUES ('email', ?, ?)`, email, stamp); err != nil {
		return IssuedCode{}, err
	}
	if ip != "" {
		if _, err := tx.Exec(`INSERT INTO auth_limits (scope, key, created_at) VALUES ('ip', ?, ?)`, ip, stamp); err != nil {
			return IssuedCode{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return IssuedCode{}, err
	}
	return IssuedCode{Code: code}, nil
}

func allowAuth(tx *sql.Tx, scope, key string, now time.Time, gap time.Duration, perHour int) error {
	if key == "" {
		return nil
	}
	if gap > 0 {
		var latest string
		err := tx.QueryRow(`SELECT created_at FROM auth_limits WHERE scope=? AND key=? ORDER BY id DESC LIMIT 1`, scope, key).Scan(&latest)
		if err == nil {
			t, parseErr := time.Parse(time.RFC3339, latest)
			if parseErr == nil && now.Sub(t) < gap {
				return ErrRateLimited
			}
		} else if err != sql.ErrNoRows {
			return err
		}
	}
	return countLimit(tx, scope, key, now.Add(-time.Hour), perHour)
}

func countLimit(tx *sql.Tx, scope, key string, since time.Time, max int) error {
	if key == "" || max <= 0 {
		return nil
	}
	var n int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM auth_limits WHERE scope=? AND key=? AND created_at>=?`,
		scope, key, since.UTC().Format(time.RFC3339),
	).Scan(&n); err != nil {
		return err
	}
	if n >= max {
		return ErrRateLimited
	}
	return nil
}

func (s *Store) CheckCode(email, code string, now time.Time) error {
	email = normalizeEmail(email)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	var codeHash, salt, expires string
	var attempts int
	err = tx.QueryRow(`
SELECT id, code_hash, salt, expires_at, attempts
FROM email_codes
WHERE email=? AND consumed=0
ORDER BY id DESC LIMIT 1`, email).Scan(&id, &codeHash, &salt, &expires, &attempts)
	if err == sql.ErrNoRows {
		return InputError{Message: "激活码不正确"}
	}
	if err != nil {
		return err
	}
	exp, _ := time.Parse(time.RFC3339, expires)
	if attempts >= maxCodeAttempts || now.After(exp) {
		_, _ = tx.Exec(`UPDATE email_codes SET consumed=1 WHERE id=?`, id)
		_ = tx.Commit()
		return InputError{Message: "激活码已失效，请重新获取"}
	}
	got := hashSecret(salt, strings.TrimSpace(code))
	if subtle.ConstantTimeCompare([]byte(got), []byte(codeHash)) != 1 {
		attempts++
		consumed := 0
		message := "激活码不正确"
		if attempts >= maxCodeAttempts {
			consumed = 1
			message = "激活码已失效，请重新获取"
		}
		if _, err := tx.Exec(`UPDATE email_codes SET attempts=?, consumed=? WHERE id=?`, attempts, consumed, id); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return InputError{Message: message}
	}
	if _, err := tx.Exec(`UPDATE email_codes SET consumed=1 WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Login(email, termsVersion string, now time.Time) (User, string, time.Time, error) {
	email = normalizeEmail(email)
	tx, err := s.db.Begin()
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	defer tx.Rollback()
	stamp := now.UTC().Format(time.RFC3339)
	var user User
	var disabled int
	err = tx.QueryRow(`SELECT id, email, terms_version_accepted, disabled FROM users WHERE email=?`, email).
		Scan(&user.ID, &user.Email, &user.TermsVersionAccepted, &disabled)
	if err == sql.ErrNoRows {
		user.ID = newID()
		user.Email = email
		user.TermsVersionAccepted = termsVersion
		if _, err := tx.Exec(`
INSERT INTO users (id, email, created_at, last_login_at, terms_version_accepted, terms_accepted_at, disabled)
VALUES (?, ?, ?, ?, ?, ?, 0)`, user.ID, email, stamp, stamp, termsVersion, stamp); err != nil {
			return User{}, "", time.Time{}, err
		}
	} else if err != nil {
		return User{}, "", time.Time{}, err
	} else if disabled != 0 {
		return User{}, "", time.Time{}, ErrDisabled
	} else {
		user.Disabled = false
		user.TermsVersionAccepted = termsVersion
		if _, err := tx.Exec(`
UPDATE users SET last_login_at=?, terms_version_accepted=?, terms_accepted_at=? WHERE id=?`,
			stamp, termsVersion, stamp, user.ID); err != nil {
			return User{}, "", time.Time{}, err
		}
	}
	raw, hash, err := newToken()
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	expires := now.UTC().Add(sessionTTL)
	if _, err := tx.Exec(`
INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		hash, user.ID, stamp, expires.Format(time.RFC3339)); err != nil {
		return User{}, "", time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, "", time.Time{}, err
	}
	return user, raw, expires, nil
}

func (s *Store) UserByToken(token string, now time.Time) (User, error) {
	hash := hashToken(strings.TrimSpace(token))
	var user User
	var disabled int
	var expires string
	err := s.db.QueryRow(`
SELECT u.id, u.email, u.terms_version_accepted, u.disabled, s.expires_at
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.token_hash=?`, hash).Scan(&user.ID, &user.Email, &user.TermsVersionAccepted, &disabled, &expires)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	exp, _ := time.Parse(time.RFC3339, expires)
	if now.After(exp) || disabled != 0 {
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, hash)
		return User{}, ErrNotFound
	}
	user.Disabled = false
	return user, nil
}

func (s *Store) Logout(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, hashToken(strings.TrimSpace(token)))
	return err
}

func (s *Store) AcceptTerms(userID, version string, now time.Time) error {
	res, err := s.db.Exec(`
UPDATE users SET terms_version_accepted=?, terms_accepted_at=? WHERE id=? AND disabled=0`,
		version, now.UTC().Format(time.RFC3339), userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteAccount(userID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var email string
	if err := tx.QueryRow(`SELECT email FROM users WHERE id=?`, userID).Scan(&email); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.Exec(`UPDATE reports SET device_id='deleted', user_id='' WHERE user_id=?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM email_codes WHERE email=?`, email); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM users WHERE id=?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetDisabled(email string, disabled bool) error {
	flag := 0
	if disabled {
		flag = 1
	}
	res, err := s.db.Exec(`UPDATE users SET disabled=? WHERE email=?`, flag, normalizeEmail(email))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validEmail(email string) bool {
	at := strings.Index(email, "@")
	if at <= 0 || at != strings.LastIndex(email, "@") || at > 64 {
		return false
	}
	domain := email[at+1:]
	if len(email) > 254 || len(domain) < 3 || !strings.Contains(domain, ".") {
		return false
	}
	for _, r := range email {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("._%+-", r):
		default:
			if r == '@' {
				continue
			}
			return false
		}
	}
	return true
}

func hashSecret(salt, value string) string {
	sum := sha256.Sum256([]byte(salt + ":" + value))
	return hex.EncodeToString(sum[:])
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, string, error) {
	raw, err := randomHex(32)
	if err != nil {
		return "", "", err
	}
	return raw, hashToken(raw), nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomDigits(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = '0' + buf[i]%10
	}
	return string(buf), nil
}
