import Foundation

/// Root-relative API paths. A path that starts with `v1/` is joined onto the
/// API origin, not onto whatever directory happened to be in the base URL.
enum APIPath {
    /// Password login that Shanghai actually serves. Failures are JSON.
    static let passwordLogin = "v1/hajimi/auth/login"
    /// Same handler as `passwordLogin`. `POST /v1/auth/login` on
    /// api.xiaomoto.cloud is the plain-text 404 build 39 renders as
    /// 「服务器返回 404」.
    static let passwordLoginAlias = "v1/auth/login"
    static let passwordRegister = "v1/hajimi/auth/register"
    static let emailCode = "v1/hajimi/auth/email/code"
    static let emailVerify = "v1/hajimi/auth/email/verify"
    static let passwordReset = "v1/hajimi/auth/password/reset"
    static let requestCode = "v1/auth/request-code"
    static let verify = "v1/auth/verify"
    static let logout = "v1/auth/logout"
    static let me = "v1/me"
    static let acceptTerms = "v1/legal/accept"
    static let terms = "v1/legal/terms"
    static let points = "v1/points"
    static let reports = "v1/reports"
    static let myReports = "v1/me/reports"
    static let appLatestIOS = "v1/app/latest?platform=ios"
    static let appLatestAndroid = "v1/app/latest?platform=android"
}

enum APIURL {
    /// Join `base` and a root-relative `path`.
    ///
    /// `https://host`, `https://host/v1`, and `https://host/v1/hajimi` all
    /// resolve a `v1/...` path onto `https://host/v1/...`, so a base that
    /// already contains the API prefix cannot produce `/v1/v1/`. A different
    /// mount such as `/gateway` is preserved. A relative path that is already
    /// doubled (`v1/v1/...`) stays doubled and therefore 404s.
    static func resolve(_ base: String, _ path: String) -> URL? {
        let raw = trimTrailingSlashes(base.trimmingCharacters(in: .whitespacesAndNewlines))
        guard !raw.isEmpty, !raw.contains("$(") else { return nil }
        guard let baseURL = URL(string: raw),
              let scheme = baseURL.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              let host = baseURL.host, !host.isEmpty,
              baseURL.user == nil
        else { return nil }
        guard let relative = split(path) else { return nil }
        let baseParts = segments(baseURL.path)
        let merged: [String]
        if relative.segments.first == "v1", baseParts.isEmpty || baseParts.first == "v1" {
            merged = relative.segments
        } else {
            merged = baseParts + relative.segments
        }
        guard !merged.isEmpty else { return nil }
        var components = URLComponents()
        components.scheme = scheme
        components.host = host
        components.port = baseURL.port
        components.path = "/" + merged.joined(separator: "/")
        if let query = relative.query {
            components.query = query
        }
        return components.url
    }

    private static func trimTrailingSlashes(_ value: String) -> String {
        var raw = value
        while raw.hasSuffix("/") {
            raw.removeLast()
        }
        return raw
    }

    private struct Relative {
        var segments: [String]
        var query: String?
    }

    private static func split(_ path: String) -> Relative? {
        var raw = path.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !raw.isEmpty, !raw.contains("://"), !raw.contains("..") else { return nil }
        while raw.hasPrefix("/") {
            raw.removeFirst()
        }
        let halves = raw.split(separator: "?", maxSplits: 1, omittingEmptySubsequences: false)
        let head = String(halves[0])
        let query = halves.count == 2 ? String(halves[1]) : nil
        if let query, query.isEmpty || query.contains(" ") {
            return nil
        }
        let parts = segments(head)
        guard !parts.isEmpty, parts.allSatisfy({ !$0.contains(" ") }) else { return nil }
        return Relative(segments: parts, query: query)
    }

    private static func segments(_ path: String) -> [String] {
        path.split(separator: "/").map(String.init).filter { !$0.isEmpty && $0 != "." }
    }
}
