package com.example.xiaomoto.core

import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder
import kotlin.math.abs
import org.json.JSONArray
import org.json.JSONObject

data class RoadStake(val km: Double, val metres: Int?)

data class RecogGlued(val code: String, val km: Double, val metres: Int?)

data class TicketParse(
    val norm: String,
    val code: String?,
    val roadName: String?,
    val nameCanon: String?,
    val nameHow: String?,
    val stakes: List<RoadStake>,
    val glued: List<RecogGlued>,
    val range: Boolean,
    val direction: String?,
    val facility: String?,
    val facilityType: String?,
    val portal: String?,
    val admin: List<String>,
)

data class OverlayFix(val code: String?, val name: String, val km: Double, val metres: Int)

data class RegionSignals(val provinces: List<Pair<String, String>>, val plate: String?, val overlay: OverlayFix?, val overlayDir: String?)

data class LocatedPoint(
    val latitude: Double,
    val longitude: Double,
    val endLatitude: Double?,
    val endLongitude: Double?,
    val method: String,
    val how: String,
    val address: String,
    val confidence: String,
    val roadCode: String,
    val roadName: String,
    val stake: String,
    val direction: String,
    val parsed: TicketParse,
) {
    val exact: Boolean get() = confidence == "exact"
}

object Recog {
    private val names = mapOf(
        "京哈" to "G1", "京沪" to "G2", "京台" to "G3", "京港澳" to "G4", "京昆" to "G5", "京藏" to "G6", "京新" to "G7",
        "鹤大" to "G11", "珲乌" to "G12", "沈海" to "G15", "丹锡" to "G16", "荣乌" to "G18", "青银" to "G20", "青兰" to "G22",
        "长深" to "G25", "连霍" to "G30", "济广" to "G35", "宁洛" to "G36", "沪陕" to "G40", "沪蓉" to "G42", "大广" to "G45",
        "沪渝" to "G50", "二广" to "G55", "杭瑞" to "G56", "呼北" to "G59", "沪昆" to "G60", "包茂" to "G65", "银百" to "G69",
        "福银" to "G70", "泉南" to "G72", "兰海" to "G75", "厦蓉" to "G76", "汕昆" to "G78", "广昆" to "G80", "银昆" to "G85",
        "成渝环线" to "G93", "珠三角环线" to "G94", "首都环线" to "G95", "杭州湾环线" to "G92", "海南环线" to "G98",
        "温丽" to "G1513", "甬莞" to "G1523", "甬金" to "G1512", "常台" to "G1522", "宁上" to "G1514", "盐洛" to "G1516",
        "青新" to "G2011", "东吕" to "G2516", "溧宁" to "G4012", "石渝" to "G5021", "德上" to "G0321", "张南" to "G5515",
        "杭长" to "G6021", "杭金衢" to "G60", "申嘉湖" to "S12", "苏锡常南部" to "S58", "沪常" to "S58", "广河" to "S2",
        "成宜昭" to "S4", "威砚" to "S11", "枣木" to "S83", "岚菏" to "S38", "芜合" to "G5011", "南芜" to "G4211",
        "沪武" to "G4221", "淮徐" to "G2513", "甬舟" to "G9211", "渝黔" to "G75", "渝湘" to "G65", "合安" to "G3",
    )
    private val provinces = listOf(
        "北京", "天津", "上海", "重庆", "河北", "山西", "辽宁", "吉林", "黑龙江", "江苏", "浙江", "安徽", "福建",
        "江西", "山东", "河南", "湖北", "湖南", "广东", "海南", "四川", "贵州", "云南", "陕西", "甘肃", "青海",
        "台湾", "内蒙古", "广西", "西藏", "宁夏", "新疆", "香港", "澳门",
    )
    private val adcode = mapOf(
        "11" to "北京", "12" to "天津", "13" to "河北", "14" to "山西", "15" to "内蒙古", "21" to "辽宁", "22" to "吉林",
        "23" to "黑龙江", "31" to "上海", "32" to "江苏", "33" to "浙江", "34" to "安徽", "35" to "福建", "36" to "江西",
        "37" to "山东", "41" to "河南", "42" to "湖北", "43" to "湖南", "44" to "广东", "45" to "广西", "46" to "海南",
        "50" to "重庆", "51" to "四川", "52" to "贵州", "53" to "云南", "54" to "西藏", "61" to "陕西", "62" to "甘肃",
        "63" to "青海", "64" to "宁夏", "65" to "新疆",
    )
    private val fac = "收费站|服务区|停车区|隧道|特大桥|大桥|枢纽|互通|立交|站口"

    fun normalize(raw: String): String {
        var t = fold(raw).replace("\r", "")
        t = joinBroken(t)
        t = Regex("""\s*\n\s*""").replace(t, " ")
        t = collapseHan(t)
        for ((a, b) in listOf("高遠" to "高速", "高远" to "高速", "高逮" to "高速", "高建" to "高速", "公理" to "公里", "千來" to "千米", "kM" to "KM", "Km" to "KM", "km" to "KM")) {
            t = t.replace(a, b)
        }
        t = t.mapIndexed { i, ch -> if (ch == 'k' && (i + 1 >= t.length || (t[i + 1] != 'm' && t[i + 1] != 'M'))) 'K' else ch }.joinToString("")
        val chars = t.toCharArray()
        for (i in 1 until chars.size - 1) {
            if (chars[i] == 'O' || chars[i] == 'o') {
                val prev = chars[i - 1]
                val next = chars[i + 1]
                if ((prev.isDigit() && next.isDigit()) || ((prev == 'G' || prev == 'S') && next.isDigit())) chars[i] = '0'
            }
        }
        return String(chars).trim()
    }

