package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"motocam/server/internal/geo"
)

var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var allowedTypes = map[string]bool{
	"banned_toll":    true,
	"banned_segment": true,
	"camera":         true,
	"other":          true,
}

func (s *Store) Submit(in ReportInput) (SubmitResult, error) {
	in, err := normalizeInput(in)
	if err != nil {
		return SubmitResult{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return SubmitResult{}, err
	}
	defer tx.Rollback()

	if err := s.enforceRate(tx, in.DeviceID); err != nil {
		return SubmitResult{}, err
	}
	id := newID()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`
INSERT INTO reports (
  id, device_id, name, type, road_code, road_name, notes, latitude, longitude,
  fine_yuan, penalty_points, violation, image_path, status, created_at,
  ocr_location, amap_name, located, user_id,
  ocr_text, parsed_json, confidence, end_latitude, end_longitude, stake, direction,
  review_result, review_reason
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', 'pending', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.DeviceID, in.Name, in.Type, in.RoadCode, in.RoadName, in.Notes,
		in.Latitude, in.Longitude, nullable(in.FineYuan), nullable(in.PenaltyPoints), in.Violation, now,
		in.OcrLocation, in.AmapName, locatedFlag(in.Unlocated), in.UserID,
		in.OcrText, in.ParsedJSON, in.Confidence, endOrNull(in.HasEnd, in.EndLatitude), endOrNull(in.HasEnd, in.EndLongitude), in.Stake, in.Direction,
		in.ReviewResult, in.ReviewReason,
	); err != nil {
		return SubmitResult{}, err
	}
	if _, err := tx.Exec(`INSERT INTO rate_events (device_id, created_at) VALUES (?, ?)`, in.DeviceID, now); err != nil {
		return SubmitResult{}, err
	}
	status, publicID, drop, err := s.consider(tx, id, in, now)
	if err != nil {
		return SubmitResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return SubmitResult{}, err
	}
	for _, name := range drop {
		s.removeImage(name)
	}
	if status == "pending" && len(in.Image) > 0 {
		filename := id + ".jpg"
		if err := os.WriteFile(filepath.Join(s.imageDir, filename), in.Image, 0o644); err != nil {
			return SubmitResult{}, err
		}
		_, _ = s.db.Exec(`UPDATE reports SET image_path=? WHERE id=? AND status='pending'`, filename, id)
	}
	return SubmitResult{ID: id, Status: status, PublicPointID: publicID}, nil
}

// ApplyRelocation writes a background recognition result onto a report that
// is still pending, then publishes or merges it the same way Submit does.
// A report that was already approved, rejected, or archived is left alone.
func (s *Store) ApplyRelocation(id string, in ReportInput) (SubmitResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return SubmitResult{}, err
	}
	defer tx.Rollback()
	report, err := s.reportByID(tx, id)
	if err != nil {
		return SubmitResult{}, err
	}
	if report.Status != "pending" {
		return SubmitResult{ID: id, Status: report.Status, PublicPointID: report.PublicID}, tx.Commit()
	}
	in.DeviceID = report.DeviceID
	in.Type = report.Type
	if in.Name == "" {
		in.Name = report.Name
	}
	if in.RoadCode == "" {
		in.RoadCode = report.RoadCode
	}
	if in.RoadName == "" {
		in.RoadName = report.RoadName
	}
	located := 1
	if in.Unlocated {
		located = 0
		in.Latitude = 0
		in.Longitude = 0
	}
	if _, err := tx.Exec(`UPDATE reports SET name=?, road_code=?, road_name=?, latitude=?, longitude=?, located=?,
		amap_name=?, confidence=?, end_latitude=?, end_longitude=?, stake=?, direction=?, parsed_json=?,
		review_result=?, review_reason=? WHERE id=? AND status='pending'`,
		in.Name, upperRoad(in.RoadCode), in.RoadName, in.Latitude, in.Longitude, located,
		in.AmapName, in.Confidence, endOrNull(in.HasEnd, in.EndLatitude), endOrNull(in.HasEnd, in.EndLongitude),
		in.Stake, in.Direction, in.ParsedJSON, in.ReviewResult, in.ReviewReason, id,
	); err != nil {
		return SubmitResult{}, err
	}
	in.RoadCode = upperRoad(in.RoadCode)
	now := time.Now().UTC().Format(time.RFC3339)
	status, publicID, drop, err := s.consider(tx, id, in, now)
	if err != nil {
		return SubmitResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return SubmitResult{}, err
	}
	if status != "pending" && report.ImagePath != "" {
		drop = append(drop, report.ImagePath)
	}
	for _, name := range drop {
		s.removeImage(name)
	}
	return SubmitResult{ID: id, Status: status, PublicPointID: publicID}, nil
}

// RecordReview stores an auto-review outcome without moving the coordinate.
// A report that is no longer pending is left unchanged.
func (s *Store) RecordReview(id, result, reason string) error {
	res, err := s.db.Exec(
		`UPDATE reports SET review_result=?, review_reason=? WHERE id=? AND status='pending'`,
		result, reason, id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		return nil
	}
	var status string
	err = s.db.QueryRow(`SELECT status FROM reports WHERE id=?`, id).Scan(&status)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	return err
}

func (s *Store) consider(tx *sql.Tx, id string, in ReportInput, now string) (string, string, []string, error) {
	if in.Unlocated {
		return "pending", "", nil, nil
	}
	if in.ForceReview || in.Confidence == "low" || in.Confidence == "none" {
		return "pending", "", nil, nil
	}
	if pid, ok, err := s.findByStake(tx, in.RoadCode, in.Stake, in.Latitude, in.Longitude); err != nil {
		return "", "", nil, err
	} else if ok {
		if err := s.attach(tx, id, pid, in.DeviceID, now); err != nil {
			return "", "", nil, err
		}
		return "approved", pid, nil, nil
	}
	if pid, ok, err := s.findPublic(tx, in.Latitude, in.Longitude, in.Type, in.RoadCode); err != nil {
		return "", "", nil, err
	} else if ok {
		if err := s.attach(tx, id, pid, in.DeviceID, now); err != nil {
			return "", "", nil, err
		}
		return "approved", pid, nil, nil
	}

	nearby, err := s.nearbyReports(tx, in.Latitude, in.Longitude, in.Type, in.RoadCode)
	if err != nil {
		return "", "", nil, err
	}
	nearby = consistentRoad(nearby, in.RoadCode)
	for _, report := range nearby {
		if report.PublicID != "" {
			if err := s.attach(tx, id, report.PublicID, in.DeviceID, now); err != nil {
				return "", "", nil, err
			}
			return "approved", report.PublicID, nil, nil
		}
	}

	latest := map[string]rowReport{}
	for _, report := range nearby {
		prev, ok := latest[report.DeviceID]
		if !ok || report.CreatedAt >= prev.CreatedAt {
			latest[report.DeviceID] = report
		}
	}
	if len(latest) < s.limits.PromoteMin {
		return "pending", "", nil, nil
	}
	members := make([]rowReport, 0, len(latest))
	for _, report := range latest {
		members = append(members, report)
	}
	pid, err := s.insertPoint(tx, members, now)
	if err != nil {
		return "", "", nil, err
	}
	var drop []string
	for _, report := range nearby {
		if _, ok := latest[report.DeviceID]; !ok {
			continue
		}
		if report.ImagePath != "" {
			drop = append(drop, report.ImagePath)
		}
		if _, err := tx.Exec(`UPDATE reports SET status='merged', public_point_id=?, image_path='' WHERE id=?`, pid, report.ID); err != nil {
			return "", "", nil, err
		}
	}
	return "approved", pid, drop, nil
}

func (s *Store) attach(tx *sql.Tx, reportID, pointID, deviceID, now string) error {
	var count int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM reports WHERE public_point_id=? AND device_id=? AND status='merged' AND id<>?`,
		pointID, deviceID, reportID,
	).Scan(&count); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE reports SET status='merged', public_point_id=?, image_path='' WHERE id=?`, pointID, reportID); err != nil {
		return err
	}
	if count == 0 {
		if _, err := tx.Exec(`UPDATE points SET source_count = source_count + 1, updated_at=? WHERE id=?`, now, pointID); err != nil {
			return err
		}
	}
	imported, err := s.isImportDevice(tx, deviceID)
	if err != nil {
		return err
	}
	if !imported {
		if _, err := tx.Exec(
			`UPDATE points SET source=?, updated_at=? WHERE id=? AND source IN ('', ?)`,
			SourceUserUpload, now, pointID, SourceImport,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) findPublic(tx *sql.Tx, lat, lon float64, typ, road string) (string, bool, error) {
	rows, err := tx.Query(`
SELECT id, latitude, longitude, type, road_code FROM points
WHERE latitude BETWEEN ? AND ? AND longitude BETWEEN ? AND ?`,
		lat-0.02, lat+0.02, lon-0.02, lon+0.02)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	type publicHit struct {
		id   string
		dist float64
		road string
	}
	var hits []publicHit
	for rows.Next() {
		var id, pointType, roadCode string
		var plat, plon float64
		if err := rows.Scan(&id, &plat, &plon, &pointType, &roadCode); err != nil {
			return "", false, err
		}
		if pointType != typ || !geo.RoadsAgree(roadCode, road) {
			continue
		}
		dist := geo.Distance(lat, lon, plat, plon)
		if dist <= s.limits.PromoteRadiusM {
			hits = append(hits, publicHit{id: id, dist: dist, road: roadCode})
		}
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	if road == "" {
		codes := map[string]bool{}
		for _, hit := range hits {
			if hit.road != "" {
				codes[hit.road] = true
			}
		}
		if len(codes) > 1 {
			return "", false, nil
		}
	}
	best := ""
	bestDist := s.limits.PromoteRadiusM + 1
	for _, hit := range hits {
		if hit.dist < bestDist {
			best = hit.id
			bestDist = hit.dist
		}
	}
	return best, best != "", nil
}

func (s *Store) nearbyReports(tx *sql.Tx, lat, lon float64, typ, road string) ([]rowReport, error) {
	rows, err := tx.Query(`
SELECT id, device_id, name, type, road_code, road_name, notes, latitude, longitude,
       fine_yuan, penalty_points, violation, image_path, status, public_point_id, created_at,
       ocr_location, amap_name, located, ocr_text, parsed_json, confidence, end_latitude, end_longitude, stake, direction,
       review_result, review_reason
FROM reports
WHERE status IN ('pending', 'merged')
  AND latitude BETWEEN ? AND ? AND longitude BETWEEN ? AND ?`,
		lat-0.02, lat+0.02, lon-0.02, lon+0.02)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rowReport
	for rows.Next() {
		report, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		if report.Type != typ || !geo.RoadsAgree(report.RoadCode, road) {
			continue
		}
		if geo.Distance(lat, lon, report.Latitude, report.Longitude) > s.limits.PromoteRadiusM {
			continue
		}
		out = append(out, report)
	}
	return out, rows.Err()
}

func (s *Store) insertPoint(tx *sql.Tx, members []rowReport, now string) (string, error) {
	sort.Slice(members, func(i, j int) bool { return members[i].CreatedAt < members[j].CreatedAt })
	var lat, lon float64
	var name, roadName, notes, violation, roadCode, typ string
	var fine, points *int
	seenNotes := map[string]bool{}
	for _, member := range members {
		lat += member.Latitude
		lon += member.Longitude
		typ = member.Type
		if utf8.RuneCountInString(member.Name) >= utf8.RuneCountInString(name) {
			name = member.Name
		}
		if utf8.RuneCountInString(member.RoadName) >= utf8.RuneCountInString(roadName) {
			roadName = member.RoadName
		}
		if member.RoadCode != "" {
			roadCode = member.RoadCode
		}
		if member.Notes != "" && !seenNotes[member.Notes] {
			if notes != "" {
				notes += "\n"
			}
			notes += member.Notes
			seenNotes[member.Notes] = true
		}
		if member.Violation != "" {
			violation = member.Violation
		}
		if member.Fine != nil {
			fine = member.Fine
		}
		if member.Points != nil {
			points = member.Points
		}
	}
	n := float64(len(members))
	id := newID()
	var endLat, endLon any
	var stake, direction, reviewResult, reviewReason string
	for _, member := range members {
		if member.EndLat.Valid && member.EndLon.Valid {
			endLat = member.EndLat.Float64
			endLon = member.EndLon.Float64
		}
		if member.Stake != "" {
			stake = member.Stake
		}
		if member.Direction != "" {
			direction = member.Direction
		}
		if member.ReviewResult != "" {
			reviewResult = member.ReviewResult
		}
		if member.ReviewReason != "" {
			reviewReason = member.ReviewReason
		}
	}
	source := SourceUserUpload
	for _, member := range members {
		imported, err := s.isImportDevice(tx, member.DeviceID)
		if err != nil {
			return "", err
		}
		if !imported {
			source = SourceUserUpload
			break
		}
		source = SourceImport
	}
	_, err := tx.Exec(`
INSERT INTO points (
  id, name, type, road_code, road_name, notes, latitude, longitude,
  fine_yuan, penalty_points, violation, source_count, created_at, updated_at,
  end_latitude, end_longitude, stake, direction, source, review_result, review_reason
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, typ, roadCode, roadName, trimRunes(notes, 500), lat/n, lon/n,
		nullable(fine), nullable(points), trimRunes(violation, 200), len(members), now, now,
		endLat, endLon, stake, direction, source, reviewResult, reviewReason,
	)
	return id, err
}

