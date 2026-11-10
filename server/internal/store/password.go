package store

import (
	"database/sql"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	passwordMinRunes = 8
	passwordMaxBytes = 72 // bcrypt ignores anything longer
	passwordCost     = bcrypt.DefaultCost

	// Wrong-password limits. Only failures are counted.
	pwFailPerEmailHour = 10
	pwFailPerIPHour    = 50
	pwTryPerIPMinute   = 10
)

// ErrPasswordLocked means too many wrong passwords for this email or IP.
var ErrPasswordLocked = InputError{Message: "密码错误次数太多，请 1 小时后再试，或用验证码登录"}

// ErrBadPassword is returned for an unknown email, an account without a
// password, or a wrong password, so the reply does not reveal which.
var ErrBadPassword = InputError{Message: "邮箱或密码不正确。还没设过密码的，请用验证码登录"}

// dummyHash keeps the timing of unknown emails close to real ones.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("xiaomoto-dummy-password"), passwordCost)

// ValidatePassword checks the rules for a password a user picks.
func ValidatePassword(password, email string) error {
	if utf8.RuneCountInString(password) < passwordMinRunes {
		return InputError{Message: "密码至少 8 位"}
	}
	if len(password) > passwordMaxBytes {
		return InputError{Message: "密码太长了，请少于 72 个字符"}
	}
	if strings.TrimSpace(password) != password {
		return InputError{Message: "密码前后不能有空格"}
	}
	if strings.EqualFold(password, normalizeEmail(email)) {
		return InputError{Message: "密码不能和邮箱一样"}
	}
	return nil
}

func hashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

func passwordLimited(tx *sql.Tx, email, ip string, now time.Time) error {
	if err := countLimit(tx, "pwfail_email", email, now.Add(-time.Hour), pwFailPerEmailHour); err != nil {
		return ErrPasswordLocked
	}
	if ip != "" {
		if err := countLimit(tx, "pwfail_ip", ip, now.Add(-time.Hour), pwFailPerIPHour); err != nil {
			return ErrPasswordLocked
		}
		if err := countLimit(tx, "pwfail_ip", ip, now.Add(-time.Minute), pwTryPerIPMinute); err != nil {
			return ErrPasswordLocked
		}
	}
	return nil
}

func (s *Store) recordPasswordFailure(email, ip string, now time.Time) {
	stamp := now.UTC().Format(time.RFC3339)
	_, _ = s.db.Exec(`INSERT INTO auth_limits (scope, key, created_at) VALUES ('pwfail_email', ?, ?)`, email, stamp)
	if ip != "" {
		_, _ = s.db.Exec(`INSERT INTO auth_limits (scope, key, created_at) VALUES ('pwfail_ip', ?, ?)`, ip, stamp)
	}
}

func (s *Store) checkPasswordLimits(email, ip string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return passwordLimited(tx, email, ip, now)
}

// CheckPassword verifies an email and password. It counts failures per email
// and per IP and refuses further tries once a limit is reached.
func (s *Store) CheckPassword(email, password, ip string, now time.Time) error {
	email = normalizeEmail(email)
	if !validEmail(email) {
		return InputError{Message: "邮箱格式不正确"}
	}
	if err := s.checkPasswordLimits(email, ip, now); err != nil {
		return err
	}
	var hash string
	var disabled int
	err := s.db.QueryRow(`SELECT password_hash, disabled FROM users WHERE email=?`, email).Scan(&hash, &disabled)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if hash == "" || len(password) > passwordMaxBytes {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		s.recordPasswordFailure(email, ip, now)
		return ErrBadPassword
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		s.recordPasswordFailure(email, ip, now)
		return ErrBadPassword
	}
	if disabled != 0 {
		return ErrDisabled
	}
	return nil
}

// ChangePassword sets a new password for a signed-in user. When the account
// already has a password, current must match it. Other sessions are signed out;
// the session with keepToken stays.
func (s *Store) ChangePassword(userID, current, next, keepToken, ip string, now time.Time) error {
	var email, hash string
	err := s.db.QueryRow(`SELECT email, password_hash FROM users WHERE id=? AND disabled=0`, userID).Scan(&email, &hash)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := ValidatePassword(next, email); err != nil {
		return err
	}
	if hash != "" {
		if err := s.checkPasswordLimits(email, ip, now); err != nil {
			return err
		}
		if len(current) > passwordMaxBytes || bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
			s.recordPasswordFailure(email, ip, now)
			return InputError{Message: "当前密码不正确"}
		}
	}
	return s.SetPassword(userID, next, keepToken, hash != "", now)
}

// SetPassword stores a new password hash for the user. With signOutOthers,
// every other session is deleted; the session with keepToken stays. Callers check the rules
// with ValidatePassword and prove ownership (current password or email code).
func (s *Store) SetPassword(userID, next, keepToken string, signOutOthers bool, now time.Time) error {
	newHash, err := hashPassword(next)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE users SET password_hash=?, password_set_at=? WHERE id=? AND disabled=0`,
		newHash, now.UTC().Format(time.RFC3339), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if signOutOthers {
		if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=? AND token_hash<>?`,
			userID, hashToken(strings.TrimSpace(keepToken))); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// HasPassword reports whether the user has set a password.
func (s *Store) HasPassword(userID string) (bool, error) {
	var hash string
	err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id=?`, userID).Scan(&hash)
	if err == sql.ErrNoRows {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return hash != "", nil
}

// PasswordAllowed reports ErrPasswordLocked once an email or IP has too many
// recent wrong passwords. PasswordFailed records one more failure. They are
// used for logins checked outside CheckPassword (the App Review account).
func (s *Store) PasswordAllowed(email, ip string, now time.Time) error {
	return s.checkPasswordLimits(normalizeEmail(email), ip, now)
}

func (s *Store) PasswordFailed(email, ip string, now time.Time) {
	s.recordPasswordFailure(normalizeEmail(email), ip, now)
}
