import XCTest
#if canImport(MotocamCore)
@testable import MotocamCore
#else
@testable import Motocam
#endif

final class QueryRankingTests: XCTestCase {
    func testTicketLocationCleansToStationAndHighway() {
        let parsed = QueryCleaner.parse("G25长深高速赤石收费站进口内广场")
        XCTAssertEqual(parsed.roadCode, "G25")
        XCTAssertEqual(parsed.roadName, "长深高速")
        XCTAssertEqual(parsed.keywords, "赤石收费站")
        XCTAssertTrue(parsed.wantsToll)
        XCTAssertFalse(parsed.cleaned.contains("内广场"))
        XCTAssertFalse(parsed.cleaned.contains("进口"))
    }

    func testFullwidthHighwayCodeAndExitPlaza() {
        let parsed = QueryCleaner.parse("Ｓ２６沪常高速安亭收费站出口外广场")
        XCTAssertEqual(parsed.roadCode, "S26")
        XCTAssertEqual(parsed.roadName, "沪常高速")
        XCTAssertEqual(parsed.keywords, "安亭收费站")
    }

    func testParallelHighwayCode() {
        let parsed = QueryCleaner.parse("G15W3常台高速某收费站进口")
        XCTAssertEqual(parsed.roadCode, "G15W3")
        XCTAssertEqual(parsed.keywords, "某收费站")
    }

    func testStationWithoutHighwayCode() {
        let parsed = QueryCleaner.parse("赤石收费站")
        XCTAssertNil(parsed.roadCode)
        XCTAssertNil(parsed.roadName)
        XCTAssertEqual(parsed.keywords, "赤石收费站")
        XCTAssertTrue(parsed.wantsToll)
    }

    func testG25OutranksGuangdongG15Station() {
        // Searching the bare station name on Amap can return the Guangdong
        // G15 深汕 booth. The ticket says G25 长深高速, so that hit must sink.
        let query = QueryCleaner.parse("G25长深高速赤石收费站进口内广场")
        let ranked = PlaceRanker.rank(samplePlaces, query: query, cityHint: nil)
        XCTAssertEqual(ranked.map(\.place.id), ["g25", "village", "g15"])
        XCTAssertTrue(ranked[0].reasons.contains("匹配公路编号 G25"))
        XCTAssertTrue(ranked[0].reasons.contains("匹配路名 长深高速"))
        XCTAssertTrue(ranked.last?.reasons.contains { $0.contains("G15") } == true)
    }

    func testHighwayCodeBeatsConflictingCityHint() {
        let query = QueryCleaner.parse("G25长深高速赤石收费站进口内广场")
        let ranked = PlaceRanker.rank(samplePlaces, query: query, cityHint: "广东")
        XCTAssertEqual(ranked.first?.place.id, "g25")
    }

    func testCityHintBreaksTieWhenTicketHasNoHighwayCode() {
        let query = QueryCleaner.parse("赤石收费站")
        let ranked = PlaceRanker.rank(samplePlaces, query: query, cityHint: "汕尾")
        XCTAssertEqual(ranked.first?.place.id, "g15")
        XCTAssertTrue(ranked.first?.reasons.contains("城市或省份与提示一致") == true)
    }

    func testTollTypeBeatsVillageWhenNoHighwayCode() {
        let query = QueryCleaner.parse("赤石收费站")
        let ranked = PlaceRanker.rank(samplePlaces, query: query, cityHint: nil)
        XCTAssertEqual(ranked.last?.place.id, "village")
        XCTAssertTrue(ranked.first?.place.typeCode.hasPrefix("1802") == true)
    }

    func testAmapURLOmitsTollTypeFilterAndDoesNotLimitCity() throws {
        let query = QueryCleaner.parse("G25长深高速赤石收费站进口内广场")
        XCTAssertTrue(query.wantsToll)
        XCTAssertNil(AmapRequest.types(for: query))
        let url = try XCTUnwrap(AmapRequest.placeTextURL(
            key: "test-key",
            keywords: query.keywords,
            city: "丽水",
            types: AmapRequest.types(for: query)
        ))
        let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems
        func value(_ name: String) -> String? { items?.first { $0.name == name }?.value }
        XCTAssertEqual(value("key"), "test-key")
        XCTAssertEqual(value("keywords"), "赤石收费站")
        XCTAssertNil(value("types"))
        XCTAssertEqual(value("city"), "丽水")
        XCTAssertEqual(value("citylimit"), "false")
        XCTAssertEqual(url.host, "restapi.amap.com")
        XCTAssertEqual(url.path, "/v3/place/text")
    }

