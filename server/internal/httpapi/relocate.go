package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
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
	if blob, err := json.Marshal(hit.Parsed); err == nil && in.ParsedJSON == "" {
		in.ParsedJSON = string(blob)
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
		in.Stake = fmt.Sprintf("K%d+%d", int(hit.StakeKM), hit.StakeM)
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
		if in.Name == "" || in.Name == "未定位违法点" {
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
