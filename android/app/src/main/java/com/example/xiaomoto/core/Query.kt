package com.example.xiaomoto.core

import java.net.URLEncoder

data class ParsedQuery(
    val original: String,
    val cleaned: String,
    val keywords: String,
    val roadCode: String?,
    val roadName: String?,
    val wantsToll: Boolean,
)

data class LocatePlan(
    val region: String?,
    val poiKeywords: List<String>,
    val geocodeAddress: String,
)

/** 12123 writes "G15643公里450米" with the trunk code glued to the stake. */
data class GluedStake(
    val roadCode: String,
    val stake: String,
    val kilometre: Int,
)

data class StakeAttempt(
    val address: String,
    val roadCode: String?,
    val kilometre: Int?,
    val query: ParsedQuery,
)

object QueryCleaner {
    val plazaPhrases = listOf(
        "进口内广场", "出口内广场", "进口外广场", "出口外广场",
        "进站口", "出站口",
        "内广场", "外广场", "收费广场",
        "进口", "出口",
        "上行", "下行",
    )

    private val codeExpression = Regex("[GS]\\d{1,4}(?:[A-Z]\\d{1,2})?")
    private val gluedExpression = Regex("(?i)([GS])(\\d{3,8})((?:公里|千米|KM)(?:\\d{1,4}(?:米|M))?)")

    fun parse(raw: String): ParsedQuery {
        val normalized = TextNormalize.collapseSpaces(TextNormalize.halfwidth(raw)).trim()
        val roadCode = gluedStakes(normalized).firstOrNull()?.roadCode ?: roadCodes(normalized).firstOrNull()
        val roadName = firstRoadName(normalized)
        var cleaned = normalized
        for (phrase in plazaPhrases) {
            cleaned = cleaned.replace(phrase, " ")
        }
        cleaned = cleaned.replace(Regex("[()（）]"), " ")
        cleaned = TextNormalize.collapseSpaces(cleaned).trim().trim(',', '，', '。', '.', ';', '；', '、')
        var keywords = cleaned
        if (roadCode != null) keywords = removeOnce(roadCode, keywords)
        if (roadName != null) keywords = removeOnce(roadName, keywords)
        keywords = TextNormalize.collapseSpaces(keywords).trim().trim('-', '_', '，', ',', '。', '.')
        if (roadCode != null && isStakeRemainder(keywords)) {
            val road = listOfNotNull(roadCode, roadName).joinToString("")
            if (road.isNotEmpty()) keywords = road
        }
        tollName(keywords)?.let { keywords = it }
        if (keywords.isEmpty()) keywords = if (cleaned.isEmpty()) normalized else cleaned
        val wantsToll = normalized.contains("收费站") || keywords.contains("收费站")
        return ParsedQuery(raw, cleaned, keywords, roadCode, roadName, wantsToll)
    }

    fun roadCodes(text: String): List<String> {
        val normalized = TextNormalize.halfwidth(text).uppercase()
        return codeExpression.findAll(normalized).map { it.value }.toList()
    }

    private fun firstRoadName(text: String): String? {
        val suffixes = listOf("高速公路", "高速", "快速路")
        val chars = text.toList()
        for (suffix in suffixes) {
            var searchStart = 0
            while (true) {
                val idx = text.indexOf(suffix, searchStart)
                if (idx < 0) break
                var index = idx - 1
                var hanCount = 0
                while (index >= 0 && isHan(chars[index]) && hanCount < 12) {
                    if (hanCount >= 2 && chars[index] in "区县市省州盟旗") break
                    hanCount += 1
                    index -= 1
                }
                if (hanCount >= 2) {
                    return text.substring(idx - hanCount, idx + suffix.length)
                }
                searchStart = idx + suffix.length
            }
        }
        return null
    }

    private fun isHan(character: Char): Boolean = character.code in 0x4E00..0x9FFF

