import XCTest
#if canImport(MotocamCore)
@testable import MotocamCore
#else
@testable import Motocam
#endif

final class TicketParserTests: XCTestCase {
    /// Fields taken from the rider's 违法处理 screenshot.
    /// 违法地点 G25长深高速赤石收费站进口内广场
    /// 违法时间 2026-01-15 14:20（按北京时间理解）
    /// 违法行为 驾驶机动车违反禁令标志指示的
    /// 罚款 100 元
    /// 记分 1
    /// 号牌已从夹具中删除，不写入仓库。
    func testChishiTicketScreenshotFields() throws {
        let text = try loadFixture("ticket_chishi.txt")
        XCTAssertFalse(containsPlate(text))
        let ticket = TicketParser.parse(text)
        XCTAssertEqual(ticket.location, "G25长深高速赤石收费站进口内广场")
        XCTAssertEqual(ticket.violation, "驾驶机动车违反禁令标志指示的")
        XCTAssertEqual(ticket.fineYuan, 100)
        XCTAssertEqual(ticket.penaltyPoints, 1)
        XCTAssertEqual(ticket.happenedAt, makeDate("2026-01-15T06:20:00Z"))

        let query = QueryCleaner.parse(ticket.location ?? "")
        XCTAssertEqual(query.roadCode, "G25")
        XCTAssertEqual(query.roadName, "长深高速")
        XCTAssertEqual(query.keywords, "赤石收费站")
    }

    func testLabelAndValueOnSeparateLines() {
        let text = """
        违法行为
        驾驶机动车违反禁令标志指示的
        违法地点
        G25长深高速赤石收费站进口内广场
        违法时间
        2026-01-15 14:20
        罚款金额：100元
        记分：1
        """
        let ticket = TicketParser.parse(text)
        XCTAssertEqual(ticket.location, "G25长深高速赤石收费站进口内广场")
        XCTAssertEqual(ticket.violation, "驾驶机动车违反禁令标志指示的")
        XCTAssertEqual(ticket.fineYuan, 100)
        XCTAssertEqual(ticket.penaltyPoints, 1)
        XCTAssertNotNil(ticket.happenedAt)
    }

    func testFullwidthColonAndCombinedFineLine() {
        let text = "违法地点：赤石收费站出口\n罚款：100元 记 1 分"
        let ticket = TicketParser.parse(text)
        XCTAssertEqual(ticket.location, "赤石收费站出口")
        XCTAssertEqual(ticket.fineYuan, 100)
        XCTAssertEqual(ticket.penaltyPoints, 1)
    }

    func testVehicleLineIsNotTreatedAsLocation() {
        let text = """
        违法车辆：已打码
        违法地点：G25长深高速赤石收费站进口内广场
        """
        let ticket = TicketParser.parse(text)
        XCTAssertEqual(ticket.location, "G25长深高速赤石收费站进口内广场")
        XCTAssertFalse(ticket.rawText.contains("浙E"))
    }

    func testDetailPageKeepsLabeledPlaceFineAndPoints() {
        let text = """
        违法行为：违反禁令标志指示
        罚款150元 记1分
        违法车辆：闽D00000
        违法地点：长深高速2862公里50米（建瓯收费站内广场）
        违法时间：2026-07-16 13:39
        """
        let ticket = TicketParser.parse(text)
        XCTAssertEqual(ticket.style, .detail)
        XCTAssertEqual(ticket.records.count, 1)
        XCTAssertEqual(ticket.location, "长深高速2862公里50米(建瓯收费站内广场)")
        XCTAssertEqual(ticket.violation, "违反禁令标志指示")
        XCTAssertEqual(ticket.fineYuan, 150)
        XCTAssertEqual(ticket.penaltyPoints, 1)
        XCTAssertFalse(ticket.location?.contains("DMR") ?? true)
        let query = QueryCleaner.parse(ticket.location ?? "")
        XCTAssertNil(query.roadCode)
        XCTAssertEqual(query.roadName, "长深高速")
        XCTAssertEqual(query.keywords, "建瓯收费站")
        XCTAssertTrue(query.wantsToll)
    }

    func testUnrelatedImageIsNotRecognized() {
        let ticket = TicketParser.parse("今天天气不错\n随便拍的一张风景")
        XCTAssertTrue(ticket.records.isEmpty)
        XCTAssertNil(ticket.location)
        XCTAssertNil(ticket.violation)
        XCTAssertNil(ticket.fineYuan)
        XCTAssertNil(ticket.penaltyPoints)
    }

    func testMissingLocationStaysNil() {
        let ticket = TicketParser.parse("违法行为：超速\n罚款200元")
        XCTAssertTrue(ticket.records.isEmpty)
        XCTAssertNil(ticket.location)
        XCTAssertNil(ticket.fineYuan)
        XCTAssertNil(ticket.violation)
        XCTAssertNil(ticket.penaltyPoints)
    }

    private func containsPlate(_ text: String) -> Bool {
        let pattern = "[京津沪渝冀豫云辽黑湘皖鲁新苏浙赣鄂桂甘晋蒙陕吉闽贵粤青藏川宁琼][A-Z][A-Z0-9]{4,6}"
        return text.range(of: pattern, options: .regularExpression) != nil
    }
}