    fun parse(text: String): TicketParse {
        val t = normalize(text)
        var code = firstCode(t)
        val gluedAll = glued(t)
        var roadName: String? = null
        var canon: String? = null
        var how: String? = null
        val nm = roadNameOf(t)
        if (nm != null) {
            roadName = nm
            val hit = nameToCode(nm, code)
            canon = hit?.second
            how = hit?.third
            if (code == null && hit != null) code = hit.first
        }
        var t2 = t
        if (gluedAll.second != null) {
            val (s, e) = gluedAll.second!!
            t2 = t.substring(0, s) + " ".repeat(e - s) + t.substring(e)
        }
        val stakes = stakesIn(t2)
        var range = false
        if (stakes.size >= 2) {
            val between = t2.substring(stakes[0].span.second, stakes[1].span.first)
            if (Regex("""\s*(?:-|—|–|~|～|至|到|_)\s*""").containsMatchIn(between) || between.length < 12) range = true
        }
        var direction: String? = null
        val tnr = Regex("""[\u4e00-\u9fff]{2,6}?(?:高速公路|高速|快速路)""").replace(t, " ")
        Regex("""([\u4e00-\u9fff]{1,6}?)方向""").find(tnr)?.let { direction = it.value }
        for (w in listOf("上行", "下行", "A道", "B道")) {
            if (t.contains(w)) direction = listOfNotNull(direction, w).joinToString(" ")
        }
        Regex("""由[东南西北]向[东南西北]""").find(t)?.let { direction = it.value }
        Regex("""北向南|南向北|东向西|西向东""").find(t)?.let { m ->
            if (direction?.contains(m.value) != true) direction = listOfNotNull(direction, m.value).joinToString(" ")
        }
        var facility: String? = null
        var ftype: String? = null
        var portal: String? = null
        Regex("""([\u4e00-\u9fff]{2,10}?)($fac)(?:([东南西北])?(入口|出口|洞口|口|进口))?""").find(t)?.let { f ->
            var base = Regex("""^.*?(?:高速公路|高速|省|市|县|区段|区|段)""").replace(f.groupValues[1], "")
            if (base.isEmpty()) base = f.groupValues[1]
            base = Regex("""^(?:米|公里|千米|处|附近|往|至)+""").replace(base, "")
            val kind = if (f.groupValues[2] == "站口") "收费站" else f.groupValues[2]
            facility = base + kind
            ftype = kind
            if (f.groupValues[4].isNotEmpty()) portal = f.groupValues[3] + f.groupValues[4]
        }
        var t3 = Regex("""[\u4e00-\u9fff]{2,6}?(?:高速公路|高速|快速路)""").replace(t, " ")
        t3 = Regex("""[\u4e00-\u9fff]{1,10}?(?:$fac)""").replace(t3, " ")
        val admin = mutableListOf<String>()
        for (a in Regex("""([\u4e00-\u9fff]{2,7}?(?:省|自治区|市|自治州|地区|县|区))""").findAll(t3)) {
            val word = a.groupValues[1]
            if (word.length <= 8 && !word.contains("方向") && !word.contains("城区") && !word.contains("区间")) admin += word
        }
        return TicketParse(t, code, roadName, canon, how, stakes.map { RoadStake(it.km, it.metres) }, gluedAll.first, range, direction, facility, ftype, portal, admin)
    }

    fun signals(full: String, overlay: String = ""): RegionSignals {
        val t = normalize(full)
        val provs = mutableListOf<Pair<String, String>>()
        for (m in Regex("""《(?:中华人民共和国)?([\u4e00-\u9fff]{2,3}?)(?:省|自治区|市)[^》]{0,30}(?:条例|办法|规定)》""").findAll(t)) {
            if (m.groupValues[1] in provinces) provs += "legal" to m.groupValues[1]
        }
        for (p in provinces) {
            when {
                t.contains(p + "交警") || t.contains(p + "高速交警") -> provs += "sender" to p
                authority(t, p) -> provs += "authority" to p
                t.startsWith(p + "省") || t.startsWith(p + "市") || t.startsWith(p + "自治区") -> provs += "text" to p
            }
        }
        Regex("""12123\s*(\d{2})\d""").find(t)?.let { m -> adcode[m.groupValues[1]]?.let { provs += "sms_port" to it } }
        val plate = Regex("""([京津沪渝冀晋辽吉黑苏浙皖闽赣鲁豫鄂湘粤琼川贵云陕甘青蒙桂藏宁新])[A-Z][A-Z0-9·]{4,6}""").find(t)?.groupValues?.get(1)
        val o = normalize(overlay).ifBlank { t }
        var fix: OverlayFix? = null
        var dir: String? = null
        Regex("""地点[:：]?\s*([GS]\d{1,4})?([\u4e00-\u9fff]{0,6})K(\d{1,4})\+(\d{1,4})""").find(o)?.let { m ->
            fix = OverlayFix(m.groupValues[1].ifBlank { null }, m.groupValues[2], m.groupValues[3].toDouble(), m.groupValues[4].toInt())
        }
        Regex("""(?:通行|行驶)方向[:：]?\s*(上行|下行)""").find(o)?.let { dir = it.groupValues[1] }
        Regex("""设备编号[:：]?\s*(\d{6})""").find(o)?.let { m -> adcode[m.groupValues[1].take(2)]?.let { provs += "device" to it } }
        return RegionSignals(provs, plate, fix, dir)
    }

