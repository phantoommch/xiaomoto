import Foundation

enum AlertBand: String, Codable, Equatable {
    case outer
    case inner
}

struct PendingAlert: Equatable {
    var point: EnforcementPoint
    var band: AlertBand
    var distanceMeters: Double
    var spokenText: String
    var noticeText: String
}

/// One pass of a point. Rings arm again only after the rider leaves the outer ring.
struct ApproachRecord: Codable, Equatable {
    var outerFired: Bool = false
    var innerFired: Bool = false
    var lastDistanceMeters: Double? = nil
}

enum AlertEngine {
    static let rearmMarginMeters = 300.0
    static let aheadDegrees = 90.0
    static let minCourseSpeedMps = 2.0
    private static let decreaseMeters = 1.0

    /// Each ring fires once per approach. A point re-arms after the rider is
    /// farther than the outer ring plus `rearmMarginMeters`. Alerts only when
    /// the point is ahead: course within 90 degrees, or, when the course
    /// cannot be trusted, when distance is decreasing.
    static func evaluate(
        latitude: Double,
        longitude: Double,
        headingDegrees: Double?,
        speedMps: Double?,
        points: [EnforcementPoint],
        settings: AlertSettings,
        approach: [String: ApproachRecord],
        mutedIDs: Set<String> = []
    ) -> (alerts: [PendingAlert], approach: [String: ApproachRecord]) {
        var settings = settings
        settings.normalize()
        var alerts: [PendingAlert] = []
        var next = approach
        var seen = Set<String>()
        let rearmAt = settings.outerMeters + rearmMarginMeters

        let ordered = deduped(points).compactMap { point -> (EnforcementPoint, Double)? in
            let distance = GeoMath.distanceMeters(latitude, longitude, point.latitude, point.longitude)
            guard distance.isFinite else { return nil }
            return (point, distance)
        }
        .sorted { $0.1 < $1.1 }

        for (point, distance) in ordered {
            seen.insert(point.id)
            if mutedIDs.contains(point.id) || !point.located {
                next.removeValue(forKey: point.id)
                continue
            }
            if point.segment, let endLat = point.endLatitude, let endLon = point.endLongitude {
                let along = GeoMath.alongSegment(latitude, longitude, point.latitude, point.longitude, endLat, endLon)
                let previous = next[point.id] ?? ApproachRecord()
                if along.distance > rearmAt {
                    next.removeValue(forKey: point.id)
                    continue
                }
                var record = previous
                record.lastDistanceMeters = along.distance
                if !record.outerFired && along.distance <= settings.outerMeters && along.fraction < 0.25 && alerts.count < settings.maxAlertsPerUpdate {
                    let line = "前方进入区间测速，\(spokenPlace(point))，请注意平均车速"
                    alerts.append(PendingAlert(point: point, band: .outer, distanceMeters: along.distance, spokenText: "注意，\(line)。", noticeText: withCrowd(line, point)))
                    record.outerFired = true
                }
                if !record.innerFired && along.distance < 400 && along.fraction >= 0.05 && along.fraction <= 0.95 && alerts.count < settings.maxAlertsPerUpdate {
                    let line = "已进入区间测速，请控制平均车速"
                    alerts.append(PendingAlert(point: point, band: .inner, distanceMeters: along.distance, spokenText: "注意，\(line)。", noticeText: withCrowd(line, point)))
                    record.innerFired = true
                    record.outerFired = true
                }
                next[point.id] = record
                continue
            }
            let previous = next[point.id] ?? ApproachRecord()
            if distance > rearmAt {
                next.removeValue(forKey: point.id)
                continue
            }
            if distance > settings.outerMeters {
                var holding = previous
                holding.lastDistanceMeters = distance
                next[point.id] = holding
                continue
            }
            let band: AlertBand = distance <= settings.innerMeters ? .inner : .outer
            let already = band == .inner ? previous.innerFired : previous.outerFired
            var record = previous
            record.lastDistanceMeters = distance
            let ahead = isAhead(
                latitude: latitude,
                longitude: longitude,
                point: point,
                heading: headingDegrees,
                speedMps: speedMps,
                distance: distance,
                lastDistance: previous.lastDistanceMeters
            )
            if !already && ahead && alerts.count < settings.maxAlertsPerUpdate {
                alerts.append(
                    PendingAlert(
                        point: point,
                        band: band,
                        distanceMeters: distance,
                        spokenText: speech(for: point, distanceMeters: distance),
                        noticeText: notice(for: point, distanceMeters: distance)
                    )
                )
                if band == .inner {
                    record.innerFired = true
                    record.outerFired = true
                } else {
                    record.outerFired = true
                }
            }
            next[point.id] = record
        }
        let stale = next.keys.filter { !seen.contains($0) }
        for key in stale {
            next.removeValue(forKey: key)
        }
        return (alerts, next)
    }

    static func stampKey(_ pointID: String, _ band: AlertBand) -> String {
        "\(pointID)|\(band.rawValue)"
    }

    static func alertTitle(_ point: EnforcementPoint) -> String {
        let name = point.name.trimmingCharacters(in: .whitespacesAndNewlines)
        let head = [point.roadCode, point.roadName].filter { part in
            !part.isEmpty && !name.contains(part)
        }
        let parts = head + [name].filter { !$0.isEmpty }
        let title = parts.isEmpty ? "点位" : parts.joined(separator: " ")
        return point.uploadSource == "user_upload" ? "\(title) 用户上传" : title
    }

