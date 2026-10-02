import Foundation
#if canImport(MotocamCore)
@testable import MotocamCore
#else
@testable import Motocam
#endif

func makeDate(_ string: String) -> Date {
    guard let date = ISO8601Codec.date(from: string) else {
        fatalError("bad date \(string)")
    }
    return date
}

func makePoint(
    id: String = "p1",
    name: String = "赤石收费站",
    type: PointType = .bannedToll,
    roadCode: String = "G25",
    roadName: String = "长深高速",
    notes: String = "",
    latitude: Double = 28.2,
    longitude: Double = 119.9,
    fineYuan: Int? = 100,
    penaltyPoints: Int? = 1,
    violation: String = "驾驶机动车违反禁令标志指示的",
    source: PointSource = .manual,
    isCommunity: Bool = false,
    createdAt: Date = makeDate("2026-01-15T06:20:00Z")
) -> EnforcementPoint {
    EnforcementPoint(
        id: id,
        name: name,
        type: type,
        roadCode: roadCode,
        roadName: roadName,
        notes: notes,
        latitude: latitude,
        longitude: longitude,
        fineYuan: fineYuan,
        penaltyPoints: penaltyPoints,
        violation: violation,
        source: source,
        province: "浙江",
        city: "丽水",
        createdAt: createdAt,
        updatedAt: createdAt,
        isCommunity: isCommunity
    )
}

func loadFixture(_ name: String) throws -> String {
    let url = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent()
        .appendingPathComponent("Fixtures")
        .appendingPathComponent(name)
    return try String(contentsOf: url, encoding: .utf8)
}
