package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxListLimit is how many public points one response may contain.
// Older servers hard-capped this at 5000. Clients that must keep working
// against that build ask for limit=5000 and follow updated_at with since.
const maxListLimit = 100000

func (s *Store) List(minLat, minLon, maxLat, maxLon *float64, since *time.Time, limit int) (ListResult, error) {
	if limit <= 0 || limit > maxListLimit {
		limit = maxListLimit
	}
	query := `
SELECT id, name, type, road_code, road_name, notes, latitude, longitude,
       fine_yuan, penalty_points, violation, source_count, updated_at,
       end_latitude, end_longitude, stake, direction, source
FROM points WHERE 1=1`
	args := []any{}
	if minLat != nil {
		query += ` AND latitude>=? AND latitude<=? AND longitude>=? AND longitude<=?`
		args = append(args, *minLat, *maxLat, *minLon, *maxLon)
	}
	var sinceText string
	if since != nil {
		sinceText = since.UTC().Format(time.RFC3339)
		query += ` AND updated_at>=?`
		args = append(args, sinceText)
	}
	query += ` ORDER BY updated_at ASC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()
	points := []PublicPoint{}
	for rows.Next() {
		var point PublicPoint
		var fine, penalty sql.NullInt64
		var endLat, endLon sql.NullFloat64
		if err := rows.Scan(
			&point.ID, &point.Name, &point.Type, &point.RoadCode, &point.RoadName, &point.Notes,
			&point.Latitude, &point.Longitude, &fine, &penalty, &point.Violation, &point.SourceCount, &point.UpdatedAt,
			&endLat, &endLon, &point.Stake, &point.Direction, &point.Source,
		); err != nil {
			return ListResult{}, err
		}
		point.Coord = "wgs84"
		point.FineYuan = nullInt(fine)
		point.PenaltyPoints = nullInt(penalty)
		if endLat.Valid && endLon.Valid {
			lat := endLat.Float64
			lon := endLon.Float64
			point.EndLatitude = &lat
			point.EndLongitude = &lon
		}
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	removed := []string{}
	if since != nil {
		rrows, err := s.db.Query(`SELECT id FROM point_tombstones WHERE removed_at>=? ORDER BY removed_at`, sinceText)
		if err != nil {
			return ListResult{}, err
		}
		defer rrows.Close()
		for rrows.Next() {
			var id string
			if err := rrows.Scan(&id); err != nil {
				return ListResult{}, err
			}
			removed = append(removed, id)
		}
		if err := rrows.Err(); err != nil {
			return ListResult{}, err
		}
	}
	return ListResult{
		ServerTime: time.Now().UTC().Format(time.RFC3339),
		Points:     points,
		RemovedIDs: removed,
	}, nil
}

func (s *Store) AdminList(status string, limit int) ([]AdminReport, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	query := `
SELECT id, device_id, name, type, road_code, road_name, notes, latitude, longitude,
       fine_yuan, penalty_points, violation, image_path, status, public_point_id, created_at,
       ocr_location, amap_name, located, ocr_text, parsed_json, confidence, end_latitude, end_longitude, stake, direction,
       review_result, review_reason
FROM reports`
	args := []any{}
	if status != "" {
		query += ` WHERE status=?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminReport{}
	for rows.Next() {
		report, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, toAdmin(report))
	}
	return out, rows.Err()
}

