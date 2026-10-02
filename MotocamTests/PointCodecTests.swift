import XCTest
#if canImport(MotocamCore)
@testable import MotocamCore
#else
@testable import Motocam
#endif

final class PointCodecTests: XCTestCase {
    func testJSONRoundTripAndWGS84Marker() throws {
        let point = makePoint(notes: "A & B <站>", source: .ocr)
        let data = try PointCodec.encodeJSON([point])
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(object["coord"] as? String, "wgs84")
        XCTAssertEqual(object["version"] as? Int, 1)
        let decoded = try PointCodec.decodeJSON(data)
        XCTAssertEqual(decoded, [point])
    }

    func testJSONImportConvertsGCJ02ToWGS84() throws {
        let gcjLat = 39.9101255001
        let gcjLon = 116.4037425753
        let json = """
        {"version":1,"coord":"gcj02","points":[{
          "id":"bj","name":"示例","type":"camera","latitude":\(gcjLat),"longitude":\(gcjLon),
          "source":"search","created_at":"2026-09-26T00:00:00Z","updated_at":"2026-09-26T00:00:00Z"
        }]}
        """
        let decoded = try PointCodec.decodeJSON(Data(json.utf8))
        XCTAssertEqual(decoded[0].latitude, 39.908722, accuracy: 1e-5)
        XCTAssertEqual(decoded[0].longitude, 116.397499, accuracy: 1e-5)
        XCTAssertEqual(decoded[0].type, .camera)
        XCTAssertEqual(decoded[0].source, .search)
    }

    func testUnknownCoordinateSystemThrows() {
        let json = #"{"version":1,"coord":"bd09","points":[]}"#
        XCTAssertThrowsError(try PointCodec.decodeJSON(Data(json.utf8))) { error in
            XCTAssertEqual(error as? PointCodecError, .unknownCoordinateSystem("bd09"))
        }
    }

    func testGPXRoundTripPreservesFields() throws {
        let point = makePoint(id: "station-1", notes: "注意 & 核对", source: .search)
        let xml = PointCodec.encodeGPX([point])
        XCTAssertTrue(xml.contains("urn:motocam:gpx:1"))
        XCTAssertTrue(xml.contains("<name>\(AppIdentity.displayName)</name>"))
        XCTAssertTrue(xml.contains("<motocam:coord>wgs84</motocam:coord>"))
        let decoded = try PointCodec.decodeGPX(xml)
        XCTAssertEqual(decoded.count, 1)
        XCTAssertEqual(decoded[0].id, point.id)
        XCTAssertEqual(decoded[0].name, point.name)
        XCTAssertEqual(decoded[0].notes, point.notes)
        XCTAssertEqual(decoded[0].type, .bannedToll)
        XCTAssertEqual(decoded[0].roadCode, "G25")
        XCTAssertEqual(decoded[0].roadName, "长深高速")
        XCTAssertEqual(decoded[0].source, .search)
        XCTAssertEqual(decoded[0].fineYuan, 100)
        XCTAssertEqual(decoded[0].penaltyPoints, 1)
        XCTAssertEqual(decoded[0].violation, point.violation)
        XCTAssertEqual(decoded[0].latitude, point.latitude, accuracy: 1e-6)
        XCTAssertEqual(decoded[0].longitude, point.longitude, accuracy: 1e-6)
        XCTAssertEqual(decoded[0].createdAt, point.createdAt)
    }

    func testPlainGPXWaypointImportsAsManualOther() throws {
        let xml = """
        <gpx version="1.1"><wpt lat="28.1000000" lon="120.2000000"><name>临时点</name></wpt></gpx>
        """
        let decoded = try PointCodec.decodeGPX(xml)
        XCTAssertEqual(decoded[0].name, "临时点")
        XCTAssertEqual(decoded[0].type, .other)
        XCTAssertEqual(decoded[0].source, .manual)
        XCTAssertEqual(decoded[0].latitude, 28.1, accuracy: 1e-6)
    }

    func testGPXRejectsGarbageAndAllowsEmptyDocument() throws {
        XCTAssertThrowsError(try PointCodec.decodeGPX("not a file"))
        let empty = try PointCodec.decodeGPX("<gpx version=\"1.1\"></gpx>")
        XCTAssertEqual(empty.count, 0)
    }

    func testMergeReplacesSameIDAndAppendsNew() {
        let original = makePoint(id: "a", name: "旧")
        let replacement = makePoint(id: "a", name: "新")
        let extra = makePoint(id: "b", name: "另")
        let merged = PointCodec.merging(existing: [original], imported: [replacement, extra])
        XCTAssertEqual(merged.map(\.id), ["a", "b"])
        XCTAssertEqual(merged[0].name, "新")
    }
}