    func testTicketLocatePlanUsesRegionFacilityAndStake() throws {
        let ningde = QueryCleaner.locatePlan(QueryCleaner.parse("福建省宁德市福安市G15沈海高速吴楼隧道"))
        XCTAssertEqual(ningde.region, "宁德市")
        XCTAssertEqual(ningde.poiKeywords.first, "吴楼隧道")
        let tianjin = QueryCleaner.locatePlan(QueryCleaner.parse("天津市滨海新区长深高速"))
        XCTAssertEqual(tianjin.region, "天津")
        XCTAssertEqual(tianjin.poiKeywords.first, "长深高速")
        let stake = QueryCleaner.locatePlan(QueryCleaner.parse("G15沈海高速K210+300"))
        XCTAssertTrue(stake.poiKeywords.contains { $0.contains("K210+300") })
    }

    func testGluedTicketStakeIsTrunkCodeNotAVillage() throws {
        let parsed = QueryCleaner.parse("G15643公里450米")
        XCTAssertEqual(parsed.roadCode, "G15")
        XCTAssertNil(parsed.roadName)
        XCTAssertEqual(QueryCleaner.stakeToken(in: parsed.cleaned), "643公里450米")
        XCTAssertEqual(QueryCleaner.stakeKilometre(in: parsed.cleaned), 643)
        XCTAssertTrue(QueryCleaner.bareStake(parsed))
        let plan = QueryCleaner.locatePlan(parsed)
        XCTAssertEqual(plan.geocodeAddress, "G15 643公里450米")
        XCTAssertEqual(QueryCleaner.stakeAttempts(parsed).map(\.address), ["G15 643公里450米", "G1564 3公里450米"])
        let stakeHit = PlaceCandidate(
            id: "stake",
            name: "江苏省连云港市赣榆区G15沈海高速643公里",
            address: "江苏省连云港市赣榆区G15沈海高速643公里",
            province: "江苏省",
            city: "连云港市",
            district: "赣榆区",
            typeCode: "190301",
            typeName: "道路",
            gcjLatitude: 34.84,
            gcjLongitude: 119.13
        )
        let gaoyou = PlaceCandidate(
            id: "village",
            name: "江苏省扬州市高邮市四百五",
            address: "江苏省扬州市高邮市四百五",
            province: "江苏省",
            city: "扬州市",
            district: "高邮市",
            typeCode: "190108",
            typeName: "村庄",
            gcjLatitude: 32.93,
            gcjLongitude: 119.43
        )
        XCTAssertTrue(QueryCleaner.matchesStake(stakeHit, roadCode: "G15", kilometre: 643))
        XCTAssertFalse(QueryCleaner.matchesStake(gaoyou, roadCode: "G15", kilometre: 643))
        let ranked = PlaceRanker.rank([gaoyou, stakeHit], query: parsed, cityHint: nil)
        XCTAssertEqual(ranked.first?.place.id, "stake")
        XCTAssertEqual(QueryCleaner.roadName(in: stakeHit.name), "沈海高速")
        let named = QueryCleaner.parse("G1513温丽高速45公里900米")
        XCTAssertEqual(named.roadCode, "G1513")
        XCTAssertEqual(QueryCleaner.stakeToken(in: named.cleaned), "45公里900米")
        XCTAssertFalse(QueryCleaner.bareStake(QueryCleaner.parse("长深高速2862公里50米(建瓯收费站内广场)")))
        let url = try XCTUnwrap(AmapRequest.placeTextURL(key: "k", keywords: "吴楼隧道", city: "宁德市", types: nil, limitToCity: true))
        let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems
        XCTAssertEqual(items?.first { $0.name == "citylimit" }?.value, "true")
        XCTAssertEqual(items?.first { $0.name == "city" }?.value, "宁德市")
    }

    func testUniqueBestRejectsSameNameFarApart() {
        let ranked = [
            rankedPlace("g25", name: "建瓯收费站", score: 40, latitude: 27.05, longitude: 118.35),
            rankedPlace("g3", name: "建瓯收费站", score: 39, latitude: 26.60, longitude: 118.10),
        ]
        XCTAssertNil(PlaceChoice.uniqueBest(ranked))
    }

