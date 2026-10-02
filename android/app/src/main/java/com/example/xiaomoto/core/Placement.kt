package com.example.xiaomoto.core

import java.time.Instant
import java.util.UUID

data class PendingPlace(
    val location: String,
    val violation: String,
    val fineYuan: Int?,
    val penaltyPoints: Int?,
)

object PlaceChoice {
    /** Index penalty is at most 19. A smaller gap is search order, not a real distinction. */
    private const val orderGap = 19

    fun uniqueBest(ranked: List<RankedPlace>): RankedPlace? {
        val top = ranked.firstOrNull() ?: return null
        val name = top.place.name.trim()
        val ambiguous = ranked.drop(1).any { other ->
            other.place.name.trim() == name &&
                other.score >= top.score - orderGap &&
                !sameSpot(top.place, other.place)
        }
        return if (ambiguous) null else top
    }

    private fun sameSpot(a: PlaceCandidate, b: PlaceCandidate): Boolean {
        val left = a.wgs84
        val right = b.wgs84
        return GeoMath.distanceMeters(left.latitude, left.longitude, right.latitude, right.longitude) < 300.0
    }
}

fun roadLabel(point: EnforcementPoint): String {
    val road = listOf(point.roadCode, point.roadName).filter { it.isNotBlank() }.joinToString(" ")
    return road.ifBlank { "未识别到道路" }
}

fun penaltyLabel(point: EnforcementPoint): String {
    val fine = point.fineYuan?.let { "罚款 $it 元" } ?: "未识别到罚款"
    val points = point.penaltyPoints?.let { "记 $it 分" } ?: "未识别到记分"
    return "$fine · $points"
}

object PlacementStatus {
    const val notFound = "高德没找到这个地点，已保存待后台核对"

    fun reviewPrompt(marked: Int): String = "已标注 $marked 个违法点，请核对后上传"

    fun sentence(marked: Int, uploaded: Int): String {
        if (marked <= 0) return ""
        return when {
            uploaded >= marked -> "已标注并上传 $marked 个违法点"
            uploaded <= 0 -> "已标注 $marked 个违法点，有网后会上传"
            else -> "已标注 $marked 个违法点，已上传 $uploaded 个"
        }
    }

    fun amapMatch(name: String): String = "高德匹配：$name"
}

object TicketPoint {
    fun fromPlace(
        ranked: RankedPlace,
        query: ParsedQuery,
        violation: String,
        fineYuan: Int?,
        penaltyPoints: Int?,
    ): EnforcementPoint {
        val place = ranked.place
        val codes = QueryCleaner.roadCodes("${place.name} ${place.address} ${place.province}")
        val placeText = "${place.name}${place.address}"
        val now = Instant.now()
        return EnforcementPoint(
            id = UUID.randomUUID().toString(),
            name = place.name,
            type = if (query.wantsToll || place.typeCode.startsWith("1802")) PointType.BANNED_TOLL else PointType.OTHER,
            roadCode = query.roadCode?.takeIf { codes.contains(it) } ?: codes.firstOrNull().orEmpty(),
            roadName = query.roadName?.takeIf { placeText.contains(it) } ?: QueryCleaner.roadName(placeText).orEmpty(),
            latitude = place.wgs84.latitude,
            longitude = place.wgs84.longitude,
            fineYuan = fineYuan,
            penaltyPoints = penaltyPoints,
            violation = violation,
            source = PointSource.OCR,
            province = place.province,
            city = place.city,
            createdAt = now,
            updatedAt = now,
            ocrLocation = query.original,
            amapName = place.name,
            located = true,
        )
    }

    fun fromLocated(hit: LocatedPoint, record: TicketRecord): EnforcementPoint {
        val now = Instant.now()
        val label = listOf(hit.roadCode, hit.stake.ifBlank { hit.how }).filter { it.isNotBlank() }.joinToString(" ")
        val parsed = org.json.JSONObject()
            .put("code", hit.roadCode)
            .put("stake", hit.stake)
            .put("method", hit.method)
            .put("how", hit.how)
            .put("direction", hit.direction)
            .toString()
        return EnforcementPoint(
            id = UUID.randomUUID().toString(),
            name = label.ifBlank { hit.how }.take(80),
            type = PointType.OTHER,
            roadCode = hit.roadCode,
            roadName = hit.roadName,
            latitude = hit.latitude,
            longitude = hit.longitude,
            fineYuan = record.fineYuan,
            penaltyPoints = record.penaltyPoints,
            violation = record.violation.orEmpty(),
            source = PointSource.OCR,
            createdAt = now,
            updatedAt = now,
            ocrLocation = record.location.orEmpty().take(300),
            amapName = hit.address.ifBlank { hit.how }.take(80),
            located = hit.latitude.isFinite() && hit.longitude.isFinite(),
            endLatitude = hit.endLatitude,
            endLongitude = hit.endLongitude,
            stake = hit.stake,
            direction = hit.direction,
            confidence = hit.confidence,
            ocrText = record.fullText.ifBlank { record.location.orEmpty() },
            parsed = parsed,
        )
    }

    fun unmatched(
        location: String,
        query: ParsedQuery,
        violation: String,
        fineYuan: Int?,
        penaltyPoints: Int?,
    ): EnforcementPoint {
        val now = Instant.now()
        val label = location.ifBlank { query.keywords }.take(80)
        return EnforcementPoint(
            id = UUID.randomUUID().toString(),
            name = label.ifBlank { "未定位违法点" },
            type = if (query.wantsToll) PointType.BANNED_TOLL else PointType.OTHER,
            roadCode = query.roadCode.orEmpty(),
            roadName = query.roadName.orEmpty(),
            latitude = 0.0,
            longitude = 0.0,
            fineYuan = fineYuan,
            penaltyPoints = penaltyPoints,
            violation = violation,
            source = PointSource.OCR,
            createdAt = now,
            updatedAt = now,
            ocrLocation = location,
            amapName = "",
            located = false,
        )
    }

    fun fromTap(
        latitude: Double,
        longitude: Double,
        location: String,
        query: ParsedQuery,
        violation: String,
        fineYuan: Int?,
        penaltyPoints: Int?,
    ): EnforcementPoint {
        val now = Instant.now()
        return EnforcementPoint(
            id = UUID.randomUUID().toString(),
            name = location.ifBlank { query.keywords },
            type = if (query.wantsToll) PointType.BANNED_TOLL else PointType.OTHER,
            roadCode = query.roadCode.orEmpty(),
            roadName = query.roadName.orEmpty(),
            latitude = latitude,
            longitude = longitude,
            fineYuan = fineYuan,
            penaltyPoints = penaltyPoints,
            violation = violation,
            source = PointSource.OCR,
            createdAt = now,
            updatedAt = now,
        )
    }
}