    fun locatePlan(query: ParsedQuery): LocatePlan {
        val source = query.cleaned.ifBlank { query.original }
        val region = adminRegion(source)
        val facility = facilityName(source)
        val glued = gluedStakes(source).firstOrNull()
        val stake = glued?.stake ?: stakeToken(source)
        val primary = when {
            facility != null -> facility
            query.roadName != null && adminOnly(query.keywords) -> query.roadName
            else -> query.keywords
        }
        val keywords = mutableListOf(primary)
        if (query.roadCode != null && stake != null) {
            val extra = if (query.roadName != null) query.roadName + stake else "${query.roadCode} $stake"
            if (extra !in keywords) keywords += extra
        }
        val geocode = glued?.let { "${it.roadCode} ${it.stake}" } ?: source
        return LocatePlan(region, keywords, geocode)
    }

    /**
     * Splits a glued ticket stake. "G15643公里450米" is G15 + 643公里, not road G1564.
     * Trunk codes (2 digits) are tried before a 4-digit code. The caller keeps the
     * candidate whose geocode actually contains that kilometre.
     */
    fun gluedStakes(text: String): List<GluedStake> {
        val compact = text.replace(" ", "")
        val match = gluedExpression.find(compact) ?: return emptyList()
        val letter = match.groupValues[1].uppercase()
        val digits = match.groupValues[2]
        val unit = match.groupValues[3]
        val found = mutableListOf<GluedStake>()
        for (roadLen in listOf(2, 4, 1)) {
            if (roadLen >= digits.length) continue
            val kmText = digits.substring(roadLen)
            if (kmText.length > 1 && kmText.startsWith('0')) continue
            val km = kmText.toIntOrNull() ?: continue
            if (km <= 0 || km > 4_500) continue
            found += GluedStake(letter + digits.substring(0, roadLen), kmText + unit, km)
        }
        return found
    }

    fun stakeAttempts(query: ParsedQuery): List<StakeAttempt> {
        val source = query.cleaned.ifBlank { query.original }
        val glued = gluedStakes(source)
        if (glued.isNotEmpty()) {
            return glued.map {
                StakeAttempt("${it.roadCode} ${it.stake}", it.roadCode, it.kilometre, query.copy(roadCode = it.roadCode))
            }
        }
        return listOf(StakeAttempt(locatePlan(query).geocodeAddress, query.roadCode, stakeKilometre(source), query))
    }

    /** A kilometre stake with no toll or tunnel name. Geocode it; do not pin a random POI. */
    fun bareStake(query: ParsedQuery): Boolean {
        if (query.wantsToll) return false
        val source = query.cleaned.ifBlank { query.original }
        if (facilityName(source) != null) return false
        return needsRoadGeocode(query) && stakeKilometre(source) != null
    }

    fun matchesStake(place: PlaceCandidate, roadCode: String?, kilometre: Int?): Boolean {
        if (kilometre == null) return false
        val blob = "${place.name}${place.address}".uppercase()
        if (!roadCode.isNullOrBlank() && !blob.contains(roadCode.uppercase())) return false
        return blob.contains("${kilometre}公里") || blob.contains("${kilometre}千米") || blob.contains("${kilometre}KM")
    }

    fun roadName(text: String): String? = firstRoadName(text)

    fun adminRegion(text: String): String? {
        val compact = text.replace(" ", "")
        for (name in listOf("北京", "天津", "上海", "重庆")) {
            if (compact.startsWith(name)) return name
        }
        val province = Regex("^(.+?(?:省|自治区))").find(compact) ?: return null
        val rest = compact.substring(province.range.last + 1)
        val city = Regex("^(.+?(?:自治州|地区|盟|州|市))").find(rest)
        return city?.groupValues?.get(1) ?: province.groupValues[1]
    }

    fun facilityName(text: String): String? {
        val compact = text.replace(" ", "")
        val suffixes = listOf("隧道", "收费站", "服务区", "互通枢纽", "互通", "枢纽", "立交桥", "立交", "大桥", "桥")
        for (suffix in suffixes) {
            val index = compact.indexOf(suffix)
            if (index < 0) continue
            var start = index
            var han = 0
            while (start > 0 && isHan(compact[start - 1]) && han < 12) {
                start -= 1
                han += 1
            }
            if (han >= 1) {
                var name = compact.substring(start, index + suffix.length)
                for (marker in listOf("高速公路", "快速路", "高速", "公路", "大道")) {
                    val at = name.indexOf(marker)
                    if (at >= 0) name = name.substring(at + marker.length)
                }
                name = name.replace(Regex("^[GS]\\d{1,4}(?:[A-Z]\\d{1,2})?"), "")
                if (name.length > suffix.length) return name
            }
        }
        return null
    }

