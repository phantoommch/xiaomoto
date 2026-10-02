package appdist

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	KeepVersions   = 3
	MaxUploadBytes = 200 << 20
)

type Release struct {
	Platform    string `json:"platform"`
	VersionCode int    `json:"versionCode"`
	VersionName string `json:"versionName"`
	Notes       string `json:"notes"`
	File        string `json:"file"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	Force       bool   `json:"force"`
	PublishedAt string `json:"publishedAt"`
}

type InputError struct{ Message string }

func (e InputError) Error() string { return e.Message }

type Catalog struct {
	dir      string
	mu       sync.Mutex
	releases []Release
}

func Open(dir string) (*Catalog, error) {
	for _, platform := range []string{"android", "ios"} {
		if err := os.MkdirAll(filepath.Join(dir, platform), 0o755); err != nil {
			return nil, err
		}
	}
	catalog := &Catalog{dir: dir}
	path := filepath.Join(dir, "releases.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return catalog, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return catalog, nil
	}
	if err := json.Unmarshal(raw, &catalog.releases); err != nil {
		return nil, err
	}
	return catalog, nil
}

func (c *Catalog) Latest(platform string) (Release, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var best *Release
	for i := range c.releases {
		rel := &c.releases[i]
		if rel.Platform != platform {
			continue
		}
		if best == nil || rel.VersionCode > best.VersionCode || (rel.VersionCode == best.VersionCode && rel.PublishedAt > best.PublishedAt) {
			best = rel
		}
	}
	if best == nil {
		return Release{}, false
	}
	return *best, true
}

func (c *Catalog) Publish(platform string, versionCode int, versionName, notes, originalName string, force bool, body io.Reader) (Release, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform != "android" && platform != "ios" {
		return Release{}, InputError{Message: "platform 只能是 android 或 ios"}
	}
	if versionCode < 1 {
		return Release{}, InputError{Message: "versionCode 无效"}
	}
	versionName = strings.TrimSpace(versionName)
	if versionName == "" || utf8.RuneCountInString(versionName) > 40 {
		return Release{}, InputError{Message: "versionName 无效"}
	}
	notes = trimRunes(strings.TrimSpace(notes), 4000)
	name := uniqueSafeName(originalName, platform, versionCode)

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, rel := range c.releases {
		if rel.Platform == platform && rel.VersionCode != versionCode && rel.File == name {
			name = disambiguate(name, versionCode)
			break
		}
	}

	dir := filepath.Join(c.dir, platform)
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return Release{}, err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	hash := sha256.New()
	written, err := io.Copy(tmp, io.TeeReader(io.LimitReader(body, MaxUploadBytes+1), hash))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Release{}, err
	}
	if written > MaxUploadBytes {
		return Release{}, InputError{Message: "安装包过大"}
	}
	if written == 0 {
		return Release{}, InputError{Message: "安装包是空的"}
	}

	dest := filepath.Join(dir, name)
	if err := os.Rename(tmpName, dest); err != nil {
		return Release{}, err
	}
	cleanup = false

	rel := Release{
		Platform:    platform,
		VersionCode: versionCode,
		VersionName: versionName,
		Notes:       notes,
		File:        name,
		SHA256:      hex.EncodeToString(hash.Sum(nil)),
		Size:        written,
		Force:       force,
		PublishedAt: time.Now().UTC().Format(time.RFC3339),
	}
	next := make([]Release, 0, len(c.releases)+1)
	for _, old := range c.releases {
		if old.Platform == platform && old.VersionCode == versionCode {
			if old.File != name {
				_ = os.Remove(filepath.Join(dir, old.File))
			}
			continue
		}
		next = append(next, old)
	}
	next = append(next, rel)
	c.releases = next
	c.trimLocked(platform)
	if err := c.saveLocked(); err != nil {
		return Release{}, err
	}
	for _, saved := range c.releases {
		if saved.Platform == platform && saved.VersionCode == versionCode && saved.File == name {
			return saved, nil
		}
	}
	return rel, nil
}

func (c *Catalog) FilePath(platform, name string) (string, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform != "android" && platform != "ios" {
		return "", os.ErrNotExist
	}
	if !safeFile(name) {
		return "", os.ErrNotExist
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, rel := range c.releases {
		if rel.Platform == platform && rel.File == name {
			return filepath.Join(c.dir, platform, name), nil
		}
	}
	return "", os.ErrNotExist
}

func (c *Catalog) trimLocked(platform string) {
	var mine []Release
	var rest []Release
	for _, rel := range c.releases {
		if rel.Platform == platform {
			mine = append(mine, rel)
		} else {
			rest = append(rest, rel)
		}
	}
	sort.SliceStable(mine, func(i, j int) bool {
		if mine[i].VersionCode != mine[j].VersionCode {
			return mine[i].VersionCode > mine[j].VersionCode
		}
		return mine[i].PublishedAt > mine[j].PublishedAt
	})
	if len(mine) > KeepVersions {
		for _, old := range mine[KeepVersions:] {
			_ = os.Remove(filepath.Join(c.dir, platform, old.File))
		}
		mine = mine[:KeepVersions]
	}
	kept := map[string]bool{}
	for _, rel := range mine {
		kept[rel.File] = true
	}
	entries, err := os.ReadDir(filepath.Join(c.dir, platform))
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			if !kept[entry.Name()] {
				_ = os.Remove(filepath.Join(c.dir, platform, entry.Name()))
			}
		}
	}
	c.releases = append(rest, mine...)
}

func (c *Catalog) saveLocked() error {
	raw, err := json.MarshalIndent(c.releases, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := filepath.Join(c.dir, ".releases.json.tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(c.dir, "releases.json"))
}

func safeFile(name string) bool {
	if name == "" || name != filepath.Base(name) || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func uniqueSafeName(original, platform string, versionCode int) string {
	base := filepath.Base(strings.TrimSpace(original))
	var b strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	name := strings.Trim(b.String(), ".")
	if name == "" || name == "apk" || name == "ipa" {
		ext := ".apk"
		if platform == "ios" {
			ext = ".ipa"
		}
		return fmt.Sprintf("xiaomoto-%d%s", versionCode, ext)
	}
	return name
}

func disambiguate(name string, versionCode int) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s-%d%s", stem, versionCode, ext)
}

func trimRunes(value string, max int) string {
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}
