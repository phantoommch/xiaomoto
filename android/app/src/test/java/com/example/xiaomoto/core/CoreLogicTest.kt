package com.example.xiaomoto.core

import java.io.File
import java.net.URI
import java.net.URLDecoder
import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class CoordinateConverterTest {
    @Test fun beijingGoldenValue() {
        val gcj = CoordinateConverter.wgs84ToGcj02(CoordinateConverter.Coord(39.908722, 116.397499))
        assertEquals(39.9101255001, gcj.latitude, 1e-6)
        assertEquals(116.4037425753, gcj.longitude, 1e-6)
        val offset = GeoMath.distanceMeters(39.908722, 116.397499, gcj.latitude, gcj.longitude)
        assertTrue(offset > 400)
        assertTrue(offset < 750)
    }

    @Test fun shanghaiGoldenValue() {
        val gcj = CoordinateConverter.wgs84ToGcj02(CoordinateConverter.Coord(31.230416, 121.473701))
        assertEquals(31.2284737422, gcj.latitude, 1e-6)
        assertEquals(121.4782240574, gcj.longitude, 1e-6)
    }

    @Test fun roundTripWithinACentimeter() {
        val samples = listOf(
            CoordinateConverter.Coord(39.908722, 116.397499),
            CoordinateConverter.Coord(31.230416, 121.473701),
            CoordinateConverter.Coord(28.0, 120.0),
        )
        for (wgs in samples) {
            val back = CoordinateConverter.gcj02ToWgs84(CoordinateConverter.wgs84ToGcj02(wgs))
            val error = GeoMath.distanceMeters(wgs.latitude, wgs.longitude, back.latitude, back.longitude)
            assertTrue("round trip $wgs error $error", error < 0.05)
            assertEquals(wgs.latitude, back.latitude, 1e-6)
            assertEquals(wgs.longitude, back.longitude, 1e-6)
        }
    }

    @Test fun outsideChinaIsIdentity() {
        assertTrue(CoordinateConverter.isInChinaBoundingBox(22.3, 114.2))
        val outside = listOf(CoordinateConverter.Coord(35.68, 139.76), CoordinateConverter.Coord(40.71, -74.00))
        for (coord in outside) {
            assertFalse(CoordinateConverter.isInChinaBoundingBox(coord.latitude, coord.longitude))
            assertEquals(coord, CoordinateConverter.wgs84ToGcj02(coord))
            assertEquals(coord, CoordinateConverter.gcj02ToWgs84(coord))
        }
    }

    @Test fun bearingDueNorth() {
        assertEquals(0.0, GeoMath.bearingDegrees(28.0, 120.0, 28.01, 120.0), 1.0)
    }
}

class QueryRankingTest {
    @Test fun ticketLocationCleansToStationAndHighway() {
        val parsed = QueryCleaner.parse("G25长深高速赤石收费站进口内广场")
        assertEquals("G25", parsed.roadCode)
        assertEquals("长深高速", parsed.roadName)
        assertEquals("赤石收费站", parsed.keywords)
        assertTrue(parsed.wantsToll)
        assertFalse(parsed.cleaned.contains("内广场"))
        assertFalse(parsed.cleaned.contains("进口"))
    }

    @Test fun fullwidthHighwayCodeAndExitPlaza() {
        val parsed = QueryCleaner.parse("Ｓ２６沪常高速安亭收费站出口外广场")
        assertEquals("S26", parsed.roadCode)
        assertEquals("沪常高速", parsed.roadName)
        assertEquals("安亭收费站", parsed.keywords)
    }

    @Test fun parallelHighwayCode() {
        val parsed = QueryCleaner.parse("G15W3常台高速某收费站进口")
        assertEquals("G15W3", parsed.roadCode)
        assertEquals("某收费站", parsed.keywords)
    }

    @Test fun stationWithoutHighwayCode() {
        val parsed = QueryCleaner.parse("赤石收费站")
        assertNull(parsed.roadCode)
        assertNull(parsed.roadName)
        assertEquals("赤石收费站", parsed.keywords)
        assertTrue(parsed.wantsToll)
    }

    @Test fun g25OutranksGuangdongG15() {
        val query = QueryCleaner.parse("G25长深高速赤石收费站进口内广场")
        val ranked = PlaceRanker.rank(samplePlaces, query, null)
        assertEquals(listOf("g25", "village", "g15"), ranked.map { it.place.id })
        assertTrue(ranked[0].reasons.contains("匹配公路编号 G25"))
        assertTrue(ranked[0].reasons.contains("匹配路名 长深高速"))
        assertTrue(ranked.last().reasons.any { it.contains("G15") })
    }

    @Test fun highwayCodeBeatsConflictingCityHint() {
        val query = QueryCleaner.parse("G25长深高速赤石收费站进口内广场")
        val ranked = PlaceRanker.rank(samplePlaces, query, "广东")
        assertEquals("g25", ranked.first().place.id)
    }

    @Test fun cityHintBreaksTieWhenTicketHasNoHighwayCode() {
        val query = QueryCleaner.parse("赤石收费站")
        val ranked = PlaceRanker.rank(samplePlaces, query, "汕尾")
        assertEquals("g15", ranked.first().place.id)
        assertTrue(ranked.first().reasons.contains("城市或省份与提示一致"))
    }

    @Test fun tollTypeBeatsVillageWhenNoHighwayCode() {
        val query = QueryCleaner.parse("赤石收费站")
        val ranked = PlaceRanker.rank(samplePlaces, query, null)
        assertEquals("village", ranked.last().place.id)
        assertTrue(ranked.first().place.typeCode.startsWith("1802"))
    }

