package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"motocam/server/internal/appdist"
	"motocam/server/internal/imageutil"
	"motocam/server/internal/mail"
	"motocam/server/internal/recog"
	"motocam/server/internal/store"
)

type Server struct {
	adminToken    string
	maxImageBytes int
	db            *store.Store
	apps          *appdist.Catalog
	publicBase    string
	auth          authSettings
	amapKey       string
	locate        func(context.Context, store.ReportInput) recog.Result
	relocDeadline time.Duration
	relocQueue    int
	jobs          chan relocJob
}

type HandlerOptions struct {
	AdminToken    string
	MaxImage      int
	DB            *store.Store
	Apps          *appdist.Catalog
	PublicBase    string
	DevMode       bool
	ReviewEmail   string
	ReviewCode    string
	TermsPath     string
	TermsVersion  string
	Mail          mail.Config
	AmapKey       string
	Locate        func(context.Context, store.ReportInput) recog.Result
	RelocDeadline time.Duration
	RelocQueue    int
}

func New(adminToken string, maxImageBytes int, db *store.Store) http.Handler {
	return NewWithOptions(HandlerOptions{
		AdminToken: adminToken,
		MaxImage:   maxImageBytes,
		DB:         db,
		DevMode:    true,
	})
}

func NewWithApps(adminToken string, maxImageBytes int, db *store.Store, apps *appdist.Catalog, publicBase string) http.Handler {
	return NewWithOptions(HandlerOptions{
		AdminToken: adminToken,
		MaxImage:   maxImageBytes,
		DB:         db,
		Apps:       apps,
		PublicBase: publicBase,
	})
}

