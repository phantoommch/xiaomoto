import Foundation
import Observation

enum LoginCopy {
    static let points = "注册登录后才能更新违法点数据"
}

enum SessionToken {
    private static let lock = NSLock()
    private static var value: String?

    static func current() -> String? {
        lock.lock()
        defer { lock.unlock() }
        return value
    }

    static func set(_ token: String?) {
        lock.lock()
        defer { lock.unlock() }
        value = token
    }
}

enum ApiHeaders {
    static func apply(_ request: inout URLRequest) {
        let build = (Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String) ?? "30"
        request.setValue(build, forHTTPHeaderField: "X-App-Build")
        if let token = SessionToken.current(), !token.isEmpty {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
    }

    static func noteUnauthorized(_ status: Int) {
        guard status == 401 else { return }
        Task { @MainActor in
            SessionStore.shared.signOutLocal()
        }
    }
}

struct AuthSession: Equatable {
    var token: String
    var email: String
    var termsVersion: String
}

struct TermsDocument: Equatable {
    var version: String
    var title: String
    var markdown: String
}

struct AccountProfile: Equatable {
    var email: String
    var termsVersion: String
    var termsCurrent: String
    var termsAccepted: Bool
}

enum AuthClientError: LocalizedError {
    case notConfigured
    case unauthorized
    case failed(String)

    var errorDescription: String? {
        switch self {
        case .notConfigured: return "还没有服务器地址"
        case .unauthorized: return "未登录"
        case .failed(let message): return message
        }
    }
}

enum AuthClient {
    static func requestCode(baseURL: String, email: String) async throws {
        let json = try await send(baseURL: baseURL, path: "v1/auth/request-code", method: "POST", body: ["email": email], token: nil)
        if json["ok"] as? Bool == true { return }
        throw AuthClientError.failed((json["error"] as? String) ?? "获取激活码失败")
    }

    static func verify(baseURL: String, email: String, code: String, termsVersion: String) async throws -> AuthSession {
        let json = try await send(
            baseURL: baseURL,
            path: "v1/auth/verify",
            method: "POST",
            body: ["email": email, "code": code, "termsVersion": termsVersion],
            token: nil
        )
        guard let token = json["token"] as? String, !token.isEmpty else {
            throw AuthClientError.failed("登录没有返回凭证")
        }
        let user = json["user"] as? [String: Any]
        return AuthSession(
            token: token,
            email: (user?["email"] as? String) ?? email,
            termsVersion: (user?["termsVersion"] as? String) ?? termsVersion
        )
    }

    static func me(baseURL: String) async throws -> AccountProfile {
        let json = try await send(baseURL: baseURL, path: "v1/me", method: "GET", body: nil, token: SessionToken.current())
        return AccountProfile(
            email: json["email"] as? String ?? "",
            termsVersion: json["termsVersion"] as? String ?? "",
            termsCurrent: json["termsCurrent"] as? String ?? "1.0",
            termsAccepted: json["termsAccepted"] as? Bool ?? false
        )
    }

    static func logout(baseURL: String) async throws {
        _ = try await send(baseURL: baseURL, path: "v1/auth/logout", method: "POST", body: nil, token: SessionToken.current())
    }

    static func deleteAccount(baseURL: String) async throws {
        _ = try await send(baseURL: baseURL, path: "v1/me", method: "DELETE", body: nil, token: SessionToken.current())
    }

    static func accept(baseURL: String, version: String) async throws {
        _ = try await send(
            baseURL: baseURL,
            path: "v1/legal/accept",
            method: "POST",
            body: ["version": version],
            token: SessionToken.current()
        )
    }

    static func terms(baseURL: String) async throws -> TermsDocument {
        let json = try await send(baseURL: baseURL, path: "v1/legal/terms", method: "GET", body: nil, token: nil)
        let markdown = json["markdown"] as? String ?? ""
        if markdown.isEmpty { throw AuthClientError.failed("协议是空的") }
        return TermsDocument(
            version: (json["version"] as? String).flatMap { $0.isEmpty ? nil : $0 } ?? "1.0",
            title: (json["title"] as? String).flatMap { $0.isEmpty ? nil : $0 } ?? "用户协议与免责声明",
            markdown: markdown
        )
    }