    fun level(method: String, conf: String, how: String): String = when (method) {
        "overlay-stake", "stake", "glued-stake", "tunnel-portal" ->
            if (conf == "low" || conf == "none" || conf.isEmpty() || how.contains("metres dropped")) "low" else "exact"
        "facility-poi" -> if (conf == "high") "exact" else "low"
        else -> "low"
    }

    fun locate(text: String, full: String, key: String): LocatedPoint? {
        val parsed = parse(text.ifBlank { full })
        val sig = signals(if (full.isBlank()) text else full)
        val region = regionWords(parsed, sig, full.ifBlank { text }, key)
        val hit = locateParsed(parsed, sig, region, key) ?: return null
        val roadName = hit.roadName.ifBlank { parsed.nameCanon ?: parsed.roadName.orEmpty() }
        val direction = listOfNotNull(parsed.direction, sig.overlayDir).filter { it.isNotBlank() }.distinct().joinToString(" ")
        return hit.copy(roadName = roadName, direction = direction, parsed = parsed, confidence = level(hit.method, hit.confidence, hit.how).let { if (!hit.latitude.isFinite()) "low" else it })
    }

    private fun locateParsed(p: TicketParse, sig: RegionSignals, region: List<String>, key: String): LocatedPoint? {
        val blank = LocatedPoint(Double.NaN, Double.NaN, null, null, "", "", "", "none", p.code.orEmpty(), p.nameCanon ?: "", "", p.direction.orEmpty(), p)
        sig.overlay?.let { ol ->
            if (!ol.code.isNullOrBlank()) {
                stakePoint(key, ol.code, ol.km, ol.metres, region)?.let { sp ->
                    return blank.copy(latitude = sp.lat, longitude = sp.lon, method = "overlay-stake", how = sp.how, address = sp.addr, confidence = "high", roadCode = ol.code, stake = stakeLabel(ol.km, ol.metres))
                }
            }
        }
        if (p.glued.isNotEmpty()) {
            val cands = p.glued.mapNotNull { g ->
                val sp = stakePoint(key, g.code, g.km, g.metres, null) ?: return@mapNotNull null
                var score = 1
                val blob = sp.prov + sp.addr
                if (region.isNotEmpty() && region.any { it in blob }) score += 5 else if (region.isNotEmpty()) score -= 5
                Quad(score, g, sp)
            }.sortedByDescending { it.score }
            if (cands.isNotEmpty()) {
                val top = cands.first()
                var conf = top.sp.conf
                val ambiguous = cands.size > 1 && cands[1].score == top.score
                if (ambiguous || (region.isNotEmpty() && top.score < 6) || (region.isEmpty() && cands.size > 1)) conf = "low"
                val me = top.g.metres ?: 0
                return blank.copy(latitude = top.sp.lat, longitude = top.sp.lon, method = "glued-stake", how = top.sp.how, address = top.sp.addr, confidence = conf, roadCode = top.g.code, stake = stakeLabel(top.g.km, me))
            }
        }
        if (!p.code.isNullOrBlank() && p.stakes.isNotEmpty()) {
            val km = p.stakes[0].km
            val me = p.stakes[0].metres
            var sp = stakePoint(key, p.code, km, me, region)
            if (sp == null && region.isNotEmpty()) sp = stakePoint(key, p.code, km, me, null)
            if (sp != null) {
                var conf = sp.conf
                if ((sp.ambiguous || p.code.startsWith("S")) && region.isEmpty()) conf = "low"
                if (region.isNotEmpty() && region.none { it in sp.prov + sp.addr }) conf = "low"
                var endLat: Double? = null
                var endLon: Double? = null
                if (p.range && p.stakes.size > 1) {
                    stakePoint(key, p.code, p.stakes[1].km, p.stakes[1].metres, region)?.let {
                        endLat = it.lat
                        endLon = it.lon
                    }
                }
                return blank.copy(
                    latitude = sp.lat, longitude = sp.lon, endLatitude = endLat, endLongitude = endLon,
                    method = "stake", how = sp.how, address = sp.addr, confidence = conf, roadCode = p.code,
                    stake = stakeLabel(km, me ?: 0),
                )
            }
            if (p.facility == null) return null
        }
        if (!p.facility.isNullOrBlank() && !p.facilityType.isNullOrBlank()) {
            poi(key, p.facility, p.facilityType, region, p.code, p.nameCanon)?.let { b ->
                var conf = if (b.score >= 15 || (b.score >= 10 && region.isNotEmpty())) "high" else "medium"
                if (b.far) conf = "low"
                var lat = b.lat
                var lon = b.lon
                var method = "facility-poi"
                var how = b.name
                val side = p.portal?.firstOrNull()?.toString().orEmpty()
                if (p.facilityType == "隧道" && side in "东西南北") {
                    osmPortal(lat, lon, side, p.direction)?.let { tp ->
                        lat = tp.first
                        lon = tp.second
                        method = "tunnel-portal"
                        how = "${b.name} ${side}口 (${tp.third})"
                        conf = "high"
                    } ?: tunnelStep(key, p.facility, b.gcjLon, b.gcjLat, side)?.let { tp ->
                        lat = tp.first
                        lon = tp.second
                        method = "tunnel-portal"
                        how = "${b.name} ${side}端"
                        conf = "medium"
                    }
                }
                return blank.copy(latitude = lat, longitude = lon, method = method, how = how, address = b.admin, confidence = conf, roadCode = p.code.orEmpty())
            }
        }
        val geo = get(key, "v3/geocode/geo", mapOf("address" to p.norm)) ?: return null
        val row = geo.optJSONArray("geocodes")?.optJSONObject(0) ?: return null
        val loc = split(row.optString("location")) ?: return null
        val w = CoordinateConverter.gcj02ToWgs84(CoordinateConverter.Coord(loc.second, loc.first))
        val levelName = row.optString("level")
        val addr = row.optString("formatted_address")
        val adminLevel = levelName in setOf("省", "市", "区县", "乡镇", "村庄", "道路", "开发区")
        val conf = if (adminLevel || p.facility != null || (region.isNotEmpty() && region.none { it in addr })) "low" else "medium"
        return blank.copy(latitude = w.latitude, longitude = w.longitude, method = "geocode", how = "level=$levelName", address = addr, confidence = conf)
    }

