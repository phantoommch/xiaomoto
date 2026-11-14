package asc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"crypto/x509"
	"encoding/pem"
)

// Open returns nil unless all four App Store Connect settings are present.
// A missing or unreadable key does not fail process startup; the first read
// reports it and the admin page shows the existing failure sentence.
func Open(keyID, issuerID, keyPath, appID string) *Cache {
	keyID = strings.TrimSpace(keyID)
	issuerID = strings.TrimSpace(issuerID)
	keyPath = strings.TrimSpace(keyPath)
	appID = strings.TrimSpace(appID)
	if keyID == "" || issuerID == "" || keyPath == "" || appID == "" {
		return nil
	}
	return NewCache(&Client{
		keyID:    keyID,
		issuerID: issuerID,
		keyPath:  keyPath,
		appID:    appID,
		base:     "https://api.appstoreconnect.apple.com",
		hc:       &http.Client{Timeout: 25 * time.Second},
	})
}

// Client reads TestFlight counts from the documented App Store Connect API.
// The original stats-admin client was not in git or the handoff. This one
// refuses to fill install counts when tester state is absent, so the page
// shows「暂时取不到」instead of guessed numbers.
type Client struct {
	keyID    string
	issuerID string
	keyPath  string
	appID    string
	base     string
	hc       *http.Client
}

func (c *Client) Fetch(ctx context.Context) (Summary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if _, err := c.jwt(time.Now()); err != nil {
		return Summary{}, err
	}

	groups, _, _, err := c.listAll(ctx, "/v1/betaGroups?filter[app]="+url.QueryEscape(c.appID)+"&limit=200")
	if err != nil {
		return Summary{}, err
	}
	people := map[string]string{}
	var out Summary
	for _, g := range groups {
		attrs := g.Attributes
		item := Group{
			Name:       strAttr(attrs, "name"),
			Internal:   boolAttr(attrs, "isInternalGroup"),
			PublicLink: boolAttr(attrs, "publicLinkEnabled"),
			LimitOn:    boolAttr(attrs, "publicLinkLimitEnabled"),
			Limit:      intAttr(attrs, "publicLinkLimit"),
		}
		testers, total, _, err := c.listAll(ctx, "/v1/betaGroups/"+url.PathEscape(g.ID)+"/betaTesters?limit=200")
		if err != nil {
			return Summary{}, err
		}
		if total < len(testers) {
			total = len(testers)
		}
		if total > 0 && len(testers) == 0 {
			return Summary{}, errors.New("asc: tester list empty")
		}
		item.Testers = total
		for _, t := range testers {
			notePerson(people, t.ID, testerState(t.Attributes))
		}
		if item.PublicLink {
			if views, ok := c.linkViews(ctx, g.ID); ok {
				item.LinkViews = views
				item.LinkMetrics = true
			}
		}
		out.Groups = append(out.Groups, item)
	}

	builds, berr := c.builds(ctx, people)
	installed, accepted, invited, other, unknown := countStates(people)
	if unknown > 0 && installed+accepted+invited+other == 0 {
		return Summary{}, errors.New("asc: tester state missing")
	}
	out.Installed = installed
	out.Accepted = accepted
	out.Invited = invited
	out.OtherSt = other + unknown
	out.Testers = out.Installed + out.Accepted + out.Invited + out.OtherSt
	if berr != nil {
		if out.Installed > 0 {
			return Summary{}, berr
		}
	} else {
		out.Builds = builds
		for _, b := range builds {
			if out.LatestBuild == "" || buildGreater(b.Build, out.LatestBuild) {
				out.LatestBuild = b.Build
			}
		}
	}
	if rev, ok := c.review(ctx); ok {
		out.Review = rev
	}
	return out, nil
}

