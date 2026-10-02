import Foundation

enum AmapRequest {
    static func placeTextURL(key: String, keywords: String, city: String?, types: String?, limitToCity: Bool = false) -> URL? {
        var components = URLComponents(string: "https://restapi.amap.com/v3/place/text")
        var items = [
            URLQueryItem(name: "key", value: key),
            URLQueryItem(name: "keywords", value: keywords),
            URLQueryItem(name: "offset", value: "20"),
            URLQueryItem(name: "page", value: "1"),
            URLQueryItem(name: "extensions", value: "base"),
            URLQueryItem(name: "output", value: "json"),
        ]
        if let city, !city.trimmingCharacters(in: .whitespaces).isEmpty {
            items.append(URLQueryItem(name: "city", value: city))
            items.append(URLQueryItem(name: "citylimit", value: limitToCity ? "true" : "false"))
        }
        if let types, !types.isEmpty {
            items.append(URLQueryItem(name: "types", value: types))
        }
        components?.queryItems = items
        return components?.url
    }

    /// Toll preference is applied when ranking, not as a request filter.
    /// `types=180200` makes Amap return count 0 for names such as 赤石收费站.
    static func types(for query: ParsedQuery) -> String? {
        _ = query
        return nil
    }
}