    private data class Quad(val score: Int, val g: RecogGlued, val sp: MarkerPoint)
    private data class MarkerPoint(val lat: Double, val lon: Double, val how: String, val addr: String, val prov: String, val conf: String, val ambiguous: Boolean)
    private data class Poi(val score: Int, val name: String, val admin: String, val lat: Double, val lon: Double, val gcjLon: Double, val gcjLat: Double, val far: Boolean)

    private fun stakeLabel(km: Double, metres: Int) = "K${km.toInt()}+$metres"

    private data class RawHit(val lat: Double, val lon: Double, val addr: String, val prov: String)

    private fun kmMarker(key: String, code: String, km: Int, region: List<String>?): MarkerPoint? {
        val d = get(key, "v3/geocode/geo", mapOf("address" to "$code ${km}公里")) ?: return null
        val rows = d.optJSONArray("geocodes") ?: return null
        val hits = mutableListOf<RawHit>()
        for (i in 0 until rows.length()) {
            val g = rows.getJSONObject(i)
            val fa = g.optString("formatted_address")
            if (code !in fa || !Regex("""(?:^|[^\d])${km}公里""").containsMatchIn(fa)) continue
            val loc = split(g.optString("location")) ?: continue
            val w = CoordinateConverter.gcj02ToWgs84(CoordinateConverter.Coord(loc.second, loc.first))
            hits += RawHit(w.latitude, w.longitude, fa, g.optString("province"))
        }
        if (hits.isEmpty()) return null
        val clusters = mutableListOf<MutableList<RawHit>>()
        for (h in hits) {
            val c = clusters.firstOrNull { GeoMath.distanceMeters(it[0].lat, it[0].lon, h.lat, h.lon) < 20000 }
            if (c != null) c += h else clusters += mutableListOf(h)
        }
        var use = clusters
        val regs = region.orEmpty().filter { it.isNotBlank() }
        if (regs.isNotEmpty()) {
            val keep = clusters.filter { c -> regs.any { it in c[0].addr || it in c[0].prov } }
            if (keep.isEmpty()) return null
            use = keep.toMutableList()
        }
        val c = use[0]
        return MarkerPoint(c.map { it.lat }.average(), c.map { it.lon }.average(), "", c[0].addr, c[0].prov, "", use.size > 1)
    }

