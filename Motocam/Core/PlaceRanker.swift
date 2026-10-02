import Foundation

struct PlaceCandidate: Equatable, Identifiable {
    var id: String
    var name: String
    var address: String
    var province: String
    var city: String
    var district: String
    var typeCode: String
    var typeName: String
    /// GCJ-02, as returned by Amap.
    var gcjLatitude: Double
    var gcjLongitude: Double

    var wgs84: CoordinateConverter.Coord {
        CoordinateConverter.gcj02ToWgs84(
            CoordinateConverter.Coord(latitude: gcjLatitude, longitude: gcjLongitude)
        )
    }
}

struct RankedPlace: Equatable, Identifiable {
    var place: PlaceCandidate
    var score: Int
    var reasons: [String]

    var id: String { place.id }
}

enum PlaceRanker {
    /// Rank Amap hits for a ticket-derived query.
    ///
    /// Highway-code agreement dominates. Searching “赤石收费站” can return the
    /// Guangdong G15 深汕 station; a query that contains G25 / 长深高速 must
    /// sink that hit even when the names look the same.
    static func rank(
        _ places: [PlaceCandidate],
        query: ParsedQuery,
        cityHint: String?
    ) -> [RankedPlace] {
        let hint = cityHint.map { TextNormalize.collapseSpaces($0) } ?? ""
        return places.enumerated().map { index, place in
            score(place, query: query, cityHint: hint, index: index)
        }
        .sorted { lhs, rhs in
            if lhs.score != rhs.score { return lhs.score > rhs.score }
            return lhs.place.id < rhs.place.id
        }
    }

    private static func score(
        _ place: PlaceCandidate,
        query: ParsedQuery,
        cityHint: String,
        index: Int
    ) -> RankedPlace {
        var score = 0
        var reasons: [String] = []
        let blob = "\(place.name) \(place.address) \(place.province) \(place.city) \(place.district)"
        let keywords = query.keywords

        if !keywords.isEmpty, place.name.contains(keywords) || keywords.contains(place.name) && place.name.count >= 2 {
            score += 40
            reasons.append("名称包含 \(keywords)")
        } else if !keywords.isEmpty, place.address.contains(keywords) {
            score += 15
            reasons.append("地址包含 \(keywords)")
        }

        let core = keywords.replacingOccurrences(of: "收费站", with: "")
        if core.count >= 2, place.name.contains(core), !reasons.contains(where: { $0.hasPrefix("名称包含") }) {
            score += 15
            reasons.append("名称含 \(core)")
        }

        let codes = QueryCleaner.roadCodes(in: blob)
        if let wanted = query.roadCode {
            if codes.contains(wanted) {
                score += 100
                reasons.append("匹配公路编号 \(wanted)")
            } else if !codes.isEmpty {
                score -= 100
                reasons.append("结果含其他公路编号 \(codes.joined(separator: "、"))")
            }
        }

        if let roadName = query.roadName, blob.contains(roadName) {
            score += 60
            reasons.append("匹配路名 \(roadName)")
        }

        let source = query.cleaned.isEmpty ? query.original : query.cleaned
        if let kilometre = QueryCleaner.stakeKilometre(in: source) {
            let compact = blob.replacingOccurrences(of: " ", with: "").uppercased()
            let hit = compact.contains("\(kilometre)公里") || compact.contains("\(kilometre)千米") || compact.contains("\(kilometre)KM")
            if hit {
                score += 80
                reasons.append("桩号 \(kilometre) 公里")
            } else if QueryCleaner.bareStake(query) {
                score -= 80
                reasons.append("没有这个桩号")
            }
        }

        if !cityHint.isEmpty, regionMatches(cityHint, province: place.province, city: place.city, district: place.district) {
            score += 40
            reasons.append("城市或省份与提示一致")
        }

        let isToll = place.typeCode.hasPrefix("1802") || place.name.contains("收费站") || place.typeName.contains("收费站")
        if query.wantsToll && isToll {
            score += 25
            reasons.append("收费站类型")
        } else if query.wantsToll && !isToll {
            score -= 15
            reasons.append("不像收费站")
        }

        let facilityWords = ["隧道", "服务区", "互通", "枢纽", "立交", "大桥", "桥"]
        if let hit = facilityWords.first(where: { word in
            (query.original.contains(word) || query.keywords.contains(word))
                && (place.name.contains(word) || place.typeName.contains(word))
        }) {
            score += 50
            reasons.append("道路设施 \(hit)")
        }

        score -= index
        if reasons.isEmpty {
            reasons.append("按搜索顺序")
        }
        return RankedPlace(place: place, score: score, reasons: reasons)
    }

    private static func regionMatches(_ hint: String, province: String, city: String, district: String) -> Bool {
        let key = TextNormalize.regionKey(hint)
        guard key.count >= 2 else { return false }
        let fields = [province, city, district].map(TextNormalize.regionKey)
        return fields.contains { field in
            !field.isEmpty && (field.contains(key) || key.contains(field))
        }
    }
}

enum PlaceChoice {
    /// Index penalty is at most 19. A smaller gap is search order, not a real distinction.
    private static let orderGap = 19

