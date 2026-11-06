package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"motocam/server/internal/recog"
	"motocam/server/internal/store"
)

const relocDeadline = 20 * time.Second

type relocJob struct {
	id string
	in store.ReportInput
}

// needsAutoReview is true for a ticket-like upload: OCR, a kilometre stake,
// or a low-confidence hit. A manual pin with coordinates and no ticket text
// keeps the immediate publish path, including builds that omit confidence.
func needsAutoReview(in store.ReportInput) bool {
	if strings.TrimSpace(in.OcrText) != "" || strings.TrimSpace(in.OcrLocation) != "" || strings.TrimSpace(in.Stake) != "" {
		return true
	}
	return in.Confidence == "low" || in.Confidence == "none"
}

// relocate is the synchronous path used when there is no AMap key. It never
// calls the network. Ticket-like uploads stay pending.
func (s *Server) relocate(in *store.ReportInput) {
	if !needsAutoReview(*in) {
		return
	}
	in.ForceReview = true
	in.ReviewResult = "pending"
	in.ReviewReason = "未配置地图密钥，已保留上报坐标"
	if in.Confidence == "" || in.Confidence == "exact" {
		in.Confidence = "low"
	}
}

func (s *Server) startRelocator() {
	n := s.relocQueue
	if n <= 0 {
		n = 64
	}
	s.jobs = make(chan relocJob, n)
	if s.locate == nil {
		s.locate = s.defaultLocate
	}
	if s.relocDeadline <= 0 {
		s.relocDeadline = relocDeadline
	}
	go func() {
		for job := range s.jobs {
			s.runRelocate(job.id, job.in)
		}
	}()
}

func (s *Server) enqueueRelocate(id string, in store.ReportInput) {
	job := relocJob{id: id, in: in}
	select {
	case s.jobs <- job:
	default:
		if err := s.db.RecordReview(id, "pending", "重定位队列已满，已保留上报坐标"); err != nil {
			log.Printf("重定位 %s 队列已满，记录失败: %v", id, err)
		}
	}
}

func (s *Server) defaultLocate(ctx context.Context, in store.ReportInput) recog.Result {
	text := in.OcrText
	if text == "" {
		text = in.OcrLocation
	}
	loc := in.OcrLocation
	if loc == "" {
		loc = text
	}
	return recog.Locate(loc, text, "", &recog.Client{
		Key:  s.amapKey,
		HTTP: &http.Client{Timeout: recog.AMapTimeout},
		Ctx:  ctx,
	})
}

func (s *Server) runRelocate(id string, in store.ReportInput) {
	ctx, cancel := context.WithTimeout(context.Background(), s.relocDeadline)
	defer cancel()
	hit := s.locate(ctx, in)
	decision := decideReview(hit, ctx.Err() != nil)
	if !decision.pass {
		if err := s.db.RecordReview(id, "pending", decision.reason); err != nil {
			log.Printf("重定位 %s 待审记录失败: %v", id, err)
			return
		}
		log.Printf("重定位 %s 待审：%s", id, decision.reason)
		return
	}
	updated := applyHit(in, hit)
	updated.ReviewResult = "pass"
	updated.ReviewReason = decision.reason
	updated.ForceReview = false
	res, err := s.db.ApplyRelocation(id, updated)
	if err != nil {
		log.Printf("重定位 %s 写入失败: %v", id, err)
		return
	}
	log.Printf("重定位 %s -> %s", id, res.Status)
}

type reviewDecision struct {
	pass   bool
	reason string
}

// decideReview publishes only a fully confident hit whose checks agree.
// A timeout, an unconfirmed tunnel, or a low-confidence partial keeps the
// coordinates the client sent.
func decideReview(hit recog.Result, timedOut bool) reviewDecision {
	if timedOut {
		return reviewDecision{reason: "定位超时，已保留上报坐标"}
	}
	if hit.Method == "tunnel-portal" && !strings.Contains(hit.How, "已核对") {
		return reviewDecision{reason: "隧道洞口尚未核对到 150 米内，已保留上报坐标"}
	}
	if !hit.HasPoint || hit.Level != "exact" {
		return reviewDecision{reason: "定位结果不一致或置信度不足，已保留上报坐标"}
	}
	if hit.Method == "tunnel-portal" {
		return reviewDecision{pass: true, reason: "驾车路线与 OSM 洞口在 150 米内一致"}
	}
	return reviewDecision{pass: true, reason: "道路编号、公里桩与高德结果在 150 米内一致"}
}

