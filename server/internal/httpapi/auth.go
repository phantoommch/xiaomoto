package httpapi

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"motocam/server/internal/legal"
	"motocam/server/internal/mail"
	"motocam/server/internal/store"
)

const authRequiredBuild = 30

type mailer interface {
	Enabled() bool
	Send(to, subject, body string) error
}

type authSettings struct {
	dev          bool
	reviewEmail  string
	reviewCode   string
	termsPath    string
	termsVersion string
	mail         mail.Config
}

var devCodes sync.Map

func (s *Server) currentTerms() legal.Document {
	return legal.Load(s.auth.termsPath, s.auth.termsVersion)
}

func (s *Server) requestCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体无效")
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if s.auth.reviewEmail != "" && email == s.auth.reviewEmail {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	issued, err := s.db.IssueCode(email, clientIP(r), time.Now())
	if err != nil {
		writeStored(w, err)
		return
	}
	if s.auth.mail.Enabled() {
		subject := "小moto安全驾驶指南 激活码"
		if err := s.auth.mail.Send(email, subject, mail.ActivationBody(issued.Code)); err != nil {
			log.Printf("发送激活码失败 email=%s err=%v", email, err)
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"ok": false, "error": "邮件发送失败", "smtpConfigured": true,
			})
			return
		}
		log.Printf("激活码邮件已发送 to=%s", mail.MaskEmail(email))
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if s.auth.dev {
		log.Printf("开发模式激活码 email=%s code=%s", email, issued.Code)
		devCodes.Store(email, issued.Code)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dev": true})
		return
	}
	log.Printf("邮件服务未配置，激活码未发送 email=%s", email)
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"ok": false, "error": "邮件服务未配置", "smtpConfigured": false,
	})
}

func (s *Server) verifyCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email        string `json:"email"`
		Code         string `json:"code"`
		TermsVersion string `json:"termsVersion"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体无效")
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	terms := s.currentTerms()
	if strings.TrimSpace(body.TermsVersion) != terms.Version {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "请先同意当前版本的用户协议", "termsVersion": terms.Version,
		})
		return
	}
	now := time.Now()
	review := s.auth.reviewEmail != "" && email == s.auth.reviewEmail && subtleCode(body.Code, s.auth.reviewCode)
	if !review {
		if err := s.db.CheckCode(email, body.Code, now); err != nil {
			writeStored(w, err)
			return
		}
	}
	user, token, expires, err := s.db.Login(email, terms.Version, now)
	if err != nil {
		writeStored(w, err)
		return
	}
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

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	terms := s.currentTerms()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":            user.ID,
		"email":         user.Email,
		"termsVersion":  user.TermsVersionAccepted,
		"termsCurrent":  terms.Version,
		"termsAccepted": user.TermsVersionAccepted == terms.Version,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "未登录")
		return
	}
	if err := s.db.Logout(token); err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) deleteMe(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteAccount(user.ID); err != nil {
		writeStored(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) terms(w http.ResponseWriter, r *http.Request) {
	doc := s.currentTerms()
	writeJSON(w, http.StatusOK, map[string]string{
		"version":   doc.Version,
		"title":     doc.Title,
		"markdown":  doc.Markdown,
		"updatedAt": doc.UpdatedAt,
	})
}

func (s *Server) acceptTerms(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体无效")
		return
	}
	terms := s.currentTerms()
	if strings.TrimSpace(body.Version) != terms.Version {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "请同意当前版本的用户协议", "termsVersion": terms.Version,
		})
		return
	}
	if err := s.db.AcceptTerms(user.ID, terms.Version, time.Now()); err != nil {
		writeStored(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "termsVersion": terms.Version})
}

func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	token := bearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "未登录")
		return store.User{}, false
	}
	user, err := s.db.UserByToken(token, time.Now())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "未登录")
		return store.User{}, false
	}
	return user, true
}

// authorize allows missing tokens from builds older than 30 when legacy is true.
// A present but invalid token is always rejected.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request, legacy bool) (store.User, bool) {
	token := bearerToken(r)
	if token == "" {
		if legacy && appBuild(r) < authRequiredBuild {
			return store.User{}, true
		}
		writeError(w, http.StatusUnauthorized, "未登录")
		return store.User{}, false
	}
	user, err := s.db.UserByToken(token, time.Now())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "未登录")
		return store.User{}, false
	}
	return user, true
}

func appBuild(r *http.Request) int {
	n, err := strconv.Atoi(strings.TrimSpace(r.Header.Get("X-App-Build")))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) < 8 || !strings.EqualFold(header[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(header[7:])
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		part, _, _ := strings.Cut(forwarded, ",")
		return strings.TrimSpace(part)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func subtleCode(got, want string) bool {
	got = strings.TrimSpace(got)
	if len(got) != len(want) || want == "" {
		return false
	}
	diff := 0
	for i := 0; i < len(got); i++ {
		diff |= int(got[i] ^ want[i])
	}
	return diff == 0
}