    private fun stakePoint(key: String, code: String, km: Double, metres: Int?, region: List<String>?): MarkerPoint? {
        val kmi = km.toInt()
        val me = metres ?: 0
        val frac = (km - kmi) + me / 1000.0
        val a = kmMarker(key, code, kmi, region)
        if (a == null) {
            val steps = listOf(1, 2, 3, 5, 8, 12)
            val lo = steps.firstNotNullOfOrNull { k -> kmMarker(key, code, kmi - k, region)?.let { (kmi - k) to it } }
            val hi = steps.firstNotNullOfOrNull { k -> kmMarker(key, code, kmi + k, region)?.let { (kmi + k) to it } }
            if (lo != null && hi != null && hi.first - lo.first > 3) {
                val f = (km + me / 1000.0 - lo.first) / (hi.first - lo.first)
                routeInterp(key, lo.second.lat, lo.second.lon, hi.second.lat, hi.second.lon, f, (hi.first - lo.first).toDouble())?.let { (lat, lon) ->
                    return lo.second.copy(lat = lat, lon = lon, how = "route-interp K${lo.first}..K${hi.first}", conf = "medium")
                }
            }
            if (lo != null && hi != null && GeoMath.distanceMeters(lo.second.lat, lo.second.lon, hi.second.lat, hi.second.lon) < 1500 * (hi.first - lo.first)) {
                val f = (km + me / 1000.0 - lo.first) / (hi.first - lo.first)
                val lat = lo.second.lat + f * (hi.second.lat - lo.second.lat)
                val lon = lo.second.lon + f * (hi.second.lon - lo.second.lon)
                return lo.second.copy(lat = lat, lon = lon, how = "interp K${lo.first}..K${hi.first}", conf = "medium")
            }
            return null
        }
        if (frac < 0.05) return a.copy(how = "marker K$kmi", conf = "high")
        for (step in listOf(1, 2, 3)) {
            val b = kmMarker(key, code, kmi + step, region)
            if (b != null && GeoMath.distanceMeters(a.lat, a.lon, b.lat, b.lon) < 1500.0 * step) {
                val f = frac / step
                return a.copy(lat = a.lat + f * (b.lat - a.lat), lon = a.lon + f * (b.lon - a.lon), how = "marker K$kmi->K${kmi + step} x${"%.2f".format(frac)}", conf = "high")
            }
            val c = kmMarker(key, code, kmi - step, region)
            if (c != null && GeoMath.distanceMeters(a.lat, a.lon, c.lat, c.lon) < 1500.0 * step) {
                val f = frac / step
                return a.copy(lat = a.lat + f * (a.lat - c.lat), lon = a.lon + f * (a.lon - c.lon), how = "marker K$kmi extrap K${kmi - step} x${"%.2f".format(frac)}", conf = "high")
            }
        }
        return a.copy(how = "marker K$kmi (metres dropped)", conf = "medium")
    }

    private fun routeInterp(key: String, aLat: Double, aLon: Double, bLat: Double, bLon: Double, frac: Double, gapKm: Double): Pair<Double, Double>? {
        val a = CoordinateConverter.wgs84ToGcj02(CoordinateConverter.Coord(aLat, aLon))
        val b = CoordinateConverter.wgs84ToGcj02(CoordinateConverter.Coord(bLat, bLon))
        val d = get(key, "v3/direction/driving", mapOf(
            "origin" to "%.6f,%.6f".format(a.longitude, a.latitude),
            "destination" to "%.6f,%.6f".format(b.longitude, b.latitude),
            "strategy" to "2", "extensions" to "base",
        )) ?: return null
        val path = d.optJSONObject("route")?.optJSONArray("paths")?.optJSONObject(0) ?: return null
        val distKm = path.optString("distance").toDoubleOrNull()?.div(1000) ?: return null
        if (gapKm <= 0 || distKm / gapKm <= 0.8 || distKm / gapKm >= 1.25) return null
        val pl = mutableListOf<Pair<Double, Double>>()
        val steps = path.optJSONArray("steps") ?: return null
        for (i in 0 until steps.length()) {
            for (q in steps.getJSONObject(i).optString("polyline").split(';')) {
                val loc = split(q) ?: continue
                if (pl.isEmpty() || pl.last() != loc) pl += loc
            }
        }
        if (pl.size < 2) return null
        val seg = (0 until pl.size - 1).map { i ->
            GeoMath.distanceMeters(pl[i].second, pl[i].first, pl[i + 1].second, pl[i + 1].first)
        }
        var target = seg.sum() * frac
        for (i in seg.indices) {
            if (target <= seg[i]) {
                val u = if (seg[i] == 0.0) 0.0 else target / seg[i]
                val lon = pl[i].first + u * (pl[i + 1].first - pl[i].first)
                val lat = pl[i].second + u * (pl[i + 1].second - pl[i].second)
                val w = CoordinateConverter.gcj02ToWgs84(CoordinateConverter.Coord(lat, lon))
                return w.latitude to w.longitude
            }
            target -= seg[i]
        }
        return null
    }

