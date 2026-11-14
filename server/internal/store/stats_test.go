package store

import (
	"strings"
	"testing"
	"time"
)

func TestRecordDownloadDedupesAndHashesIP(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 3, PromoteRadiusM: 200, RatePerHour: 20, RatePerDay: 50})
	now := time.Now()
	add := func(kind, ua, ip string, at time.Time) bool {
		t.Helper()
		ok, err := st.RecordDownload(kind, ua, ip, at)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !add(DownloadAndroidAPK, "android", "203.0.113.7", now) {
		t.Fatal("first event not recorded")
	}
	if add(DownloadAndroidAPK, "android", "203.0.113.7", now.Add(30*time.Second)) {
		t.Fatal("repeat within window recorded")
	}
	if !add(DownloadAndroidAPK, UAKindApp, "203.0.113.7", now.Add(40*time.Second)) {
		t.Fatal("app update from same network should be its own event")
	}
	if !add(DownloadAndroidAPK, "android", "203.0.113.7", now.Add(3*time.Minute)) {
		t.Fatal("after window should count again")
	}
	if !add(DownloadPageView, "ios", "198.51.100.1", now) {
		t.Fatal("page view")
	}
	var raw int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM download_events WHERE ip_hash LIKE '%203.0.113%' OR ip_hash=''`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != 0 {
		t.Fatal("raw ip stored")
	}
	s, err := st.Stats(now.Add(4*time.Minute), StatsOptions{Loc: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	if s.AndroidWeb.Total != 2 || s.AndroidWeb.Unique != 1 || s.AndroidApp.Total != 1 || s.PageViews.Total != 1 {
		t.Fatalf("windows web=%+v app=%+v page=%+v", s.AndroidWeb, s.AndroidApp, s.PageViews)
	}
	if s.Since.IsZero() {
		t.Fatal("stats start not recorded")
	}
}

func TestStatsCountsUsersAndReports(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 3, PromoteRadiusM: 200, RatePerHour: 20, RatePerDay: 50})
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, loc)
	login := func(email string, at time.Time) User {
		t.Helper()
		u, _, _, err := st.Login(email, "v1", at)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	login("review@xiaomoto.app", now.AddDate(0, 0, -20))
	login("old@example.com", now.AddDate(0, 0, -40))
	login("week@example.com", now.AddDate(0, 0, -3))
	// 00:30 local today is still "today" although it is the previous UTC day.
	early := login("early@example.com", time.Date(2026, 10, 4, 0, 30, 0, 0, loc))
	login("test-account-1@example.com", now.Add(-time.Hour))
	if err := st.SetPassword(early.ID, "a-good-password", "", false, now); err != nil {
		t.Fatal(err)
	}
	base := ReportInput{Name: "赤石收费站", Type: "banned_toll", RoadCode: "G25", Latitude: 28.2, Longitude: 119.9}
	mustSubmit(t, st, withDevice(base, 1))
	mustSubmit(t, st, withDevice(base, 2))
	imp := base
	imp.DeviceID = ImportDeviceID
	imp.Latitude = 30
	mustSubmit(t, st, imp)

	s, err := st.Stats(now, StatsOptions{Loc: loc, Days: 30, ReviewEmail: "review@xiaomoto.app", TestEmails: []string{"test-account-1@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Users != 5 || s.TestUsers != 2 || s.UsersToday != 2 || s.UsersWeek != 3 || s.WithPassword != 1 {
		t.Fatalf("users %+v", s)
	}
	// Login sets last_login_at at creation; old@ (40 days) and review@ (20 days) are not recent.
	if s.Active7d != 3 {
		t.Fatalf("active %d", s.Active7d)
	}
	if len(s.Daily) != 30 || s.Daily[29].Count != 2 || s.Daily[26].Count != 1 || s.Daily[9].Count != 1 {
		t.Fatalf("daily %+v", s.Daily)
	}
	if !s.Recent[0].Test || s.Recent[0].Review || !strings.HasPrefix(s.Recent[len(s.Recent)-1].Email, "old@") {
		t.Fatalf("recent order/tags %+v", s.Recent)
	}
	for _, u := range s.Recent {
		if u.Email == "review@xiaomoto.app" && (!u.Review || !u.Test) {
			t.Fatal("review account not marked")
		}
	}
	if s.Reports.Total != 2 || s.Reports.Pending != 2 || s.Reports.Imported != 1 {
		t.Fatalf("reports %+v", s.Reports)
	}
}

func TestAdminSessionsAndLimits(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 20, RatePerDay: 50})
	now := time.Now()
	tok, err := st.CreateAdminSession(now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !st.AdminSessionValid(tok, now) || st.AdminSessionValid(tok, now.Add(2*time.Hour)) || st.AdminSessionValid("nope", now) {
		t.Fatal("session validity")
	}
	st.DeleteAdminSession(tok)
	if st.AdminSessionValid(tok, now) {
		t.Fatal("deleted session still valid")
	}
	for i := 0; i < adminFailPerIP15m; i++ {
		if err := st.AdminLoginAllowed("1.2.3.4", now); err != nil {
			t.Fatalf("locked too early at %d", i)
		}
		st.AdminLoginFailed("1.2.3.4", now)
	}
	if st.AdminLoginAllowed("1.2.3.4", now) == nil {
		t.Fatal("not locked")
	}
	if st.AdminLoginAllowed("5.6.7.8", now) != nil {
		t.Fatal("other ip locked")
	}
	if st.AdminLoginAllowed("1.2.3.4", now.Add(16*time.Minute)) != nil {
		t.Fatal("lock did not expire")
	}
}