func applyHit(in store.ReportInput, hit recog.Result) store.ReportInput {
	if blob, err := json.Marshal(hit.Parsed); err == nil {
		in.ParsedJSON = string(blob)
	}
	if hit.Parsed.Norm != "" && (hit.Parsed.Range || len(hit.Parsed.Stakes) > 0) {
		in.OcrLocation = hit.Parsed.Norm
	}
	if hit.Road != "" {
		in.RoadCode = hit.Road
	}
	if hit.RoadName != "" {
		in.RoadName = hit.RoadName
	}
	if hit.Direction != "" {
		in.Direction = hit.Direction
	}
	if hit.HasStake {
		in.Stake = hit.StakeLabel()
	}
	if hit.HasEnd {
		in.HasEnd = true
		in.EndLatitude = hit.EndLat
		in.EndLongitude = hit.EndLon
	}
	if hit.HasPoint {
		in.Latitude = hit.Lat
		in.Longitude = hit.Lon
		in.Unlocated = false
		if hit.Addr != "" {
			in.AmapName = hit.Addr
		}
		if hit.HasStake {
			label := strings.TrimSpace(strings.TrimSpace(hit.Road) + " " + hit.StakeLabel())
			bare := in.Name == "" || in.Name == "未定位违法点" || strings.HasPrefix(in.Name, "level=") || !strings.Contains(in.Name, "K")
			if label != "" && bare {
				in.Name = label
			}
		} else if in.Name == "" || in.Name == "未定位违法点" {
			in.Name = hit.How
		}
		in.Confidence = hit.Level
		in.ForceReview = hit.Level != "exact"
		return in
	}
	in.ForceReview = true
	if in.Unlocated {
		in.Confidence = "none"
	} else if in.Confidence == "" || in.Confidence == "exact" {
		in.Confidence = "low"
	}
	return in
}

// exactStake is the only result allowed to reopen a rejected upload.
// A village or county geocode, ticket chrome, and an unresolved road stay put.
func exactStake(hit recog.Result) bool {
	if !hit.HasPoint || hit.Level != "exact" || !hit.HasStake {
		return false
	}
	switch hit.Method {
	case "stake", "overlay-stake", "glued-stake":
		return true
	default:
		return false
	}
}

func inputFromReport(rep store.AdminReport) store.ReportInput {
	in := store.ReportInput{
		Name: rep.Name, Type: rep.Type, RoadCode: rep.RoadCode, RoadName: rep.RoadName,
		Notes: rep.Notes, Violation: rep.Violation, Latitude: rep.Latitude, Longitude: rep.Longitude,
		FineYuan: rep.FineYuan, PenaltyPoints: rep.PenaltyPoints,
		OcrLocation: rep.OcrLocation, AmapName: rep.AmapName, OcrText: rep.OcrText,
		ParsedJSON: rep.ParsedJSON, Confidence: rep.Confidence, Stake: rep.Stake, Direction: rep.Direction,
	}
	if rep.EndLatitude != nil && rep.EndLongitude != nil {
		in.HasEnd = true
		in.EndLatitude = *rep.EndLatitude
		in.EndLongitude = *rep.EndLongitude
	}
	if !rep.Located {
		in.Unlocated = true
	}
	return in
}

type reprocessItem struct {
	ID       string `json:"id"`
	Status   string `json:"status,omitempty"`
	Reason   string `json:"reason,omitempty"`
	RoadCode string `json:"road_code,omitempty"`
	Stake    string `json:"stake,omitempty"`
	Name     string `json:"name,omitempty"`
}

// reprocessReports runs the current locator on pending and rejected tickets.
// Reject deletes the photo, so this uses the stored OCR text. Only an exact
// kilometre-stake hit is written and published. Everything else is left as it was.
func (s *Server) reprocessReports(w http.ResponseWriter, r *http.Request) {
	approved, skipped, err := s.runReprocess(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approved": approved, "skipped": skipped})
}

func (s *Server) startReprocess() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		approved, skipped, err := s.runReprocess(ctx)
		if err != nil {
			log.Printf("重新识别已有罚单失败: %v", err)
			return
		}
		log.Printf("重新识别已有罚单：通过 %d 条，跳过 %d 条", len(approved), len(skipped))
	}()
}

func (s *Server) runReprocess(ctx context.Context) (approved, skipped []reprocessItem, err error) {
	candidates, err := s.db.ReprocessCandidates()
	if err != nil {
		return nil, nil, err
	}
	itemDeadline := s.relocDeadline
	if itemDeadline <= 0 {
		itemDeadline = relocDeadline
	}
	approved = []reprocessItem{}
	skipped = []reprocessItem{}
	for _, rep := range candidates {
		if ctx.Err() != nil {
			skipped = append(skipped, reprocessItem{ID: rep.ID, Status: rep.Status, Reason: "定位超时"})
			continue
		}
		itemCtx, cancel := context.WithTimeout(ctx, itemDeadline)
		hit := s.locate(itemCtx, inputFromReport(rep))
		cancel()
		if !exactStake(hit) {
			reason := "不是已确认的公里桩"
			switch {
			case hit.Method == "invalid" || hit.Method == "geocode" || strings.HasPrefix(hit.How, "level="):
				reason = "行政级别地理编码或界面文字不能当作桩号"
			case hit.How != "":
				reason = hit.How
			}
			skipped = append(skipped, reprocessItem{ID: rep.ID, Status: rep.Status, Reason: reason})
			continue
		}
		updated := applyHit(inputFromReport(rep), hit)
		updated.Type = rep.Type
		updated.ReviewResult = "pass"
		updated.ReviewReason = "重新识别到公里桩，已通过"
		updated.ForceReview = false
		res, pubErr := s.db.PublishCorrected(rep.ID, updated)
		if pubErr != nil {
			skipped = append(skipped, reprocessItem{ID: rep.ID, Status: rep.Status, Reason: "写入失败"})
			continue
		}
		approved = append(approved, reprocessItem{
			ID: rep.ID, Status: res.Status, RoadCode: updated.RoadCode, Stake: updated.Stake, Name: updated.Name,
		})
	}
	return approved, skipped, nil
}
