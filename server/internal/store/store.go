package store

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	// ImportDeviceID is the fixed device id in import_points.py.
	// Reports from this device are imports, not rider uploads.
	ImportDeviceID = "f4e1d000-0000-4000-8000-000000000001"
	// SourceImport is a point loaded by that importer.
	SourceImport = "import"
	// SourceUserUpload is a point that has at least one real device report.
	SourceUserUpload = "user_upload"
	// sourceBackfillKey marks the one-time source correction. The value is the
	// import device id used for that pass, or empty when none was found.
	sourceBackfillKey = "point_source_backfill_v2"
	// importSoleMajorityMin is how many points one device must solely own
	// before a database that never used ImportDeviceID is still corrected.
	// Production's importer is that device; a handful of rider points is not.
	importSoleMajorityMin = 10
)

var (
	ErrInvalid     = errors.New("invalid")
	ErrRateLimited = errors.New("rate limited")
	ErrNotFound    = errors.New("not found")
	ErrConflict    = errors.New("conflict")
)

type InputError struct {
	Message string
}

func (e InputError) Error() string { return e.Message }
func (e InputError) Unwrap() error { return ErrInvalid }

type Limits struct {
	PromoteMin     int
	PromoteRadiusM float64
	RatePerHour    int
	RatePerDay     int
}

type ReportInput struct {
	DeviceID      string
	Name          string
	Type          string
	RoadCode      string
	RoadName      string
	Notes         string
	Latitude      float64
	Longitude     float64
	FineYuan      *int
	PenaltyPoints *int
	Violation     string
	Image         []byte
	OcrLocation   string
	AmapName      string
	UserID        string
	OcrText       string
	ParsedJSON    string
	Confidence    string
	EndLatitude   float64
	EndLongitude  float64
	HasEnd        bool
	Stake         string
	Direction     string
	// ForceReview keeps a low-confidence hit in the queue instead of publishing.
	ForceReview bool
	// Unlocated reports have no map coordinate. The zero value stays located,
	// so older clients that only send latitude and longitude keep working.
	Unlocated    bool
	ReviewResult string
	ReviewReason string
}

type SubmitResult struct {
	ID            string
	Status        string
	PublicPointID string
}

type PublicPoint struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	RoadCode      string   `json:"road_code"`
	RoadName      string   `json:"road_name"`
	Notes         string   `json:"notes"`
	Latitude      float64  `json:"latitude"`
	Longitude     float64  `json:"longitude"`
	Coord         string   `json:"coord"`
	FineYuan      *int     `json:"fine_yuan,omitempty"`
	PenaltyPoints *int     `json:"penalty_points,omitempty"`
	Violation     string   `json:"violation,omitempty"`
	SourceCount   int      `json:"source_count"`
	UpdatedAt     string   `json:"updated_at"`
	EndLatitude   *float64 `json:"end_latitude,omitempty"`
	EndLongitude  *float64 `json:"end_longitude,omitempty"`
	Stake         string   `json:"stake,omitempty"`
	Direction     string   `json:"direction,omitempty"`
	Source        string   `json:"source,omitempty"`
}

type ListResult struct {
	ServerTime string        `json:"server_time"`
	Points     []PublicPoint `json:"points"`
	RemovedIDs []string      `json:"removed_ids"`
}

type AdminReport struct {
	ID            string   `json:"id"`
	DeviceID      string   `json:"device_id"`
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	RoadCode      string   `json:"road_code"`
	RoadName      string   `json:"road_name"`
	Notes         string   `json:"notes"`
	Latitude      float64  `json:"latitude"`
	Longitude     float64  `json:"longitude"`
	FineYuan      *int     `json:"fine_yuan,omitempty"`
	PenaltyPoints *int     `json:"penalty_points,omitempty"`
	Violation     string   `json:"violation"`
	OcrLocation   string   `json:"ocr_location,omitempty"`
	AmapName      string   `json:"amap_name,omitempty"`
	Located       bool     `json:"located"`
	Status        string   `json:"status"`
	PublicPointID string   `json:"public_point_id,omitempty"`
	HasImage      bool     `json:"has_image"`
	CreatedAt     string   `json:"created_at"`
	OcrText       string   `json:"ocr_text,omitempty"`
	ParsedJSON    string   `json:"parsed,omitempty"`
	Confidence    string   `json:"confidence,omitempty"`
	EndLatitude   *float64 `json:"end_latitude,omitempty"`
	EndLongitude  *float64 `json:"end_longitude,omitempty"`
	Stake         string   `json:"stake,omitempty"`
	Direction     string   `json:"direction,omitempty"`
	ReviewResult  string   `json:"review_result"`
	ReviewReason  string   `json:"review_reason"`
}

