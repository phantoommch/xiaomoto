import Foundation

/// Routes the Mac signing client actually calls. These are not a license to
/// send 小moto sessions to another handler.
///
/// `v1/hajimi/auth/login` answers on the same host and is a different handler:
/// its bad-JSON text is `请求格式不对`, while 小moto `/v1/auth/verify` says
/// `请求体无效`, and `/v1/hajimi/me` is 404. Nothing in this repo shows a shared
/// users table, session hash, or `token` claim with that handler. Do not point
/// password login at it.
enum AuthRoutes {
    static let passwordLogin = "v1/auth/login-password"
    static let resetPassword = "v1/auth/reset-password"
    static let setPassword = "v1/auth/set-password"
    static let requestCode = "v1/auth/request-code"
    static let verify = "v1/auth/verify"
    /// Present on the gateway. Not the 小moto account namespace.
    static let unprovenForeignLogin = "v1/hajimi/auth/login"

    static func joined(base: String, path: String) -> String? {
        var trimmed = base.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        while trimmed.hasSuffix("/") {
            trimmed.removeLast()
        }
        return trimmed + "/" + path
    }

    /// Body build 39 sends. `termsVersion` stays; the UI agreement check uses it.
    static func passwordLoginBody(email: String, password: String, termsVersion: String) -> [String: String] {
        ["email": email, "password": password, "termsVersion": termsVersion]
    }
}
