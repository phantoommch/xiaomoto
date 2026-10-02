import Foundation

/// WGS-84 ↔ GCJ-02 (火星坐标).
///
/// Storage convention for this app: persisted coordinates are WGS-84.
/// Amap Web Service returns GCJ-02 and must be converted before saving.
/// MapKit's mainland China basemap is GCJ-02, so map annotations and
/// camera centers are converted to GCJ-02 for display. Core Location
/// stays WGS-84 and is used for distance checks without conversion.
///
/// The transform is the commonly published approximation. It is typically
/// within a few meters, not a survey-grade grid. Outside the standard
/// China bounding box the functions are identity. Taiwan, Hong Kong, and
/// Macau fall inside that box in this implementation; MapKit does not use
/// GCJ-02 for all of them, so those regions can be offset.
enum CoordinateConverter {
    struct Coord: Equatable {
        var latitude: Double
        var longitude: Double
    }

    private static let a = 6378245.0
    private static let ee = 0.00669342162296594323

    static func isInChinaBoundingBox(latitude: Double, longitude: Double) -> Bool {
        longitude >= 72.004 && longitude <= 137.8347 && latitude >= 0.8293 && latitude <= 55.8271
    }

    static func wgs84ToGcj02(_ coord: Coord) -> Coord {
        guard isInChinaBoundingBox(latitude: coord.latitude, longitude: coord.longitude) else {
            return coord
        }
        var dLat = transformLatitude(x: coord.longitude - 105.0, y: coord.latitude - 35.0)
        var dLon = transformLongitude(x: coord.longitude - 105.0, y: coord.latitude - 35.0)
        let radLat = coord.latitude / 180.0 * Double.pi
        var magic = sin(radLat)
        magic = 1 - ee * magic * magic
        let sqrtMagic = sqrt(magic)
        dLat = (dLat * 180.0) / ((a * (1 - ee)) / (magic * sqrtMagic) * Double.pi)
        dLon = (dLon * 180.0) / (a / sqrtMagic * cos(radLat) * Double.pi)
        return Coord(latitude: coord.latitude + dLat, longitude: coord.longitude + dLon)
    }

    static func gcj02ToWgs84(_ coord: Coord) -> Coord {
        guard isInChinaBoundingBox(latitude: coord.latitude, longitude: coord.longitude) else {
            return coord
        }
        // Iterative inverse of the forward transform. Ten passes is enough
        // for sub-centimeter residual on points inside China.
        var guess = coord
        for _ in 0..<12 {
            let forward = wgs84ToGcj02(guess)
            guess.latitude += coord.latitude - forward.latitude
            guess.longitude += coord.longitude - forward.longitude
        }
        return guess
    }

    private static func transformLatitude(x: Double, y: Double) -> Double {
        var ret = -100.0 + 2.0 * x + 3.0 * y + 0.2 * y * y + 0.1 * x * y + 0.2 * sqrt(abs(x))
        ret += (20.0 * sin(6.0 * x * Double.pi) + 20.0 * sin(2.0 * x * Double.pi)) * 2.0 / 3.0
        ret += (20.0 * sin(y * Double.pi) + 40.0 * sin(y / 3.0 * Double.pi)) * 2.0 / 3.0
        ret += (160.0 * sin(y / 12.0 * Double.pi) + 320 * sin(y * Double.pi / 30.0)) * 2.0 / 3.0
        return ret
    }

    private static func transformLongitude(x: Double, y: Double) -> Double {
        var ret = 300.0 + x + 2.0 * y + 0.1 * x * x + 0.1 * x * y + 0.1 * sqrt(abs(x))
        ret += (20.0 * sin(6.0 * x * Double.pi) + 20.0 * sin(2.0 * x * Double.pi)) * 2.0 / 3.0
        ret += (20.0 * sin(x * Double.pi) + 40.0 * sin(x / 3.0 * Double.pi)) * 2.0 / 3.0
        ret += (150.0 * sin(x / 12.0 * Double.pi) + 300.0 * sin(x / 30.0 * Double.pi)) * 2.0 / 3.0
        return ret
    }
}
