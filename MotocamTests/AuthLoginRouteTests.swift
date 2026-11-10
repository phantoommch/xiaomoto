import XCTest
#if canImport(MotocamCore)
@testable import MotocamCore
#else
@testable import Motocam
#endif

final class AuthLoginRouteTests: XCTestCase {
    func testSigningPasswordRoutesStayOnXiaomotoAuth() {
        XCTAssertEqual(AuthRoutes.passwordLogin, "v1/auth/login-password")
        XCTAssertEqual(AuthRoutes.resetPassword, "v1/auth/reset-password")
        XCTAssertEqual(AuthRoutes.setPassword, "v1/auth/set-password")
        XCTAssertEqual(
            AuthRoutes.joined(base: "https://api.xiaomoto.cloud", path: AuthRoutes.passwordLogin),
            "https://api.xiaomoto.cloud/v1/auth/login-password"
        )
        XCTAssertEqual(
            AuthRoutes.joined(base: "https://api.xiaomoto.cloud/", path: AuthRoutes.resetPassword),
            "https://api.xiaomoto.cloud/v1/auth/reset-password"
        )
        XCTAssertEqual(
            AuthRoutes.joined(base: "https://api.xiaomoto.cloud", path: AuthRoutes.setPassword),
            "https://api.xiaomoto.cloud/v1/auth/set-password"
        )
    }

    func testPasswordLoginBodyKeepsTermsVersion() {
        let body = AuthRoutes.passwordLoginBody(email: "rider@example.com", password: "x", termsVersion: "1.0")
        XCTAssertEqual(Set(body.keys), Set(["email", "password", "termsVersion"]))
    }
}
