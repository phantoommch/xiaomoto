import Foundation

enum CommunityClientError: LocalizedError {
    case notConfigured
    case badURL
    case transport(String)
    case server(String)

    var errorDescription: String? {
        switch self {
        case .notConfigured:
            return "还没有填写社区服务器地址。不填也可以离线使用本机点位。"
        case .badURL:
            return "社区服务器地址无效。"
        case .transport(let message):
            return "连接社区服务器失败：\(message)"
        case .server(let message):
            return message
        }
    }
}

struct CommunitySubmitResult: Decodable, Equatable {
    var id: String
    var status: String
    var publicPointID: String?

    enum CodingKeys: String, CodingKey {
        case id, status
        case publicPointID = "public_point_id"
    }
}

struct CommunitySyncResult {
    var points: [EnforcementPoint]
    var removedIDs: [String]
    var serverTime: String
}

enum CommunityClient {
    static func submit(
        baseURL: String,
        deviceID: String,
        point: EnforcementPoint,
        imageJPEG: Data?
    ) async throws -> CommunitySubmitResult {
        let root = try endpoint(baseURL, "v1/reports")
        var request = URLRequest(url: root)
        request.httpMethod = "POST"
        request.timeoutInterval = 40
        request.setValue(deviceID, forHTTPHeaderField: "X-Device-Id")
        request.setValue("Motocam/1.0", forHTTPHeaderField: "User-Agent")
        ApiHeaders.apply(&request)
        if let imageJPEG {
            let boundary = "motocam-\(UUID().uuidString)"
            request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
            request.httpBody = multipart(point: point, image: imageJPEG, boundary: boundary)
        } else {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONEncoder().encode(reportJSON(point))
        }
        return try await send(request, as: CommunitySubmitResult.self)
    }

    /// Nationwide download. No bounding box.
    /// Pages at 5000 so a server that still caps each response there is exhausted
    /// by following `since` with the last `updated_at`. A newer server can return
    /// the whole set in one page.
    static func sync(baseURL: String) async throws -> CommunitySyncResult {
        var ordered: [EnforcementPoint] = []
        var index: [String: Int] = [:]
        var removed = Set<String>()
        var since: String?
        var serverTime = ""
        for _ in 0..<40 {
            let page = try await fetchPage(baseURL: baseURL, since: since)
            if !page.serverTime.isEmpty { serverTime = page.serverTime }
            for id in page.removedIDs ?? [] { removed.insert(id) }
            var added = 0
            for remote in page.points {
                let point = mapPoint(remote)
                if let existing = index[point.id] {
                    ordered[existing] = point
                } else {
                    index[point.id] = ordered.count
                    ordered.append(point)
                    added += 1
                }
            }
            let cursor = page.points.last?.updatedAt
            if page.points.count < pageLimit || added == 0 || cursor == nil || cursor?.isEmpty == true || cursor == since {
                break
            }
            since = cursor
        }
        let points = ordered.filter { !removed.contains($0.id) }
        return CommunitySyncResult(points: points, removedIDs: Array(removed), serverTime: serverTime)
    }

    private static let pageLimit = 5000

    private static func fetchPage(baseURL: String, since: String?) async throws -> ListEnvelope {
        let root = try endpoint(baseURL, "v1/points")
        var components = URLComponents(url: root, resolvingAgainstBaseURL: false)
        var items = [URLQueryItem(name: "limit", value: String(pageLimit))]
        if let since, !since.isEmpty {
            items.append(URLQueryItem(name: "since", value: since))
        }
        components?.queryItems = items
        guard let url = components?.url else { throw CommunityClientError.badURL }
        var request = URLRequest(url: url)
        request.timeoutInterval = 30
        request.setValue("Motocam/1.0", forHTTPHeaderField: "User-Agent")
        ApiHeaders.apply(&request)
        return try await send(request, as: ListEnvelope.self)
    }

    private static func endpoint(_ raw: String, _ path: String) throws -> URL {
        var trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { throw CommunityClientError.notConfigured }
        while trimmed.hasSuffix("/") { trimmed.removeLast() }
        guard let url = URL(string: trimmed + "/" + path), url.scheme == "http" || url.scheme == "https" else {
            throw CommunityClientError.badURL
        }
        return url
    }