func (c *Client) builds(ctx context.Context, people map[string]string) ([]BuildCount, error) {
	items, _, included, err := c.listAll(ctx, "/v1/builds?filter[app]="+url.QueryEscape(c.appID)+"&filter[expired]=false&include=preReleaseVersion&limit=30&sort=-uploadedDate")
	if err != nil {
		return nil, err
	}
	versions := map[string]string{}
	for _, inc := range included {
		if inc.Type == "preReleaseVersions" {
			versions[inc.ID] = strAttr(inc.Attributes, "version")
		}
	}
	var out []BuildCount
	for _, b := range items {
		state := strAttr(b.Attributes, "processingState")
		if state == "FAILED" || state == "INVALID" {
			continue
		}
		if boolAttr(b.Attributes, "expired") {
			continue
		}
		number := strAttr(b.Attributes, "version")
		if number == "" {
			continue
		}
		testers, status, err := c.listStatus(ctx, "/v1/builds/"+url.PathEscape(b.ID)+"/individualTesters?limit=200")
		if err != nil {
			return nil, err
		}
		if status == http.StatusNotFound {
			return nil, errors.New("asc: build testers unavailable")
		}
		n := 0
		for _, t := range testers {
			st := testerState(t.Attributes)
			notePerson(people, t.ID, st)
			if strings.EqualFold(st, "INSTALLED") {
				n++
			}
		}
		version := ""
		if rel, ok := b.Relationships["preReleaseVersion"]; ok && rel.One != nil {
			version = versions[rel.One.ID]
		}
		out = append(out, BuildCount{Build: number, Version: version, Testers: n})
	}
	return out, nil
}

func (c *Client) review(ctx context.Context) (*Review, bool) {
	items, _, _, err := c.listAll(ctx, "/v1/apps/"+url.PathEscape(c.appID)+"/appStoreVersions?filter[platform]=IOS&limit=10")
	if err != nil || len(items) == 0 {
		return nil, false
	}
	chosen := items[0]
	for _, item := range items {
		state := strAttr(item.Attributes, "appStoreState")
		if state != "" && state != "READY_FOR_SALE" && state != "READY_FOR_DISTRIBUTION" {
			chosen = item
			break
		}
	}
	state := strAttr(chosen.Attributes, "appStoreState")
	version := strAttr(chosen.Attributes, "versionString")
	if state == "" && version == "" {
		return nil, false
	}
	return &Review{Version: version, State: state}, true
}

func (c *Client) linkViews(ctx context.Context, groupID string) (int, bool) {
	body, status, err := c.get(ctx, "/v1/betaGroups/"+url.PathEscape(groupID)+"/metrics/publicLinkUsages")
	if err != nil || status != http.StatusOK {
		return 0, false
	}
	return viewCount(body)
}

type resource struct {
	Type          string         `json:"type"`
	ID            string         `json:"id"`
	Attributes    map[string]any `json:"attributes"`
	Relationships map[string]rel `json:"relationships"`
}

type rel struct {
	One *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
}

func (r *rel) UnmarshalJSON(b []byte) error {
	var raw struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if len(raw.Data) == 0 || raw.Data[0] == 'n' || raw.Data[0] == '[' {
		return nil
	}
	var one struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw.Data, &one); err != nil {
		return err
	}
	r.One = &one
	return nil
}

func (c *Client) listAll(ctx context.Context, first string) ([]resource, int, []resource, error) {
	items, included, total, status, err := c.pages(ctx, first)
	if err != nil {
		return nil, 0, nil, err
	}
	if status != http.StatusOK {
		return nil, 0, nil, fmt.Errorf("asc: http %d", status)
	}
	return items, total, included, nil
}

func (c *Client) listStatus(ctx context.Context, first string) ([]resource, int, error) {
	items, _, _, status, err := c.pages(ctx, first)
	if err != nil {
		return nil, 0, err
	}
	if status == http.StatusNotFound {
		return nil, status, nil
	}
	if status != http.StatusOK {
		return nil, status, fmt.Errorf("asc: http %d", status)
	}
	return items, status, nil
}

