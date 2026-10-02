import XCTest
#if canImport(MotocamCore)
@testable import MotocamCore
#else
@testable import Motocam
#endif

final class AppUpdateLinkTests: XCTestCase {
    func testAppStoreLinkStaysHiddenUntilAnIDIsSet() {
        XCTAssertEqual(AppUpdateLink.appStoreID, "")
        XCTAssertNil(AppUpdateLink.appStoreURL())
        XCTAssertNil(AppUpdateLink.appStoreURL(appStoreID: " "))
        XCTAssertNil(AppUpdateLink.appStoreURL(appStoreID: "not-a-number"))
        XCTAssertEqual(
            AppUpdateLink.appStoreURL(appStoreID: "1234567890")?.absoluteString,
            "https://apps.apple.com/app/id1234567890"
        )
        XCTAssertEqual(AppUpdateLink.currentVersionLine(33), "当前版本：第 33 版")
    }
}
