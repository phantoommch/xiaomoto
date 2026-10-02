import Foundation

enum PointType: String, Codable, CaseIterable, Identifiable {
    case bannedToll = "banned_toll"
    case bannedSegment = "banned_segment"
    case camera = "camera"
    case other = "other"

    var id: String { rawValue }

    var title: String {
        switch self {
        case .bannedToll: return "禁摩收费站"
        case .bannedSegment: return "禁摩路段"
        case .camera: return "抓拍摄像头"
        case .other: return "其他"
        }
    }
}

enum PointSource: String, Codable, CaseIterable, Identifiable {
    case manual
    case search
    case ocr
    case community

    var id: String { rawValue }

    var title: String {
        switch self {
        case .manual: return "手动"
        case .search: return "搜索"
        case .ocr: return "截图识别"
        case .community: return "社区"
        }
    }
}

struct AlertSettings: Codable, Equatable {
    var outerMeters: Double = 2_000
    var innerMeters: Double = 500
    var cooldownSeconds: TimeInterval = 30 * 60
    var onlyWhenApproaching: Bool = false
    var approachAngleDegrees: Double = 60
    var maxAlertsPerUpdate: Int = 2

    mutating func normalize() {
        outerMeters = min(AlertDistance.outerMax, max(AlertDistance.outerMin, outerMeters))
        innerMeters = min(outerMeters - 50, max(AlertDistance.innerMin, innerMeters))
        cooldownSeconds = min(6 * 3600, max(60, cooldownSeconds))
        approachAngleDegrees = min(180, max(10, approachAngleDegrees))
        maxAlertsPerUpdate = min(5, max(1, maxAlertsPerUpdate))
    }
}

/// Outer-ring choices. Finer near the rider, then 5 km steps out to 80 km.
enum AlertDistance {
    static let outerMin = 300.0
    static let outerMax = 80_000.0
    static let innerMin = 50.0
    static let nearbyFloorMeters = 15_000.0

    static let outerStops: [Double] = {
        var meters = outerMin
        var stops: [Double] = []
        while meters < 1_000 {
            stops.append(meters)
            meters += 100
        }
        while meters < 5_000 {
            stops.append(meters)
            meters += 500
        }
        while meters < 20_000 {
            stops.append(meters)
            meters += 1_000
        }
        while meters <= outerMax {
            stops.append(meters)
            meters += 5_000
        }
        return stops
    }()

    static func index(of meters: Double) -> Int {
        var best = 0
        var bestGap = Double.greatestFiniteMagnitude
        for (index, stop) in outerStops.enumerated() {
            let gap = abs(stop - meters)
            if gap < bestGap {
                best = index
                bestGap = gap
            }
        }
        return best
    }

    /// Map and sync look at least 15 km, and at least as far as the outer ring.
    static func nearbyRadius(_ outerMeters: Double) -> Double {
        max(nearbyFloorMeters, min(outerMax, max(outerMin, outerMeters)))
    }

    static func nearbyKilometers(_ outerMeters: Double) -> Int {
        Int((nearbyRadius(outerMeters) / 1_000).rounded())
    }

    static func format(_ meters: Double) -> String {
        if meters < 1_000 {
            return "\(Int(meters.rounded())) 米"
        }
        let tenths = Int((meters / 100).rounded())
        let whole = tenths / 10
        let fraction = tenths % 10
        if fraction == 0 {
            return "\(whole) 公里"
        }
        return "\(whole).\(fraction) 公里"
    }
}

/// A saved enforcement point. `latitude` / `longitude` are always WGS-84.
struct EnforcementPoint: Codable, Identifiable, Equatable {
    var id: String
    var name: String
    var type: PointType
    var roadCode: String
    var roadName: String
    var notes: String
    var latitude: Double
    var longitude: Double
    var fineYuan: Int?
    var penaltyPoints: Int?
    var violation: String
    var source: PointSource
    var province: String
    var city: String
    var createdAt: Date
    var updatedAt: Date
    var isCommunity: Bool
    var communitySourceCount: Int?
    var ocrLocation: String
    var amapName: String
    var located: Bool
    var endLatitude: Double?
    var endLongitude: Double?
    var stake: String
    var direction: String
    var confidence: String
    var ocrText: String
    var parsed: String
    var uploadSource: String