// AdminPoint is one published point plus the auto-review that created it.
type AdminPoint struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	RoadCode     string  `json:"road_code"`
	RoadName     string  `json:"road_name"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	Stake        string  `json:"stake,omitempty"`
	Direction    string  `json:"direction,omitempty"`
	Source       string  `json:"source"`
	ReviewResult string  `json:"review_result"`
	ReviewReason string  `json:"review_reason"`
	UpdatedAt    string  `json:"updated_at"`
}

type Store struct {
	db       *sql.DB
	imageDir string
	limits   Limits
}

func Open(dbPath, imageDir string, limits Limits) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		return nil, err
	}
	dsn := dbPath + "?_busy_timeout=5000&_txlock=immediate&_foreign_keys=on"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db, imageDir: imageDir, limits: limits}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS reports (
  id TEXT PRIMARY KEY,
  device_id TEXT NOT NULL,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  road_code TEXT NOT NULL DEFAULT '',
  road_name TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  latitude REAL NOT NULL,
  longitude REAL NOT NULL,
  fine_yuan INTEGER,
  penalty_points INTEGER,
  violation TEXT NOT NULL DEFAULT '',
  image_path TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  public_point_id TEXT,
  created_at TEXT NOT NULL,
  ocr_location TEXT NOT NULL DEFAULT '',
  amap_name TEXT NOT NULL DEFAULT '',
  located INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS points (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  road_code TEXT NOT NULL DEFAULT '',
  road_name TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  latitude REAL NOT NULL,
  longitude REAL NOT NULL,
  fine_yuan INTEGER,
  penalty_points INTEGER,
  violation TEXT NOT NULL DEFAULT '',
  source_count INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS point_tombstones (
  id TEXT PRIMARY KEY,
  removed_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS rate_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  device_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_rate_device_time ON rate_events(device_id, created_at);
CREATE INDEX IF NOT EXISTS idx_reports_status ON reports(status);
CREATE INDEX IF NOT EXISTS idx_points_updated ON points(updated_at);
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  last_login_at TEXT NOT NULL,
  terms_version_accepted TEXT NOT NULL DEFAULT '',
  terms_accepted_at TEXT,
  disabled INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS email_codes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL,
  code_hash TEXT NOT NULL,
  salt TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  consumed INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  ip TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_email_codes_email ON email_codes(email, id);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE TABLE IF NOT EXISTS auth_limits (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  scope TEXT NOT NULL,
  key TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_auth_limits_lookup ON auth_limits(scope, key, created_at);
`)
	if err != nil {
		return err
	}
	if err := s.ensureColumn("reports", "ocr_location", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("reports", "amap_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("reports", "located", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if err := s.ensureColumn("reports", "user_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	extra := []struct{ table, name, decl string }{
		{"reports", "ocr_text", "TEXT NOT NULL DEFAULT ''"},
		{"reports", "parsed_json", "TEXT NOT NULL DEFAULT ''"},
		{"reports", "confidence", "TEXT NOT NULL DEFAULT ''"},
		{"reports", "end_latitude", "REAL"},
		{"reports", "end_longitude", "REAL"},
		{"reports", "stake", "TEXT NOT NULL DEFAULT ''"},
		{"reports", "direction", "TEXT NOT NULL DEFAULT ''"},
		{"points", "end_latitude", "REAL"},
		{"points", "end_longitude", "REAL"},
		{"points", "stake", "TEXT NOT NULL DEFAULT ''"},
		{"points", "direction", "TEXT NOT NULL DEFAULT ''"},
		{"reports", "review_result", "TEXT NOT NULL DEFAULT ''"},
		{"reports", "review_reason", "TEXT NOT NULL DEFAULT ''"},
		{"points", "source", "TEXT NOT NULL DEFAULT ''"},
		{"points", "review_result", "TEXT NOT NULL DEFAULT ''"},
		{"points", "review_reason", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, col := range extra {
		if err := s.ensureColumn(col.table, col.name, col.decl); err != nil {
			return err
		}
	}
	return s.backfillPointSource()
}

// backfillPointSource runs once. Points whose reports are all from the import
// device become "import", including rows the old pass had labeled user_upload.
// Any other non-empty source is left as it is. A later startup does not fill
// a source that was cleared.
func (s *Store) backfillPointSource() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
)`); err != nil {
		return err
	}
	var already int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_meta WHERE key=?`, sourceBackfillKey).Scan(&already); err != nil {
		return err
	}
	if already > 0 {
		return nil
	}
	device, ok, err := s.historicalImportDevice()
	if err != nil {
		return err
	}
	if ok {
		if _, err := s.db.Exec(`
UPDATE points SET source=?
WHERE source IN ('', ?)
AND id IN (
  SELECT public_point_id FROM reports
  WHERE public_point_id IS NOT NULL AND public_point_id<>''
  GROUP BY public_point_id
  HAVING SUM(CASE WHEN device_id<>? THEN 1 ELSE 0 END)=0
)`, SourceImport, SourceUserUpload, device); err != nil {
			return err
		}
	}
	other := device
	if !ok {
		other = "\x00"
	}
	if _, err := s.db.Exec(`
UPDATE points SET source=?
WHERE source=''
AND id IN (
  SELECT public_point_id FROM reports
  WHERE public_point_id IS NOT NULL AND public_point_id<>''
    AND device_id<>?
)`, SourceUserUpload, other); err != nil {
		return err
	}
	recorded := ""
	if ok {
		recorded = device
	}
	_, err = s.db.Exec(`INSERT INTO schema_meta (key, value) VALUES (?, ?)`, sourceBackfillKey, recorded)
	return err
}

