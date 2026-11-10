package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"motocam/server/internal/store"
)

func (s *Server) routePassword(mux *http.ServeMux) {
	for _, prefix := range []string{"/v1/auth", "/v1/hajimi/auth"} {
		mux.HandleFunc("POST "+prefix+"/login", s.passwordLogin)
		mux.HandleFunc("POST "+prefix+"/register", s.passwordRegister)
		mux.HandleFunc("POST "+prefix+"/email/code", s.requestCode)
		mux.HandleFunc("POST "+prefix+"/email/verify", s.passwordVerify)
		mux.HandleFunc("POST "+prefix+"/password/reset", s.passwordReset)
	}
	mux.HandleFunc("POST /v1/hajimi/auth/logout", s.logout)
}

func (s *Server) passwordLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeStrict(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不对")
		return
	}
	user, token, expires, err := s.db.LoginWithPassword(body.Email, body.Password, time.Now())
	if err != nil {
		if errors.Is(err, store.ErrDisabled) {
			writeStored(w, err)
			return
		}
		if errors.Is(err, store.ErrPassword) {
			writeError(w, http.StatusBadRequest, "邮箱或密码不对")
			return
		}
		writeStored(w, err)
		return
	}
	writeSession(w, token, expires, user)
}

func (s *Server) passwordRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email        string `json:"email"`
		Code         string `json:"code"`
		Password     string `json:"password"`
		TermsVersion string `json:"termsVersion"`
	}
	if err := decodeStrict(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不对")
		return
	}
	if !acceptablePassword(body.Password) {
		writePasswordShape(w, body.Password)
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if !strings.Contains(email, "@") || strings.Count(email, "@") != 1 {
		writeError(w, http.StatusBadRequest, "邮箱格式不正确")
		return
	}
	terms := s.currentTerms()
	if strings.TrimSpace(body.TermsVersion) != terms.Version {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "请先同意当前版本的用户协议", "termsVersion": terms.Version,
		})
		return
	}
	now := time.Now()
	if s.auth.reviewEmail != "" && email == s.auth.reviewEmail {
		writeError(w, http.StatusBadRequest, "邮箱或密码不对")
		return
	}
	if err := s.db.CheckCode(email, body.Code, now); err != nil {
		writeStored(w, err)
		return
	}
	user, token, expires, err := s.db.Login(email, terms.Version, now)
	if err != nil {
		writeStored(w, err)
		return
	}
	if err := s.db.SetPassword(user.ID, body.Password); err != nil {
		writeStored(w, err)
		return
	}
	writeSession(w, token, expires, user)
}

func (s *Server) passwordVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email        string `json:"email"`
		Code         string `json:"code"`
		TermsVersion string `json:"termsVersion"`
	}
	if err := decodeStrict(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不对")
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	now := time.Now()
	terms := s.currentTerms()
	review := s.auth.reviewEmail != "" && email == s.auth.reviewEmail && subtleCode(body.Code, s.auth.reviewCode)
	if !review {
		if err := s.db.CheckCode(email, body.Code, now); err != nil {
			writeStored(w, err)
			return
		}
	}
	if strings.TrimSpace(body.TermsVersion) != terms.Version {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "请先同意当前版本的用户协议", "termsVersion": terms.Version,
		})
		return
	}
	user, token, expires, err := s.db.Login(email, terms.Version, now)
	if err != nil {
		writeStored(w, err)
		return
	}
	writeSession(w, token, expires, user)
}

func (s *Server) passwordReset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeStrict(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不对")
		return
	}
	if !acceptablePassword(body.Password) {
		writePasswordShape(w, body.Password)
		return
	}
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if err := s.db.SetPassword(user.ID, body.Password); err != nil {
		writeStored(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeSession(w http.ResponseWriter, token string, expires time.Time, user store.User) {
	writeJSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"expiresAt": expires.UTC().Format(time.RFC3339),
		"user": map[string]any{
			"id":           user.ID,
			"email":        user.Email,
			"termsVersion": user.TermsVersionAccepted,
		},
	})
}

func acceptablePassword(password string) bool {
	n := utf8.RuneCountInString(password)
	return n >= 8 && n <= 128
}

func writePasswordShape(w http.ResponseWriter, password string) {
	if utf8.RuneCountInString(password) < 8 {
		writeError(w, http.StatusBadRequest, "密码至少 8 位")
		return
	}
	writeError(w, http.StatusBadRequest, "请求格式不对")
}

func decodeStrict(r *http.Request, dest any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if dec.More() {
		_, _ = io.Copy(io.Discard, dec.Buffered())
		return errors.New("trailing json")
	}
	return nil
}