func NewWithOptions(opts HandlerOptions) http.Handler {
	s := &Server{
		adminToken:    opts.AdminToken,
		maxImageBytes: opts.MaxImage,
		db:            opts.DB,
		apps:          opts.Apps,
		publicBase:    strings.TrimRight(strings.TrimSpace(opts.PublicBase), "/"),
		auth: authSettings{
			dev:          opts.DevMode,
			reviewEmail:  strings.ToLower(strings.TrimSpace(opts.ReviewEmail)),
			reviewCode:   strings.TrimSpace(opts.ReviewCode),
			termsPath:    opts.TermsPath,
			termsVersion: strings.TrimSpace(opts.TermsVersion),
			mail:         opts.Mail,
		},
		amapKey:       strings.TrimSpace(opts.AmapKey),
		locate:        opts.Locate,
		relocDeadline: opts.RelocDeadline,
		relocQueue:    opts.RelocQueue,
	}
	s.startRelocator()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /privacy", s.privacyPage)
	mux.HandleFunc("GET /terms", s.termsPage)
	mux.HandleFunc("GET /support", s.supportPage)
	mux.HandleFunc("POST /v1/auth/request-code", s.requestCode)
	mux.HandleFunc("POST /v1/auth/verify", s.verifyCode)
	mux.HandleFunc("POST /v1/auth/logout", s.logout)
	mux.HandleFunc("GET /v1/me", s.me)
	mux.HandleFunc("DELETE /v1/me", s.deleteMe)
	mux.HandleFunc("GET /v1/legal/terms", s.terms)
	mux.HandleFunc("POST /v1/legal/accept", s.acceptTerms)
	mux.HandleFunc("POST /v1/reports", s.submit)
	mux.HandleFunc("GET /v1/points", s.list)
	mux.HandleFunc("GET /v1/app/latest", s.latestApp)
	mux.HandleFunc("GET /v1/app/download/{platform}/{file}", s.downloadApp)
	mux.HandleFunc("GET /v1/app/ios/manifest.plist", s.iosManifest)
	mux.HandleFunc("GET /v1/admin/reports", s.admin(s.adminList))
	mux.HandleFunc("GET /v1/admin/points", s.admin(s.adminPoints))
	mux.HandleFunc("GET /v1/admin/reports/{id}/image", s.admin(s.adminImage))
	mux.HandleFunc("POST /v1/admin/reports/{id}/approve", s.admin(s.approve))
	mux.HandleFunc("POST /v1/admin/reports/{id}/reject", s.admin(s.reject))
	mux.HandleFunc("POST /v1/admin/reports/{id}", s.admin(s.editReport))
	mux.HandleFunc("POST /v1/admin/reports/merge", s.admin(s.merge))
	mux.HandleFunc("POST /v1/admin/points/{id}/segment", s.admin(s.makeSegment))
	mux.HandleFunc("DELETE /v1/admin/points/{id}", s.admin(s.deletePoint))
	mux.HandleFunc("POST /v1/admin/app", s.admin(s.publishApp))
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) submit(w http.ResponseWriter, r *http.Request) {
	// Builds before 30 (or no X-App-Build) upload without an account, as in
	// build 28. Build 30+ must log in. A token that is sent is always checked
	// and its user is recorded on the report.
	user, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(s.maxImageBytes)+1<<20)
	input, err := s.readReport(r)
	if err != nil {
		writeStored(w, err)
		return
	}
	input.DeviceID = strings.TrimSpace(r.Header.Get("X-Device-Id"))
	input.UserID = user.ID
	if needsAutoReview(input) && s.amapKey != "" {
		input.ForceReview = true
		result, err := s.db.Submit(input)
		if err != nil {
			writeStored(w, err)
			return
		}
		s.enqueueRelocate(result.ID, input)
		writeJSON(w, http.StatusCreated, map[string]any{"id": result.ID, "status": "pending"})
		return
	}
	s.relocate(&input)
	result, err := s.db.Submit(input)
	if err != nil {
		writeStored(w, err)
		return
	}
	body := map[string]any{"id": result.ID, "status": result.Status}
	if result.PublicPointID != "" {
		body["public_point_id"] = result.PublicPointID
	}
	writeJSON(w, http.StatusCreated, body)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, true); !ok {
		return
	}
	q := r.URL.Query()
	var minLat, minLon, maxLat, maxLon *float64
	bboxKeys := []string{"min_lat", "min_lon", "max_lat", "max_lon"}
	present := 0
	for _, key := range bboxKeys {
		if q.Get(key) != "" {
			present++
		}
	}
	if present != 0 && present != 4 {
		writeError(w, http.StatusBadRequest, "范围需要同时提供 min_lat、min_lon、max_lat、max_lon")
		return
	}
	if present == 4 {
		vals := make([]float64, 4)
		for i, key := range bboxKeys {
			n, err := strconv.ParseFloat(q.Get(key), 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "范围坐标无效")
				return
			}
			vals[i] = n
		}
		if vals[0] > vals[2] || vals[1] > vals[3] {
			writeError(w, http.StatusBadRequest, "范围坐标无效")
			return
		}
		minLat, minLon, maxLat, maxLon = &vals[0], &vals[1], &vals[2], &vals[3]
	}
	var since *time.Time
	if raw := q.Get("since"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "since 需要 RFC3339 时间")
			return
		}
		since = &parsed
	}
	limit := 0
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit 无效")
			return
		}
		limit = n
	}
	result, err := s.db.List(minLat, minLon, maxLat, maxLon, since, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取失败")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) adminPoints(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	points, err := s.db.AdminPoints(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"points": points})
}

func (s *Server) adminList(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	switch status {
	case "", "pending", "merged", "rejected", "archived":
	default:
		writeError(w, http.StatusBadRequest, "状态无效")
		return
	}
	if status == "" {
		status = "pending"
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	reports, err := s.db.AdminList(status, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports})
}

