package store

import (
	"database/sql"
	"strings"
	"time"
)

// Download counting and the owner's stats page.
//
// download_events holds one row per counted event. Raw IPs are never stored:
// ip_hash is a salted SHA-256 (salt kept in schema_meta), used only for rough
// unique counts and to drop quick repeats from the same network.

const (
	DownloadPageView   = "page_view"
	DownloadAndroidAPK = "android_apk"
	DownloadTestFlight = "ios_testflight_click"

	// UAKindApp is the in-app updater (User-Agent XiaoMoto/... or X-App-Build).
	UAKindApp = "app"
	UAKindBot = "bot"

	statsSaltKey  = "download_stats_salt"
	statsSinceKey = "download_stats_since"

	// downloadRepeatWindow drops the same event from the same network and
	// client type within this window (browser + system downloader handoff,
	// double taps, reloads).
	downloadRepeatWindow = 2 * time.Minute

	adminFailPerIP15m   = 5
	adminFailGlobalHour = 30
)

// ErrAdminLocked means too many wrong admin passwords recently.
var ErrAdminLocked = InputError{Message: "试错次数太多，请 15 分钟后再试"}

func (s *Store) migrateStats() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS download_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,
  created_at TEXT NOT NULL,
  ip_hash TEXT NOT NULL DEFAULT '',
  ua_kind TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_download_events_kind_time ON download_events(kind, created_at);
CREATE INDEX IF NOT EXISTS idx_download_events_ip ON download_events(ip_hash, kind, created_at);
CREATE TABLE IF NOT EXISTS admin_sessions (
  token_hash TEXT PRIMARY KEY,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS schema_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`)
	if err != nil {
		return err
	}
	salt, err := randomHex(32)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO schema_meta (key, value) VALUES (?, ?)`, statsSaltKey, salt); err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR IGNORE INTO schema_meta (key, value) VALUES (?, ?)`, statsSinceKey, stamp(time.Now()))
	return err
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func (s *Store) meta(key string) string {
	var v string
	_ = s.db.QueryRow(`SELECT value FROM schema_meta WHERE key=?`, key).Scan(&v)
	return v
}

func (s *Store) ipHash(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	return hashSecret(s.meta(statsSaltKey), ip)[:32]
}

// RecordDownload stores one event unless the same kind, client type and
// network was already counted within downloadRepeatWindow. It reports whether
// a row was added.
func (s *Store) RecordDownload(kind, uaKind, ip string, now time.Time) (bool, error) {
	h := s.ipHash(ip)
	if h != "" {
		var one int
		err := s.db.QueryRow(`SELECT 1 FROM download_events WHERE ip_hash=? AND kind=? AND ua_kind=? AND created_at>=? LIMIT 1`,
			h, kind, uaKind, stamp(now.Add(-downloadRepeatWindow))).Scan(&one)
		if err == nil {
			return false, nil
		}
		if err != sql.ErrNoRows {
			return false, err
		}
	}
	_, err := s.db.Exec(`INSERT INTO download_events (kind, created_at, ip_hash, ua_kind) VALUES (?, ?, ?, ?)`,
		kind, stamp(now), h, uaKind)
	return err == nil, err
}

// --- admin sessions and login limits ---

func (s *Store) CreateAdminSession(now time.Time, ttl time.Duration) (string, error) {
	raw, hashed, err := newToken()
	if err != nil {
		return "", err
	}
	_, _ = s.db.Exec(`DELETE FROM admin_sessions WHERE expires_at<?`, stamp(now))
	if _, err := s.db.Exec(`INSERT INTO admin_sessions (token_hash, created_at, expires_at) VALUES (?, ?, ?)`,
		hashed, stamp(now), stamp(now.Add(ttl))); err != nil {
		return "", err
	}
	return raw, nil
}

func (s *Store) AdminSessionValid(token string, now time.Time) bool {
	if token == "" || len(token) > 128 {
		return false
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM admin_sessions WHERE token_hash=? AND expires_at>?`, hashToken(token), stamp(now)).Scan(&n)
	return err == nil && n > 0
}

