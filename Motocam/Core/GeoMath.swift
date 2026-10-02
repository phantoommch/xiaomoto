import Foundation

enum GeoMath {
    static let earthRadiusMeters = 6_371_000.0

    static func distanceMeters(_ aLat: Double, _ aLon: Double, _ bLat: Double, _ bLon: Double) -> Double {
        guard [aLat, aLon, bLat, bLon].allSatisfy(\.isFinite) else { return .infinity }
        let lat1 = aLat * .pi / 180
        let lat2 = bLat * .pi / 180
        let dLat = (bLat - aLat) * .pi / 180
        let dLon = (bLon - aLon) * .pi / 180
        let h = sin(dLat / 2) * sin(dLat / 2)
            + cos(lat1) * cos(lat2) * sin(dLon / 2) * sin(dLon / 2)
        return 2 * earthRadiusMeters * asin(min(1, sqrt(max(0, h))))
    }

    /// Distance to the segment and the fraction along it, 0 at the start and 1 at the end.
    static func alongSegment(_ lat: Double, _ lon: Double, _ aLat: Double, _ aLon: Double, _ bLat: Double, _ bLon: Double) -> (distance: Double, fraction: Double) {
        let mean = (aLat + bLat) / 2
        let kx = 111_320.0 * cos(mean * .pi / 180)
        let ky = 111_320.0
        let abx = (bLon - aLon) * kx
        let aby = (bLat - aLat) * ky
        let apx = (lon - aLon) * kx
        let apy = (lat - aLat) * ky
        let ab2 = abx * abx + aby * aby
        if ab2 < 1 { return (distanceMeters(lat, lon, aLat, aLon), 0) }
        let raw = (apx * abx + apy * aby) / ab2
        let t = min(1, max(0, raw))
        let lonC = aLon + (bLon - aLon) * t
        let latC = aLat + (bLat - aLat) * t
        return (distanceMeters(lat, lon, latC, lonC), t)
    }

    /// Initial bearing in degrees, 0 = north, clockwise.
    static func bearingDegrees(_ aLat: Double, _ aLon: Double, _ bLat: Double, _ bLon: Double) -> Double {
        let lat1 = aLat * .pi / 180
        let lat2 = bLat * .pi / 180
        let dLon = (bLon - aLon) * .pi / 180
        let y = sin(dLon) * cos(lat2)
        let x = cos(lat1) * sin(lat2) - sin(lat1) * cos(lat2) * cos(dLon)
        let degrees = atan2(y, x) * 180 / .pi
        return (degrees + 360).truncatingRemainder(dividingBy: 360)
    }

    /// Smallest absolute difference between two headings, in 0...180.
    static func angleDelta(_ a: Double, _ b: Double) -> Double {
        let raw = abs(a - b).truncatingRemainder(dividingBy: 360)
        return raw > 180 ? 360 - raw : raw
    }

    static func offset(
        latitude: Double,
        longitude: Double,
        northMeters: Double,
        eastMeters: Double
    ) -> (latitude: Double, longitude: Double) {
        let dLat = northMeters / 111_320
        let cosLat = cos(latitude * .pi / 180)
        let dLon = eastMeters / (111_320 * max(0.2, abs(cosLat)))
        return (latitude + dLat, longitude + dLon)
    }
}
