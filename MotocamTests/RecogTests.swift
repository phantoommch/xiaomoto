import XCTest
@testable import MotocamCore

final class RecogTests: XCTestCase {
    func testHardTexts() {
        let a = Recog.parse("汕昆高速1807公里910米-汕昆高速1798公里760米")
        XCTAssertEqual(a.code, "G78")
        XCTAssertEqual(a.stakes.count, 2)
        XCTAssertEqual(a.stakes[0].metres, 910)
        XCTAssertTrue(a.range)
        let b = Recog.parse("G201152公里700米")
        XCTAssertEqual(b.glued.count, 3)
        XCTAssertTrue(b.glued.contains { $0.code == "G2011" && $0.km == 52 && $0.metres == 700 })
        let ocr = Recog.parse("G56抗瑞高速安徽方向73公里300米")
        XCTAssertEqual(ocr.code, "G56")
        XCTAssertEqual(ocr.nameCanon, "杭瑞")
        XCTAssertEqual(ocr.nameHow, "ocrfix")
        let sms = "云南交警\n121235300\n在汕昆高速1807\n公里910米-汕昆高速1798公里76\n0米"
        let c = Recog.parse(sms)
        XCTAssertEqual(c.code, "G78")
        XCTAssertEqual(c.stakes[0].metres, 910)
        XCTAssertEqual(c.stakes[1].metres, 760)
        let sig = Recog.signals(sms)
        XCTAssertTrue(sig.provinces.contains { $0.1 == "云南" })
        XCTAssertNil(sig.plate)
        XCTAssertEqual(Recog.parse("江南大道隧道东向西入口").facility, "江南大道隧道")
        let tunnel = Recog.parse("太湖隧道东入口,由东向西")
        XCTAssertEqual(tunnel.facility, "太湖隧道")
        XCTAssertEqual(tunnel.portal, "东入口")
        XCTAssertTrue(Recog.parse("G15沈海高速1709公里910米（北向南").direction?.contains("北向南") == true)
        let e = Recog.parse("杭瑞高速K2501+600M至杭瑞高速K2514+500M大理方向")
        XCTAssertEqual(e.code, "G56")
        XCTAssertEqual(e.stakes[0].metres, 600)
        XCTAssertEqual(e.stakes[1].metres, 500)
        XCTAssertTrue(e.range)
        let overlay = Recog.signals("《山东省高速公路交通安全条例》", overlay: "地点:G2011青新K52+700 设备编号:370211")
        XCTAssertEqual(overlay.overlay?.code, "G2011")
        XCTAssertEqual(overlay.overlay?.metres, 700)
        XCTAssertTrue(overlay.provinces.contains { $0 == ("legal", "山东") })
        XCTAssertTrue(overlay.provinces.contains { $0 == ("device", "山东") })
        let list = TicketParser.parse("超速\n江南大道隧道东向西入口\n罚款100元 记3分")
        XCTAssertEqual(list.records.first?.location, "江南大道隧道东向西入口")
        XCTAssertEqual(TicketParser.parse(sms).records.count, 1)
    }

    func testSegmentDistance() {
        let along = GeoMath.alongSegment(0, 0.001, 0, 0, 0, 0.01)
        XCTAssertGreaterThan(along.fraction, 0)
        XCTAssertLessThan(along.fraction, 1)
    }
}