    @Test fun amapUrlDoesNotSendTollTypeFilter() {
        val query = QueryCleaner.parse("G25长深高速赤石收费站进口内广场")
        assertTrue(query.wantsToll)
        assertNull(AmapRequest.types(query))
        val url = AmapRequest.placeTextURL("test-key", query.keywords, "丽水", AmapRequest.types(query))
        val values = queryMap(url)
        assertEquals("test-key", values["key"])
        assertEquals("赤石收费站", values["keywords"])
        assertFalse(values.containsKey("types"))
        assertEquals("丽水", values["city"])
        assertEquals("false", values["citylimit"])
        val uri = URI(url)
        assertEquals("restapi.amap.com", uri.host)
        assertEquals("/v3/place/text", uri.path)
    }

    @Test fun uniqueBestRejectsSameNameFarApart() {
        val ranked = listOf(
            ranked("g25", "建瓯收费站", 40, 27.05, 118.35),
            ranked("g3", "建瓯收费站", 39, 26.60, 118.10),
        )
        assertNull(PlaceChoice.uniqueBest(ranked))
    }

    @Test fun uniqueBestKeepsClearRoadWinner() {
        val ranked = listOf(
            ranked("g25", "建瓯收费站", 160, 27.05, 118.35),
            ranked("g3", "建瓯收费站", 40, 26.60, 118.10),
        )
        assertEquals("g25", PlaceChoice.uniqueBest(ranked)?.place?.id)
    }

    @Test fun uniqueBestTreatsNearbyDuplicatesAsOne() {
        val ranked = listOf(
            ranked("a", "建瓯收费站", 40, 27.0500, 118.3500),
            ranked("b", "建瓯收费站", 39, 27.0502, 118.3502),
        )
        assertEquals("a", PlaceChoice.uniqueBest(ranked)?.place?.id)
    }

    @Test fun emptySearchNeedsATap() {
        assertNull(PlaceChoice.uniqueBest(emptyList()))
    }

    @Test fun placementStatusSentences() {
        assertEquals("已标注并上传 2 个违法点", PlacementStatus.sentence(2, 2))
        assertEquals("已标注 2 个违法点，有网后会上传", PlacementStatus.sentence(2, 0))
        assertEquals("已标注 2 个违法点，已上传 1 个", PlacementStatus.sentence(2, 1))
        assertEquals("已标注 2 个违法点，请核对后上传", PlacementStatus.reviewPrompt(2))
        assertEquals("高德匹配：吴楼隧道", PlacementStatus.amapMatch("吴楼隧道"))
        assertEquals("高德没找到这个地点，已保存待后台核对", PlacementStatus.notFound)
    }

    @Test fun ticketPointCopiesFineAndMarksOcr() {
        val query = QueryCleaner.parse("长深高速2862公里50米(建瓯收费站内广场)")
        val point = TicketPoint.fromPlace(ranked("g25", "建瓯收费站", 80, 27.05, 118.36), query, "违反禁令标志指示", 150, 1)
        assertEquals(PointSource.OCR, point.source)
        assertEquals(150, point.fineYuan)
        assertEquals(1, point.penaltyPoints)
        assertEquals("违反禁令标志指示", point.violation)
        assertEquals("建瓯收费站", point.name)
        assertEquals("建瓯收费站", point.amapName)
        assertEquals("长深高速2862公里50米(建瓯收费站内广场)", point.ocrLocation)
        assertTrue(point.located)
    }

    @Test fun ticketLocatePlanUsesRegionFacilityAndStake() {
        val ningde = QueryCleaner.locatePlan(QueryCleaner.parse("福建省宁德市福安市G15沈海高速吴楼隧道"))
        assertEquals("宁德市", ningde.region)
        assertEquals("吴楼隧道", ningde.poiKeywords.first())
        val tianjin = QueryCleaner.locatePlan(QueryCleaner.parse("天津市滨海新区长深高速"))
        assertEquals("天津", tianjin.region)
        assertEquals("长深高速", tianjin.poiKeywords.first())
        val stake = QueryCleaner.locatePlan(QueryCleaner.parse("G15沈海高速K210+300"))
        assertTrue(stake.poiKeywords.any { it.contains("K210+300") })
    }

