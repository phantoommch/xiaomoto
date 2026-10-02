import Foundation

enum TicketStyle: Equatable {
    /// 未处理列表：没有「违法地点」字段，桩号以 G 或 S 开头，一张图可以有多条。
    case list
    /// 违法详情：有「违法地点」字段，上面是抓拍照片，一张图只有一条。
    case detail
}

struct TicketRecord: Equatable {
    var location: String?
    var happenedAt: Date?
    var violation: String?
    var fineYuan: Int?
    var penaltyPoints: Int?
    var fullText: String = ""
}

struct TicketFields: Equatable {
    var location: String?
    var happenedAt: Date?
    var violation: String?
    var fineYuan: Int?
    var penaltyPoints: Int?
    var rawText: String
    var records: [TicketRecord] = []
    var style: TicketStyle?
}

enum TicketParser {
    /// Labels that can appear on a 交通违法 screenshot. Vehicle-plate lines are
    /// recognized only so their values are skipped — plates are never stored.
    private static let labels = [
        "违法地点", "违法时间", "违法行为", "违法车辆", "号牌号码",
        "罚款金额", "记分", "罚款",
    ]

    private static let valueLabels: Set<String> = ["违法地点", "违法时间", "违法行为"]

    static func parse(_ text: String) -> TicketFields {
        let normalized = TextNormalize.halfwidth(text).replacingOccurrences(of: "\r\n", with: "\n")
        let lines = normalized
            .components(separatedBy: "\n")
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }

        var values: [String: String] = [:]
        var index = 0
        while index < lines.count {
            let line = lines[index]
            if let label = matchingLabel(line) {
                let inline = inlineValue(after: label, in: line)
                if !inline.isEmpty {
                    values[label] = inline
                } else if valueLabels.contains(label), index + 1 < lines.count, matchingLabel(lines[index + 1]) == nil {
                    values[label] = lines[index + 1]
                    index += 1
                }
            }
            index += 1
        }