    private fun poi(key: String, name: String, ftype: String, provs: List<String>, code: String?, roadname: String?): Poi? {
        var base = if (name.endsWith(ftype)) name.removeSuffix(ftype) else name
        base = Regex("""^.*?(?:段|高速|方向)""").replace(base, "").ifBlank { base }
        val syn = mapOf("服务区" to listOf("服务区", "停车区"), "停车区" to listOf("停车区", "服务区"), "站口" to listOf("收费站"), "收费站" to listOf("收费站"))
        val types = syn[ftype] ?: listOf(ftype)
        val names = types.map { base + it }
        val regs = provs.filter { it.isNotBlank() }.sortedBy { it in provinces }
        val reqs = mutableListOf<Pair<String, String?>>()
        for (pv in regs.take(3)) for (kw in names) reqs += kw to pv
        for (kw in names) reqs += kw to null
        if (!roadname.isNullOrBlank()) reqs += (roadname + names[0]) to null
        val cands = mutableListOf<Poi>()
        for ((kw, city) in reqs) {
            val q = mutableMapOf("keywords" to kw, "offset" to "20", "extensions" to "base")
            if (city != null) { q["city"] = city; q["citylimit"] = "true" }
            val d = get(key, "v3/place/text", q) ?: continue
            val pois = d.optJSONArray("pois") ?: JSONArray()
            for (i in 0 until pois.length()) {
                val p = pois.getJSONObject(i)
                val nm = p.optString("name")
                val tp = p.optString("type")
                val core = Regex("""[（(].*?[)）]""").replace(nm, "")
                if (names.none { core == it || core.startsWith(it) || (core.endsWith(it) && core.length - it.length <= 3) }) continue
                if (ftype == "隧道" && "隧道" !in tp + nm) continue
                var sc = if (core in names) 10 else 5
                if (!code.isNullOrBlank() && code in nm + p.optString("address")) sc += 5
                if (!roadname.isNullOrBlank() && roadname.take(2) in nm) sc += 3
                val loc = split(p.optString("location")) ?: continue
                val w = CoordinateConverter.gcj02ToWgs84(CoordinateConverter.Coord(loc.second, loc.first))
                val admin = p.optString("pname") + p.optString("cityname") + p.optString("adname")
                cands += Poi(sc, nm, admin, w.latitude, w.longitude, loc.first, loc.second, false)
            }
            if (cands.maxOfOrNull { it.score } ?: 0 >= 15) break
        }
        if (cands.isEmpty()) return null
        var pool: List<Poi> = cands
        if (provs.any { it.isNotBlank() }) {
            val inn = cands.filter { c -> provs.any { it.isNotBlank() && it in c.admin } }.map { it.copy(score = it.score + 5) }
            if (inn.isEmpty()) return null
            pool = inn
        }
        val top = pool.maxBy { it.score }
        val far = pool.any { GeoMath.distanceMeters(it.lat, it.lon, top.lat, top.lon) > 30000 && it.score >= top.score - 5 }
        return top.copy(far = far)
    }

    private fun tunnelStep(key: String, name: String, lon: Double, lat: Double, side: String): Triple<Double, Double, String>? {
        val pairs = listOf(
            (lon - 0.08 to lat + 0.035) to (lon + 0.04 to lat - 0.015),
            (lon + 0.04 to lat - 0.015) to (lon - 0.08 to lat + 0.035),
        )
        for ((o, dest) in pairs) {
            val d = get(key, "v3/direction/driving", mapOf(
                "origin" to "%.6f,%.6f".format(o.first, o.second),
                "destination" to "%.6f,%.6f".format(dest.first, dest.second),
                "strategy" to "2", "extensions" to "base",
            )) ?: continue
            val steps = d.optJSONObject("route")?.optJSONArray("paths")?.optJSONObject(0)?.optJSONArray("steps") ?: continue
            for (i in 0 until steps.length()) {
                val s = steps.getJSONObject(i)
                if (s.optString("road") != name) continue
                val pts = s.optString("polyline").split(';').mapNotNull { split(it) }
                if (pts.size < 2) continue
                val ends = listOf(pts.first(), pts.last())
                val e = ends.maxBy { sideScore(side, it.first, it.second) }
                val w = CoordinateConverter.gcj02ToWgs84(CoordinateConverter.Coord(e.second, e.first))
                return Triple(w.latitude, w.longitude, "step")
            }
        }
        return null
    }

    private fun osmPortal(lat: Double, lon: Double, side: String, travel: String?): Triple<Double, Double, String>? {
        val q = """[out:json][timeout:25];way["tunnel"="yes"]["highway"~"motorway|trunk|primary"](around:6000,$lat,$lon);out geom;"""
        val body = postForm("https://overpass-api.de/api/interpreter", q) ?: return null
        val els = runCatching { JSONObject(body).optJSONArray("elements") }.getOrNull() ?: return null
        data class Way(val geom: List<Pair<Double, Double>>)
        val ways = mutableListOf<Way>()
        for (i in 0 until els.length()) {
            val g = els.getJSONObject(i).optJSONArray("geometry") ?: continue
            val pts = (0 until g.length()).map { g.getJSONObject(it).let { o -> o.optDouble("lat") to o.optDouble("lon") } }
            if (pts.size > 1) ways += Way(pts)
        }
        if (ways.isEmpty()) return null
        fun len(w: Way) = w.geom.zipWithNext().sumOf { (a, b) -> GeoMath.distanceMeters(a.first, a.second, b.first, b.second) }
        val longest = ways.maxOf { len(it) }
        val ends = mutableListOf<Triple<Double, Double, String>>()
        for (w in ways) {
            if (len(w) < 0.5 * longest) continue
            ends += Triple(w.geom.first().first, w.geom.first().second, "start")
            ends += Triple(w.geom.last().first, w.geom.last().second, "end")
        }
        if (ends.isEmpty()) return null
        var best = ends.maxBy { sideScore(side, it.second, it.first) }
        if (!travel.isNullOrBlank()) {
            ends.firstOrNull { it.third == "start" && GeoMath.distanceMeters(it.first, it.second, best.first, best.second) < 300 }?.let { best = it }
        }
        return Triple(best.first, best.second, "OSM tunnel way ${best.third}")
    }