    static func withCrowd(_ line: String, _ point: EnforcementPoint) -> String {
        if point.uploadSource != "user_upload" || line.contains("用户上传") { return line }
        return "\(line)，用户上传"
    }

    /// The rider has gone past the point that raised the current alert.
    static func hasPassed(
        latitude: Double,
        longitude: Double,
        headingDegrees: Double?,
        speedMps: Double?,
        point: EnforcementPoint,
        closestMeters: Double,
        currentMeters: Double
    ) -> Bool {
        if courseTrusted(headingDegrees, speedMps), let headingDegrees {
            let bearing = GeoMath.bearingDegrees(latitude, longitude, point.latitude, point.longitude)
            return GeoMath.angleDelta(headingDegrees, bearing) > aheadDegrees
        }
        return currentMeters > closestMeters + 60
    }

    /// Local points win over a community pin within 80 m so one booth is not spoken twice.
    static func deduped(_ points: [EnforcementPoint]) -> [EnforcementPoint] {
        var kept: [EnforcementPoint] = []
        for point in points {
            if let index = kept.firstIndex(where: { existing in
                !existing.segment && !point.segment &&
                    GeoMath.distanceMeters(existing.latitude, existing.longitude, point.latitude, point.longitude) <= 80
            }) {
                kept[index] = preferred(kept[index], point)
            } else {
                kept.append(point)
            }
        }
        return kept
    }

    static func speech(for point: EnforcementPoint, distanceMeters: Double) -> String {
        sentence(spokenDistance(distanceMeters), spokenPlace(point), point)
    }

    /// Notification and on-screen card. Road numbers stay digits, like G15.
    static func notice(for point: EnforcementPoint, distanceMeters: Double) -> String {
        let text = sentence(spokenDistance(distanceMeters), displayPlace(point), point)
        guard point.uploadSource == "user_upload" else { return text }
        if text.hasSuffix("。") {
            return String(text.dropLast()) + "，用户上传。"
        }
        return text + "用户上传"
    }

    private static func sentence(_ distance: String, _ place: String, _ point: EnforcementPoint) -> String {
        let kind: String
        switch point.type {
        case .bannedToll: kind = "禁止摩托车进入"
        case .bannedSegment: kind = "是禁摩路段"
        case .camera: kind = "有抓拍摄像头"
        case .other: kind = "请注意"
        }
        return "注意，前方\(distance)，\(place)，\(kind)。"
    }

    private static func displayPlace(_ point: EnforcementPoint) -> String {
        let name = point.name.trimmingCharacters(in: .whitespacesAndNewlines)
        let code = point.roadCode.trimmingCharacters(in: .whitespacesAndNewlines)
        let roadName = point.roadName.trimmingCharacters(in: .whitespacesAndNewlines)
        let covered = !name.isEmpty
            && (code.isEmpty || name.contains(code))
            && (roadName.isEmpty || name.contains(roadName))
        if covered { return name }
        let road = [code, roadName].filter { !$0.isEmpty }.joined(separator: " ")
        if road.isEmpty { return name }
        if name.isEmpty { return road }
        return "\(road)，\(name)"
    }

    private static func spokenPlace(_ point: EnforcementPoint) -> String {
        let road = spokenRoad(point)
        return road.isEmpty ? point.name : "\(road)，\(point.name)"
    }

    static func spokenRoadCode(_ code: String) -> String {
        let digits = ["0": "零", "1": "一", "2": "二", "3": "三", "4": "四", "5": "五", "6": "六", "7": "七", "8": "八", "9": "九"]
        var spoken = ""
        for character in code {
            let token = String(character)
            if let word = digits[token] {
                spoken.append(word)
            } else {
                spoken.append(token)
            }
        }
        return spoken
    }

    private static func isAhead(
        latitude: Double,
        longitude: Double,
        point: EnforcementPoint,
        heading: Double?,
        speedMps: Double?,
        distance: Double,
        lastDistance: Double?
    ) -> Bool {
        if courseTrusted(heading, speedMps), let heading {
            let bearing = GeoMath.bearingDegrees(latitude, longitude, point.latitude, point.longitude)
            return GeoMath.angleDelta(heading, bearing) <= aheadDegrees
        }
        guard let last = lastDistance else { return false }
        return distance < last - decreaseMeters
    }

    private static func courseTrusted(_ heading: Double?, _ speedMps: Double?) -> Bool {
        guard let heading, heading.isFinite, heading >= 0 else { return false }
        if let speedMps, speedMps < minCourseSpeedMps { return false }
        return true
    }

    private static func preferred(_ a: EnforcementPoint, _ b: EnforcementPoint) -> EnforcementPoint {
        if a.isCommunity != b.isCommunity {
            return a.isCommunity ? b : a
        }
        return (a.communitySourceCount ?? 0) >= (b.communitySourceCount ?? 0) ? a : b
    }

    private static func spokenDistance(_ meters: Double) -> String {
        if meters >= 1000 {
            let km = meters / 1000
            if abs(km - km.rounded()) < 0.15 {
                return "约\(Int(km.rounded()))公里"
            }
            return String(format: "约%.1f公里", km)
        }
        let rounded = Int((meters / 50).rounded() * 50)
        return "约\(max(50, rounded))米"
    }

    private static func spokenRoad(_ point: EnforcementPoint) -> String {
        var parts: [String] = []
        if !point.roadCode.isEmpty {
            parts.append(spokenRoadCode(point.roadCode))
        }
        if !point.roadName.isEmpty {
            parts.append(point.roadName)
        }
        return parts.joined()
    }
}