    @Test fun gluedTicketStakeIsTrunkCodeNotAVillage() {
        val parsed = QueryCleaner.parse("G15643公里450米")
        assertEquals("G15", parsed.roadCode)
        assertNull(parsed.roadName)
        assertEquals("643公里450米", QueryCleaner.stakeToken(parsed.cleaned))
        assertEquals(643, QueryCleaner.stakeKilometre(parsed.cleaned))
        assertTrue(QueryCleaner.bareStake(parsed))
        val plan = QueryCleaner.locatePlan(parsed)
        assertEquals("G15 643公里450米", plan.geocodeAddress)
        val attempts = QueryCleaner.stakeAttempts(parsed).map { it.address }
        assertEquals(listOf("G15 643公里450米", "G1564 3公里450米"), attempts)
        val stakeHit = PlaceCandidate(
            "stake",
            "江苏省连云港市赣榆区G15沈海高速643公里",
            "江苏省连云港市赣榆区G15沈海高速643公里",
            "江苏省",
            "连云港市",
            "赣榆区",
            "190301",
            "道路",
            34.84,
            119.13,
        )
        val gaoyou = PlaceCandidate(
            "village",
            "江苏省扬州市高邮市四百五",
            "江苏省扬州市高邮市四百五",
            "江苏省",
            "扬州市",
            "高邮市",
            "190108",
            "村庄",
            32.93,
            119.43,
        )
        assertTrue(QueryCleaner.matchesStake(stakeHit, "G15", 643))
        assertFalse(QueryCleaner.matchesStake(gaoyou, "G15", 643))
        val ranked = PlaceRanker.rank(listOf(gaoyou, stakeHit), parsed, null)
        assertEquals("stake", ranked.first().place.id)
        assertEquals("沈海高速", QueryCleaner.roadName(stakeHit.name))
        val named = QueryCleaner.parse("G1513温丽高速45公里900米")
        assertEquals("G1513", named.roadCode)
        assertEquals("45公里900米", QueryCleaner.stakeToken(named.cleaned))
        assertFalse(QueryCleaner.bareStake(QueryCleaner.parse("长深高速2862公里50米(建瓯收费站内广场)")))
        val url = AmapRequest.placeTextURL("k", "吴楼隧道", "宁德市", null, true)
        assertEquals("true", queryMap(url)["citylimit"])
        assertEquals("宁德市", queryMap(url)["city"])
    }

    private fun ranked(id: String, name: String, score: Int, latitude: Double, longitude: Double): RankedPlace {
        return RankedPlace(
            PlaceCandidate(id, name, name, "福建省", "南平市", "", "180200", "收费站", latitude, longitude),
            score,
            emptyList(),
        )
    }

    @Test fun amapUrlOmitsCityAndTypesWhenNotNeeded() {
        val query = QueryCleaner.parse("某路口抓拍")
        assertFalse(query.wantsToll)
        assertNull(AmapRequest.types(query))
        val url = AmapRequest.placeTextURL("k", query.keywords, "  ", null)
        val names = queryMap(url).keys
        assertFalse(names.contains("city"))
        assertFalse(names.contains("types"))
    }

    private val samplePlaces = listOf(
        PlaceCandidate("g15", "赤石收费站", "广东省汕尾市海丰县G15深汕高速", "广东省", "汕尾市", "海丰县", "180200", "收费站", 22.82, 115.32),
        PlaceCandidate("g25", "赤石收费站", "G25长深高速", "浙江省", "丽水市", "", "180200", "收费站", 28.2, 119.9),
        PlaceCandidate("village", "赤石村", "某村", "浙江省", "温州市", "", "190108", "村庄", 28.0, 120.5),
    )
}

class TicketParserTest {
    @Test fun chishiTicketScreenshotFields() {
        val text = javaClass.classLoader!!.getResource("ticket_chishi.txt")!!.readText()
        assertFalse(containsPlate(text))
        val ticket = TicketParser.parse(text)
        assertEquals("G25长深高速赤石收费站进口内广场", ticket.location)
        assertEquals("驾驶机动车违反禁令标志指示的", ticket.violation)
        assertEquals(100, ticket.fineYuan)
        assertEquals(1, ticket.penaltyPoints)
        assertEquals(Instant.parse("2026-01-15T06:20:00Z"), ticket.happenedAt)
        val query = QueryCleaner.parse(ticket.location ?: "")
        assertEquals("G25", query.roadCode)
        assertEquals("长深高速", query.roadName)
        assertEquals("赤石收费站", query.keywords)
    }

    @Test fun labelAndValueOnSeparateLines() {
        val text = """
            违法行为
            驾驶机动车违反禁令标志指示的
            违法地点
            G25长深高速赤石收费站进口内广场
            违法时间
            2026-01-15 14:20
            罚款金额：100元
            记分：1
        """.trimIndent()
        val ticket = TicketParser.parse(text)
        assertEquals("G25长深高速赤石收费站进口内广场", ticket.location)
        assertEquals("驾驶机动车违反禁令标志指示的", ticket.violation)
        assertEquals(100, ticket.fineYuan)
        assertEquals(1, ticket.penaltyPoints)
        assertNotNull(ticket.happenedAt)
    }

    @Test fun fullwidthColonAndCombinedFineLine() {
        val ticket = TicketParser.parse("违法地点：赤石收费站出口\n罚款：100元 记 1 分")
        assertEquals("赤石收费站出口", ticket.location)
        assertEquals(100, ticket.fineYuan)
        assertEquals(1, ticket.penaltyPoints)
    }

    @Test fun vehicleLineIsNotTreatedAsLocation() {
        val text = "违法车辆：已打码\n违法地点：G25长深高速赤石收费站进口内广场"
        val ticket = TicketParser.parse(text)
        assertEquals("G25长深高速赤石收费站进口内广场", ticket.location)
        assertFalse(ticket.rawText.contains("浙E"))
    }

    @Test fun spacesBetweenHanCharactersStillParse() {
        val ticket = TicketParser.parse("违 法 地 点：G25 长 深 高 速 赤 石 收 费 站\n罚 款：100元 记 1 分")
        assertEquals("G25 长深高速赤石收费站", ticket.location)
        assertEquals(100, ticket.fineYuan)
        assertEquals(1, ticket.penaltyPoints)
    }

