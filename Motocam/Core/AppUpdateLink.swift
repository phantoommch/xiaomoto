import Foundation

enum AppUpdateLink {
    /// Numeric App Store id, such as "1234567890". Empty until the store page exists.
    /// Settings hides 「检查更新」 while this stays empty.
    static let appStoreID = ""

    static func currentVersionLine(_ code: Int) -> String {
        "当前版本：第 \(code) 版"
    }

    /// Opens the App Store product page. Nil when `appStoreID` is empty or not numeric.
    static func appStoreURL(appStoreID: String = AppUpdateLink.appStoreID) -> URL? {
        let id = appStoreID.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !id.isEmpty, id.allSatisfy({ $0.isNumber }) else { return nil }
        return URL(string: "https://apps.apple.com/app/id\(id)")
    }
}