    fun stakeToken(text: String): String? {
        gluedStakes(text).firstOrNull()?.let { return it.stake }
        val compact = text.replace(" ", "")
        Regex("(?i)K\\d+(?:\\+\\d+)?").find(compact)?.let { return it.value.uppercase() }
        return Regex("(?i)\\d+(?:\\.\\d+)?(?:公里|千米|KM)(?:\\d+(?:米|M))?").find(compact)?.value?.uppercase()
    }

    fun stakeKilometre(text: String): Int? {
        gluedStakes(text).firstOrNull()?.let { return it.kilometre }
        val stake = stakeToken(text) ?: return null
        return Regex("(\\d+)").find(stake)?.groupValues?.get(1)?.toIntOrNull()
    }

    private fun adminOnly(text: String): Boolean {
        var rest = text.replace(" ", "")
        val unit = Regex("^(?:北京市|天津市|上海市|重庆市|北京|天津|上海|重庆|.+?(?:自治区|自治州|特别行政区|地区|省|盟|州|市|新区|区|县|旗))")
        while (rest.isNotEmpty()) {
            val match = unit.find(rest) ?: break
            if (match.range.first != 0 || match.value.isEmpty()) break
            val next = rest.substring(match.range.last + 1)
            if (next == rest) break
            rest = next
        }
        return rest.isEmpty()
    }

    fun needsRoadGeocode(query: ParsedQuery): Boolean {
        val hasStake = Regex("(?i)公里|千米|KM").containsMatchIn(query.cleaned)
        if (!hasStake) return false
        return query.roadCode != null || query.roadName != null || query.wantsToll
    }

    /** "建瓯收费站" inside a kilometre stake, after the road name is removed. */
    private fun tollName(text: String): String? {
        return Regex("[\\u4e00-\\u9fff]{1,8}收费站").find(text)?.value
    }

    /** "45公里900米（东向西方向）" is a stake, not a place name. */
    private fun isStakeRemainder(text: String): Boolean {
        if (text.isBlank()) return false
        val left = text.replace(Regex("(?i)[0-9]+|公里|千米|KM|米|加|方向|[东南西北]向?|[（）()]|\\s"), "")
        return left.isEmpty()
    }

    private fun removeOnce(token: String, text: String): String {
        val index = text.indexOf(token, ignoreCase = true)
        if (index < 0) return text
        return text.removeRange(index, index + token.length)
    }
}

data class PlaceCandidate(
    val id: String,
    val name: String,
    val address: String,
    val province: String,
    val city: String,
    val district: String,
    val typeCode: String,
    val typeName: String,
    val gcjLatitude: Double,
    val gcjLongitude: Double,
) {
    val wgs84: CoordinateConverter.Coord
        get() = CoordinateConverter.gcj02ToWgs84(CoordinateConverter.Coord(gcjLatitude, gcjLongitude))
}

data class RankedPlace(
    val place: PlaceCandidate,
    val score: Int,
    val reasons: List<String>,
)

object PlaceRanker {
    private val facilityWords = listOf("隧道", "收费站", "服务区", "互通", "枢纽", "立交", "大桥", "桥")

    fun rank(places: List<PlaceCandidate>, query: ParsedQuery, cityHint: String?): List<RankedPlace> {
        val hint = cityHint?.let { TextNormalize.collapseSpaces(it) } ?: ""
        return places.mapIndexed { index, place -> score(place, query, hint, index) }
            .sortedWith(compareByDescending<RankedPlace> { it.score }.thenBy { it.place.id })
    }

