import XCTest
#if canImport(MotocamCore)
@testable import MotocamCore
#else
@testable import Motocam
#endif

final class AlertEngineTests: XCTestCase {
    private let userLat = 28.0
    private let userLon = 120.0

    func testOuterThenInnerOncePerApproachThenRearm() {
        let settings = AlertSettings()
        let first = evaluate(points: [point(id: "booth", north: 1_500)], heading: 0, speed: 5)
        XCTAssertEqual(first.alerts.map(\.band), [.outer])
        XCTAssertEqual(first.alerts[0].spokenText.contains("禁止摩托车进入"), true)
        XCTAssertTrue(first.alerts[0].spokenText.contains("G二五"))
        XCTAssertTrue(first.alerts[0].noticeText.contains("G25"))
        XCTAssertFalse(first.alerts[0].noticeText.contains("G二五"))
        XCTAssertTrue(first.alerts[0].spokenText.contains("长深高速"))
        XCTAssertTrue(first.alerts[0].spokenText.contains("前方"))
        XCTAssertEqual(first.approach["booth"]?.outerFired, true)

        let again = evaluate(points: [point(id: "booth", north: 1_500)], heading: 0, speed: 5, approach: first.approach)
        XCTAssertTrue(again.alerts.isEmpty)

        let inner = evaluate(points: [point(id: "booth", north: 400)], heading: 0, speed: 5, approach: first.approach)
        XCTAssertEqual(inner.alerts.map(\.band), [.inner])
        XCTAssertEqual(inner.approach["booth"]?.outerFired, true)
        XCTAssertEqual(inner.approach["booth"]?.innerFired, true)

        let still = evaluate(points: [point(id: "booth", north: 400)], heading: 0, speed: 5, approach: inner.approach)
        XCTAssertTrue(still.alerts.isEmpty)

        let left = evaluate(points: [point(id: "booth", north: 2_800)], heading: 0, speed: 5, approach: inner.approach, settings: settings)
        XCTAssertTrue(left.alerts.isEmpty)
        XCTAssertNil(left.approach["booth"])

        let returned = evaluate(points: [point(id: "booth", north: 1_500)], heading: 0, speed: 5, approach: left.approach)
        XCTAssertEqual(returned.alerts.map(\.band), [.outer])
    }

    func testNoAlertBeyondOuterRadius() {
        let result = evaluate(points: [point(id: "far", north: 3_000)], heading: 0, speed: 5)
        XCTAssertTrue(result.alerts.isEmpty)
    }

    func testBufferInsideRearmDoesNotClear() {
        let fired = evaluate(points: [point(id: "booth", north: 1_500)], heading: 0, speed: 5)
        let holding = evaluate(points: [point(id: "booth", north: 2_100)], heading: 0, speed: 5, approach: fired.approach)
        XCTAssertTrue(holding.alerts.isEmpty)
        XCTAssertEqual(holding.approach["booth"]?.outerFired, true)
    }

    func testStartingInsideInnerMarksOuterFired() {
        let first = evaluate(points: [point(id: "booth", north: 400)], heading: 0, speed: 5)
        XCTAssertEqual(first.alerts.map(\.band), [.inner])
        XCTAssertEqual(first.approach["booth"]?.outerFired, true)

        let pulled = evaluate(points: [point(id: "booth", north: 1_500)], heading: 0, speed: 5, approach: first.approach)
        XCTAssertTrue(pulled.alerts.isEmpty)
    }

    func testPassedPointDoesNotAlert() {
        let away = evaluate(points: [point(id: "booth", north: 1_000)], heading: 180, speed: 8)
        XCTAssertTrue(away.alerts.isEmpty)

        var settings = AlertSettings()
        settings.onlyWhenApproaching = false
        let stillBehind = evaluate(points: [point(id: "booth", north: 1_000)], heading: 180, speed: 8, settings: settings)
        XCTAssertTrue(stillBehind.alerts.isEmpty)
    }

    func testLowSpeedUsesDistanceNotCourse() {
        let farther = ApproachRecord(lastDistanceMeters: 1_800)
        let closing = evaluate(
            points: [point(id: "booth", north: 1_500)],
            heading: 180,
            speed: 0.4,
            approach: ["booth": farther]
        )
        XCTAssertEqual(closing.alerts.map(\.band), [.outer])

        let opening = evaluate(
            points: [point(id: "booth", north: 308)],
            heading: nil,
            speed: 0.4,
            approach: ["booth": ApproachRecord(lastDistanceMeters: 200)]
        )
        XCTAssertTrue(opening.alerts.isEmpty)
    }

