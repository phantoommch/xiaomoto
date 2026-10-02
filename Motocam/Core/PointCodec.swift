import Foundation

enum PointCodecError: Error, Equatable {
    case unknownCoordinateSystem(String)
    case invalidDocument
}

enum ISO8601Codec {
    static func string(from date: Date) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.string(from: date)
    }

    static func date(from string: String) -> Date? {
        let formatter = ISO8601DateFormatter()
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = formatter.date(from: string) {
            return date
        }
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.date(from: string)
    }
}

enum PointCodec {
    static func encodeJSON(_ points: [EnforcementPoint]) throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        encoder.dateEncodingStrategy = .custom { date, encoder in
            var container = encoder.singleValueContainer()
            try container.encode(ISO8601Codec.string(from: date))
        }
        return try encoder.encode(PointFile(points: points))
    }

    static func decodeJSON(_ data: Data) throws -> [EnforcementPoint] {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let raw = try container.decode(String.self)
            guard let date = ISO8601Codec.date(from: raw) else {
                throw DecodingError.dataCorruptedError(in: container, debugDescription: raw)
            }
            return date
        }
        let file = try decoder.decode(PointFile.self, from: data)
        return try applyCoordinateSystem(file.coord, to: file.points)
    }

    static func encodeGPX(_ points: [EnforcementPoint]) -> String {
        var lines = [
            "<?xml version=\"1.0\" encoding=\"UTF-8\"?>",
            "<gpx version=\"1.1\" creator=\"motocam\" xmlns=\"http://www.topografix.com/GPX/1/1\" xmlns:motocam=\"urn:motocam:gpx:1\">",
            "  <metadata><name>\(xmlEscape(AppIdentity.displayName))</name></metadata>",
        ]
        for point in points {
            lines.append(String(format: "  <wpt lat=\"%.7f\" lon=\"%.7f\">", point.latitude, point.longitude))
            lines.append("    <name>\(xmlEscape(point.name))</name>")
            if !point.notes.isEmpty {
                lines.append("    <desc>\(xmlEscape(point.notes))</desc>")
            }
            lines.append("    <time>\(ISO8601Codec.string(from: point.createdAt))</time>")
            lines.append("    <extensions>")
            lines.append("      <motocam:id>\(xmlEscape(point.id))</motocam:id>")
            lines.append("      <motocam:type>\(xmlEscape(point.type.rawValue))</motocam:type>")
            lines.append("      <motocam:source>\(xmlEscape(point.source.rawValue))</motocam:source>")
            lines.append("      <motocam:roadCode>\(xmlEscape(point.roadCode))</motocam:roadCode>")
            lines.append("      <motocam:roadName>\(xmlEscape(point.roadName))</motocam:roadName>")
            lines.append("      <motocam:violation>\(xmlEscape(point.violation))</motocam:violation>")
            if let fine = point.fineYuan {
                lines.append("      <motocam:fineYuan>\(fine)</motocam:fineYuan>")
            }
            if let penalty = point.penaltyPoints {
                lines.append("      <motocam:penaltyPoints>\(penalty)</motocam:penaltyPoints>")
            }
            lines.append("      <motocam:province>\(xmlEscape(point.province))</motocam:province>")
            lines.append("      <motocam:city>\(xmlEscape(point.city))</motocam:city>")
            lines.append("      <motocam:coord>wgs84</motocam:coord>")
            if point.isCommunity {
                lines.append("      <motocam:community>true</motocam:community>")
            }
            lines.append("    </extensions>")
            lines.append("  </wpt>")
        }
        lines.append("</gpx>")
        lines.append("")
        return lines.joined(separator: "\n")
    }

    static func decodeGPX(_ xml: String) throws -> [EnforcementPoint] {
        let trimmed = xml.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.contains("<gpx") || trimmed.contains("<wpt") else {
            throw PointCodecError.invalidDocument
        }
        guard let expression = try? NSRegularExpression(
            pattern: "<wpt\\b([^>]*)>(.*?)</wpt>",
            options: [.dotMatchesLineSeparators, .caseInsensitive]
        ) else {
            throw PointCodecError.invalidDocument
        }
        let range = NSRange(trimmed.startIndex..., in: trimmed)
        let matches = expression.matches(in: trimmed, range: range)
        var points: [EnforcementPoint] = []
        for match in matches {
            guard let attrRange = Range(match.range(at: 1), in: trimmed),
                  let bodyRange = Range(match.range(at: 2), in: trimmed) else { continue }
            let attrs = String(trimmed[attrRange])
            let body = String(trimmed[bodyRange])
            guard let lat = doubleAttr("lat", in: attrs), let lon = doubleAttr("lon", in: attrs) else {
                throw PointCodecError.invalidDocument
            }
            let coord = tagText("coord", in: body) ?? "wgs84"
            var latitude = lat
            var longitude = lon
            let converted = try applyCoordinateSystem(coord, to: [
                EnforcementPoint(name: "tmp", type: .other, latitude: latitude, longitude: longitude, source: .manual),
            ])
            latitude = converted[0].latitude
            longitude = converted[0].longitude
            let created = tagText("time", in: body).flatMap(ISO8601Codec.date(from:)) ?? Date()
            let type = PointType(rawValue: tagText("type", in: body) ?? "") ?? .other
            let source = PointSource(rawValue: tagText("source", in: body) ?? "") ?? .manual
            let fine = tagText("fineYuan", in: body).flatMap(Int.init)
            let penalty = tagText("penaltyPoints", in: body).flatMap(Int.init)
            let point = EnforcementPoint(
                id: tagText("id", in: body) ?? UUID().uuidString,
                name: tagText("name", in: body) ?? "未命名",
                type: type,
                roadCode: tagText("roadCode", in: body) ?? "",
                roadName: tagText("roadName", in: body) ?? "",
                notes: tagText("desc", in: body) ?? "",
                latitude: latitude,
                longitude: longitude,
                fineYuan: fine,
                penaltyPoints: penalty,
                violation: tagText("violation", in: body) ?? "",
                source: source,
                province: tagText("province", in: body) ?? "",
                city: tagText("city", in: body) ?? "",
                createdAt: created,
                updatedAt: created,
                isCommunity: (tagText("community", in: body) ?? "") == "true"
            )
            points.append(point)
        }
        return points
    }

    static func merging(existing: [EnforcementPoint], imported: [EnforcementPoint]) -> [EnforcementPoint] {
        var byID = Dictionary(uniqueKeysWithValues: existing.map { ($0.id, $0) })
        var order = existing.map(\.id)
        for point in imported {
            if byID[point.id] == nil {
                order.append(point.id)
            }
            byID[point.id] = point
        }
        return order.compactMap { byID[$0] }
    }

    private static func applyCoordinateSystem(_ coord: String, to points: [EnforcementPoint]) throws -> [EnforcementPoint] {
        let system = coord.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        switch system {
        case "", "wgs84", "wgs-84":
            return points
        case "gcj02", "gcj-02":
            return points.map { point in
                var copy = point
                let wgs = CoordinateConverter.gcj02ToWgs84(
                    CoordinateConverter.Coord(latitude: point.latitude, longitude: point.longitude)
                )
                copy.latitude = wgs.latitude
                copy.longitude = wgs.longitude
                return copy
            }
        default:
            throw PointCodecError.unknownCoordinateSystem(coord)
        }
    }

    private static func xmlEscape(_ text: String) -> String {
        text
            .replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;")
            .replacingOccurrences(of: "\"", with: "&quot;")
            .replacingOccurrences(of: "'", with: "&apos;")
    }

    private static func xmlUnescape(_ text: String) -> String {
        text
            .replacingOccurrences(of: "&lt;", with: "<")
            .replacingOccurrences(of: "&gt;", with: ">")
            .replacingOccurrences(of: "&quot;", with: "\"")
            .replacingOccurrences(of: "&apos;", with: "'")
            .replacingOccurrences(of: "&amp;", with: "&")
    }

    private static func doubleAttr(_ name: String, in attrs: String) -> Double? {
        guard let regex = try? NSRegularExpression(pattern: "\\b\(name)\\s*=\\s*\"([^\"]+)\""),
              let match = regex.firstMatch(in: attrs, range: NSRange(attrs.startIndex..., in: attrs)),
              let range = Range(match.range(at: 1), in: attrs) else { return nil }
        return Double(attrs[range])
    }

    private static func tagText(_ name: String, in body: String) -> String? {
        let pattern = "<(?:[A-Za-z0-9]+:)?\(NSRegularExpression.escapedPattern(for: name))>(.*?)</(?:[A-Za-z0-9]+:)?\(NSRegularExpression.escapedPattern(for: name))>"
        guard let regex = try? NSRegularExpression(pattern: pattern, options: [.dotMatchesLineSeparators]),
              let match = regex.firstMatch(in: body, range: NSRange(body.startIndex..., in: body)),
              let range = Range(match.range(at: 1), in: body) else { return nil }
        let value = xmlUnescape(String(body[range])).trimmingCharacters(in: .whitespacesAndNewlines)
        return value.isEmpty ? nil : value
    }
}