    @Test fun trafficListKeepsEveryRowWithFineAndPoints() {
        val text = """
            驾驶校车、中型以上载客载货汽车
            G1513温丽高速丽水方向87KM700M
            2026-09-20 14:37
            罚款1500元 记12分
            驾驶机动车违反禁令标志指示的
            G1513温丽高速45公里900米（东向西方向）
            2026-09-20 14:12
            罚款100元 记1分
        """.trimIndent()
        val ticket = TicketParser.parse(text)
        assertEquals(TicketStyle.LIST, ticket.style)
        assertEquals(2, ticket.records.size)
        val first = ticket.records[0]
        assertEquals("G1513温丽高速丽水方向87KM700M", first.location)
        assertEquals("驾驶校车、中型以上载客载货汽车", first.violation)
        assertEquals(1500, first.fineYuan)
        assertEquals(12, first.penaltyPoints)
        assertEquals("罚款 1500 元 · 记 12 分", first.penaltyLabel())
        assertEquals(Instant.parse("2026-09-20T06:37:00Z"), first.happenedAt)
        val second = ticket.records[1]
        assertEquals("G1513温丽高速45公里900米(东向西方向)", second.location)
        assertEquals("驾驶机动车违反禁令标志指示的", second.violation)
        assertEquals(100, second.fineYuan)
        assertEquals(1, second.penaltyPoints)
        assertEquals("罚款 100 元 · 记 1 分", second.penaltyLabel())
        assertEquals(Instant.parse("2026-09-20T06:12:00Z"), second.happenedAt)
        val query = QueryCleaner.parse(second.location ?: "")
        assertEquals("G1513", query.roadCode)
        assertEquals("温丽高速", query.roadName)
        assertEquals("G1513温丽高速", query.keywords)
        assertTrue(QueryCleaner.needsRoadGeocode(query))
    }

    @Test fun detailPageKeepsLabeledPlaceFineAndPoints() {
        val text = """
            违法行为：违反禁令标志指示
            罚款150元 记1分
            违法车辆：闽D00000
            违法地点：长深高速2862公里50米（建瓯收费站内广场）
            违法时间：2026-07-16 13:39
            法律依据：《中华人民共和国道路交通安全法》第三十八条
        """.trimIndent()
        val ticket = TicketParser.parse(text)
        assertEquals(TicketStyle.DETAIL, ticket.style)
        assertEquals(1, ticket.records.size)
        val record = ticket.records[0]
        assertEquals("长深高速2862公里50米(建瓯收费站内广场)", record.location)
        assertEquals("违反禁令标志指示", record.violation)
        assertEquals(150, record.fineYuan)
        assertEquals(1, record.penaltyPoints)
        assertEquals("罚款 150 元 · 记 1 分", record.penaltyLabel())
        assertEquals(Instant.parse("2026-07-16T05:39:00Z"), record.happenedAt)
        assertFalse(record.location!!.contains("DMR"))
        assertFalse(record.location!!.contains("法律依据"))
        val query = QueryCleaner.parse(record.location ?: "")
        assertNull(query.roadCode)
        assertEquals("长深高速", query.roadName)
        assertEquals("建瓯收费站", query.keywords)
        assertTrue(query.wantsToll)
        assertTrue(QueryCleaner.needsRoadGeocode(query))
        assertFalse(query.cleaned.contains("内广场"))
    }

    @Test fun detailPageIsNotSplitLikeTheList() {
        val text = """
            违法行为：违反禁令标志指示
            罚款150元 记1分
            违法地点：长深高速2862公里50米（建瓯收费站内广场）
            违法时间：2026-07-16 13:39
            驾驶机动车违反禁令标志指示的
            G1513温丽高速45公里900米(东向西方向)
            2026-09-20 14:12
            罚款100元 记1分
        """.trimIndent()
        val ticket = TicketParser.parse(text)
        assertEquals(TicketStyle.DETAIL, ticket.style)
        assertEquals(1, ticket.records.size)
        assertEquals("长深高速2862公里50米(建瓯收费站内广场)", ticket.location)
    }

    @Test fun listPageDoesNotInventADetailLocation() {
        val text = """
            未处理
            驾驶机动车违反禁令标志指示的
            G1513温丽高速45公里900米（东向西方向）
            2026-09-20 14:12
            罚款100元 记1分
        """.trimIndent()
        val ticket = TicketParser.parse(text)
        assertEquals(TicketStyle.LIST, ticket.style)
        assertEquals(1, ticket.records.size)
        assertEquals("G1513温丽高速45公里900米(东向西方向)", ticket.location)
    }