    private static func send<T: Decodable>(_ request: URLRequest, as type: T.Type) async throws -> T {
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await URLSession.shared.data(for: request)
        } catch {
            throw CommunityClientError.transport(error.localizedDescription)
        }
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        if status == 401 {
            ApiHeaders.noteUnauthorized(401)
        }
        if !(200..<300).contains(status) {
            let message = (try? JSONDecoder().decode(ErrorEnvelope.self, from: data))?.error
            throw CommunityClientError.server(message ?? "服务器返回 \(status)")
        }
        do {
            return try JSONDecoder().decode(T.self, from: data)
        } catch {
            throw CommunityClientError.server("服务器响应无法解析")
        }
    }

    private struct ReportJSON: Encodable {
        var name: String
        var type: String
        var road_code: String
        var road_name: String
        var notes: String
        var latitude: Double?
        var longitude: Double?
        var fine_yuan: Int?
        var penalty_points: Int?
        var violation: String
        var ocr_location: String
        var amap_name: String
        var located: Bool?
        var ocr_text: String
        var parsed: String
        var confidence: String
        var stake: String
        var direction: String
        var end_latitude: Double?
        var end_longitude: Double?

        func encode(to encoder: Encoder) throws {
            var c = encoder.container(keyedBy: CodingKeys.self)
            try c.encode(name, forKey: .name)
            try c.encode(type, forKey: .type)
            try c.encode(road_code, forKey: .road_code)
            try c.encode(road_name, forKey: .road_name)
            try c.encode(notes, forKey: .notes)
            try c.encode(violation, forKey: .violation)
            try c.encode(ocr_location, forKey: .ocr_location)
            try c.encode(amap_name, forKey: .amap_name)
            try c.encode(ocr_text, forKey: .ocr_text)
            try c.encode(parsed, forKey: .parsed)
            try c.encode(confidence, forKey: .confidence)
            try c.encode(stake, forKey: .stake)
            try c.encode(direction, forKey: .direction)
            try c.encodeIfPresent(end_latitude, forKey: .end_latitude)
            try c.encodeIfPresent(end_longitude, forKey: .end_longitude)
            try c.encodeIfPresent(fine_yuan, forKey: .fine_yuan)
            try c.encodeIfPresent(penalty_points, forKey: .penalty_points)
            if let latitude, let longitude {
                try c.encode(latitude, forKey: .latitude)
                try c.encode(longitude, forKey: .longitude)
            } else {
                try c.encodeNil(forKey: .latitude)
                try c.encodeNil(forKey: .longitude)
                try c.encode(false, forKey: .located)
            }
        }

        enum CodingKeys: String, CodingKey {
            case name, type, road_code, road_name, notes, latitude, longitude, violation
            case fine_yuan, penalty_points, ocr_location, amap_name, located
            case ocr_text, parsed, confidence, stake, direction, end_latitude, end_longitude
        }
    }

    private static func reportJSON(_ point: EnforcementPoint) -> ReportJSON {
        ReportJSON(
            name: point.name,
            type: point.type.rawValue,
            road_code: point.roadCode,
            road_name: point.roadName,
            notes: point.notes,
            latitude: point.located ? point.latitude : nil,
            longitude: point.located ? point.longitude : nil,
            fine_yuan: point.fineYuan,
            penalty_points: point.penaltyPoints,
            violation: point.violation,
            ocr_location: point.ocrLocation,
            amap_name: point.amapName,
            located: point.located ? nil : false,
            ocr_text: point.ocrText,
            parsed: point.parsed,
            confidence: point.confidence,
            stake: point.stake,
            direction: point.direction,
            end_latitude: point.endLatitude,
            end_longitude: point.endLongitude
        )
    }

    private static func multipart(point: EnforcementPoint, image: Data, boundary: String) -> Data {
        var body = Data()
        func field(_ name: String, _ value: String) {
            body.append(Data("--\(boundary)\r\n".utf8))
            body.append(Data("Content-Disposition: form-data; name=\"\(name)\"\r\n\r\n".utf8))
            body.append(Data(value.utf8))
            body.append(Data("\r\n".utf8))
        }
        let json = reportJSON(point)
        field("name", json.name)
        field("type", json.type)
        field("road_code", json.road_code)
        field("road_name", json.road_name)
        field("notes", json.notes)
        field("ocr_location", json.ocr_location)
        field("amap_name", json.amap_name)
        field("ocr_text", json.ocr_text)
        field("parsed", json.parsed)
        field("confidence", json.confidence)
        field("stake", json.stake)
        field("direction", json.direction)
        if let end = json.end_latitude { field("end_latitude", String(end)) }
        if let end = json.end_longitude { field("end_longitude", String(end)) }
        if let latitude = json.latitude, let longitude = json.longitude {
            field("latitude", String(latitude))
            field("longitude", String(longitude))
        } else {
            field("located", "0")
        }
        field("violation", json.violation)
        if let fine = json.fine_yuan { field("fine_yuan", String(fine)) }
        if let points = json.penalty_points { field("penalty_points", String(points)) }
        body.append(Data("--\(boundary)\r\n".utf8))
        body.append(Data("Content-Disposition: form-data; name=\"image\"; filename=\"report.jpg\"\r\n".utf8))
        body.append(Data("Content-Type: image/jpeg\r\n\r\n".utf8))
        body.append(image)
        body.append(Data("\r\n--\(boundary)--\r\n".utf8))
        return body
    }

    private struct ErrorEnvelope: Decodable { var error: String }

    private struct ListEnvelope: Decodable {
        var serverTime: String
        var points: [RemotePoint]
        var removedIDs: [String]?

        enum CodingKeys: String, CodingKey {
            case points
            case serverTime = "server_time"
            case removedIDs = "removed_ids"
        }
    }

    private struct RemotePoint: Decodable {
        var id: String
        var name: String
        var type: String
        var roadCode: String?
        var roadName: String?
        var notes: String?
        var latitude: Double
        var longitude: Double
        var coord: String?
        var fineYuan: Int?
        var penaltyPoints: Int?
        var violation: String?
        var sourceCount: Int?
        var updatedAt: String?
        var endLatitude: Double?
        var endLongitude: Double?
        var stake: String?
        var direction: String?
        var origin: String?

        enum CodingKeys: String, CodingKey {
            case id, name, type, notes, latitude, longitude, coord, violation
            case roadCode = "road_code"
            case roadName = "road_name"
            case fineYuan = "fine_yuan"
            case penaltyPoints = "penalty_points"
            case sourceCount = "source_count"
            case updatedAt = "updated_at"
            case endLatitude = "end_latitude"
            case endLongitude = "end_longitude"
            case stake, direction
            case origin = "source"
        }
    }

    private static func mapPoint(_ remote: RemotePoint) -> EnforcementPoint {
        var latitude = remote.latitude
        var longitude = remote.longitude
        let system = (remote.coord ?? "wgs84").lowercased()
        if system == "gcj02" || system == "gcj-02" {
            let wgs = CoordinateConverter.gcj02ToWgs84(
                CoordinateConverter.Coord(latitude: latitude, longitude: longitude)
            )
            latitude = wgs.latitude
            longitude = wgs.longitude
        }
        let updated = remote.updatedAt.flatMap(ISO8601Codec.date(from:)) ?? Date()
        return EnforcementPoint(
            id: remote.id,
            name: remote.name,
            type: PointType(rawValue: remote.type) ?? .other,
            roadCode: remote.roadCode ?? "",
            roadName: remote.roadName ?? "",
            notes: remote.notes ?? "",
            latitude: latitude,
            longitude: longitude,
            fineYuan: remote.fineYuan,
            penaltyPoints: remote.penaltyPoints,
            violation: remote.violation ?? "",
            source: .community,
            createdAt: updated,
            updatedAt: updated,
            isCommunity: true,
            communitySourceCount: remote.sourceCount,
            endLatitude: remote.endLatitude,
            endLongitude: remote.endLongitude,
            stake: remote.stake ?? "",
            direction: remote.direction ?? "",
            uploadSource: remote.origin ?? ""
        )
    }
}