    private fun sideScore(side: String, lon: Double, lat: Double) = when (side) {
        "东" -> lon
        "西" -> -lon
        "北" -> lat
        "南" -> -lat
        else -> 0.0
    }

    private val cityCache = mutableListOf<String>()
    private var citiesLoaded = false

    private fun cities(key: String): List<String> {
        if (citiesLoaded) return cityCache
        citiesLoaded = true
        val d = get(key, "v3/config/district", mapOf("keywords" to "中国", "subdistrict" to "2", "extensions" to "base")) ?: return cityCache
        val provs = d.optJSONArray("districts")?.optJSONObject(0)?.optJSONArray("districts") ?: return cityCache
        for (i in 0 until provs.length()) {
            val cs = provs.getJSONObject(i).optJSONArray("districts") ?: continue
            for (j in 0 until cs.length()) {
                var name = cs.getJSONObject(j).optString("name")
                for (suf in listOf("自治州", "地区", "市", "盟")) if (name.endsWith(suf)) name = name.removeSuffix(suf)
                if (name.length >= 2 && name !in provinces) cityCache += name
            }
        }
        return cityCache
    }

    private fun regionWords(p: TicketParse, sig: RegionSignals, full: String, key: String): List<String> {
        val w = sig.provinces.map { it.second }.toMutableList()
        if (key.isNotBlank()) for (c in cities(key)) if (c in full || c in p.norm) w += c
        for (a in p.admin) {
            for (piece in Regex("""[\u4e00-\u9fff]+?(?:省|自治区|自治州|市|地区|县|区)""").findAll(a)) {
                val core = Regex("""(省|自治区|自治州|市|地区|县|区)$""").replace(piece.value, "")
                if (core.length >= 2) w += core
            }
        }
        return w.distinct()
    }

    private fun firstCode(t: String): String? {
        for (m in Regex("""([GS]\d{1,4})""").findAll(t)) {
            val s = m.range.first
            if (s > 0) {
                val prev = t[s - 1]
                if (prev in 'A'..'Z' || prev.isDigit()) continue
            }
            val e = m.range.last + 1
            if (e < t.length) {
                val next = t[e]
                if (next !in '\u4e00'..'\u9fff' && !next.isWhitespace() && next != 'K') continue
            }
            return m.value
        }
        return null
    }

    private fun roadNameOf(t: String): String? {
        val m = Regex("""([\u4e00-\u9fff]{2,6}?)(?:高速公路|高速|快速路)""").find(t) ?: return null
        var nm = m.groupValues[1]
        val stripped = Regex("""^.*(?:省|市|县|区|州|盟|旗)""").replace(nm, "")
        if (stripped.isNotEmpty()) nm = stripped
        return nm
    }

    private fun nameToCode(nm: String, hint: String?): Triple<String, String, String>? {
        names[nm]?.let { return Triple(it, nm, "exact") }
        names.entries.filter { nm.endsWith(it.key) && it.key.length >= 2 }.maxByOrNull { it.key.length }?.let { return Triple(it.value, it.key, "suffix") }
        val cands = names.filter { edit1(it.key, nm) }
        if (hint != null) cands.entries.firstOrNull { it.value == hint }?.let { return Triple(it.value, it.key, "ocrfix") }
        if (cands.size == 1) {
            val e = cands.entries.first()
            return Triple(e.value, e.key, "fuzzy")
        }
        return null
    }

    private fun edit1(a: String, b: String): Boolean {
        if (a.length != b.length) return false
        return a.zip(b).count { it.first != it.second } <= 1
    }

    private data class Found(val km: Double, val metres: Int?, val span: Pair<Int, Int>)

    private fun stakesIn(t: String): List<Found> {
        val found = mutableListOf<Found>()
        fun add(km: Double, metres: Int?, s: Int, e: Int) {
            if (found.any { !(e <= it.span.first || s >= it.span.second) }) return
            found += Found(km, metres, s to e)
        }
        for (m in Regex("""K(\d{1,4})\s*\+\s*(\d{1,4})\s*(?:M|米)?""").findAll(t)) {
            val me = m.groupValues[2].toInt()
            if (me < 1000) add(m.groupValues[1].toDouble(), me, m.range.first, m.range.last + 1)
        }
        for (m in Regex("""(\d{1,4}(?:\.\d+)?)\s*(?:公里|千米|KM)\s*\+?\s*(\d{1,4})?\s*(?:米|M)?""").findAll(t)) {
            if (m.range.first > 0) {
                val prev = t[m.range.first - 1]
                if (prev.isDigit() || prev == 'G' || prev == 'S') continue
            }
            val me = m.groupValues[2].toIntOrNull()
            if (me != null && me >= 1000) continue
            add(m.groupValues[1].toDouble(), me, m.range.first, m.range.last + 1)
        }
        for (m in Regex("""K(\d{1,4})""").findAll(t)) {
            if (m.range.first > 0) {
                val prev = t[m.range.first - 1]
                if (prev in 'A'..'Z' || prev.isDigit()) continue
            }
            val end = m.range.last + 1
            if (end < t.length) {
                val next = t[end]
                if (next.isDigit() || next == '+') continue
            }
            add(m.groupValues[1].toDouble(), null, m.range.first, m.range.last + 1)
        }
        return found.sortedBy { it.span.first }
    }

