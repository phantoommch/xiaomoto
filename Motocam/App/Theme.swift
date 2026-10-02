import SwiftUI

enum MotocamTheme {
    static let amber = Color(red: 0.86, green: 0.42, blue: 0.05)
    static let ink = Color(red: 0.10, green: 0.14, blue: 0.20)
    static let community = Color(red: 0.10, green: 0.52, blue: 0.46)

    static func color(for type: PointType) -> Color {
        switch type {
        case .bannedToll: return Color(red: 0.80, green: 0.18, blue: 0.16)
        case .bannedSegment: return Color(red: 0.88, green: 0.46, blue: 0.08)
        case .camera: return Color(red: 0.40, green: 0.28, blue: 0.74)
        case .other: return Color(red: 0.33, green: 0.40, blue: 0.46)
        }
    }

    static func shortMark(for type: PointType) -> String {
        switch type {
        case .bannedToll: return "禁"
        case .bannedSegment: return "段"
        case .camera: return "拍"
        case .other: return "他"
        }
    }
}

struct TypeBadge: View {
    var type: PointType

    var body: some View {
        Text(type.title)
            .font(.caption.weight(.semibold))
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .foregroundStyle(.white)
            .background(MotocamTheme.color(for: type), in: Capsule())
    }
}

struct InlineNotice: View {
    var text: String
    var systemImage: String = "info.circle"

    var body: some View {
        Label(text, systemImage: systemImage)
            .font(.footnote)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}