func (c *Client) pages(ctx context.Context, first string) ([]resource, []resource, int, int, error) {
	var items, included []resource
	next := first
	prev := ""
	total := 0
	status := 0
	for pages := 0; pages < 50 && next != ""; pages++ {
		if next == prev {
			return nil, nil, 0, 0, errors.New("asc: truncated")
		}
		body, code, err := c.get(ctx, next)
		if err != nil {
			return nil, nil, 0, 0, err
		}
		status = code
		if code != http.StatusOK {
			return nil, nil, 0, code, nil
		}
		var page struct {
			Data     []resource `json:"data"`
			Included []resource `json:"included"`
			Links    struct {
				Next string `json:"next"`
			} `json:"links"`
			Meta struct {
				Paging struct {
					Total int `json:"total"`
				} `json:"paging"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, nil, 0, 0, errors.New("asc: unexpected payload")
		}
		items = append(items, page.Data...)
		included = append(included, page.Included...)
		if pages == 0 {
			total = page.Meta.Paging.Total
		}
		prev = next
		next = page.Links.Next
		if pages == 49 && next != "" {
			return nil, nil, 0, 0, errors.New("asc: truncated")
		}
	}
	if total < len(items) {
		total = len(items)
	}
	return items, included, total, status, nil
}

func (c *Client) get(ctx context.Context, ref string) ([]byte, int, error) {
	full := ref
	if strings.HasPrefix(ref, "/") {
		full = strings.TrimRight(c.base, "/") + ref
	}
	u, err := url.Parse(full)
	if err != nil {
		return nil, 0, errors.New("asc: bad url")
	}
	base, err := url.Parse(c.base)
	if err != nil || u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) {
		return nil, 0, errors.New("asc: refused url")
	}
	tok, err := c.jwt(time.Now())
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, errors.New("asc: request")
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, errors.New("asc: request failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, errors.New("asc: response")
	}
	return body, resp.StatusCode, nil
}

func (c *Client) jwt(now time.Time) (string, error) {
	key, err := loadP256(c.keyPath)
	if err != nil {
		return "", err
	}
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": c.keyID, "typ": "JWT"})
	payload, _ := json.Marshal(map[string]any{
		"iss": c.issuerID,
		"iat": now.Unix(),
		"exp": now.Add(15 * time.Minute).Unix(),
		"aud": "appstoreconnect-v1",
	})
	signing := rawB64(header) + "." + rawB64(payload)
	sum := sha256.Sum256([]byte(signing))
	sig, err := signES256(key, sum[:])
	if err != nil {
		return "", errors.New("asc: cannot sign")
	}
	return signing + "." + rawB64(sig), nil
}

func loadP256(path string) (*ecdsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("asc: cannot read ASC_KEY_PATH")
	}
	defer func() {
		for i := range raw {
			raw[i] = 0
		}
	}()
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("asc: private key is not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("asc: private key is not PKCS8")
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("asc: private key is not P-256")
	}
	return key, nil
}

func signES256(key *ecdsa.PrivateKey, digest []byte) ([]byte, error) {
	r, s, err := ecdsa.Sign(rand.Reader, key, digest)
	if err != nil {
		return nil, err
	}
	rb, sb := r.Bytes(), s.Bytes()
	if len(rb) > 32 || len(sb) > 32 {
		return nil, errors.New("asc: cannot sign")
	}
	out := make([]byte, 64)
	copy(out[32-len(rb):32], rb)
	copy(out[64-len(sb):], sb)
	return out, nil
}

func rawB64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func testerState(attrs map[string]any) string {
	for _, key := range []string{"state", "betaTesterState"} {
		if s, ok := attrs[key].(string); ok {
			return strings.ToUpper(strings.TrimSpace(s))
		}
	}
	return ""
}

func notePerson(people map[string]string, id, state string) {
	if id == "" {
		return
	}
	prev, ok := people[id]
	if !ok || stateRank(state) >= stateRank(prev) {
		people[id] = state
	}
}

func stateRank(s string) int {
	switch s {
	case "INSTALLED":
		return 4
	case "ACCEPTED":
		return 3
	case "INVITED":
		return 2
	case "":
		return 0
	default:
		return 1
	}
}

func countStates(people map[string]string) (installed, accepted, invited, other, unknown int) {
	for _, st := range people {
		switch st {
		case "INSTALLED":
			installed++
		case "ACCEPTED":
			accepted++
		case "INVITED":
			invited++
		case "":
			unknown++
		default:
			other++
		}
	}
	return
}

func buildGreater(a, b string) bool {
	ai, aerr := atoi(a)
	bi, berr := atoi(b)
	if aerr == nil && berr == nil {
		return ai > bi
	}
	return a > b
}

func atoi(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, errors.New("empty")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("nan")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

func strAttr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func boolAttr(m map[string]any, key string) bool {
	b, _ := m[key].(bool)
	return b
}

func intAttr(m map[string]any, key string) int {
	switch n := m[key].(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func viewCount(body []byte) (int, bool) {
	var doc struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &doc) != nil || len(doc.Data) == 0 {
		return 0, false
	}
	var rows []resource
	if doc.Data[0] == '[' {
		if json.Unmarshal(doc.Data, &rows) != nil {
			return 0, false
		}
	} else {
		var one resource
		if json.Unmarshal(doc.Data, &one) != nil {
			return 0, false
		}
		rows = []resource{one}
	}
	for _, row := range rows {
		for _, key := range []string{"viewCount", "views"} {
			if _, ok := row.Attributes[key]; ok {
				return intAttr(row.Attributes, key), true
			}
		}
	}
	return 0, false
}
