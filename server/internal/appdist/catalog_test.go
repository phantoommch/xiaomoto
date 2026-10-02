package appdist

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishKeepsNewestThreeAndHashes(t *testing.T) {
	dir := t.TempDir()
	catalog, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("apk-bytes-v1")
	sum := sha256.Sum256(payload)
	rel, err := catalog.Publish("android", 2, "1.0", "说明", "../evil/../app debug.apk", false, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if rel.File != "appdebug.apk" && rel.File != "app debug.apk" {
		if strings.Contains(rel.File, "/") || strings.Contains(rel.File, "..") {
			t.Fatalf("unsafe name %q", rel.File)
		}
	}
	if strings.Contains(rel.File, "..") || strings.Contains(rel.File, "/") {
		t.Fatalf("unsafe name %q", rel.File)
	}
	if rel.SHA256 != hex.EncodeToString(sum[:]) || rel.Size != int64(len(payload)) {
		t.Fatalf("hash/size %+v", rel)
	}
	if _, err := os.Stat(filepath.Join(dir, "android", rel.File)); err != nil {
		t.Fatal(err)
	}
	for code := 3; code <= 5; code++ {
		body := []byte(strings.Repeat("x", code))
		if _, err := catalog.Publish("android", code, "1.0", "", "xiaomoto.apk", false, bytes.NewReader(body)); err != nil {
			t.Fatal(err)
		}
	}
	latest, ok := catalog.Latest("android")
	if !ok || latest.VersionCode != 5 {
		t.Fatalf("latest %+v %v", latest, ok)
	}
	if _, ok := catalog.Latest("ios"); ok {
		t.Fatal("ios should be empty")
	}
	kept := 0
	for _, rel := range catalog.releases {
		if rel.Platform == "android" {
			kept++
			if rel.VersionCode < 3 {
				t.Fatalf("old version kept %+v", rel)
			}
		}
	}
	if kept != KeepVersions {
		t.Fatalf("kept %d", kept)
	}
	if _, err := catalog.FilePath("android", "../releases.json"); err == nil {
		t.Fatal("traversal was accepted")
	}
	path, err := catalog.FilePath("android", latest.File)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte(strings.Repeat("x", 5))) {
		t.Fatalf("file bytes %q", got)
	}
}

func TestPublishRejectsBadInput(t *testing.T) {
	catalog, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Publish("windows", 1, "1.0", "", "a.apk", false, bytes.NewReader([]byte("x"))); err == nil {
		t.Fatal("expected platform error")
	}
	if _, err := catalog.Publish("ios", 0, "1.0", "", "a.ipa", false, bytes.NewReader([]byte("x"))); err == nil {
		t.Fatal("expected version error")
	}
	if _, err := catalog.Publish("ios", 1, "1.0", "", "a.ipa", true, bytes.NewReader(nil)); err == nil {
		t.Fatal("expected empty file error")
	}
}