    func testUniqueBestKeepsClearRoadWinner() {
        let ranked = [
            rankedPlace("g25", name: "建瓯收费站", score: 160, latitude: 27.05, longitude: 118.35),
            rankedPlace("g3", name: "建瓯收费站", score: 40, latitude: 26.60, longitude: 118.10),
        ]
        XCTAssertEqual(PlaceChoice.uniqueBest(ranked)?.place.id, "g25")
    }

    func testUniqueBestTreatsNearbyDuplicatesAsOne() {
        let ranked = [
            rankedPlace("a", name: "建瓯收费站", score: 40, latitude: 27.0500, longitude: 118.3500),
            rankedPlace("b", name: "建瓯收费站", score: 39, latitude: 27.0502, longitude: 118.3502),
        ]
        XCTAssertEqual(PlaceChoice.uniqueBest(ranked)?.place.id, "a")
    }

    func testPlacementStatusSentences() {
        XCTAssertEqual(PlacementStatus.sentence(marked: 2, uploaded: 2), "已标注并上传 2 个违法点")
        XCTAssertEqual(PlacementStatus.sentence(marked: 2, uploaded: 0), "已标注 2 个违法点，有网后会上传")
        XCTAssertEqual(PlacementStatus.sentence(marked: 2, uploaded: 1), "已标注 2 个违法点，已上传 1 个")
        XCTAssertEqual(PlacementStatus.amapMatch("吴楼隧道"), "高德匹配：吴楼隧道")
        XCTAssertEqual(PlacementStatus.notFound, "高德没找到这个地点，已保存待后台核对")
        XCTAssertEqual(PlacementStatus.reviewPrompt(marked: 2), "已标注 2 个违法点，请核对后上传")
    }

    func testTicketPointCopiesFineAndMarksOCR() {
        let place = rankedPlace("g25", name: "建瓯收费站", score: 80, latitude: 27.05, longitude: 118.36)
        let query = QueryCleaner.parse("长深高速2862公里50米(建瓯收费站内广场)")
        let point = TicketPoint.fromPlace(place, query: query, violation: "违反禁令标志指示", fineYuan: 150, penaltyPoints: 1)
        XCTAssertEqual(point.source, .ocr)
        XCTAssertEqual(point.fineYuan, 150)
        XCTAssertEqual(point.penaltyPoints, 1)
        XCTAssertEqual(point.violation, "违反禁令标志指示")
        XCTAssertEqual(point.name, "建瓯收费站")
    }

    private func rankedPlace(_ id: String, name: String, score: Int, latitude: Double, longitude: Double) -> RankedPlace {
        RankedPlace(
            place: PlaceCandidate(
                id: id,
                name: name,
                address: name,
                province: "福建省",
                city: "南平市",
                district: "",
                typeCode: "180200",
                typeName: "收费站",
                gcjLatitude: latitude,
                gcjLongitude: longitude
            ),
            score: score,
            reasons: []
        )
    }

    func testAmapURLOmitsCityAndTypesWhenNotNeeded() throws {
        let query = QueryCleaner.parse("某路口抓拍")
        XCTAssertFalse(query.wantsToll)
        XCTAssertNil(AmapRequest.types(for: query))
        let url = try XCTUnwrap(AmapRequest.placeTextURL(
            key: "k",
            keywords: query.keywords,
            city: "  ",
            types: nil
        ))
        let names = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.map(\.name) ?? []
        XCTAssertFalse(names.contains("city"))
        XCTAssertFalse(names.contains("types"))
    }

    private var samplePlaces: [PlaceCandidate] {
        [
            PlaceCandidate(
                id: "g15",
                name: "赤石收费站",
                address: "广东省汕尾市海丰县G15深汕高速",
                province: "广东省",
                city: "汕尾市",
                district: "海丰县",
                typeCode: "180200",
                typeName: "收费站",
                gcjLatitude: 22.82,
                gcjLongitude: 115.32
            ),
            PlaceCandidate(
                id: "g25",
                name: "赤石收费站",
                address: "G25长深高速",
                province: "浙江省",
                city: "丽水市",
                district: "",
                typeCode: "180200",
                typeName: "收费站",
                gcjLatitude: 28.2,
                gcjLongitude: 119.9
            ),
            PlaceCandidate(
                id: "village",
                name: "赤石村",
                address: "某村",
                province: "浙江省",
                city: "温州市",
                district: "",
                typeCode: "190108",
                typeName: "村庄",
                gcjLatitude: 28.0,
                gcjLongitude: 120.5
            ),
        ]
    }
}