    @Test fun hardTicketTextsParseLikeThePrototype() {
        val a = Recog.parse("汕昆高速1807公里910米-汕昆高速1798公里760米")
        assertEquals("G78", a.code)
        assertEquals(2, a.stakes.size)
        assertEquals(910, a.stakes[0].metres)
        assertTrue(a.range)
        val b = Recog.parse("G201152公里700米")
        assertEquals(3, b.glued.size)
        assertTrue(b.glued.any { it.code == "G2011" && it.km == 52.0 && it.metres == 700 })
        val ocr = Recog.parse("G56抗瑞高速安徽方向73公里300米")
        assertEquals("G56", ocr.code)
        assertEquals("杭瑞", ocr.nameCanon)
        assertEquals("ocrfix", ocr.nameHow)
        val sms = "云南交警\n121235300\n在汕昆高速1807\n公里910米-汕昆高速1798公里76\n0米"
        val c = Recog.parse(sms)
        assertEquals("G78", c.code)
        assertEquals(910, c.stakes[0].metres)
        assertEquals(760, c.stakes[1].metres)
        val sig = Recog.signals(sms)
        assertTrue(sig.provinces.any { it.second == "云南" })
        assertNull(sig.plate)
        val facility = Recog.parse("江南大道隧道东向西入口")
        assertEquals("江南大道隧道", facility.facility)
        val tunnel = Recog.parse("太湖隧道东入口,由东向西")
        assertEquals("太湖隧道", tunnel.facility)
        assertEquals("东入口", tunnel.portal)
        val d = Recog.parse("G15沈海高速1709公里910米（北向南")
        assertEquals("G15", d.code)
        assertTrue(d.direction.orEmpty().contains("北向南"))
        val e = Recog.parse("杭瑞高速K2501+600M至杭瑞高速K2514+500M大理方向")
        assertEquals("G56", e.code)
        assertEquals(600, e.stakes[0].metres)
        assertEquals(500, e.stakes[1].metres)
        assertTrue(e.range)
        val overlay = Recog.signals("《山东省高速公路交通安全条例》", "地点:G2011青新K52+700 设备编号:370211")
        assertEquals("G2011", overlay.overlay?.code)
        assertEquals(700, overlay.overlay?.metres)
        assertTrue(overlay.provinces.any { it.first == "legal" && it.second == "山东" })
        assertTrue(overlay.provinces.any { it.first == "device" && it.second == "山东" })
        val list = TicketParser.parse("超速\n江南大道隧道东向西入口\n罚款100元 记3分")
        assertEquals("江南大道隧道东向西入口", list.records.first().location)
        val message = TicketParser.parse(sms)
        assertEquals(1, message.records.size)
        assertTrue(message.records.first().fullText.contains("云南交警"))
    }

    @Test fun unrelatedImageIsNotRecognized() {
        val ticket = TicketParser.parse("今天天气不错\n随便拍的一张风景")
        assertTrue(ticket.records.isEmpty())
        assertNull(ticket.location)
        assertNull(ticket.violation)
        assertNull(ticket.fineYuan)
        assertNull(ticket.penaltyPoints)
    }

    @Test fun stakeWithoutFineIsNotATicket() {
        val ticket = TicketParser.parse("驾驶机动车违反禁令标志指示的\nG1513温丽高速45公里900米")
        assertTrue(ticket.records.isEmpty())
        assertNull(ticket.location)
        assertNull(ticket.fineYuan)
    }

    @Test fun missingLocationStaysNull() {
        val ticket = TicketParser.parse("违法行为：超速\n罚款200元")
        assertTrue(ticket.records.isEmpty())
        assertNull(ticket.location)
        assertNull(ticket.fineYuan)
        assertNull(ticket.violation)
        assertNull(ticket.penaltyPoints)
    }
}

class PointCodecTest {
    @Test fun jsonRoundTripAndWgs84Marker() {
        val point = makePoint(notes = "A & B <站>", source = PointSource.OCR)
        val text = PointCodec.encodeJSON(listOf(point))
        val root = kotlinx.serialization.json.Json.parseToJsonElement(text).jsonObject()
        assertEquals("wgs84", root.string("coord"))
        assertEquals(1, root.int("version"))
        assertEquals(listOf(point), PointCodec.decodeJSON(text))
    }

    @Test fun jsonImportConvertsGcj02ToWgs84() {
        val json = """
            {"version":1,"coord":"gcj02","points":[{
              "id":"bj","name":"示例","type":"camera","latitude":39.9101255001,"longitude":116.4037425753,
              "source":"search","created_at":"2026-09-26T00:00:00Z","updated_at":"2026-09-26T00:00:00Z"
            }]}
        """.trimIndent()
        val decoded = PointCodec.decodeJSON(json)
        assertEquals(39.908722, decoded[0].latitude, 1e-5)
        assertEquals(116.397499, decoded[0].longitude, 1e-5)
        assertEquals(PointType.CAMERA, decoded[0].type)
        assertEquals(PointSource.SEARCH, decoded[0].source)
    }

    @Test fun unknownCoordinateSystemThrows() {
        val error = assertThrows(PointCodecException::class.java) {
            PointCodec.decodeJSON("""{"version":1,"coord":"bd09","points":[]}""")
        }
        assertEquals("bd09", error.system)
    }

    @Test fun gpxRoundTripPreservesFields() {
        val point = makePoint(id = "station-1", notes = "注意 & 核对", source = PointSource.SEARCH)
        val xml = PointCodec.encodeGPX(listOf(point))
        assertTrue(xml.contains("urn:motocam:gpx:1"))
        assertTrue(xml.contains("<name>${AppIdentity.displayName}</name>"))
        assertTrue(xml.contains("<motocam:coord>wgs84</motocam:coord>"))
        val decoded = PointCodec.decodeGPX(xml)
        assertEquals(1, decoded.size)
        assertEquals(point.id, decoded[0].id)
        assertEquals(point.name, decoded[0].name)
        assertEquals(point.notes, decoded[0].notes)
        assertEquals(PointType.BANNED_TOLL, decoded[0].type)
        assertEquals("G25", decoded[0].roadCode)
        assertEquals("长深高速", decoded[0].roadName)
        assertEquals(PointSource.SEARCH, decoded[0].source)
        assertEquals(100, decoded[0].fineYuan)
        assertEquals(1, decoded[0].penaltyPoints)
        assertEquals(point.violation, decoded[0].violation)
        assertEquals(point.latitude, decoded[0].latitude, 1e-6)
        assertEquals(point.longitude, decoded[0].longitude, 1e-6)
        assertEquals(point.createdAt, decoded[0].createdAt)
    }

