package asc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenUnconfigured(t *testing.T) {
	if Open("", "", "", "") != nil || Open("kid", "iss", "key.p8", "") != nil {
		t.Fatal("partial ASC config returned a cache")
	}
	var none *Cache
	if res := none.Get(context.Background()); res.Configured || res.Summary != nil {
		t.Fatalf("nil cache %+v", res)
	}
	missing := Open("kid", "iss", filepath.Join(t.TempDir(), "missing.p8"), "1")
	if missing == nil {
		t.Fatal("complete config did not open")
	}
	res := missing.Get(context.Background())
	if !res.Configured || !res.Failed || res.Summary != nil {
		t.Fatalf("missing key %+v", res)
	}
}

func TestClientReadsFixtureAndRefusesGuesses(t *testing.T) {
	keyPath := writeTestKey(t)
	var sawKid bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
		if len(parts) != 3 {
			http.Error(w, "jwt", http.StatusUnauthorized)
			return
		}
		hdr, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil || !strings.Contains(string(hdr), `"kid":"TESTKEY"`) || !strings.Contains(string(hdr), `"alg":"ES256"`) {
			http.Error(w, "header", http.StatusUnauthorized)
			return
		}
		sawKid = true
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/betaGroups") && !strings.Contains(r.URL.Path, "/betaTesters") && !strings.Contains(r.URL.Path, "/metrics"):
			_, _ = w.Write([]byte(`{"data":[
				{"type":"betaGroups","id":"g1","attributes":{"name":"1组","isInternalGroup":false,"publicLinkEnabled":true,"publicLinkLimitEnabled":true,"publicLinkLimit":10}},
				{"type":"betaGroups","id":"g2","attributes":{"name":"内部","isInternalGroup":true,"publicLinkEnabled":false}}
			]}`))
		case r.URL.Path == "/v1/betaGroups/g1/betaTesters":
			_, _ = w.Write([]byte(`{"data":[
				{"type":"betaTesters","id":"t1","attributes":{"state":"INSTALLED"}},
				{"type":"betaTesters","id":"t2","attributes":{"state":"INSTALLED"}},
				{"type":"betaTesters","id":"t3","attributes":{"state":"ACCEPTED"}}
			],"meta":{"paging":{"total":3}}}`))
		case r.URL.Path == "/v1/betaGroups/g2/betaTesters":
			_, _ = w.Write([]byte(`{"data":[],"meta":{"paging":{"total":0}}}`))
		case r.URL.Path == "/v1/betaGroups/g1/metrics/publicLinkUsages":
			_, _ = w.Write([]byte(`{"data":[{"type":"betaPublicLinkUsages","attributes":{"viewCount":11}}]}`))
		case strings.HasPrefix(r.URL.Path, "/v1/builds") && !strings.Contains(r.URL.Path, "/individualTesters"):
			_, _ = w.Write([]byte(`{"data":[{"type":"builds","id":"b1","attributes":{"version":"34","processingState":"VALID","expired":false},"relationships":{"preReleaseVersion":{"data":{"type":"preReleaseVersions","id":"pv1"}}}}],"included":[{"type":"preReleaseVersions","id":"pv1","attributes":{"version":"1.0"}}]}`))
		case r.URL.Path == "/v1/builds/b1/individualTesters":
			_, _ = w.Write([]byte(`{"data":[
				{"type":"betaTesters","id":"t1","attributes":{"state":"INSTALLED"}},
				{"type":"betaTesters","id":"t2","attributes":{"state":"INSTALLED"}}
			]}`))
		case strings.HasPrefix(r.URL.Path, "/v1/apps/123/appStoreVersions"):
			_, _ = w.Write([]byte(`{"data":[{"type":"appStoreVersions","id":"v1","attributes":{"versionString":"1.0","appStoreState":"WAITING_FOR_REVIEW"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{keyID: "TESTKEY", issuerID: "issuer-test", keyPath: keyPath, appID: "123", base: srv.URL, hc: srv.Client()}
	sum, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !sawKid || sum.Testers != 3 || sum.Installed != 2 || sum.Accepted != 1 || sum.LatestBuild != "34" {
		t.Fatalf("summary %+v", sum)
	}
	if len(sum.Groups) != 2 || sum.Groups[0].Testers != 3 || !sum.Groups[0].LinkMetrics || sum.Groups[0].LinkViews != 11 || !sum.Groups[1].Internal {
		t.Fatalf("groups %+v", sum.Groups)
	}
	if len(sum.Builds) != 1 || sum.Builds[0].Testers != 2 || sum.Builds[0].Version != "1.0" {
		t.Fatalf("builds %+v", sum.Builds)
	}
	if sum.Review == nil || sum.Review.State != "WAITING_FOR_REVIEW" || StateName(sum.Review.State) != "等待审核" {
		t.Fatalf("review %+v", sum.Review)
	}
	if _, _, err := c.get(context.Background(), "https://evil.example/v1/betaGroups"); err == nil {
		t.Fatal("off-host url was fetched")
	}

	// No tester state: do not invent install counts.
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/betaTesters"):
			_, _ = w.Write([]byte(`{"data":[{"type":"betaTesters","id":"t1","attributes":{"inviteType":"PUBLIC_LINK"}}]}`))
		case strings.Contains(r.URL.Path, "/individualTesters"):
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/v1/betaGroups"):
			_, _ = w.Write([]byte(`{"data":[{"type":"betaGroups","id":"g1","attributes":{"name":"1组"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer plain.Close()
	c.base = plain.URL
	c.hc = plain.Client()
	if _, err := c.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "tester state") {
		t.Fatalf("missing state err %v", err)
	}
}

func writeTestKey(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "AuthKey.p8")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
