import Foundation

enum AmapClientError: LocalizedError {
    case missingKey
    case badURL
    case transport(String)
    case service(String)

    var errorDescription: String? {
        switch self {
        case .missingKey:
            return "还没有高德 Web 服务 Key。"
        case .badURL:
            return "搜索地址无法生成。"
        case .transport(let message):
            return "搜索失败：\(message)"
        case .service(let message):
            return message
        }
    }
}

private struct FlexibleString: Decodable {
    var value: String
    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if let string = try? container.decode(String.self) {
            value = string
        } else {
            value = ""
        }
    }
}

private struct AmapResponse: Decodable {
    var status: String?
    var info: String?
    var infocode: String?
    var pois: [AmapPOI]?
}

private struct GeocodeResponse: Decodable {
    var status: String?
    var geocodes: [GeocodeRow]?
}

private struct GeocodeRow: Decodable {
    var formattedAddress: FlexibleString?
    var province: FlexibleString?
    var city: FlexibleString?
    var district: FlexibleString?
    var location: String?
    var level: FlexibleString?

    enum CodingKeys: String, CodingKey {
        case province, city, district, location, level
        case formattedAddress = "formatted_address"
    }
}

private struct AmapPOI: Decodable {
    var id: String?
    var name: String?
    var type: String?
    var typecode: String?
    var address: FlexibleString?
    var location: String?
    var pname: String?
    var cityname: String?
    var adname: String?
}

enum AmapClient {
    static func search(key: String, keywords: String, city: String?, types: String?, limitToCity: Bool = false) async throws -> [PlaceCandidate] {
        let trimmedKey = key.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedKey.isEmpty else { throw AmapClientError.missingKey }
        guard let url = AmapRequest.placeTextURL(key: trimmedKey, keywords: keywords, city: city, types: types, limitToCity: limitToCity) else {
            throw AmapClientError.badURL
        }
        var request = URLRequest(url: url)
        request.timeoutInterval = 20
        request.setValue("Motocam/1.0", forHTTPHeaderField: "User-Agent")
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await URLSession.shared.data(for: request)
        } catch {
            throw AmapClientError.transport(error.localizedDescription)
        }
        if let http = response as? HTTPURLResponse, !(200..<300).contains(http.statusCode) {
            throw AmapClientError.transport("HTTP \(http.statusCode)")
        }
        let decoded = try JSONDecoder().decode(AmapResponse.self, from: data)
        guard decoded.status == "1" else {
            throw AmapClientError.service(explain(info: decoded.info, code: decoded.infocode))
        }
        return (decoded.pois ?? []).compactMap(candidate(from:))
    }

    static func geocode(key: String, address: String, city: String? = nil) async -> [PlaceCandidate] {
        let trimmedKey = key.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedAddress = address.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedKey.isEmpty, !trimmedAddress.isEmpty else { return [] }
        var components = URLComponents(string: "https://restapi.amap.com/v3/geocode/geo")
        var items = [
            URLQueryItem(name: "key", value: trimmedKey),
            URLQueryItem(name: "address", value: trimmedAddress),
        ]
        if let city, !city.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            items.append(URLQueryItem(name: "city", value: city))
        }
        components?.queryItems = items
        guard let url = components?.url else { return [] }
        var request = URLRequest(url: url)
        request.timeoutInterval = 20
        request.setValue("Motocam/1.0", forHTTPHeaderField: "User-Agent")
        guard let (data, response) = try? await URLSession.shared.data(for: request) else { return [] }
        if let http = response as? HTTPURLResponse, !(200..<300).contains(http.statusCode) { return [] }
        guard let decoded = try? JSONDecoder().decode(GeocodeResponse.self, from: data), decoded.status == "1" else {
            return []
        }
        var seen = Set<String>()
        return (decoded.geocodes ?? []).compactMap { row -> PlaceCandidate? in
            guard let location = row.location else { return nil }
            let parts = location.split(separator: ",")
            guard parts.count == 2, let longitude = Double(parts[0]), let latitude = Double(parts[1]) else { return nil }
            let key = "\(latitude),\(longitude)"
            guard seen.insert(key).inserted else { return nil }
            let name = (row.formattedAddress?.value ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            guard !name.isEmpty else { return nil }
            return PlaceCandidate(
                id: "geo-\(longitude),\(latitude)",
                name: name,
                address: name,
                province: row.province?.value ?? "",
                city: row.city?.value ?? "",
                district: row.district?.value ?? "",
                typeCode: "190301",
                typeName: row.level?.value ?? "道路",
                gcjLatitude: latitude,
                gcjLongitude: longitude
            )
        }
    }

    private static func candidate(from poi: AmapPOI) -> PlaceCandidate? {
        guard let location = poi.location else { return nil }
        let parts = location.split(separator: ",")
        guard parts.count == 2, let longitude = Double(parts[0]), let latitude = Double(parts[1]) else { return nil }
        let name = (poi.name ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty else { return nil }
        return PlaceCandidate(
            id: poi.id ?? UUID().uuidString,
            name: name,
            address: poi.address?.value ?? "",
            province: poi.pname ?? "",
            city: poi.cityname ?? "",
            district: poi.adname ?? "",
            typeCode: poi.typecode ?? "",
            typeName: poi.type ?? "",
            gcjLatitude: latitude,
            gcjLongitude: longitude
        )
    }

    private static func explain(info: String?, code: String?) -> String {
        switch info {
        case "INVALID_USER_KEY":
            return "高德 Key 无效。"
        case "USERKEY_PLAT_NOMATCH":
            return "这个 Key 不是 Web 服务类型，或平台不匹配。请使用 Web 服务 Key，不要用 iOS SDK Key。"
        case "INVALID_USER_IP", "INVALID_USER_IP_ACCESS":
            return "当前网络 IP 不在 Key 白名单。手机直接请求时，需要放行出口 IP，或暂时关闭 IP 限制。"
        case "DAILY_QUERY_OVER_LIMIT":
            return "高德 Key 今日调用次数已用完。"
        default:
            let detail = info ?? code ?? "未知错误"
            return "高德没有返回结果（\(detail)）。"
        }
    }
}

enum TicketPlacer {
    static func bestMatch(key: String, query: ParsedQuery) async throws -> RankedPlace? {
        let plan = QueryCleaner.locatePlan(query)
        if QueryCleaner.bareStake(query) {
            for attempt in QueryCleaner.stakeAttempts(query) {
                let hits = await AmapClient.geocode(key: key, address: attempt.address, city: plan.region)
                    .filter { QueryCleaner.matchesStake($0, roadCode: attempt.roadCode, kilometre: attempt.kilometre) }
                if let best = PlaceRanker.rank(hits, query: attempt.query, cityHint: plan.region).first {
                    return best
                }
            }
            return nil
        }
        let limit = plan.region != nil
        var places: [PlaceCandidate] = []
        for keyword in plan.poiKeywords where !keyword.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            places += try await AmapClient.search(key: key, keywords: keyword, city: plan.region, types: nil, limitToCity: limit)
        }
        let located = places.isEmpty
            ? await AmapClient.geocode(key: key, address: plan.geocodeAddress, city: plan.region)
            : []
        return PlaceRanker.rank(located + places, query: query, cityHint: plan.region).first
    }
}
