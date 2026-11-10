import Foundation

/// Paths the Mac signing client already calls on the 小moto API.
/// Password login, reset, and set-password are part of that client.
/// This repository does not implement them and must not send them elsewhere.
enum AuthRoutes {
    static let passwordLogin = "v1/auth/login-password"
    static let resetPassword = "v1/auth/reset-password"
    static let setPassword = "v1/auth/set-password"
    static let requestCode = "v1/auth/request-code"
    static let verify = "v1/auth/verify"

    static func joined(base: String, path: String) -> String? {
        var trimmed = base.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        while trimmed.hasSuffix("/") {
            trimmed.removeLast()
        }
        return trimmed + "/" + path
    }

    /// Body build 39 sends for password login.
    static func passwordLoginBody(email: String, password: String, termsVersion: String) -> [String: String] {
        ["email": email, "password": password, "termsVersion": termsVersion]
    }
}