func (s *Store) DeleteAdminSession(token string) {
	if token != "" {
		_, _ = s.db.Exec(`DELETE FROM admin_sessions WHERE token_hash=?`, hashToken(token))
	}
}

// AdminLoginAllowed fails after adminFailPerIP15m wrong passwords from one IP
// in 15 minutes, or adminFailGlobalHour from anywhere in an hour.
func (s *Store) AdminLoginAllowed(ip string, now time.Time) error {
	var perIP, global int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM auth_limits WHERE scope='admin_fail_ip' AND key=? AND created_at>=?`,
		ip, stamp(now.Add(-15*time.Minute))).Scan(&perIP); err != nil {
		return err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM auth_limits WHERE scope='admin_fail_ip' AND created_at>=?`,
		stamp(now.Add(-time.Hour))).Scan(&global); err != nil {
		return err
	}
	if perIP >= adminFailPerIP15m || global >= adminFailGlobalHour {
		return ErrAdminLocked
	}
	return nil
}

func (s *Store) AdminLoginFailed(ip string, now time.Time) {
	_, _ = s.db.Exec(`INSERT INTO auth_limits (scope, key, created_at) VALUES ('admin_fail_ip', ?, ?)`, ip, stamp(now))
}

// --- numbers for the stats page ---

type Window struct {
	Today  int
	Week   int // the last 7 calendar days including today
	Total  int
	Unique int // distinct networks, all time
}

type DayCount struct {
	Day   time.Time // local midnight
	Count int
}

type UserRow struct {
	Email       string
	CreatedAt   time.Time
	LastLogin   time.Time
	HasPassword bool
	Disabled    bool
	Review      bool
	Test        bool
}

type ReportCounts struct {
	Total    int // rider uploads, imports excluded
	Merged   int
	Pending  int
	Rejected int
	Archived int
	Today    int
	Week     int
	Imported int
}

type Stats struct {
	Users        int
	TestUsers    int
	UsersToday   int
	UsersWeek    int
	WithPassword int
	Active7d     int
	Disabled     int
	Daily        []DayCount
	Recent       []UserRow

	AndroidWeb Window
	AndroidApp Window
	PageViews  Window
	TestFlight Window
	Since      time.Time

	Reports      ReportCounts
	Points       int
	PointsUser   int
	PointsImport int
}

type StatsOptions struct {
	Loc         *time.Location
	Days        int
	RecentLimit int
	ReviewEmail string
	TestEmails  []string
}