// findByStake merges a second report of the same road code and stake when it
// sits within 300 m of the published point.
func (s *Store) findByStake(tx *sql.Tx, road, stake string, lat, lon float64) (string, bool, error) {
	km, ok := stakeKilometres(stake)
	if !ok || strings.TrimSpace(road) == "" {
		return "", false, nil
	}
	rows, err := tx.Query(`SELECT id, latitude, longitude, stake FROM points WHERE road_code=?`, upperRoad(road))
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	best := ""
	bestDist := 301.0
	for rows.Next() {
		var id, st string
		var plat, plon float64
		if err := rows.Scan(&id, &plat, &plon, &st); err != nil {
			return "", false, err
		}
		other, ok := stakeKilometres(st)
		if !ok || abs(other-km) > 0.30 {
			continue
		}
		dist := geo.Distance(lat, lon, plat, plon)
		if dist <= 300 && dist < bestDist {
			best = id
			bestDist = dist
		}
	}
	return best, best != "", rows.Err()
}

func stakeKilometres(stake string) (float64, bool) {
	stake = strings.TrimSpace(strings.ToUpper(stake))
	stake = strings.TrimPrefix(stake, "K")
	if stake == "" {
		return 0, false
	}
	plus := strings.Index(stake, "+")
	kmPart := stake
	metres := 0.0
	if plus >= 0 {
		kmPart = stake[:plus]
		m, err := strconv.Atoi(strings.TrimSpace(stake[plus+1:]))
		if err != nil {
			return 0, false
		}
		metres = float64(m) / 1000
	}
	km, err := strconv.ParseFloat(strings.TrimSpace(kmPart), 64)
	if err != nil {
		return 0, false
	}
	return km + metres, true
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func endOrNull(ok bool, v float64) any {
	if !ok {
		return nil
	}
	return v
}

func consistentRoad(reports []rowReport, triggerRoad string) []rowReport {
	if triggerRoad != "" {
		return reports
	}
	codes := map[string]bool{}
	for _, report := range reports {
		if report.RoadCode != "" {
			codes[report.RoadCode] = true
		}
	}
	if len(codes) <= 1 {
		return reports
	}
	var out []rowReport
	for _, report := range reports {
		if report.RoadCode == "" {
			out = append(out, report)
		}
	}
	return out
}

func (s *Store) enforceRate(tx *sql.Tx, deviceID string) error {
	now := time.Now().UTC()
	hourAgo := now.Add(-time.Hour).Format(time.RFC3339)
	dayAgo := now.Add(-24 * time.Hour).Format(time.RFC3339)
	var hourCount, dayCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM rate_events WHERE device_id=? AND created_at>=?`, deviceID, hourAgo).Scan(&hourCount); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM rate_events WHERE device_id=? AND created_at>=?`, deviceID, dayAgo).Scan(&dayCount); err != nil {
		return err
	}
	if hourCount >= s.limits.RatePerHour || dayCount >= s.limits.RatePerDay {
		return ErrRateLimited
	}
	_, _ = tx.Exec(`DELETE FROM rate_events WHERE created_at < ?`, dayAgo)
	return nil
}