    func testMissingCourseWithoutHistoryDoesNotAlert() {
        let result = evaluate(points: [point(id: "booth", north: 1_000)], heading: nil, speed: nil)
        XCTAssertTrue(result.alerts.isEmpty)
    }

    func testMissingCourseAlertsWhenClosing() {
        let result = evaluate(
            points: [point(id: "booth", north: 1_500)],
            heading: nil,
            speed: nil,
            approach: ["booth": ApproachRecord(lastDistanceMeters: 1_800)]
        )
        XCTAssertEqual(result.alerts.map(\.band), [.outer])
        XCTAssertTrue(result.alerts[0].spokenText.contains("前方"))
    }

    func testHighSpeedBehindDoesNotAlertEvenWhenClosing() {
        let result = evaluate(
            points: [point(id: "booth", north: 1_500)],
            heading: 180,
            speed: 8,
            approach: ["booth": ApproachRecord(lastDistanceMeters: 1_800)]
        )
        XCTAssertTrue(result.alerts.isEmpty)
    }

    func testCapsToNearestPoints() {
        let points = [
            point(id: "c", north: 1_400),
            point(id: "a", north: 1_000),
            point(id: "b", north: 1_200),
        ]
        var settings = AlertSettings()
        settings.maxAlertsPerUpdate = 2
        let result = evaluate(points: points, heading: 0, speed: 5, settings: settings)
        XCTAssertEqual(result.alerts.map(\.point.id), ["a", "b"])
    }

    func testCommunityDuplicateWithin80MetersDoesNotDoubleAlert() {
        let local = point(id: "local", north: 1_000, community: false)
        var crowd = point(id: "crowd", north: 1_030, community: true)
        crowd.name = "社区赤石"
        let result = evaluate(points: [crowd, local], heading: 0, speed: 5)
        XCTAssertEqual(result.alerts.map(\.point.id), ["local"])
    }

    func testMutedAndUnlocatedAreSkipped() {
        let muted = evaluate(points: [point(id: "booth", north: 1_000)], heading: 0, speed: 5, muted: ["booth"])
        XCTAssertTrue(muted.alerts.isEmpty)

        var hidden = point(id: "booth", north: 1_000)
        hidden.located = false
        let unlocated = evaluate(points: [hidden], heading: 0, speed: 5)
        XCTAssertTrue(unlocated.alerts.isEmpty)
    }

    func testHasPassedWhenBehindOrReceding() {
        let booth = point(id: "booth", north: 400)
        XCTAssertTrue(AlertEngine.hasPassed(
            latitude: userLat, longitude: userLon, headingDegrees: 180, speedMps: 8,
            point: booth, closestMeters: 400, currentMeters: 420
        ))
        XCTAssertFalse(AlertEngine.hasPassed(
            latitude: userLat, longitude: userLon, headingDegrees: 0, speedMps: 8,
            point: booth, closestMeters: 1_500, currentMeters: 400
        ))
        XCTAssertFalse(AlertEngine.hasPassed(
            latitude: userLat, longitude: userLon, headingDegrees: nil, speedMps: 0.2,
            point: booth, closestMeters: 400, currentMeters: 430
        ))
        XCTAssertTrue(AlertEngine.hasPassed(
            latitude: userLat, longitude: userLon, headingDegrees: nil, speedMps: 0.2,
            point: booth, closestMeters: 400, currentMeters: 470
        ))
    }

    func testSpokenRoadCodeAndTitle() {
        XCTAssertEqual(AlertEngine.spokenRoadCode("G25"), "G二五")
        XCTAssertEqual(AlertEngine.spokenRoadCode("G15W3"), "G一五W三")
        var camera = point(id: "booth", north: 100)
        camera.name = "G15 沈海高速 K1408 上海方向"
        camera.roadCode = "G15"
        camera.roadName = "沈海高速"
        camera.type = .camera
        XCTAssertEqual(AlertEngine.alertTitle(camera), "G15 沈海高速 K1408 上海方向")
        var uploaded = camera
        uploaded.uploadSource = "user_upload"
        XCTAssertEqual(AlertEngine.alertTitle(uploaded), "G15 沈海高速 K1408 上海方向 用户上传")
        XCTAssertTrue(AlertEngine.notice(for: uploaded, distanceMeters: 500).contains("用户上传"))
        let shown = AlertEngine.notice(for: camera, distanceMeters: 500)
        XCTAssertTrue(shown.contains("G15 沈海高速 K1408 上海方向"))
        XCTAssertFalse(shown.contains("G一五"))
        XCTAssertEqual(shown.components(separatedBy: "G15").count - 1, 1)
        XCTAssertEqual(AlertEngine.alertTitle(point(id: "booth", north: 100)), "G25 长深高速 赤石收费站")
    }