        let flat = lines.joined(separator: " ")
        let labeledLocation = cleaned(values["违法地点"])
        // 两种版式互斥。详情页一旦读到「违法地点」，就只保留这一条，不再按列表去拆卡片。
        let records: [TicketRecord]
        let style: TicketStyle?
        if let labeledLocation {
            style = .detail
            records = [
                TicketRecord(
                    location: labeledLocation,
                    happenedAt: values["违法时间"].flatMap(parseDate),
                    violation: cleaned(values["违法行为"]),
                    fineYuan: parseFine(flat),
                    penaltyPoints: parsePoints(flat),
                    fullText: normalized.trimmingCharacters(in: .whitespacesAndNewlines)
                ),
            ]
        } else {
            let blocks = roadBlocks(lines).filter { $0.fineYuan != nil || $0.penaltyPoints != nil }
            if blocks.isEmpty, let sms = smsRecord(normalized, flat: flat) {
                records = [sms]
                style = .detail
            } else {
                records = blocks
                style = records.isEmpty ? nil : .list
            }
        }
        let first = records.first
        return TicketFields(
            location: first?.location,
            happenedAt: first?.happenedAt,
            violation: first?.violation,
            fineYuan: first?.fineYuan,
            penaltyPoints: first?.penaltyPoints,
            rawText: normalized.trimmingCharacters(in: .whitespacesAndNewlines),
            records: records,
            style: style
        )
    }

    /// 交管未处理列表没有「违法地点」。每一条要有公路桩号，以及罚款或记分。
    private static func roadBlocks(_ lines: [String]) -> [TicketRecord] {
        var cards: [[String]] = []
        for line in lines {
            if cards.isEmpty || isViolationTitle(line) {
                cards.append([line])
            } else {
                cards[cards.count - 1].append(line)
            }
        }
        return cards.flatMap { card in
            let ordered = card.compactMap(placeLine).reduce(into: [String]()) { acc, line in
                if !acc.contains(line) { acc.append(line) }
            }
            guard !ordered.isEmpty else { return [TicketRecord]() }
            let text = card.joined(separator: "\n")
            return ordered.map { location in
                TicketRecord(
                    location: location,
                    happenedAt: card.compactMap(parseDate).first,
                    violation: card.first(where: isViolationTitle),
                    fineYuan: parseFine(text),
                    penaltyPoints: parsePoints(text),
                    fullText: lines.joined(separator: "\n")
                )
            }
        }
    }

    private static func smsRecord(_ normalized: String, flat: String) -> TicketRecord? {
        guard normalized.contains("交警") || normalized.contains("12123") else { return nil }
        let parsed = Recog.parse(normalized)
        guard !parsed.stakes.isEmpty || !parsed.glued.isEmpty || parsed.facility != nil else { return nil }
        return TicketRecord(
            location: parsed.norm,
            happenedAt: parseDate(flat),
            violation: nil,
            fineYuan: parseFine(flat),
            penaltyPoints: parsePoints(flat),
            fullText: normalized.trimmingCharacters(in: .whitespacesAndNewlines)
        )
    }

    private static func placeLine(_ line: String) -> String? {
        if let stake = stakeLine(line) { return stake }
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard trimmed.count >= 4, trimmed.count <= 40 else { return nil }
        guard trimmed.range(of: "隧道|收费站|服务区|停车区|大桥|枢纽|互通", options: .regularExpression) != nil else { return nil }
        guard matchingLabel(trimmed) == nil else { return nil }
        return trimmed
    }

    private static func stakeLine(_ line: String) -> String? {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard trimmed.range(of: "(?i)^[GS]\\d{1,4}", options: .regularExpression) != nil else { return nil }
        guard trimmed.range(of: "(?i)高速|公里|千米|KM", options: .regularExpression) != nil else { return nil }
        return trimmed
    }

    private static func isViolationTitle(_ line: String) -> Bool {
        if matchingLabel(line) != nil { return false }
        return line.contains("禁令") || line.contains("驾驶") || line.contains("超速")
    }

    static func chinaTimeZone() -> TimeZone {
        TimeZone(identifier: "Asia/Shanghai") ?? TimeZone(secondsFromGMT: 8 * 3600)!
    }

    private static func matchingLabel(_ line: String) -> String? {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        for label in labels.sorted(by: { $0.count > $1.count }) {
            if trimmed == label || trimmed.hasPrefix(label) {
                let restIndex = trimmed.index(trimmed.startIndex, offsetBy: label.count)
                let rest = trimmed[restIndex...]
                if rest.isEmpty || rest.hasPrefix(":") || rest.hasPrefix(" ") || rest.hasPrefix("\t") {
                    return label
                }
                // “罚款100元” has no separator. Accept when the next char is a digit.
                if label == "罚款" || label == "记分" || label == "罚款金额", let first = rest.first, first.isNumber {
                    return label
                }
            }
        }
        return nil
    }

    private static func inlineValue(after label: String, in line: String) -> String {
        guard let range = line.range(of: label) else { return "" }
        var rest = line[range.upperBound...]
        while let first = rest.first, first == ":" || first.isWhitespace {
            rest.removeFirst()
        }
        return String(rest).trimmingCharacters(in: .whitespaces)
    }

    private static func cleaned(_ value: String?) -> String? {
        guard let value else { return nil }
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }

    private static func parseFine(_ text: String) -> Int? {
        if let amount = firstInt(matching: "罚款金额\\s*:?\\s*(\\d+)", in: text) {
            return amount
        }
        return firstInt(matching: "罚款\\s*:?\\s*(\\d+)\\s*元", in: text)
    }

    private static func parsePoints(_ text: String) -> Int? {
        if let points = firstInt(matching: "记分\\s*:?\\s*(\\d+)", in: text) {
            return points
        }
        if let points = firstInt(matching: "记\\s*(\\d+)\\s*分", in: text) {
            return points
        }
        return firstInt(matching: "扣\\s*(\\d+)\\s*分", in: text)
    }

    private static func parseDate(_ text: String) -> Date? {
        let pattern = "(\\d{4})[-/.年](\\d{1,2})[-/.月](\\d{1,2})日?\\s*(\\d{1,2}):(\\d{2})(?::(\\d{2}))?"
        guard let regex = try? NSRegularExpression(pattern: pattern),
              let match = regex.firstMatch(in: text, range: NSRange(text.startIndex..., in: text))
        else { return nil }
        func group(_ index: Int) -> Int? {
            guard let range = Range(match.range(at: index), in: text) else { return nil }
            return Int(text[range])
        }
        guard let year = group(1), let month = group(2), let day = group(3),
              let hour = group(4), let minute = group(5) else { return nil }
        var components = DateComponents()
        components.calendar = Calendar(identifier: .gregorian)
        components.timeZone = chinaTimeZone()
        components.year = year
        components.month = month
        components.day = day
        components.hour = hour
        components.minute = minute
        components.second = group(6) ?? 0
        return components.date
    }

    private static func firstInt(matching pattern: String, in text: String) -> Int? {
        guard let regex = try? NSRegularExpression(pattern: pattern),
              let match = regex.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)),
              let range = Range(match.range(at: 1), in: text) else { return nil }
        return Int(text[range])
    }
}