func (s *Server) editReport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         *string  `json:"name"`
		RoadCode     *string  `json:"road_code"`
		RoadName     *string  `json:"road_name"`
		Stake        *string  `json:"stake"`
		Direction    *string  `json:"direction"`
		Latitude     *float64 `json:"latitude"`
		Longitude    *float64 `json:"longitude"`
		EndLatitude  *float64 `json:"end_latitude"`
		EndLongitude *float64 `json:"end_longitude"`
		ClearEnd     bool     `json:"clear_end"`
		Confidence   *string  `json:"confidence"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体无效")
		return
	}
	err := s.db.EditReport(r.PathValue("id"), store.ReportEdit{
		Name: body.Name, RoadCode: body.RoadCode, RoadName: body.RoadName,
		Stake: body.Stake, Direction: body.Direction,
		Latitude: body.Latitude, Longitude: body.Longitude,
		EndLatitude: body.EndLatitude, EndLongitude: body.EndLongitude,
		ClearEnd: body.ClearEnd, Confidence: body.Confidence,
	})
	if err != nil {
		writeStored(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Approve(r.PathValue("id")); err != nil {
		writeStored(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) reject(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Reject(r.PathValue("id")); err != nil {
		writeStored(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) merge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ReportIDs []string `json:"report_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体无效")
		return
	}
	id, err := s.db.Merge(body.ReportIDs)
	if err != nil {
		writeStored(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"public_point_id": id})
}

func (s *Server) deletePoint(w http.ResponseWriter, r *http.Request) {
	if err := s.db.DeletePoint(r.PathValue("id")); err != nil {
		writeStored(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) makeSegment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EndPointID   string   `json:"end_point_id"`
		EndLatitude  *float64 `json:"end_latitude"`
		EndLongitude *float64 `json:"end_longitude"`
		Stake        string   `json:"stake"`
		Direction    string   `json:"direction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体无效")
		return
	}
	seg := store.SegmentInput{EndPointID: body.EndPointID, Stake: body.Stake, Direction: body.Direction}
	if body.EndLatitude != nil && body.EndLongitude != nil {
		seg.HasCoord = true
		seg.EndLat = *body.EndLatitude
		seg.EndLon = *body.EndLongitude
	}
	if err := s.db.MakeSegment(r.PathValue("id"), seg); err != nil {
		writeStored(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminImage(w http.ResponseWriter, r *http.Request) {
	path, err := s.db.ImagePath(r.PathValue("id"))
	if err != nil {
		writeStored(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, path)
}

func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token := strings.TrimPrefix(header, "Bearer ")
		if header == token || s.adminToken == "" || subtleCompare(token, s.adminToken) != 1 {
			writeError(w, http.StatusUnauthorized, "未授权")
			return
		}
		next(w, r)
	}
}

func (s *Server) readReport(r *http.Request) (store.ReportInput, error) {
	content := r.Header.Get("Content-Type")
	if strings.HasPrefix(content, "multipart/form-data") {
		return s.readMultipart(r)
	}
	var body struct {
		Name          string   `json:"name"`
		Type          string   `json:"type"`
		RoadCode      string   `json:"road_code"`
		RoadName      string   `json:"road_name"`
		Notes         string   `json:"notes"`
		Latitude      *float64 `json:"latitude"`
		Longitude     *float64 `json:"longitude"`
		FineYuan      *int     `json:"fine_yuan"`
		PenaltyPoints *int     `json:"penalty_points"`
		Violation     string   `json:"violation"`
		OcrLocation   string   `json:"ocr_location"`
		AmapName      string   `json:"amap_name"`
		Located       *bool    `json:"located"`
		OcrText       string   `json:"ocr_text"`
		Parsed        string   `json:"parsed"`
		Confidence    string   `json:"confidence"`
		EndLatitude   *float64 `json:"end_latitude"`
		EndLongitude  *float64 `json:"end_longitude"`
		Stake         string   `json:"stake"`
		Direction     string   `json:"direction"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&body); err != nil {
		return store.ReportInput{}, store.InputError{Message: "请求体无效"}
	}
	input := store.ReportInput{
		Name: body.Name, Type: body.Type, RoadCode: body.RoadCode, RoadName: body.RoadName,
		Notes: body.Notes, FineYuan: body.FineYuan, PenaltyPoints: body.PenaltyPoints,
		Violation: body.Violation, OcrLocation: body.OcrLocation, AmapName: body.AmapName,
		OcrText: body.OcrText, ParsedJSON: body.Parsed, Confidence: body.Confidence,
		Stake: body.Stake, Direction: body.Direction,
	}
	if body.EndLatitude != nil && body.EndLongitude != nil {
		input.HasEnd = true
		input.EndLatitude = *body.EndLatitude
		input.EndLongitude = *body.EndLongitude
	}
	if body.Located != nil && !*body.Located {
		input.Unlocated = true
		return input, nil
	}
	if body.Latitude == nil || body.Longitude == nil {
		return store.ReportInput{}, store.InputError{Message: "坐标无效"}
	}
	input.Latitude = *body.Latitude
	input.Longitude = *body.Longitude
	return input, nil
}

