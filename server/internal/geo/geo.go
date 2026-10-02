package geo

import "math"

const earthRadius = 6371000.0

func Distance(lat1, lon1, lat2, lon2 float64) float64 {
	p1 := lat1 * math.Pi / 180
	p2 := lat2 * math.Pi / 180
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	if h > 1 {
		h = 1
	}
	return 2 * earthRadius * math.Asin(math.Sqrt(h))
}

func Offset(lat, lon, northMeters, eastMeters float64) (float64, float64) {
	dLat := northMeters / 111320
	cosLat := math.Cos(lat * math.Pi / 180)
	if cosLat < 0.2 && cosLat > -0.2 {
		if cosLat < 0 {
			cosLat = -0.2
		} else {
			cosLat = 0.2
		}
	}
	dLon := eastMeters / (111320 * math.Abs(cosLat))
	return lat + dLat, lon + dLon
}

func RoadsAgree(a, b string) bool {
	a = normalizeRoad(a)
	b = normalizeRoad(b)
	if a == "" || b == "" {
		return true
	}
	return a == b
}

func normalizeRoad(value string) string {
	out := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == ' ' || c == '\t' {
			continue
		}
		if c >= 'a' && c <= 'z' {
			c = c - 'a' + 'A'
		}
		out = append(out, c)
	}
	return string(out)
}