    var segment: Bool { endLatitude != nil && endLongitude != nil }

    init(
        id: String = UUID().uuidString,
        name: String,
        type: PointType,
        roadCode: String = "",
        roadName: String = "",
        notes: String = "",
        latitude: Double,
        longitude: Double,
        fineYuan: Int? = nil,
        penaltyPoints: Int? = nil,
        violation: String = "",
        source: PointSource,
        province: String = "",
        city: String = "",
        createdAt: Date = Date(),
        updatedAt: Date = Date(),
        isCommunity: Bool = false,
        communitySourceCount: Int? = nil,
        ocrLocation: String = "",
        amapName: String = "",
        located: Bool = true,
        endLatitude: Double? = nil,
        endLongitude: Double? = nil,
        stake: String = "",
        direction: String = "",
        confidence: String = "",
        ocrText: String = "",
        parsed: String = "",
        uploadSource: String = ""
    ) {
        self.id = id
        self.name = name
        self.type = type
        self.roadCode = roadCode
        self.roadName = roadName
        self.notes = notes
        self.latitude = latitude
        self.longitude = longitude
        self.fineYuan = fineYuan
        self.penaltyPoints = penaltyPoints
        self.violation = violation
        self.source = source
        self.province = province
        self.city = city
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.isCommunity = isCommunity
        self.communitySourceCount = communitySourceCount
        self.ocrLocation = ocrLocation
        self.amapName = amapName
        self.located = located
        self.endLatitude = endLatitude
        self.endLongitude = endLongitude
        self.stake = stake
        self.direction = direction
        self.confidence = confidence
        self.ocrText = ocrText
        self.parsed = parsed
        self.uploadSource = uploadSource
    }

    var gcj02: CoordinateConverter.Coord {
        CoordinateConverter.wgs84ToGcj02(
            CoordinateConverter.Coord(latitude: latitude, longitude: longitude)
        )
    }

    var roadLabel: String {
        [roadCode, roadName].filter { !$0.isEmpty }.joined(separator: " ")
    }