func dayStart(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func (s *Store) Stats(now time.Time, opt StatsOptions) (Stats, error) {
	loc := opt.Loc
	if loc == nil {
		loc = time.UTC
	}
	if opt.Days <= 0 {
		opt.Days = 30
	}
	if opt.RecentLimit <= 0 {
		opt.RecentLimit = 20
	}
	review := normalizeEmail(opt.ReviewEmail)
	tests := map[string]bool{}
	for _, e := range opt.TestEmails {
		if e = normalizeEmail(e); e != "" {
			tests[e] = true
		}
	}
	if review != "" {
		tests[review] = true
	}
	today := dayStart(now, loc)
	week := today.AddDate(0, 0, -6)
	first := today.AddDate(0, 0, -(opt.Days - 1))
	var st Stats
	st.Daily = make([]DayCount, opt.Days)
	for i := range st.Daily {
		st.Daily[i].Day = first.AddDate(0, 0, i)
	}

	rows, err := s.db.Query(`SELECT email, created_at, last_login_at, password_hash<>'', disabled FROM users ORDER BY created_at DESC`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var u UserRow
		var created, last string
		var disabled int
		if err := rows.Scan(&u.Email, &created, &last, &u.HasPassword, &disabled); err != nil {
			rows.Close()
			return st, err
		}
		u.CreatedAt, _ = time.Parse(time.RFC3339, created)
		u.LastLogin, _ = time.Parse(time.RFC3339, last)
		u.Disabled = disabled != 0
		e := normalizeEmail(u.Email)
		u.Review = review != "" && e == review
		u.Test = tests[e]
		st.Users++
		if u.Test {
			st.TestUsers++
		}
		if u.Disabled {
			st.Disabled++
		}
		if u.HasPassword {
			st.WithPassword++
		}
		if !u.LastLogin.IsZero() && now.Sub(u.LastLogin) <= 7*24*time.Hour {
			st.Active7d++
		}
		if !u.CreatedAt.Before(today) {
			st.UsersToday++
		}
		if !u.CreatedAt.Before(week) {
			st.UsersWeek++
		}
		if !u.CreatedAt.Before(first) {
			d := dayStart(u.CreatedAt, loc)
			for i := range st.Daily {
				if st.Daily[i].Day.Equal(d) {
					st.Daily[i].Count++
					break
				}
			}
		}
		if len(st.Recent) < opt.RecentLimit {
			st.Recent = append(st.Recent, u)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return st, err
	}

	win := func(kind, where string) (Window, error) {
		var w Window
		err := s.db.QueryRow(`SELECT COUNT(*),
  COALESCE(SUM(CASE WHEN created_at>=? THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN created_at>=? THEN 1 ELSE 0 END),0),
  COUNT(DISTINCT NULLIF(ip_hash,''))
FROM download_events WHERE kind=? AND ua_kind<>'bot' AND `+where,
			stamp(today), stamp(week), kind).Scan(&w.Total, &w.Today, &w.Week, &w.Unique)
		return w, err
	}
	if st.AndroidWeb, err = win(DownloadAndroidAPK, `ua_kind<>'app'`); err != nil {
		return st, err
	}
	if st.AndroidApp, err = win(DownloadAndroidAPK, `ua_kind='app'`); err != nil {
		return st, err
	}
	if st.PageViews, err = win(DownloadPageView, `1`); err != nil {
		return st, err
	}
	if st.TestFlight, err = win(DownloadTestFlight, `1`); err != nil {
		return st, err
	}
	st.Since, _ = time.Parse(time.RFC3339, s.meta(statsSinceKey))

	importDev := s.meta(sourceBackfillKey)
	if importDev == "" {
		importDev = ImportDeviceID
	}
	r := &st.Reports
	err = s.db.QueryRow(`SELECT
  COALESCE(SUM(CASE WHEN device_id IN (?, ?) THEN 0 ELSE 1 END),0),
  COALESCE(SUM(CASE WHEN device_id IN (?, ?) THEN 0 WHEN status='merged' THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN device_id IN (?, ?) THEN 0 WHEN status='pending' THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN device_id IN (?, ?) THEN 0 WHEN status='rejected' THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN device_id IN (?, ?) THEN 0 WHEN status='archived' THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN device_id IN (?, ?) THEN 0 WHEN created_at>=? THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN device_id IN (?, ?) THEN 0 WHEN created_at>=? THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN device_id IN (?, ?) THEN 1 ELSE 0 END),0)
FROM reports`,
		ImportDeviceID, importDev, ImportDeviceID, importDev, ImportDeviceID, importDev,
		ImportDeviceID, importDev, ImportDeviceID, importDev,
		ImportDeviceID, importDev, stamp(today),
		ImportDeviceID, importDev, stamp(week),
		ImportDeviceID, importDev,
	).Scan(&r.Total, &r.Merged, &r.Pending, &r.Rejected, &r.Archived, &r.Today, &r.Week, &r.Imported)
	if err != nil {
		return st, err
	}
	err = s.db.QueryRow(`SELECT COUNT(*),
  COALESCE(SUM(CASE WHEN source=? THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN source=? THEN 1 ELSE 0 END),0)
FROM points`, SourceUserUpload, SourceImport).Scan(&st.Points, &st.PointsUser, &st.PointsImport)
	return st, err
}