    static func uniqueBest(_ ranked: [RankedPlace]) -> RankedPlace? {
        guard let top = ranked.first else { return nil }
        let name = top.place.name.trimmingCharacters(in: .whitespacesAndNewlines)
        let ambiguous = ranked.dropFirst().contains { other in
            other.place.name.trimmingCharacters(in: .whitespacesAndNewlines) == name
                && other.score >= top.score - orderGap
                && !sameSpot(top.place, other.place)
        }
        return ambiguous ? nil : top
    }

    private static func sameSpot(_ a: PlaceCandidate, _ b: PlaceCandidate) -> Bool {
        GeoMath.distanceMeters(
            a.wgs84.latitude, a.wgs84.longitude,
            b.wgs84.latitude, b.wgs84.longitude
        ) < 300
    }
}

enum PlacementStatus {
    static func reviewPrompt(marked: Int) -> String {
        "已标注 \(marked) 个违法点，请核对后上传"
    }

    static let notFound = "高德没找到这个地点，已保存待后台核对"

    static func sentence(marked: Int, uploaded: Int) -> String {
        guard marked > 0 else { return "" }
        if uploaded >= marked { return "已标注并上传 \(marked) 个违法点" }
        if uploaded <= 0 { return "已标注 \(marked) 个违法点，有网后会上传" }
        return "已标注 \(marked) 个违法点，已上传 \(uploaded) 个"
    }

    static func amapMatch(_ name: String) -> String {
        "高德匹配：\(name)"
    }
}

enum TicketPoint {
    static func fromPlace(
        _ ranked: RankedPlace,
        query: ParsedQuery,
        violation: String,
        fineYuan: Int?,
        penaltyPoints: Int?
    ) -> EnforcementPoint {
        let place = ranked.place
        let codes = QueryCleaner.roadCodes(in: "\(place.name) \(place.address) \(place.province)")
        let placeText = place.name + place.address
        let roadCode = query.roadCode.flatMap { codes.contains($0) ? $0 : nil } ?? codes.first ?? ""
        let roadName = query.roadName.flatMap { placeText.contains($0) ? $0 : nil } ?? QueryCleaner.roadName(in: placeText) ?? ""
        let toll = query.wantsToll || place.typeCode.hasPrefix("1802")
        return EnforcementPoint(
            name: place.name,
            type: toll ? .bannedToll : .other,
            roadCode: roadCode,
            roadName: roadName,
            latitude: place.wgs84.latitude,
            longitude: place.wgs84.longitude,
            fineYuan: fineYuan,
            penaltyPoints: penaltyPoints,
            violation: violation,
            source: .ocr,
            province: place.province,
            city: place.city,
            ocrLocation: query.original,
            amapName: place.name,
            located: true
        )
    }

    static func fromLocated(_ hit: LocatedPoint, record: TicketRecord) -> EnforcementPoint {
        let label = [hit.roadCode, hit.stake.isEmpty ? hit.how : hit.stake].filter { !$0.isEmpty }.joined(separator: " ")
        let parsed = "{\"code\":\"\(hit.roadCode)\",\"stake\":\"\(hit.stake)\",\"method\":\"\(hit.method)\",\"how\":\"\(hit.how)\"}"
        let name = String((label.isEmpty ? hit.how : label).prefix(80))
        return EnforcementPoint(
            name: name.isEmpty ? "未定位违法点" : name,
            type: .other,
            roadCode: hit.roadCode,
            roadName: hit.roadName,
            latitude: hit.latitude,
            longitude: hit.longitude,
            fineYuan: record.fineYuan,
            penaltyPoints: record.penaltyPoints,
            violation: record.violation ?? "",
            source: .ocr,
            ocrLocation: String((record.location ?? "").prefix(300)),
            amapName: String((hit.address.isEmpty ? hit.how : hit.address).prefix(80)),
            located: hit.latitude.isFinite && hit.longitude.isFinite,
            endLatitude: hit.endLatitude,
            endLongitude: hit.endLongitude,
            stake: hit.stake,
            direction: hit.direction,
            confidence: hit.confidence,
            ocrText: record.fullText.isEmpty ? (record.location ?? "") : record.fullText,
            parsed: parsed
        )
    }

    static func unmatched(
        location: String,
        query: ParsedQuery,
        violation: String,
        fineYuan: Int?,
        penaltyPoints: Int?
    ) -> EnforcementPoint {
        let label = location.isEmpty ? query.keywords : location
        let name = String(label.prefix(80))
        return EnforcementPoint(
            name: name.isEmpty ? "未定位违法点" : name,
            type: query.wantsToll ? .bannedToll : .other,
            roadCode: query.roadCode ?? "",
            roadName: query.roadName ?? "",
            latitude: 0,
            longitude: 0,
            fineYuan: fineYuan,
            penaltyPoints: penaltyPoints,
            violation: violation,
            source: .ocr,
            ocrLocation: location,
            amapName: "",
            located: false
        )
    }

    static func fromTap(
        latitude: Double,
        longitude: Double,
        location: String,
        query: ParsedQuery,
        violation: String,
        fineYuan: Int?,
        penaltyPoints: Int?
    ) -> EnforcementPoint {
        EnforcementPoint(
            name: location.isEmpty ? query.keywords : location,
            type: query.wantsToll ? .bannedToll : .other,
            roadCode: query.roadCode ?? "",
            roadName: query.roadName ?? "",
            latitude: latitude,
            longitude: longitude,
            fineYuan: fineYuan,
            penaltyPoints: penaltyPoints,
            violation: violation,
            source: .ocr
        )
    }
}