    private fun score(place: PlaceCandidate, query: ParsedQuery, cityHint: String, index: Int): RankedPlace {
        var score = 0
        val reasons = mutableListOf<String>()
        val blob = "${place.name} ${place.address} ${place.province} ${place.city} ${place.district}"
        val keywords = query.keywords
        if (keywords.isNotEmpty() && (place.name.contains(keywords) || (keywords.contains(place.name) && place.name.length >= 2))) {
            score += 40
            reasons += "名称包含 $keywords"
        } else if (keywords.isNotEmpty() && place.address.contains(keywords)) {
            score += 15
            reasons += "地址包含 $keywords"
        }
        val core = keywords.replace("收费站", "")
        if (core.length >= 2 && place.name.contains(core) && reasons.none { it.startsWith("名称包含") }) {
            score += 15
            reasons += "名称含 $core"
        }
        val codes = QueryCleaner.roadCodes(blob)
        val wanted = query.roadCode
        if (wanted != null) {
            if (codes.contains(wanted)) {
                score += 100
                reasons += "匹配公路编号 $wanted"
            } else if (codes.isNotEmpty()) {
                score -= 100
                reasons += "结果含其他公路编号 ${codes.joinToString("、")}"
            }
        }
        val roadName = query.roadName
        if (roadName != null && blob.contains(roadName)) {
            score += 60
            reasons += "匹配路名 $roadName"
        }
        val kilometre = QueryCleaner.stakeKilometre(query.cleaned.ifBlank { query.original })
        if (kilometre != null) {
            val compact = blob.replace(" ", "").uppercase()
            val hit = compact.contains("${kilometre}公里") || compact.contains("${kilometre}千米") || compact.contains("${kilometre}KM")
            if (hit) {
                score += 80
                reasons += "桩号 $kilometre 公里"
            } else if (QueryCleaner.bareStake(query)) {
                score -= 80
                reasons += "没有这个桩号"
            }
        }
        if (cityHint.isNotEmpty() && regionMatches(cityHint, place.province, place.city, place.district)) {
            score += 40
            reasons += "城市或省份与提示一致"
        }
        val isToll = place.typeCode.startsWith("1802") || place.name.contains("收费站") || place.typeName.contains("收费站")
        if (query.wantsToll && isToll) {
            score += 25
            reasons += "收费站类型"
        } else if (query.wantsToll && !isToll) {
            score -= 15
            reasons += "不像收费站"
        }
        val facilityHit = facilityWords.firstOrNull { word ->
            word != "收费站" &&
                (query.original.contains(word) || query.keywords.contains(word)) &&
                (place.name.contains(word) || place.typeName.contains(word))
        }
        if (facilityHit != null) {
            score += 50
            reasons += "道路设施 $facilityHit"
        }
        score -= index
        if (reasons.isEmpty()) reasons += "按搜索顺序"
        return RankedPlace(place, score, reasons)
    }

    private fun regionMatches(hint: String, province: String, city: String, district: String): Boolean {
        val key = TextNormalize.regionKey(hint)
        if (key.length < 2) return false
        return listOf(province, city, district).map(TextNormalize::regionKey).any { field ->
            field.isNotEmpty() && (field.contains(key) || key.contains(field))
        }
    }
}

object AmapRequest {
    fun placeTextURL(key: String, keywords: String, city: String?, types: String?, limitToCity: Boolean = false): String {
        val items = mutableListOf(
            "key" to key,
            "keywords" to keywords,
            "offset" to "20",
            "page" to "1",
            "extensions" to "base",
            "output" to "json",
        )
        val trimmedCity = city?.trim().orEmpty()
        if (trimmedCity.isNotEmpty()) {
            items += "city" to trimmedCity
            items += "citylimit" to if (limitToCity) "true" else "false"
        }
        if (!types.isNullOrEmpty()) items += "types" to types
        val query = items.joinToString("&") { (name, value) ->
            "${enc(name)}=${enc(value)}"
        }
        return "https://restapi.amap.com/v3/place/text?$query"
    }

    /**
     * Toll preference is applied when ranking results, not as a request filter.
     * Amap returns count 0 for keywords such as "赤石收费站" when types=180200,
     * and returns the plazas when the type filter is omitted.
     */
    @Suppress("UNUSED_PARAMETER")
    fun types(query: ParsedQuery): String? = null

    private fun enc(value: String): String = URLEncoder.encode(value, Charsets.UTF_8.name())
}