    @Test fun plainGpxWaypointImportsAsManualOther() {
        val xml = """<gpx version="1.1"><wpt lat="28.1000000" lon="120.2000000"><name>临时点</name></wpt></gpx>"""
        val decoded = PointCodec.decodeGPX(xml)
        assertEquals("临时点", decoded[0].name)
        assertEquals(PointType.OTHER, decoded[0].type)
        assertEquals(PointSource.MANUAL, decoded[0].source)
        assertEquals(28.1, decoded[0].latitude, 1e-6)
    }

    @Test fun gpxRejectsGarbageAndAllowsEmptyDocument() {
        assertThrows(PointCodecException::class.java) { PointCodec.decodeGPX("not a file") }
        assertEquals(0, PointCodec.decodeGPX("<gpx version=\"1.1\"></gpx>").size)
    }

    @Test fun mergeReplacesSameIdAndAppendsNew() {
        val original = makePoint(id = "a", name = "旧")
        val replacement = makePoint(id = "a", name = "新")
        val extra = makePoint(id = "b", name = "另")
        val merged = PointCodec.merging(listOf(original), listOf(replacement, extra))
        assertEquals(listOf("a", "b"), merged.map { it.id })
        assertEquals("新", merged[0].name)
    }
}

class AlertEngineTest {
    private val userLat = 28.0
    private val userLon = 120.0

    @Test fun outerThenInnerOncePerApproachThenRearm() {
        val first = eval(listOf(point("booth", 1_500.0)), heading = 0.0, speed = 5.0)
        assertEquals(listOf(AlertBand.OUTER), first.first.map { it.band })
        assertTrue(first.first[0].spokenText.contains("禁止摩托车进入"))
        assertTrue(first.first[0].spokenText.contains("G二五"))
        assertTrue(first.first[0].noticeText.contains("G25"))
        assertFalse(first.first[0].noticeText.contains("G二五"))
        assertTrue(first.first[0].spokenText.contains("长深高速"))
        assertTrue(first.first[0].spokenText.contains("前方"))
        assertEquals(true, first.second["booth"]?.outerFired)

        val again = eval(listOf(point("booth", 1_500.0)), heading = 0.0, speed = 5.0, approach = first.second)
        assertTrue(again.first.isEmpty())

        val inner = eval(listOf(point("booth", 400.0)), heading = 0.0, speed = 5.0, approach = first.second)
        assertEquals(listOf(AlertBand.INNER), inner.first.map { it.band })
        assertEquals(true, inner.second["booth"]?.outerFired)
        assertEquals(true, inner.second["booth"]?.innerFired)

        val still = eval(listOf(point("booth", 400.0)), heading = 0.0, speed = 5.0, approach = inner.second)
        assertTrue(still.first.isEmpty())

        val left = eval(listOf(point("booth", 2_800.0)), heading = 0.0, speed = 5.0, approach = inner.second)
        assertTrue(left.first.isEmpty())
        assertNull(left.second["booth"])

        val returned = eval(listOf(point("booth", 1_500.0)), heading = 0.0, speed = 5.0, approach = left.second)
        assertEquals(listOf(AlertBand.OUTER), returned.first.map { it.band })
    }

    @Test fun noAlertBeyondOuterRadius() {
        val result = eval(listOf(point("far", 3_000.0)), heading = 0.0, speed = 5.0)
        assertTrue(result.first.isEmpty())
    }

    @Test fun bufferInsideRearmDoesNotClear() {
        val fired = eval(listOf(point("booth", 1_500.0)), heading = 0.0, speed = 5.0)
        val holding = eval(listOf(point("booth", 2_100.0)), heading = 0.0, speed = 5.0, approach = fired.second)
        assertTrue(holding.first.isEmpty())
        assertEquals(true, holding.second["booth"]?.outerFired)
    }

    @Test fun startingInsideInnerMarksOuterFired() {
        val first = eval(listOf(point("booth", 400.0)), heading = 0.0, speed = 5.0)
        assertEquals(listOf(AlertBand.INNER), first.first.map { it.band })
        assertEquals(true, first.second["booth"]?.outerFired)
        val second = eval(listOf(point("booth", 1_500.0)), heading = 0.0, speed = 5.0, approach = first.second)
        assertTrue(second.first.isEmpty())
    }

    @Test fun passedPointDoesNotAlert() {
        assertTrue(eval(listOf(point("booth", 1_000.0)), heading = 180.0, speed = 8.0).first.isEmpty())
        val settings = AlertSettings()
        settings.onlyWhenApproaching = false
        assertTrue(eval(listOf(point("booth", 1_000.0)), heading = 180.0, speed = 8.0, settings = settings).first.isEmpty())
    }

    @Test fun lowSpeedUsesDistanceNotCourse() {
        val closing = eval(
            listOf(point("booth", 1_500.0)),
            heading = 180.0,
            speed = 0.4,
            approach = mapOf("booth" to ApproachRecord(lastDistanceMeters = 1_800.0)),
        )
        assertEquals(listOf(AlertBand.OUTER), closing.first.map { it.band })
        val opening = eval(
            listOf(point("booth", 308.0)),
            heading = null,
            speed = 0.4,
            approach = mapOf("booth" to ApproachRecord(lastDistanceMeters = 200.0)),
        )
        assertTrue(opening.first.isEmpty())
    }

    @Test fun missingCourseWithoutHistoryDoesNotAlert() {
        assertTrue(eval(listOf(point("booth", 1_000.0)), heading = null, speed = null).first.isEmpty())
    }

