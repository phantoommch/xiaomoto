package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"motocam/server/internal/appdist"
)

const iosBundleID = "com.example.xiaomoto"
const iosTitle = "小moto"

func (s *Server) latestApp(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, true); !ok {
		return
	}
	platform := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("platform")))
	if platform != "android" && platform != "ios" {
		writeError(w, http.StatusBadRequest, "platform 只能是 android 或 ios")
		return
	}
	if s.apps == nil {
		writeError(w, http.StatusNotFound, "还没有发布")
		return
	}
	rel, ok := s.apps.Latest(platform)
	if !ok {
		writeError(w, http.StatusNotFound, "还没有发布")
		return
	}
	writeJSON(w, http.StatusOK, publicRelease(s.publicPrefix(r), rel))
}

func (s *Server) publishApp(w http.ResponseWriter, r *http.Request) {
	if s.apps == nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, appdist.MaxUploadBytes+1<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "请求体过大或无法解析")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "缺少安装包")
		return
	}
	defer file.Close()
	versionCode, err := strconv.Atoi(strings.TrimSpace(r.FormValue("versionCode")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "versionCode 无效")
		return
	}
	name := ""
	if header != nil {
		name = header.Filename
	}
	rel, err := s.apps.Publish(
		r.FormValue("platform"),
		versionCode,
		r.FormValue("versionName"),
		r.FormValue("notes"),
		name,
		parseForce(r.FormValue("force")),
		file,
	)
	if err != nil {
		var input appdist.InputError
		if errors.As(err, &input) {
			writeError(w, http.StatusBadRequest, input.Message)
			return
		}
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}
	writeJSON(w, http.StatusCreated, publicRelease(s.publicPrefix(r), rel))
}

func (s *Server) downloadApp(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, true); !ok {
		return
	}
	if s.apps == nil {
		writeError(w, http.StatusNotFound, "还没有发布")
		return
	}
	platform := r.PathValue("platform")
	name := r.PathValue("file")
	path, err := s.apps.FilePath(platform, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "还没有发布")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "还没有发布")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusNotFound, "还没有发布")
		return
	}
	w.Header().Set("Content-Type", appContentType(platform))
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func (s *Server) iosManifest(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, true); !ok {
		return
	}
	if s.apps == nil {
		writeError(w, http.StatusNotFound, "还没有发布")
		return
	}
	rel, ok := s.apps.Latest("ios")
	if !ok {
		writeError(w, http.StatusNotFound, "还没有发布")
		return
	}
	ipa := s.publicPrefix(r) + "/v1/app/download/ios/" + rel.File
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>items</key>
	<array>
		<dict>
			<key>assets</key>
			<array>
				<dict>
					<key>kind</key>
					<string>software-package</string>
					<key>url</key>
					<string>%s</string>
				</dict>
			</array>
			<key>metadata</key>
			<dict>
				<key>bundle-identifier</key>
				<string>%s</string>
				<key>bundle-version</key>
				<string>%s</string>
				<key>kind</key>
				<string>software</string>
				<key>title</key>
				<string>%s</string>
			</dict>
		</dict>
	</array>
</dict>
</plist>
`, xmlEscape(ipa), xmlEscape(iosBundleID), xmlEscape(rel.VersionName), xmlEscape(iosTitle))
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func publicRelease(prefix string, rel appdist.Release) map[string]any {
	return map[string]any{
		"versionCode": rel.VersionCode,
		"versionName": rel.VersionName,
		"notes":       rel.Notes,
		"url":         prefix + "/v1/app/download/" + rel.Platform + "/" + rel.File,
		"sha256":      rel.SHA256,
		"size":        rel.Size,
		"force":       rel.Force,
	}
}

func (s *Server) publicPrefix(r *http.Request) string {
	if s.publicBase != "" {
		return s.publicBase
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := forwardedProto(r); proto != "" {
		scheme = proto
	}
	return scheme + "://" + r.Host
}

func forwardedProto(r *http.Request) string {
	raw := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if raw == "" {
		return ""
	}
	proto := strings.ToLower(strings.TrimSpace(strings.Split(raw, ",")[0]))
	if proto == "http" || proto == "https" {
		return proto
	}
	return ""
}

func appContentType(platform string) string {
	if strings.EqualFold(platform, "android") {
		return "application/vnd.android.package-archive"
	}
	return "application/octet-stream"
}

func parseForce(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func xmlEscape(value string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	).Replace(value)
}