    private static func send(
        baseURL: String,
        path: String,
        method: String,
        body: [String: String]?,
        token: String?
    ) async throws -> [String: Any] {
        var trimmed = baseURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { throw AuthClientError.notConfigured }
        while trimmed.hasSuffix("/") { trimmed.removeLast() }
        guard let url = URL(string: trimmed + "/" + path) else { throw AuthClientError.failed("服务器地址无效") }
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.timeoutInterval = 20
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("Motocam/1.0", forHTTPHeaderField: "User-Agent")
        ApiHeaders.apply(&request)
        if let token, !token.isEmpty {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await URLSession.shared.data(for: request)
        } catch {
            throw AuthClientError.failed("网络不通")
        }
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        let json = (try? JSONSerialization.jsonObject(with: data) as? [String: Any]) ?? [:]
        if status == 401 {
            ApiHeaders.noteUnauthorized(401)
            throw AuthClientError.unauthorized
        }
        if !(200..<300).contains(status) {
            throw AuthClientError.failed((json["error"] as? String) ?? "服务器返回 \(status)")
        }
        return json
    }
}

enum TermsLoader {
    static func load(baseURL: String) async -> TermsDocument {
        if let remote = try? await AuthClient.terms(baseURL: baseURL) {
            return remote
        }
        return TermsDocument(version: "1.0", title: "用户协议与免责声明", markdown: bundled())
    }

    static func bundled() -> String {
        if let url = Bundle.main.url(forResource: "terms", withExtension: "md"),
           let text = try? String(contentsOf: url, encoding: .utf8),
           !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return text
        }
        return "暂时无法加载用户协议。请稍后再试。"
    }
}

@MainActor
@Observable
final class SessionStore {
    static let shared = SessionStore()

    private(set) var email = ""
    private(set) var acceptedTerms = ""
    private(set) var loggedIn = false
    var currentTerms = "1.0"
    var showLogin = false
    var loginHint: String?
    var needsReaccept = false

    var isLoggedIn: Bool { loggedIn }

    private init() {
        email = KeychainStore.load(account: Self.emailAccount) ?? ""
        acceptedTerms = KeychainStore.load(account: Self.termsAccount) ?? ""
        let stored = KeychainStore.load(account: Self.tokenAccount)
        SessionToken.set(stored)
        loggedIn = stored?.isEmpty == false
    }

    func requireLogin(hint: String) {
        loginHint = hint
        showLogin = true
    }

    func adopt(_ session: AuthSession) {
        email = session.email
        acceptedTerms = session.termsVersion
        currentTerms = session.termsVersion
        needsReaccept = false
        showLogin = false
        SessionToken.set(session.token)
        loggedIn = !session.token.isEmpty
        try? KeychainStore.save(session.token, account: Self.tokenAccount)
        try? KeychainStore.save(session.email, account: Self.emailAccount)
        try? KeychainStore.save(session.termsVersion, account: Self.termsAccount)
    }

    func noteAccepted(_ version: String) {
        acceptedTerms = version
        currentTerms = version
        needsReaccept = false
        try? KeychainStore.save(version, account: Self.termsAccount)
    }

    func signOutLocal() {
        email = ""
        acceptedTerms = ""
        needsReaccept = false
        SessionToken.set(nil)
        loggedIn = false
        KeychainStore.delete(account: Self.tokenAccount)
        KeychainStore.delete(account: Self.emailAccount)
        KeychainStore.delete(account: Self.termsAccount)
    }

    func refresh(baseURL: String) async {
        guard isLoggedIn else { return }
        do {
            let profile = try await AuthClient.me(baseURL: baseURL)
            email = profile.email
            acceptedTerms = profile.termsVersion
            currentTerms = profile.termsCurrent
            needsReaccept = !profile.termsAccepted
            try? KeychainStore.save(profile.email, account: Self.emailAccount)
            try? KeychainStore.save(profile.termsVersion, account: Self.termsAccount)
        } catch AuthClientError.unauthorized {
            signOutLocal()
        } catch {
        }
    }

    private static let tokenAccount = "session.token"
    private static let emailAccount = "session.email"
    private static let termsAccount = "session.terms"
}
