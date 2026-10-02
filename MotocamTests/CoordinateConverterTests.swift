import XCTest
#if canImport(MotocamCore)
@testable import MotocamCore
#else
@testable import Motocam
#endif

final class CoordinateConverterTests: XCTestCase {
    func testBeijingWGS84ToGCJ02GoldenValue() {
        let wgs = CoordinateConverter.Coord(latitude: 39.908722, longitude: 116.397499)
        let gcj = CoordinateConverter.wgs84ToGcj02(wgs)
        XCTAssertEqual(gcj.latitude, 39.9101255001, accuracy: 1e-6)
        XCTAssertEqual(gcj.longitude, 116.4037425753, accuracy: 1e-6)
        let offset = GeoMath.distanceMeters(wgs.latitude, wgs.longitude, gcj.latitude, gcj.longitude)
        XCTAssertGreaterThan(offset, 400)
        XCTAssertLessThan(offset, 750)
    }

    func testShanghaiWGS84ToGCJ02GoldenValue() {
        let wgs = CoordinateConverter.Coord(latitude: 31.230416, longitude: 121.473701)
        let gcj = CoordinateConverter.wgs84ToGcj02(wgs)
        XCTAssertEqual(gcj.latitude, 31.2284737422, accuracy: 1e-6)
        XCTAssertEqual(gcj.longitude, 121.4782240574, accuracy: 1e-6)
    }

    func testRoundTripStaysWithinACentimeter() {
        let samples = [
            CoordinateConverter.Coord(latitude: 39.908722, longitude: 116.397499),
            CoordinateConverter.Coord(latitude: 31.230416, longitude: 121.473701),
            CoordinateConverter.Coord(latitude: 28.0, longitude: 120.0),
        ]
        for wgs in samples {
            let gcj = CoordinateConverter.wgs84ToGcj02(wgs)
            let back = CoordinateConverter.gcj02ToWgs84(gcj)
            let error = GeoMath.distanceMeters(wgs.latitude, wgs.longitude, back.latitude, back.longitude)
            XCTAssertLessThan(error, 0.05, "round trip \(wgs)")
            XCTAssertEqual(back.latitude, wgs.latitude, accuracy: 1e-6)
            XCTAssertEqual(back.longitude, wgs.longitude, accuracy: 1e-6)
        }
    }

    func testOutsideChinaIsIdentity() {
        // Hong Kong sits inside the commonly published rectangle, so this
        // implementation still shifts it. Tokyo and New York do not.
        XCTAssertTrue(CoordinateConverter.isInChinaBoundingBox(latitude: 22.3, longitude: 114.2))
        let outside = [
            CoordinateConverter.Coord(latitude: 35.68, longitude: 139.76),
            CoordinateConverter.Coord(latitude: 40.71, longitude: -74.00),
        ]
        for coord in outside {
            XCTAssertFalse(CoordinateConverter.isInChinaBoundingBox(latitude: coord.latitude, longitude: coord.longitude))
            XCTAssertEqual(CoordinateConverter.wgs84ToGcj02(coord), coord)
            XCTAssertEqual(CoordinateConverter.gcj02ToWgs84(coord), coord)
        }
    }

    func testBearingDueNorth() {
        let bearing = GeoMath.bearingDegrees(28, 120, 28.01, 120)
        XCTAssertEqual(bearing, 0, accuracy: 1)
    }
}
