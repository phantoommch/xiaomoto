package recog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type osmEnd struct {
	lat, lon float64
	kind     string
}

type osmPortal struct {
	lat, lon float64
	src      string
}

var (
	osmCache = map[string][]osmWay{}
	osmMu    sync.Mutex
)

type osmWay struct {
	geom [][2]float64
}

// OverpassTimeout bounds one Overpass interpreter call.
const OverpassTimeout = 8 * time.Second

// testOSMEnds replaces Overpass in tests. Values are GCJ-02 (lon, lat).
var testOSMEnds func(lat, lon float64) [][2]float64

func osmTunnelPortal(ctx context.Context, lat, lon float64, side, travel, name string) *osmPortal {
	ways := loadTunnelWaysCached(ctx, lat, lon, name)
	if len(ways) == 0 {
		return nil
	}
	longest := ways[0]
	longLen := wayLen(longest)
	for _, w := range ways[1:] {
		if L := wayLen(w); L > longLen {
			longest = w
			longLen = L
		}
	}
	if longLen <= 0 {
		return nil
	}
	var ends []osmEnd
	for _, w := range ways {
		if wayLen(w) < 0.5*longLen || len(w.geom) < 2 {
			continue
		}
		g := w.geom
		ends = append(ends, osmEnd{g[0][0], g[0][1], "start"}, osmEnd{g[len(g)-1][0], g[len(g)-1][1], "end"})
	}
	if len(ends) == 0 {
		return nil
	}
	best := ends[0]
	bestScore := sideScore(side, best.lon, best.lat)
	for _, e := range ends[1:] {
		if s := sideScore(side, e.lon, e.lat); s > bestScore {
			best = e
			bestScore = s
		}
	}
	if travel != "" {
		for _, e := range ends {
			if e.kind == "start" && hav(e.lat, e.lon, best.lat, best.lon) < 300 {
				best = e
				break
			}
		}
	}
	return &osmPortal{best.lat, best.lon, "OSM tunnel way " + best.kind}
}

func loadTunnelWaysCached(ctx context.Context, lat, lon float64, name string) []osmWay {
	if ctx == nil {
		ctx = context.Background()
	}
	key := fmt.Sprintf("%.3f,%.3f|%s", lat, lon, tunnelCore(name))
	osmMu.Lock()
	ways, ok := osmCache[key]
	osmMu.Unlock()
	if ok {
		return ways
	}
	fetched, cacheable := fetchTunnelWays(ctx, lat, lon, name)
	if !cacheable {
		return nil
	}
	osmMu.Lock()
	osmCache[key] = fetched
	osmMu.Unlock()
	return fetched
}

// osmEndsGCJ returns tunnel way endpoints converted to GCJ-02 (lon, lat)
// so they can be compared with an AMap polyline.
func osmEndsGCJ(ctx context.Context, lat, lon float64, name string) [][2]float64 {
	if testOSMEnds != nil {
		return testOSMEnds(lat, lon)
	}
	var out [][2]float64
	for _, w := range loadTunnelWaysCached(ctx, lat, lon, name) {
		if len(w.geom) < 2 {
			continue
		}
		for _, end := range [][2]float64{w.geom[0], w.geom[len(w.geom)-1]} {
			glat, glon := wgsToGCJ(end[0], end[1])
			out = append(out, [2]float64{glon, glat})
		}
	}
	return out
}

func wayLen(w osmWay) float64 {
	sum := 0.0
	for i := 0; i+1 < len(w.geom); i++ {
		sum += hav(w.geom[i][0], w.geom[i][1], w.geom[i+1][0], w.geom[i+1][1])
	}
	return sum
}

func fetchTunnelWays(ctx context.Context, lat, lon float64, name string) ([]osmWay, bool) {
	core := strings.NewReplacer(`"`, "", `\`, "", `'`, "").Replace(tunnelCore(name))
	var queries []string
	if len([]rune(core)) >= 2 {
		queries = append(queries, fmt.Sprintf(`[out:json][timeout:8];way["tunnel"="yes"]["name"~"%s"](around:8000,%f,%f);out geom;`, core, lat, lon))
	}
	queries = append(queries, fmt.Sprintf(`[out:json][timeout:8];way["tunnel"="yes"]["highway"~"motorway|trunk|primary"](around:3500,%f,%f);out geom;`, lat, lon))
	for i, q := range queries {
		ways, ok := queryOverpass(ctx, q)
		if !ok {
			if i == 0 && len(queries) > 1 {
				continue
			}
			return nil, false
		}
		if len(ways) > 0 {
			return ways, true
		}
	}
	return nil, true
}

func queryOverpass(ctx context.Context, q string) ([]osmWay, bool) {
	client := &http.Client{Timeout: OverpassTimeout}
	for _, srv := range overpassServers {
		if err := ctx.Err(); err != nil {
			return nil, false
		}
		form := url.Values{"data": {q}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv, strings.NewReader(form.Encode()))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "XiaoMoto/32")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil || len(body) == 0 || body[0] != '{' {
			continue
		}
		var parsed struct {
			Remark   string `json:"remark"`
			Elements []struct {
				Geometry []struct {
					Lat float64 `json:"lat"`
					Lon float64 `json:"lon"`
				} `json:"geometry"`
			} `json:"elements"`
		}
		if json.Unmarshal(body, &parsed) != nil || (parsed.Remark != "" && len(parsed.Elements) == 0) {
			continue
		}
		var ways []osmWay
		for _, el := range parsed.Elements {
			if len(el.Geometry) < 2 {
				continue
			}
			w := osmWay{}
			for _, g := range el.Geometry {
				w.geom = append(w.geom, [2]float64{g.Lat, g.Lon})
			}
			ways = append(ways, w)
		}
		return ways, true
	}
	return nil, false
}

// overpassServers is the optional fallback. Tests replace it.
var overpassServers = []string{
	"https://overpass-api.de/api/interpreter",
	"https://overpass.openstreetmap.fr/api/interpreter",
}