    private fun glued(t: String): Pair<List<RecogGlued>, Pair<Int, Int>?> {
        val m = Regex("""([GS])(\d{3,8})\s*(?:公里|千米|KM)\s*(?:(\d{1,3})\s*(?:米|M)?)?""").find(t) ?: return emptyList<RecogGlued>() to null
        val letter = m.groupValues[1]
        val digits = m.groupValues[2]
        val metres = m.groupValues[3].toIntOrNull()
        val out = mutableListOf<RecogGlued>()
        for (n in 1..4) {
            if (n >= digits.length) continue
            val kmDigits = digits.substring(n)
            if (kmDigits.length > 1 && kmDigits[0] == '0') continue
            val km = kmDigits.toIntOrNull() ?: continue
            if (km <= 0 || km > 5000) continue
            out += RecogGlued(letter + digits.substring(0, n), km.toDouble(), metres)
        }
        return out to (m.range.first to m.range.last + 1)
    }

    private fun authority(t: String, p: String): Boolean {
        val idx = t.indexOf(p)
        if (idx < 0) return false
        var rest = t.substring(idx + p.length)
        for (prefix in listOf("省", "市", "自治区")) if (rest.startsWith(prefix)) rest = rest.removePrefix(prefix)
        return rest.startsWith("公安") || rest.startsWith("交通") || (rest.startsWith("高速") && !rest.startsWith("高速公路交通安全条例"))
    }

    private fun fold(s: String) = buildString {
        for (r in s) {
            when {
                r in '\uFF01'..'\uFF5E' -> append((r.code - 0xFEE0).toChar())
                r == '\u3000' -> append(' ')
                else -> append(r)
            }
        }
    }

    private fun joinBroken(s: String): String {
        val runes = s.toList()
        val b = StringBuilder()
        var i = 0
        fun left(c: Char) = c.isDigit() || c == 'K' || c == '+' || c == '公' || c == '千' || c == '米'
        while (i < runes.size) {
            val r = runes[i]
            if (left(r)) {
                var j = i + 1
                while (j < runes.size && runes[j] in charArrayOf(' ', '\t', '\n')) j++
                val next = runes.getOrNull(j)
                val ok = next != null && j > i + 1 && (
                    ((r.isDigit() || r == 'K' || r == '+') && (next.isDigit() || next == '+' || next in "公千米KM")) ||
                        ((r == '公' || r == '千' || r == '米' || r == '+') && next.isDigit())
                    )
                if (ok) {
                    b.append(r)
                    i = j
                    continue
                }
            }
            b.append(r)
            i++
        }
        return b.toString()
    }

    private fun collapseHan(s: String): String {
        var cur = s
        val re = Regex("""([\u4e00-\u9fff])[ \t]+([\u4e00-\u9fff])""")
        while (true) {
            val next = re.replace(cur, "$1$2")
            if (next == cur) return cur
            cur = next
        }
    }

    private fun split(s: String): Pair<Double, Double>? {
        val p = s.split(',')
        if (p.size != 2) return null
        val lon = p[0].toDoubleOrNull() ?: return null
        val lat = p[1].toDoubleOrNull() ?: return null
        if (abs(lat) > 90 || abs(lon) > 180) return null
        return lon to lat
    }

    private val httpCache = mutableMapOf<String, JSONObject>()

    private fun get(key: String, path: String, query: Map<String, String>): JSONObject? {
        if (key.isBlank()) return null
        val cacheKey = path + query.toSortedMap().toString()
        httpCache[cacheKey]?.let { return it }
        val q = (query + ("key" to key)).entries.joinToString("&") { "${enc(it.key)}=${enc(it.value)}" }
        val url = "https://restapi.amap.com/$path?$q"
        val text = http(url) ?: return null
        val json = runCatching { JSONObject(text) }.getOrNull() ?: return null
        if (json.optString("status") == "1") httpCache[cacheKey] = json
        return json
    }

    private fun http(url: String): String? = try {
        val c = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 20_000
            readTimeout = 20_000
            setRequestProperty("User-Agent", "XiaoMoto/31")
        }
        val stream = if (c.responseCode in 200..299) c.inputStream else c.errorStream
        stream?.bufferedReader()?.use { it.readText() }
    } catch (_: Exception) {
        null
    }

    private fun postForm(url: String, data: String): String? = try {
        val c = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            connectTimeout = 20_000
            readTimeout = 30_000
            doOutput = true
            setRequestProperty("Content-Type", "application/x-www-form-urlencoded")
            setRequestProperty("User-Agent", "XiaoMoto/31")
        }
        c.outputStream.use { it.write("data=${enc(data)}".toByteArray()) }
        val stream = if (c.responseCode in 200..299) c.inputStream else null
        val body = stream?.bufferedReader()?.use { it.readText() }
        if (body != null && body.trimStart().startsWith("{")) body else null
    } catch (_: Exception) {
        null
    }

    private fun enc(v: String) = URLEncoder.encode(v, "UTF-8")
}
