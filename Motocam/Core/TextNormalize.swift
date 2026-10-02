import Foundation

enum TextNormalize {
    /// Maps fullwidth ASCII (U+FF01...U+FF5E) and ideographic space to ASCII.
    static func halfwidth(_ text: String) -> String {
        var scalars = String.UnicodeScalarView()
        for scalar in text.unicodeScalars {
            let value = scalar.value
            if value >= 0xFF01 && value <= 0xFF5E, let mapped = UnicodeScalar(value - 0xFEE0) {
                scalars.append(mapped)
            } else if value == 0x3000 {
                scalars.append(" ")
            } else {
                scalars.append(scalar)
            }
        }
        return String(scalars)
    }

    static func collapseSpaces(_ text: String) -> String {
        text.split(whereSeparator: \.isWhitespace).joined(separator: " ")
    }

    /// Drops 省/市/区/县 suffixes so “丽水” matches “丽水市”.
    static func regionKey(_ text: String) -> String {
        var value = collapseSpaces(halfwidth(text)).trimmingCharacters(in: .whitespacesAndNewlines)
        let suffixes = ["特别行政区", "自治州", "地区", "省", "市", "区", "县"]
        var changed = true
        while changed {
            changed = false
            for suffix in suffixes where value.hasSuffix(suffix) && value.count > suffix.count + 1 {
                value.removeLast(suffix.count)
                changed = true
                break
            }
        }
        return value
    }
}