    enum CodingKeys: String, CodingKey {
        case id, name, type, notes, latitude, longitude, violation, source, province, city
        case roadCode = "road_code"
        case roadName = "road_name"
        case fineYuan = "fine_yuan"
        case penaltyPoints = "penalty_points"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
        case isCommunity = "is_community"
        case communitySourceCount = "community_source_count"
        case ocrLocation = "ocr_location"
        case amapName = "amap_name"
        case located
        case endLatitude = "end_latitude"
        case endLongitude = "end_longitude"
        case stake, direction, confidence, parsed
        case ocrText = "ocr_text"
        case uploadSource = "upload_source"
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decodeIfPresent(String.self, forKey: .id) ?? UUID().uuidString
        name = try c.decode(String.self, forKey: .name)
        type = try c.decodeIfPresent(PointType.self, forKey: .type) ?? .other
        roadCode = try c.decodeIfPresent(String.self, forKey: .roadCode) ?? ""
        roadName = try c.decodeIfPresent(String.self, forKey: .roadName) ?? ""
        notes = try c.decodeIfPresent(String.self, forKey: .notes) ?? ""
        latitude = try c.decode(Double.self, forKey: .latitude)
        longitude = try c.decode(Double.self, forKey: .longitude)
        fineYuan = try c.decodeIfPresent(Int.self, forKey: .fineYuan)
        penaltyPoints = try c.decodeIfPresent(Int.self, forKey: .penaltyPoints)
        violation = try c.decodeIfPresent(String.self, forKey: .violation) ?? ""
        source = try c.decodeIfPresent(PointSource.self, forKey: .source) ?? .manual
        province = try c.decodeIfPresent(String.self, forKey: .province) ?? ""
        city = try c.decodeIfPresent(String.self, forKey: .city) ?? ""
        let now = Date()
        createdAt = try c.decodeIfPresent(Date.self, forKey: .createdAt) ?? now
        updatedAt = try c.decodeIfPresent(Date.self, forKey: .updatedAt) ?? createdAt
        isCommunity = try c.decodeIfPresent(Bool.self, forKey: .isCommunity) ?? false
        communitySourceCount = try c.decodeIfPresent(Int.self, forKey: .communitySourceCount)
        ocrLocation = try c.decodeIfPresent(String.self, forKey: .ocrLocation) ?? ""
        amapName = try c.decodeIfPresent(String.self, forKey: .amapName) ?? ""
        located = try c.decodeIfPresent(Bool.self, forKey: .located) ?? true
        endLatitude = try c.decodeIfPresent(Double.self, forKey: .endLatitude)
        endLongitude = try c.decodeIfPresent(Double.self, forKey: .endLongitude)
        stake = try c.decodeIfPresent(String.self, forKey: .stake) ?? ""
        direction = try c.decodeIfPresent(String.self, forKey: .direction) ?? ""
        confidence = try c.decodeIfPresent(String.self, forKey: .confidence) ?? ""
        ocrText = try c.decodeIfPresent(String.self, forKey: .ocrText) ?? ""
        parsed = try c.decodeIfPresent(String.self, forKey: .parsed) ?? ""
        uploadSource = try c.decodeIfPresent(String.self, forKey: .uploadSource) ?? ""
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
        try c.encode(name, forKey: .name)
        try c.encode(type, forKey: .type)
        try c.encode(roadCode, forKey: .roadCode)
        try c.encode(roadName, forKey: .roadName)
        try c.encode(notes, forKey: .notes)
        try c.encode(latitude, forKey: .latitude)
        try c.encode(longitude, forKey: .longitude)
        try c.encodeIfPresent(fineYuan, forKey: .fineYuan)
        try c.encodeIfPresent(penaltyPoints, forKey: .penaltyPoints)
        try c.encode(violation, forKey: .violation)
        try c.encode(source, forKey: .source)
        try c.encode(province, forKey: .province)
        try c.encode(city, forKey: .city)
        try c.encode(createdAt, forKey: .createdAt)
        try c.encode(updatedAt, forKey: .updatedAt)
        try c.encode(isCommunity, forKey: .isCommunity)
        try c.encodeIfPresent(communitySourceCount, forKey: .communitySourceCount)
        try c.encode(ocrLocation, forKey: .ocrLocation)
        try c.encode(amapName, forKey: .amapName)
        try c.encode(located, forKey: .located)
        try c.encodeIfPresent(endLatitude, forKey: .endLatitude)
        try c.encodeIfPresent(endLongitude, forKey: .endLongitude)
        try c.encode(stake, forKey: .stake)
        try c.encode(direction, forKey: .direction)
        try c.encode(confidence, forKey: .confidence)
        try c.encode(ocrText, forKey: .ocrText)
        try c.encode(parsed, forKey: .parsed)
        try c.encode(uploadSource, forKey: .uploadSource)
    }
}

struct PointFile: Codable, Equatable {
    var version: Int
    var coord: String
    var points: [EnforcementPoint]

    init(version: Int = 1, coord: String = "wgs84", points: [EnforcementPoint]) {
        self.version = version
        self.coord = coord
        self.points = points
    }

    enum CodingKeys: String, CodingKey {
        case version, coord, points
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        version = try container.decodeIfPresent(Int.self, forKey: .version) ?? 1
        coord = try container.decodeIfPresent(String.self, forKey: .coord) ?? "wgs84"
        points = try container.decode([EnforcementPoint].self, forKey: .points)
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(version, forKey: .version)
        try container.encode(coord, forKey: .coord)
        try container.encode(points, forKey: .points)
    }
}
