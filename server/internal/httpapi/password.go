package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"motocam/server/internal/store"
)

// Password login (build 34+). The code endpoints in auth.go stay unchanged so
// builds 30–33 keep working.

func (s *Server) isReview(email string) bool {
	return s.auth.reviewEmail != "" && email == s.auth.reviewEmail
}

// reviewPassword is REVIEW_PASSWORD, or the fixed review code when unset.
func (s *Server) reviewPassword() string {
	if s.auth.reviewPassword != "" {
		return s.auth.reviewPassword
	}
	return s.auth.reviewCode
}

func (s *Server) termsOK(w http.ResponseWriter, version string) (string, bool) {
	terms := s.currentTerms()
	if strings.TrimSpace(version) != terms.Version {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "请先同意当前版本的用户协议", "termsVersion": terms.Version,
		})
		return "", false
	}
	return terms.Version, true
}

func (s *Server) writeSession(w http.ResponseWriter, user store.User, token string, expires time.Time) {
	writeJSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"expiresAt": expires.UTC().Format(time.RFC3339),
		"user": map[string]any{
			"id":           user.ID,
			"email":        user.Email,
			"termsVersion": user.TermsVersionAccepted,
			"hasPassword":  true,
		},
	})
}

func (s *Server) loginPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email        string `json:"email"`
		Password     string `json:"password"`
		TermsVersion string `json:"termsVersion"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体无效")
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	version, ok := s.termsOK(w, body.TermsVersion)
	if !ok {
		return
	}
	now := time.Now()
	ip := clientIP(r)
	if s.isReview(email) {
		if err := s.db.PasswordAllowed(email, ip, now); err != nil {
			writeAuthErr(w, err)
			return
		}
		if !subtleCode(body.Password, s.reviewPassword()) {
			s.db.PasswordFailed(email, ip, now)
			writeStored(w, store.ErrBadPassword)
			return
		}
	} else if err := s.db.CheckPassword(email, body.Password, ip, now); err != nil {
		writeAuthErr(w, err)
		return
	}
	user, token, expires, err := s.db.Login(email, version, now)
	if err != nil {
		writeStored(w, err)
		return
	}
	s.writeSession(w, user, token, expires)
}

// resetPassword sets a new password with an emailed code (forgot password, or
// a first password for a new email) and signs the user in. Other sessions are
// signed out.
func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email        string `json:"email"`
		Code         string `json:"code"`
		Password     string `json:"password"`
		TermsVersion string `json:"termsVersion"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体无效")
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	version, ok := s.termsOK(w, body.TermsVersion)
	if !ok {
		return
	}
	if s.isReview(email) {
		writeError(w, http.StatusForbidden, "审核账号不能改密码")
		return
	}
	// Check the password rules before the code is used up.
	if err := store.ValidatePassword(body.Password, email); err != nil {
		writeStored(w, err)
		return
	}
	now := time.Now()
	if err := s.db.CheckCode(email, body.Code, now); err != nil {
		writeStored(w, err)
		return
	}
	user, token, expires, err := s.db.Login(email, version, now)
	if err != nil {
		writeStored(w, err)
		return
	}
	if err := s.db.SetPassword(user.ID, body.Password, token, true, now); err != nil {
		writeStored(w, err)
		return
	}
	s.writeSession(w, user, token, expires)
}

// setPassword sets or changes the password of the signed-in user. Changing an
// existing password needs the current one and signs out other sessions.
func (s *Server) setPassword(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Password        string `json:"password"`
		CurrentPassword string `json:"currentPassword"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体无效")
		return
	}
	if s.isReview(user.Email) {
		writeError(w, http.StatusForbidden, "审核账号不能改密码")
		return
	}
	if err := s.db.ChangePassword(user.ID, body.CurrentPassword, body.Password, bearerToken(r), clientIP(r), time.Now()); err != nil {
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "hasPassword": true})
}

// writeAuthErr is writeStored with a 429 for too many wrong passwords.
func writeAuthErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrPasswordLocked) {
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusTooManyRequests, store.ErrPasswordLocked.Message)
		return
	}
	writeStored(w, err)
}

func (s *Server) userHasPassword(user store.User) bool {
	if s.isReview(user.Email) {
		return s.reviewPassword() != ""
	}
	has, err := s.db.HasPassword(user.ID)
	return err == nil && has
}
