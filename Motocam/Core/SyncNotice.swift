import Foundation

enum SyncNotice {
    static let noneNearby = "附近 15 公里内暂时没有点位，缩小地图可以看到其他地方的点。"
    static let noServer = "还没有服务器地址"

    static func nearbyLine(kilometers: Int) -> String {
        "附近 \(kilometers) 公里内暂时没有点位，缩小地图可以看到其他地方的点。"
    }

    static func success(count: Int, nearbyCount: Int?, nearbyKilometers: Int = 15) -> String {
        let base = "已更新，共 \(count) 个点位"
        if nearbyCount == 0 {
            let line = nearbyKilometers == 15 ? noneNearby : nearbyLine(kilometers: nearbyKilometers)
            return base + "。" + line
        }
        return base
    }

    static func failure(_ reason: String) -> String {
        let trimmed = reason.trimmingCharacters(in: .whitespacesAndNewlines)
        let text = trimmed.isEmpty ? "网络不通" : trimmed
        return "更新失败：\(text)"
    }
}
