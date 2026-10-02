import XCTest
#if canImport(MotocamCore)
@testable import MotocamCore
#else
@testable import Motocam
#endif

final class SyncNoticeTests: XCTestCase {
    func testSuccessMentionsNearbyOnlyWhenEmpty() {
        XCTAssertEqual(SyncNotice.success(count: 88, nearbyCount: 3), "已更新，共 88 个点位")
        XCTAssertEqual(SyncNotice.success(count: 88, nearbyCount: nil), "已更新，共 88 个点位")
        XCTAssertEqual(
            SyncNotice.success(count: 88, nearbyCount: 0),
            "已更新，共 88 个点位。附近 15 公里内暂时没有点位，缩小地图可以看到其他地方的点。"
        )
        XCTAssertEqual(SyncNotice.failure("网络不通"), "更新失败：网络不通")
        XCTAssertEqual(SyncNotice.noServer, "还没有服务器地址")
        XCTAssertEqual(
            SyncNotice.success(count: 88, nearbyCount: 0, nearbyKilometers: 80),
            "已更新，共 88 个点位。附近 80 公里内暂时没有点位，缩小地图可以看到其他地方的点。"
        )
    }
}