func (s *Server) readMultipart(r *http.Request) (store.ReportInput, error) {
	if err := r.ParseMultipartForm(int64(s.maxImageBytes) + 1<<20); err != nil {
		return store.ReportInput{}, store.InputError{Message: "请求体过大或无法解析"}
	}
	input := store.ReportInput{
		Name:        r.FormValue("name"),
		Type:        r.FormValue("type"),
		RoadCode:    r.FormValue("road_code"),
		RoadName:    r.FormValue("road_name"),
		Notes:       r.FormValue("notes"),
		Violation:   r.FormValue("violation"),
		OcrLocation: r.FormValue("ocr_location"),
		AmapName:    r.FormValue("amap_name"),
		OcrText:     r.FormValue("ocr_text"),
		ParsedJSON:  r.FormValue("parsed"),
		Confidence:  r.FormValue("confidence"),
		Stake:       r.FormValue("stake"),
		Direction:   r.FormValue("direction"),
	}
	if endLat, err1 := strconv.ParseFloat(r.FormValue("end_latitude"), 64); err1 == nil {
		if endLon, err2 := strconv.ParseFloat(r.FormValue("end_longitude"), 64); err2 == nil {
			input.HasEnd = true
			input.EndLatitude = endLat
			input.EndLongitude = endLon
		}
	}
	locatedRaw := strings.TrimSpace(r.FormValue("located"))
	if locatedRaw == "0" || strings.EqualFold(locatedRaw, "false") {
		input.Unlocated = true
	} else {
		lat, err1 := strconv.ParseFloat(r.FormValue("latitude"), 64)
		lon, err2 := strconv.ParseFloat(r.FormValue("longitude"), 64)
		if err1 != nil || err2 != nil {
			return store.ReportInput{}, store.InputError{Message: "坐标无效"}
		}
		input.Latitude = lat
		input.Longitude = lon
	}
	if raw := r.FormValue("fine_yuan"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return store.ReportInput{}, store.InputError{Message: "罚款金额无效"}
		}
		input.FineYuan = &n
	}
	if raw := r.FormValue("penalty_points"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return store.ReportInput{}, store.InputError{Message: "记分无效"}
		}
		input.PenaltyPoints = &n
	}
	file, _, err := r.FormFile("image")
	if err == nil {
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, int64(s.maxImageBytes)+1))
		if err != nil {
			return store.ReportInput{}, store.InputError{Message: "图片无法读取"}
		}
		stripped, err := imageutil.Strip(data, s.maxImageBytes)
		if err != nil {
			if errors.Is(err, imageutil.ErrTooLarge) {
				return store.ReportInput{}, store.InputError{Message: "图片过大"}
			}
			return store.ReportInput{}, store.InputError{Message: "仅支持 JPEG 和 PNG，且需要能正常解码"}
		}
		input.Image = stripped
	}
	return input, nil
}

func writeStored(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrRateLimited):
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusTooManyRequests, "请求过于频繁，请稍后再试")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "记录不存在")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "当前状态不能这样处理")
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrDisabled):
		writeError(w, http.StatusForbidden, "账号已停用")
	default:
		writeError(w, http.StatusInternalServerError, "服务器错误")
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func subtleCompare(got, want string) int {
	if len(got) != len(want) {
		return 0
	}
	diff := 0
	for i := 0; i < len(got); i++ {
		diff |= int(got[i] ^ want[i])
	}
	if diff == 0 {
		return 1
	}
	return 0
}
