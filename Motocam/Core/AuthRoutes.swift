import Foundation

/// Password-login contract for the Mac signing tree (build 39 and the next archive).
///
/// Build 39 posts `v1/auth/login-password` with `termsVersion`. That URL is a
/// plain-text 404 on `https://api.xiaomoto.cloud`. The live password route is
/// `v1/hajimi/auth/login` and accepts only `email` and `password`.
/// `reset-password` and `set-password` are still unregistered; their bodies do
/// not match the live hajimi reset handler, so they are left unchanged.
enum AuthRoutes {
    static let passwordLogin = "v1/hajimi/auth/login"
    static let rejectedBuild39PasswordLogin = "v1/auth/login-password"

    static func joined(base: String, path: String) -> String? {
        var trimmed = base.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        while trimmed.hasSuffix("/") {
            trimmed.removeLast()
        }
        return trimmed + "/" + path
    }

    /// Keys the password login body may contain. `termsVersion` is enforced in
    /// the UI and passed into the session fallback; the live handler rejects it.
    static func passwordLoginBody(email: String, password: String) -> [String: String] {
        ["email": email, "password": password]
    }
}