func (s *Store) removeImage(name string) {
	if name == "" {
		return
	}
	_ = os.Remove(filepath.Join(s.imageDir, filepath.Base(name)))
}

func normalizeInput(in ReportInput) (ReportInput, error) {
	in.DeviceID = strings.TrimSpace(in.DeviceID)
	if !uuidPattern.MatchString(in.DeviceID) {
		return in, InputError{Message: "缺少有效的设备标识"}
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return in, InputError{Message: "名称不能为空"}
	}
	if utf8.RuneCountInString(in.Name) > 80 {
		return in, InputError{Message: "名称过长"}
	}
	if !allowedTypes[in.Type] {
		return in, InputError{Message: "类型无效"}
	}
	in.RoadCode = upperRoad(in.RoadCode)
	if utf8.RuneCountInString(in.RoadCode) > 16 {
		return in, InputError{Message: "公路编号过长"}
	}
	in.RoadName = strings.TrimSpace(in.RoadName)
	in.Notes = trimRunes(strings.TrimSpace(in.Notes), 500)
	in.Violation = trimRunes(strings.TrimSpace(in.Violation), 200)
	in.OcrLocation = trimRunes(strings.TrimSpace(in.OcrLocation), 300)
	in.AmapName = trimRunes(strings.TrimSpace(in.AmapName), 80)
	in.OcrText = trimRunes(strings.TrimSpace(in.OcrText), 8000)
	in.ParsedJSON = trimRunes(strings.TrimSpace(in.ParsedJSON), 4000)
	switch in.Confidence {
	case "exact", "low", "none", "":
	default:
		in.Confidence = ""
	}
	in.Stake = trimRunes(strings.TrimSpace(in.Stake), 32)
	in.Direction = trimRunes(strings.TrimSpace(in.Direction), 40)
	if in.Unlocated {
		in.Latitude = 0
		in.Longitude = 0
	} else if in.Latitude < -90 || in.Latitude > 90 || in.Longitude < -180 || in.Longitude > 180 {
		return in, InputError{Message: "坐标无效"}
	}
	if in.FineYuan != nil && (*in.FineYuan < 0 || *in.FineYuan > 20000) {
		return in, InputError{Message: "罚款金额无效"}
	}
	if in.PenaltyPoints != nil && (*in.PenaltyPoints < 0 || *in.PenaltyPoints > 12) {
		return in, InputError{Message: "记分无效"}
	}
	return in, nil
}

func locatedFlag(unlocated bool) int {
	if unlocated {
		return 0
	}
	return 1
}

func nullable(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}