    @Test fun missingCourseAlertsWhenClosing() {
        val result = eval(
            listOf(point("booth", 1_500.0)),
            heading = null,
            speed = null,
            approach = mapOf("booth" to ApproachRecord(lastDistanceMeters = 1_800.0)),
        )
        assertEquals(listOf(AlertBand.OUTER), result.first.map { it.band })
        assertTrue(result.first[0].spokenText.contains("前方"))
    }

    @Test fun highSpeedBehindDoesNotAlertEvenWhenClosing() {
        val result = eval(
            listOf(point("booth", 1_500.0)),
            heading = 180.0,
            speed = 8.0,
            approach = mapOf("booth" to ApproachRecord(lastDistanceMeters = 1_800.0)),
        )
        assertTrue(result.first.isEmpty())
    }

    @Test fun capsToNearestPoints() {
        val points = listOf(point("c", 1_400.0), point("a", 1_000.0), point("b", 1_200.0))
        val settings = AlertSettings()
        settings.maxAlertsPerUpdate = 2
        val result = eval(points, heading = 0.0, speed = 5.0, settings = settings)
        assertEquals(listOf("a", "b"), result.first.map { it.point.id })
    }

    @Test fun communityDuplicateWithin80MetersDoesNotDoubleAlert() {
        val local = point("local", 1_000.0, false)
        val crowd = point("crowd", 1_030.0, true).also { it.name = "社区赤石" }
        val result = eval(listOf(crowd, local), heading = 0.0, speed = 5.0)
        assertEquals(listOf("local"), result.first.map { it.point.id })
    }

    @Test fun mutedAndUnlocatedAreSkipped() {
        assertTrue(eval(listOf(point("booth", 1_000.0)), heading = 0.0, speed = 5.0, muted = setOf("booth")).first.isEmpty())
        val hidden = point("booth", 1_000.0).copy(located = false)
        assertTrue(eval(listOf(hidden), heading = 0.0, speed = 5.0).first.isEmpty())
    }

    @Test fun hasPassedWhenBehindOrReceding() {
        val booth = point("booth", 400.0)
        assertTrue(AlertEngine.hasPassed(userLat, userLon, 180.0, 8.0, booth, 400.0, 420.0))
        assertFalse(AlertEngine.hasPassed(userLat, userLon, 0.0, 8.0, booth, 1_500.0, 400.0))
        assertFalse(AlertEngine.hasPassed(userLat, userLon, null, 0.2, booth, 400.0, 430.0))
        assertTrue(AlertEngine.hasPassed(userLat, userLon, null, 0.2, booth, 400.0, 470.0))
    }

    @Test fun outerRingExtendsTo80Kilometers() {
        val wide = AlertSettings(outerMeters = 90_000.0, innerMeters = 2_000.0)
        wide.normalize()
        assertEquals(80_000.0, wide.outerMeters, 0.1)
        assertEquals(2_000.0, wide.innerMeters, 0.1)
        val tight = AlertSettings(outerMeters = 100.0, innerMeters = 2_000.0)
        tight.normalize()
        assertEquals(300.0, tight.outerMeters, 0.1)
        assertEquals(250.0, tight.innerMeters, 0.1)
        assertTrue(tight.innerMeters < tight.outerMeters)

        val far = eval(
            listOf(point("far", 70_000.0)),
            heading = 0.0,
            speed = 5.0,
            settings = AlertSettings(outerMeters = 80_000.0),
        )
        assertEquals(listOf(AlertBand.OUTER), far.first.map { it.band })
        assertTrue(far.first[0].noticeText.contains("70"))
        val beyond = eval(
            listOf(point("far", 80_500.0)),
            heading = 0.0,
            speed = 5.0,
            settings = AlertSettings(outerMeters = 80_000.0),
        )
        assertTrue(beyond.first.isEmpty())
        assertTrue(eval(listOf(point("far", 70_000.0)), heading = 0.0, speed = 5.0).first.isEmpty())
    }

    @Test fun outerStopsStayEasyToSet() {
        val stops = AlertDistance.outerStops
        assertEquals(300.0, stops.first(), 0.1)
        assertEquals(80_000.0, stops.last(), 0.1)
        assertTrue(stops.contains(2_000.0))
        assertEquals(100.0, stops[1] - stops[0], 0.1)
        assertEquals(5_000.0, stops.last() - stops[stops.lastIndex - 1], 0.1)
        for (index in 1 until stops.size) assertTrue(stops[index] > stops[index - 1])
        assertEquals("300 米", AlertDistance.format(300.0))
        assertEquals("2 公里", AlertDistance.format(2_000.0))
        assertEquals("2.5 公里", AlertDistance.format(2_500.0))
        assertEquals("80 公里", AlertDistance.format(80_000.0))
        assertEquals(15_000.0, AlertDistance.nearbyRadius(2_000.0), 0.1)
        assertEquals(80_000.0, AlertDistance.nearbyRadius(80_000.0), 0.1)
        assertEquals(20_000.0, AlertDistance.nearbyRadius(20_000.0), 0.1)
        assertEquals(15, AlertDistance.nearbyKilometers(2_000.0))
        assertEquals(80, AlertDistance.nearbyKilometers(80_000.0))
    }