    func testOuterRingExtendsTo80Kilometers() {
        var wide = AlertSettings(outerMeters: 90_000, innerMeters: 2_000)
        wide.normalize()
        XCTAssertEqual(wide.outerMeters, 80_000, accuracy: 0.1)
        XCTAssertEqual(wide.innerMeters, 2_000, accuracy: 0.1)
        var tight = AlertSettings(outerMeters: 100, innerMeters: 2_000)
        tight.normalize()
        XCTAssertEqual(tight.outerMeters, 300, accuracy: 0.1)
        XCTAssertEqual(tight.innerMeters, 250, accuracy: 0.1)
        XCTAssertLessThan(tight.innerMeters, tight.outerMeters)

        var farSettings = AlertSettings()
        farSettings.outerMeters = 80_000
        let far = evaluate(points: [point(id: "far", north: 70_000)], heading: 0, speed: 5, settings: farSettings)
        XCTAssertEqual(far.alerts.map(\.band), [.outer])
        XCTAssertTrue(far.alerts[0].noticeText.contains("70"))
        let beyond = evaluate(points: [point(id: "far", north: 80_500)], heading: 0, speed: 5, settings: farSettings)
        XCTAssertTrue(beyond.alerts.isEmpty)
        let defaults = evaluate(points: [point(id: "far", north: 70_000)], heading: 0, speed: 5)
        XCTAssertTrue(defaults.alerts.isEmpty)
    }

    func testOuterStopsStayEasyToSet() {
        let stops = AlertDistance.outerStops
        XCTAssertEqual(stops.first ?? -1, 300, accuracy: 0.1)
        XCTAssertEqual(stops.last ?? -1, 80_000, accuracy: 0.1)
        XCTAssertTrue(stops.contains(2_000))
        XCTAssertEqual(stops[1] - stops[0], 100, accuracy: 0.1)
        XCTAssertEqual(stops[stops.count - 1] - stops[stops.count - 2], 5_000, accuracy: 0.1)
        for index in 1..<stops.count {
            XCTAssertGreaterThan(stops[index], stops[index - 1])
        }
        XCTAssertEqual(AlertDistance.format(300), "300 米")
        XCTAssertEqual(AlertDistance.format(2_000), "2 公里")
        XCTAssertEqual(AlertDistance.format(2_500), "2.5 公里")
        XCTAssertEqual(AlertDistance.format(80_000), "80 公里")
        XCTAssertEqual(AlertDistance.nearbyRadius(2_000), 15_000, accuracy: 0.1)
        XCTAssertEqual(AlertDistance.nearbyRadius(80_000), 80_000, accuracy: 0.1)
        XCTAssertEqual(AlertDistance.nearbyRadius(20_000), 20_000, accuracy: 0.1)
        XCTAssertEqual(AlertDistance.nearbyKilometers(2_000), 15)
        XCTAssertEqual(AlertDistance.nearbyKilometers(80_000), 80)
    }

    private func evaluate(
        points: [EnforcementPoint],
        heading: Double?,
        speed: Double?,
        approach: [String: ApproachRecord] = [:],
        muted: Set<String> = [],
        settings: AlertSettings = AlertSettings()
    ) -> (alerts: [PendingAlert], approach: [String: ApproachRecord]) {
        AlertEngine.evaluate(
            latitude: userLat,
            longitude: userLon,
            headingDegrees: heading,
            speedMps: speed,
            points: points,
            settings: settings,
            approach: approach,
            mutedIDs: muted
        )
    }

    private func point(id: String, north: Double, community: Bool = false) -> EnforcementPoint {
        let shifted = GeoMath.offset(latitude: userLat, longitude: userLon, northMeters: north, eastMeters: 0)
        return makePoint(
            id: id,
            latitude: shifted.latitude,
            longitude: shifted.longitude,
            source: community ? .community : .manual,
            isCommunity: community
        )
    }
}
