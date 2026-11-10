import XCTest
#if canImport(MotocamCore)
@testable import MotocamCore
#else
@testable import Motocam
#endif

final class AuthLoginRouteTests: XCTestCase {
    func testSigningPasswordLoginUsesTheLiveRoute() {
        XCTAssertEqual(AuthRoutes.passwordLogin, "v1/hajimi/auth/login")
        XCTAssertNotEqual(AuthRoutes.passwordLogin, AuthRoutes.rejectedBuild39PasswordLogin)
        XCTAssertEqual(
            AuthRoutes.joined(base: "https://api.xiaomoto.cloud/", path: AuthRoutes.passwordLogin),
            "https://api.xiaomoto.cloud/v1/hajimi/auth/login"
        )
        XCTAssertEqual(
            AuthRoutes.joined(base: "https://api.xiaomoto.cloud", path: AuthRoutes.rejectedBuild39PasswordLogin),
            "https://api.xiaomoto.cloud/v1/auth/login-password"
        )
    }

    func testPasswordLoginBodyOmitsTermsVersion() {
        let body = AuthRoutes.passwordLoginBody(email: "rider@example.com", password: "x")
        XCTAssertEqual(Set(body.keys), Set(["email", "password"]))
        XCTAssertNil(body["termsVersion"])
        XCTAssertNil(body["code"])
    }
}