func (s *Store) AdminPoints(limit int) ([]AdminPoint, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := s.db.Query(`
SELECT id, name, type, road_code, road_name, latitude, longitude, stake, direction,
       source, review_result, review_reason, updated_at
FROM points ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminPoint{}
	for rows.Next() {
		var p AdminPoint
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Type, &p.RoadCode, &p.RoadName, &p.Latitude, &p.Longitude,
			&p.Stake, &p.Direction, &p.Source, &p.ReviewResult, &p.ReviewReason, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Approve(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	report, err := s.reportByID(tx, id)
	if err != nil {
		return err
	}
	if !report.Located {
		return InputError{Message: "还没有坐标，不能公开"}
	}
	if report.Status == "rejected" || report.Status == "archived" {
		return ErrConflict
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var drop []string
	if report.Status == "merged" && report.PublicID != "" {
		return tx.Commit()
	}
	pid, ok, err := s.findByStake(tx, report.RoadCode, report.Stake, report.Latitude, report.Longitude)
	if err != nil {
		return err
	}
	if !ok {
		pid, ok, err = s.findPublic(tx, report.Latitude, report.Longitude, report.Type, report.RoadCode)
	}
	if err != nil {
		return err
	}
	if ok {
		if err := s.attach(tx, id, pid, report.DeviceID, now); err != nil {
			return err
		}
	} else {
		pid, err = s.insertPoint(tx, []rowReport{report}, now)
		if err != nil {
			return err
		}
		if report.ImagePath != "" {
			drop = append(drop, report.ImagePath)
		}
		if _, err := tx.Exec(`UPDATE reports SET status='merged', public_point_id=?, image_path='' WHERE id=?`, pid, id); err != nil {
			return err
		}
	}
	if report.ImagePath != "" {
		drop = append(drop, report.ImagePath)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, name := range drop {
		s.removeImage(name)
	}
	return nil
}

func (s *Store) Reject(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	report, err := s.reportByID(tx, id)
	if err != nil {
		return err
	}
	if report.Status != "pending" {
		return ErrConflict
	}
	if _, err := tx.Exec(`UPDATE reports SET status='rejected', image_path='' WHERE id=?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.removeImage(report.ImagePath)
	return nil
}

func (s *Store) Merge(ids []string) (string, error) {
	if len(ids) == 0 {
		return "", InputError{Message: "没有要合并的报告"}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var members []rowReport
	var typ, road string
	var drop []string
	for i, id := range ids {
		report, err := s.reportByID(tx, id)
		if err != nil {
			return "", err
		}
		if report.Status == "rejected" || report.Status == "archived" {
			return "", ErrConflict
		}
		if i == 0 {
			typ = report.Type
			road = report.RoadCode
		} else if report.Type != typ || !geoAgree(road, report.RoadCode) {
			return "", InputError{Message: "这些报告的类型或公路编号不一致"}
		}
		if report.RoadCode != "" {
			road = report.RoadCode
		}
		if report.ImagePath != "" {
			drop = append(drop, report.ImagePath)
		}
		members = append(members, report)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	pid, err := s.insertPoint(tx, members, now)
	if err != nil {
		return "", err
	}
	for _, report := range members {
		if _, err := tx.Exec(`UPDATE reports SET status='merged', public_point_id=?, image_path='' WHERE id=?`, pid, report.ID); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	for _, name := range drop {
		s.removeImage(name)
	}
	return pid, nil
}

// SegmentInput turns an existing published point into a section-speed segment.
// The start point keeps its coordinates. EndPointID, when set, supplies the
// end coordinates and that point stays published so older apps still see a
// plain point at the far end. Otherwise EndLat/EndLon are used directly.
type SegmentInput struct {
	EndPointID string
	EndLat     float64
	EndLon     float64
	HasCoord   bool
	Stake      string
	Direction  string
}

func (s *Store) MakeSegment(startID string, in SegmentInput) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var one int
	err = tx.QueryRow(`SELECT 1 FROM points WHERE id=?`, startID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	endLat, endLon := in.EndLat, in.EndLon
	switch {
	case strings.TrimSpace(in.EndPointID) != "":
		if in.EndPointID == startID {
			return InputError{Message: "终点不能是同一个点"}
		}
		err = tx.QueryRow(`SELECT latitude, longitude FROM points WHERE id=?`, in.EndPointID).Scan(&endLat, &endLon)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
	case in.HasCoord:
	default:
		return InputError{Message: "需要终点坐标或终点 id"}
	}
	if endLat < -90 || endLat > 90 || endLon < -180 || endLon > 180 {
		return InputError{Message: "终点坐标无效"}
	}
	stake := trimRunes(strings.TrimSpace(in.Stake), 32)
	direction := trimRunes(strings.TrimSpace(in.Direction), 40)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := tx.Exec(`UPDATE points SET end_latitude=?, end_longitude=?, stake=?, direction=?, updated_at=? WHERE id=?`,
		endLat, endLon, stake, direction, now, startID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) DeletePoint(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM points WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`INSERT INTO point_tombstones (id, removed_at) VALUES (?, ?)
ON CONFLICT(id) DO UPDATE SET removed_at=excluded.removed_at`, id, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE reports SET status='archived', public_point_id=NULL WHERE public_point_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ImagePath(id string) (string, error) {
	var name string
	err := s.db.QueryRow(`SELECT image_path FROM reports WHERE id=?`, id).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) || name == "" {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	full := filepath.Join(s.imageDir, filepath.Base(name))
	if _, err := os.Stat(full); err != nil {
		return "", ErrNotFound
	}
	return full, nil
}

func (s *Store) reportByID(tx *sql.Tx, id string) (rowReport, error) {
	row := tx.QueryRow(`
SELECT id, device_id, name, type, road_code, road_name, notes, latitude, longitude,
       fine_yuan, penalty_points, violation, image_path, status, public_point_id, created_at,
       ocr_location, amap_name, located, ocr_text, parsed_json, confidence, end_latitude, end_longitude, stake, direction,
       review_result, review_reason
FROM reports WHERE id=?`, id)
	report, err := scanReport(row)
	if errors.Is(err, sql.ErrNoRows) {
		return rowReport{}, ErrNotFound
	}
	return report, err
}

func toAdmin(r rowReport) AdminReport {
	out := AdminReport{
		ID: r.ID, DeviceID: r.DeviceID, Name: r.Name, Type: r.Type,
		RoadCode: r.RoadCode, RoadName: r.RoadName, Notes: r.Notes,
		Latitude: r.Latitude, Longitude: r.Longitude,
		FineYuan: r.Fine, PenaltyPoints: r.Points, Violation: r.Violation,
		Status: r.Status, PublicPointID: r.PublicID, HasImage: r.ImagePath != "",
		OcrLocation: r.OcrLocation, AmapName: r.AmapName, Located: r.Located,
		CreatedAt: r.CreatedAt, OcrText: r.OcrText, ParsedJSON: r.ParsedJSON,
		Confidence: r.Confidence, Stake: r.Stake, Direction: r.Direction,
		ReviewResult: r.ReviewResult, ReviewReason: r.ReviewReason,
	}
	if r.EndLat.Valid && r.EndLon.Valid {
		lat, lon := r.EndLat.Float64, r.EndLon.Float64
		out.EndLatitude = &lat
		out.EndLongitude = &lon
	}
	return out
}

// ReportEdit is an admin correction before approve. Nil fields stay as stored.
type ReportEdit struct {
	Name         *string
	RoadCode     *string
	RoadName     *string
	Stake        *string
	Direction    *string
	Latitude     *float64
	Longitude    *float64
	EndLatitude  *float64
	EndLongitude *float64
	ClearEnd     bool
	Confidence   *string
}

func (s *Store) EditReport(id string, edit ReportEdit) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	report, err := s.reportByID(tx, id)
	if err != nil {
		return err
	}
	if report.Status == "rejected" || report.Status == "archived" || report.Status == "merged" {
		return ErrConflict
	}
	if edit.Name != nil {
		report.Name = trimRunes(strings.TrimSpace(*edit.Name), 80)
	}
	if edit.RoadCode != nil {
		report.RoadCode = upperRoad(*edit.RoadCode)
	}
	if edit.RoadName != nil {
		report.RoadName = trimRunes(strings.TrimSpace(*edit.RoadName), 40)
	}
	if edit.Stake != nil {
		report.Stake = trimRunes(strings.TrimSpace(*edit.Stake), 32)
	}
	if edit.Direction != nil {
		report.Direction = trimRunes(strings.TrimSpace(*edit.Direction), 40)
	}
	if edit.Latitude != nil && edit.Longitude != nil {
		report.Latitude = *edit.Latitude
		report.Longitude = *edit.Longitude
		report.Located = true
	}
	if edit.ClearEnd {
		report.EndLat = sql.NullFloat64{}
		report.EndLon = sql.NullFloat64{}
	}
	if edit.EndLatitude != nil && edit.EndLongitude != nil {
		report.EndLat = sql.NullFloat64{Float64: *edit.EndLatitude, Valid: true}
		report.EndLon = sql.NullFloat64{Float64: *edit.EndLongitude, Valid: true}
	}
	if edit.Confidence != nil {
		switch *edit.Confidence {
		case "exact", "low", "none", "":
			report.Confidence = *edit.Confidence
		}
	}
	if report.Name == "" {
		return InputError{Message: "名称不能为空"}
	}
	_, err = tx.Exec(`UPDATE reports SET name=?, road_code=?, road_name=?, latitude=?, longitude=?, located=?,
		confidence=?, end_latitude=?, end_longitude=?, stake=?, direction=? WHERE id=?`,
		report.Name, report.RoadCode, report.RoadName, report.Latitude, report.Longitude, locatedFlag(!report.Located),
		report.Confidence, nullFloat(report.EndLat), nullFloat(report.EndLon), report.Stake, report.Direction, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func nullFloat(v sql.NullFloat64) any {
	if !v.Valid {
		return nil
	}
	return v.Float64
}

func geoAgree(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	return a == b
}
