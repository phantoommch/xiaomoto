package store

import (
	"database/sql"
	"strings"
	"time"
)

// SubmissionLabel is the word an uploader sees for a report status.
// merged is what Approve and a successful reprocess write.
func SubmissionLabel(status string) string {
	switch status {
	case "merged", "approved":
		return "已通过"
	case "rejected":
		return "已驳回"
	case "archived":
		return "已归档"
	default:
		return "审核中"
	}
}

// MineReport is one of the signed-in user's uploads. Coordinates are omitted
// so a rejected village pin is not shown again as if it were the place.
type MineReport struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Label     string `json:"label"`
	RoadCode  string `json:"road_code"`
	RoadName  string `json:"road_name"`
	Stake     string `json:"stake"`
	Direction string `json:"direction"`
	CreatedAt string `json:"created_at"`
}

func (s *Store) ListMine(userID string) ([]MineReport, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return []MineReport{}, nil
	}
	rows, err := s.db.Query(`
SELECT id, name, status, road_code, road_name, stake, direction, created_at
FROM reports WHERE user_id=? ORDER BY created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MineReport{}
	for rows.Next() {
		var row MineReport
		if err := rows.Scan(&row.ID, &row.Name, &row.Status, &row.RoadCode, &row.RoadName, &row.Stake, &row.Direction, &row.CreatedAt); err != nil {
			return nil, err
		}
		row.Label = SubmissionLabel(row.Status)
		out = append(out, row)
	}
	return out, rows.Err()
}

// ReprocessCandidates are pending and rejected ticket uploads. The photo is
// deleted on reject, so the stored OCR text is the input. Merged reports are
// not opened again.
func (s *Store) ReprocessCandidates() ([]AdminReport, error) {
	rows, err := s.db.Query(`
SELECT id, device_id, name, type, road_code, road_name, notes, latitude, longitude,
       fine_yuan, penalty_points, violation, image_path, status, public_point_id, created_at,
       ocr_location, amap_name, located, ocr_text, parsed_json, confidence, end_latitude, end_longitude, stake, direction,
       review_result, review_reason
FROM reports
WHERE status IN ('pending', 'rejected')
  AND (ocr_text <> '' OR ocr_location <> '')
ORDER BY created_at ASC`)
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

// PublishCorrected writes an exact stake result onto a pending or rejected
// report and publishes it. Approve and EditReport both refuse a rejected row,
// so this is the only path that can replace a village pin after 驳回.
// The caller must already have decided the hit is an exact kilometre stake.
func (s *Store) PublishCorrected(id string, in ReportInput) (SubmitResult, error) {
	if in.Unlocated || in.Confidence != "exact" || strings.TrimSpace(in.Stake) == "" || strings.TrimSpace(in.RoadCode) == "" {
		return SubmitResult{}, InputError{Message: "只有已确认的公里桩才能重新通过"}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return SubmitResult{}, err
	}
	defer tx.Rollback()
	report, err := s.reportByID(tx, id)
	if err != nil {
		return SubmitResult{}, err
	}
	if report.Status != "pending" && report.Status != "rejected" {
		return SubmitResult{ID: id, Status: report.Status, PublicPointID: report.PublicID}, tx.Commit()
	}
	if in.Name != "" {
		report.Name = trimRunes(strings.TrimSpace(in.Name), 80)
	}
	if in.Type != "" {
		report.Type = in.Type
	}
	report.RoadCode = upperRoad(in.RoadCode)
	report.RoadName = strings.TrimSpace(in.RoadName)
	report.Latitude = in.Latitude
	report.Longitude = in.Longitude
	report.Located = true
	report.Stake = trimRunes(strings.TrimSpace(in.Stake), 32)
	report.Direction = trimRunes(strings.TrimSpace(in.Direction), 40)
	report.Confidence = "exact"
	report.AmapName = trimRunes(strings.TrimSpace(in.AmapName), 80)
	if in.OcrLocation != "" {
		report.OcrLocation = trimRunes(strings.TrimSpace(in.OcrLocation), 300)
	}
	if in.ParsedJSON != "" {
		report.ParsedJSON = trimRunes(strings.TrimSpace(in.ParsedJSON), 4000)
	}
	report.ReviewResult = "pass"
	if in.ReviewReason != "" {
		report.ReviewReason = trimRunes(strings.TrimSpace(in.ReviewReason), 200)
	} else {
		report.ReviewReason = "重新识别到公里桩，已通过"
	}
	if in.HasEnd {
		report.EndLat = sql.NullFloat64{Float64: in.EndLatitude, Valid: true}
		report.EndLon = sql.NullFloat64{Float64: in.EndLongitude, Valid: true}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	pid, err := s.insertPoint(tx, []rowReport{report}, now)
	if err != nil {
		return SubmitResult{}, err
	}
	res, err := tx.Exec(`UPDATE reports SET
		name=?, type=?, road_code=?, road_name=?, latitude=?, longitude=?, located=1,
		amap_name=?, confidence=?, end_latitude=?, end_longitude=?, stake=?, direction=?,
		ocr_location=?, parsed_json=?, review_result=?, review_reason=?,
		status='merged', public_point_id=?, image_path=''
		WHERE id=? AND status IN ('pending', 'rejected')`,
		report.Name, report.Type, report.RoadCode, report.RoadName, report.Latitude, report.Longitude,
		report.AmapName, report.Confidence, endOrNull(report.EndLat.Valid, report.EndLat.Float64), endOrNull(report.EndLon.Valid, report.EndLon.Float64),
		report.Stake, report.Direction, report.OcrLocation, report.ParsedJSON, report.ReviewResult, report.ReviewReason,
		pid, id,
	)
	if err != nil {
		return SubmitResult{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return SubmitResult{}, ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return SubmitResult{}, err
	}
	return SubmitResult{ID: id, Status: "merged", PublicPointID: pid}, nil
}