    @Test fun spokenRoadCodeAndTitle() {
        assertEquals("G二五", AlertEngine.spokenRoadCode("G25"))
        assertEquals("G一五W三", AlertEngine.spokenRoadCode("G15W3"))
        assertEquals("G25 长深高速 赤石收费站", AlertEngine.alertTitle(point("booth", 100.0)))
        val camera = point("booth", 100.0).copy(
            name = "G15 沈海高速 K1408 上海方向",
            roadCode = "G15",
            roadName = "沈海高速",
            type = PointType.CAMERA,
        )
        assertEquals("G15 沈海高速 K1408 上海方向", AlertEngine.alertTitle(camera))
        val uploaded = camera.copy(uploadSource = "user_upload")
        assertEquals("G15 沈海高速 K1408 上海方向 用户上传", AlertEngine.alertTitle(uploaded))
        assertTrue(AlertEngine.notice(uploaded, 500.0).contains("用户上传"))
        val shown = AlertEngine.notice(camera, 500.0)
        assertTrue(shown.contains("G15 沈海高速 K1408 上海方向"))
        assertFalse(shown.contains("G一五"))
        assertEquals(1, shown.split("G15").size - 1)
    }

    private fun eval(
        points: List<EnforcementPoint>,
        heading: Double?,
        speed: Double?,
        approach: Map<String, ApproachRecord> = emptyMap(),
        muted: Set<String> = emptySet(),
        settings: AlertSettings = AlertSettings(),
    ) = AlertEngine.evaluate(userLat, userLon, heading, speed, points, settings, approach, muted)

    private fun point(id: String, north: Double, community: Boolean = false): EnforcementPoint {
        val shifted = GeoMath.offset(userLat, userLon, north, 0.0)
        return makePoint(
            id = id,
            latitude = shifted.latitude,
            longitude = shifted.longitude,
            source = if (community) PointSource.COMMUNITY else PointSource.MANUAL,
            isCommunity = community,
        )
    }
}

private fun makePoint(
    id: String = "p1",
    name: String = "赤石收费站",
    notes: String = "",
    latitude: Double = 28.2,
    longitude: Double = 119.9,
    source: PointSource = PointSource.MANUAL,
    isCommunity: Boolean = false,
    createdAt: Instant = Instant.parse("2026-01-15T06:20:00Z"),
): EnforcementPoint = EnforcementPoint(
    id = id,
    name = name,
    type = PointType.BANNED_TOLL,
    roadCode = "G25",
    roadName = "长深高速",
    notes = notes,
    latitude = latitude,
    longitude = longitude,
    fineYuan = 100,
    penaltyPoints = 1,
    violation = "驾驶机动车违反禁令标志指示的",
    source = source,
    province = "浙江",
    city = "丽水",
    createdAt = createdAt,
    updatedAt = createdAt,
    isCommunity = isCommunity,
)

class SyncNoticeTest {
    @Test fun successMentionsNearbyOnlyWhenEmpty() {
        assertEquals("已更新，共 88 个点位", SyncNotice.success(88, 3))
        assertEquals("已更新，共 88 个点位", SyncNotice.success(88, null))
        assertEquals(
            "已更新，共 88 个点位。附近 15 公里内暂时没有点位，缩小地图可以看到其他地方的点。",
            SyncNotice.success(88, 0),
        )
        assertEquals("更新失败：网络不通", SyncNotice.failure("网络不通"))
        assertEquals("更新失败：网络不通", SyncNotice.failure("  "))
        assertEquals("还没有服务器地址", SyncNotice.noServer)
        assertEquals(
            "已更新，共 88 个点位。附近 80 公里内暂时没有点位，缩小地图可以看到其他地方的点。",
            SyncNotice.success(88, 0, 80),
        )
    }
}

class AppReleaseTest {
    @Test fun newerBuildIsDetected() {
        assertTrue(AppRelease.isNewer(21, 1))
        assertFalse(AppRelease.isNewer(21, 21))
        assertFalse(AppRelease.isNewer(1, 21))
    }

    @Test fun versionLinesUseBuildNumber() {
        assertEquals("当前版本：第 27 版", AppRelease.currentVersionLine(27))
        assertEquals("已是最新版本（第 27 版）", AppRelease.upToDateLine(27))
        assertEquals("有新版本：第 28 版（当前第 27 版）", AppRelease.updateAvailableLine(28, 27))
    }

    @Test fun sha256MatchesKnownDigest() {
        val file = File.createTempFile("apk", ".bin")
        file.writeText("hello")
        assertEquals("2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", AppRelease.sha256Hex(file))
        file.delete()
    }
}

private fun queryMap(url: String): Map<String, String> {
    val query = URI(url).rawQuery ?: return emptyMap()
    return query.split("&").associate { part ->
        val bits = part.split("=", limit = 2)
        URLDecoder.decode(bits[0], Charsets.UTF_8.name()) to URLDecoder.decode(bits.getOrElse(1) { "" }, Charsets.UTF_8.name())
    }
}

private fun containsPlate(text: String): Boolean {
    return Regex("[京津沪渝冀豫云辽黑湘皖鲁新苏浙赣鄂桂甘晋蒙陕吉闽贵粤青藏川宁琼][A-Z][A-Z0-9]{4,6}").containsMatchIn(text)
}

private fun kotlinx.serialization.json.JsonElement.jsonObject() = this as kotlinx.serialization.json.JsonObject

private fun kotlinx.serialization.json.JsonObject.string(key: String) = getValue(key).let { (it as kotlinx.serialization.json.JsonPrimitive).content }

private fun kotlinx.serialization.json.JsonObject.int(key: String) = string(key).toInt()
