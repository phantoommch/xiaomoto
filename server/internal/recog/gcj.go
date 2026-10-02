package recog

import "math"

// Persisted coordinates are WGS-84. AMap returns GCJ-02.
// Same iteration as the apps, so a marker lands on the same point.

func inChina(lat, lon float64) bool {
	return lon >= 72.004 && lon <= 137.8347 && lat >= 0.8293 && lat <= 55.8271
}

func wgsToGCJ(lat, lon float64) (float64, float64) {
	if !inChina(lat, lon) {
		return lat, lon
	}
	dLat := transformLat(lon-105.0, lat-35.0)
	dLon := transformLon(lon-105.0, lat-35.0)
	rad := lat / 180.0 * math.Pi
	magic := math.Sin(rad)
	magic = 1 - ee*magic*magic
	sqrtMagic := math.Sqrt(magic)
	dLat = (dLat * 180.0) / ((a * (1 - ee)) / (magic * sqrtMagic) * math.Pi)
	dLon = (dLon * 180.0) / (a / sqrtMagic * math.Cos(rad) * math.Pi)
	return lat + dLat, lon + dLon
}

func gcjToWGS(lat, lon float64) (float64, float64) {
	if !inChina(lat, lon) {
		return lat, lon
	}
	gLat, gLon := lat, lon
	for i := 0; i < 12; i++ {
		fLat, fLon := wgsToGCJ(gLat, gLon)
		gLat += lat - fLat
		gLon += lon - fLon
	}
	return gLat, gLon
}

const (
	a  = 6378245.0
	ee = 0.00669342162296594323
)

func transformLat(x, y float64) float64 {
	ret := -100.0 + 2.0*x + 3.0*y + 0.2*y*y + 0.1*x*y + 0.2*math.Sqrt(math.Abs(x))
	ret += (20.0*math.Sin(6.0*x*math.Pi) + 20.0*math.Sin(2.0*x*math.Pi)) * 2.0 / 3.0
	ret += (20.0*math.Sin(y*math.Pi) + 40.0*math.Sin(y/3.0*math.Pi)) * 2.0 / 3.0
	ret += (160.0*math.Sin(y/12.0*math.Pi) + 320*math.Sin(y*math.Pi/30.0)) * 2.0 / 3.0
	return ret
}

func transformLon(x, y float64) float64 {
	ret := 300.0 + x + 2.0*y + 0.1*x*x + 0.1*x*y + 0.1*math.Sqrt(math.Abs(x))
	ret += (20.0*math.Sin(6.0*x*math.Pi) + 20.0*math.Sin(2.0*x*math.Pi)) * 2.0 / 3.0
	ret += (20.0*math.Sin(x*math.Pi) + 40.0*math.Sin(x/3.0*math.Pi)) * 2.0 / 3.0
	ret += (150.0*math.Sin(x/12.0*math.Pi) + 300.0*math.Sin(x/30.0*math.Pi)) * 2.0 / 3.0
	return ret
}

func hav(lat1, lon1, lat2, lon2 float64) float64 {
	p1 := lat1 * math.Pi / 180
	p2 := lat2 * math.Pi / 180
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	if h > 1 {
		h = 1
	}
	return 2 * 6371000 * math.Asin(math.Sqrt(h))
}