// historicalImportDevice is ImportDeviceID when that id has reports. Otherwise
// it is the single device that solely authored a strict majority of reported
// points, and at least importSoleMajorityMin of them. That is the device the
// already-deployed importer used.
func (s *Store) historicalImportDevice() (string, bool, error) {
	var known int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM reports WHERE device_id=?`, ImportDeviceID).Scan(&known); err != nil {
		return "", false, err
	}
	if known > 0 {
		return ImportDeviceID, true, nil
	}
	var reported int
	if err := s.db.QueryRow(`
SELECT COUNT(DISTINCT public_point_id) FROM reports
WHERE public_point_id IS NOT NULL AND public_point_id<>''`).Scan(&reported); err != nil {
		return "", false, err
	}
	rows, err := s.db.Query(`
SELECT device_id, COUNT(*) AS n FROM (
  SELECT public_point_id, MIN(device_id) AS device_id
  FROM reports
  WHERE public_point_id IS NOT NULL AND public_point_id<>''
  GROUP BY public_point_id
  HAVING COUNT(DISTINCT device_id)=1
)
GROUP BY device_id
ORDER BY n DESC, device_id ASC`)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	var bestID, secondID string
	var best, second int
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return "", false, err
		}
		if bestID == "" {
			bestID, best = id, n
			continue
		}
		if secondID == "" {
			secondID, second = id, n
		}
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	if bestID == "" || best < importSoleMajorityMin || best*2 <= reported || best == second {
		return "", false, nil
	}
	return bestID, true, nil
}

func (s *Store) isImportDevice(tx *sql.Tx, deviceID string) (bool, error) {
	if deviceID == ImportDeviceID {
		return true, nil
	}
	var recorded string
	err := tx.QueryRow(`SELECT value FROM schema_meta WHERE key=?`, sourceBackfillKey).Scan(&recorded)
	if err == sql.ErrNoRows || recorded == "" {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return deviceID == recorded, nil
}

func (s *Store) ensureColumn(table, name, decl string) error {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var col, typ string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &col, &typ, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if col == name {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + name + " " + decl)
	return err
}

type rowReport struct {
	ID           string
	DeviceID     string
	Name         string
	Type         string
	RoadCode     string
	RoadName     string
	Notes        string
	Latitude     float64
	Longitude    float64
	Fine         *int
	Points       *int
	Violation    string
	ImagePath    string
	Status       string
	PublicID     string
	CreatedAt    string
	OcrLocation  string
	AmapName     string
	Located      bool
	OcrText      string
	ParsedJSON   string
	Confidence   string
	EndLat       sql.NullFloat64
	EndLon       sql.NullFloat64
	Stake        string
	Direction    string
	ReviewResult string
	ReviewReason string
}

func scanReport(sc interface{ Scan(...any) error }) (rowReport, error) {
	var r rowReport
	var fine, points sql.NullInt64
	var publicID sql.NullString
	var located int
	err := sc.Scan(
		&r.ID, &r.DeviceID, &r.Name, &r.Type, &r.RoadCode, &r.RoadName, &r.Notes,
		&r.Latitude, &r.Longitude, &fine, &points, &r.Violation, &r.ImagePath,
		&r.Status, &publicID, &r.CreatedAt, &r.OcrLocation, &r.AmapName, &located,
		&r.OcrText, &r.ParsedJSON, &r.Confidence, &r.EndLat, &r.EndLon, &r.Stake, &r.Direction,
		&r.ReviewResult, &r.ReviewReason,
	)
	if err != nil {
		return r, err
	}
	r.Fine = nullInt(fine)
	r.Points = nullInt(points)
	r.Located = located != 0
	if publicID.Valid {
		r.PublicID = publicID.String
	}
	return r, nil
}

func nullInt(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func upperRoad(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}
