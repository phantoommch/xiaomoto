import XCTest
#if canImport(MotocamCore)
@testable import MotocamCore
#else
@testable import Motocam
#endif

final class APIURLTests: XCTestCase {
    private let origin = "https://api.xiaomoto.cloud"
    private let login = "https://api.xiaomoto.cloud/v1/hajimi/auth/login"
    private let otp = "https://api.xiaomoto.cloud/v1/auth/request-code"
    private let verify = "https://api.xiaomoto.cloud/v1/auth/verify"
    private let reset = "https://api.xiaomoto.cloud/v1/hajimi/auth/password/reset"
    private let code = "https://api.xiaomoto.cloud/v1/hajimi/auth/email/code"
    private let terms = "https://api.xiaomoto.cloud/v1/legal/terms"

    func testV1PrefixIsNotDoubledForLoginRegisterOTPOrReset() {
        let bases = [
            origin,
            origin + "/",
            origin + "/v1",
            origin + "/v1/",
            "  " + origin + "/v1/hajimi  ",
            origin + "/v1/hajimi/",
        ]
        let paths = [
            APIPath.passwordLogin: login,
            APIPath.passwordRegister: origin + "/v1/hajimi/auth/register",
            APIPath.requestCode: otp,
            APIPath.verify: verify,
            APIPath.emailCode: code,
            APIPath.emailVerify: origin + "/v1/hajimi/auth/email/verify",
            APIPath.passwordReset: reset,
            APIPath.terms: terms,
            APIPath.points: origin + "/v1/points",
            APIPath.logout: origin + "/v1/auth/logout",
        ]
        for base in bases {
            for (path, want) in paths {
                XCTAssertEqual(APIURL.resolve(base, path)?.absoluteString, want, "\(base) + \(path)")
            }
        }
    }

    func testNonV1MountPrefixIsKept() {
        XCTAssertEqual(
            APIURL.resolve("https://example.test/gateway/", APIPath.points)?.absoluteString,
            "https://example.test/gateway/v1/points"
        )
        XCTAssertEqual(
            APIURL.resolve("http://127.0.0.1:8787", APIPath.verify)?.absoluteString,
            "http://127.0.0.1:8787/v1/auth/verify"
        )
    }

    func testAlreadyDoubledPathStaysA404URL() {
        XCTAssertEqual(
            APIURL.resolve(origin, "v1/v1/hajimi/auth/login")?.absoluteString,
            origin + "/v1/v1/hajimi/auth/login"
        )
        XCTAssertEqual(
            APIURL.resolve(origin + "/v1", "/v1/v1/auth/login")?.absoluteString,
            origin + "/v1/v1/auth/login"
        )
    }

    func testQueryAndAliasStayIntact() {
        XCTAssertEqual(
            APIURL.resolve(origin + "/v1", APIPath.appLatestIOS)?.absoluteString,
            origin + "/v1/app/latest?platform=ios"
        )
        XCTAssertEqual(
            APIURL.resolve(origin, APIPath.passwordLoginAlias)?.absoluteString,
            origin + "/v1/auth/login"
        )
    }

    func testRejectsEmptyPlaceholderAndUnsafeBases() {
        XCTAssertNil(APIURL.resolve("", APIPath.passwordLogin))
        XCTAssertNil(APIURL.resolve("   ", APIPath.passwordLogin))
        XCTAssertNil(APIURL.resolve("$(COMMUNITY_API_BASE_URL)", APIPath.passwordLogin))
        XCTAssertNil(APIURL.resolve("ftp://api.xiaomoto.cloud", APIPath.passwordLogin))
        XCTAssertNil(APIURL.resolve("https://", APIPath.passwordLogin))
        XCTAssertNil(APIURL.resolve(origin, ""))
        XCTAssertNil(APIURL.resolve(origin, "../v1/auth/login"))
        XCTAssertNil(APIURL.resolve("https://user:secret@api.xiaomoto.cloud", APIPath.passwordLogin))
    }
}
