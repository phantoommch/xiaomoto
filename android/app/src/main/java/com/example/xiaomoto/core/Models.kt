package com.example.xiaomoto.core

import java.time.Instant
import kotlin.math.abs
import kotlin.math.roundToInt
import kotlinx.serialization.Serializable

@Serializable
enum class PointType(val title: String) {
    @kotlinx.serialization.SerialName("banned_toll") BANNED_TOLL("禁摩收费站"),
    @kotlinx.serialization.SerialName("banned_segment") BANNED_SEGMENT("禁摩路段"),
    @kotlinx.serialization.SerialName("camera") CAMERA("抓拍摄像头"),
    @kotlinx.serialization.SerialName("other") OTHER("其他"),
}

@Serializable
enum class PointSource(val title: String) {
    @kotlinx.serialization.SerialName("manual") MANUAL("手动"),
    @kotlinx.serialization.SerialName("search") SEARCH("搜索"),
    @kotlinx.serialization.SerialName("ocr") OCR("截图识别"),
    @kotlinx.serialization.SerialName("community") COMMUNITY("社区"),
}

class AlertSettings(
    var outerMeters: Double = 2_000.0,
    var innerMeters: Double = 500.0,
    var cooldownSeconds: Double = 30.0 * 60,
    var onlyWhenApproaching: Boolean = false,
    var approachAngleDegrees: Double = 60.0,
    var maxAlertsPerUpdate: Int = 2,
) {
    fun normalize() {
        outerMeters = minOf(AlertDistance.OUTER_MAX, maxOf(AlertDistance.OUTER_MIN, outerMeters))
        innerMeters = minOf(outerMeters - 50, maxOf(AlertDistance.INNER_MIN, innerMeters))
        cooldownSeconds = minOf(6 * 3600.0, maxOf(60.0, cooldownSeconds))
        approachAngleDegrees = minOf(180.0, maxOf(10.0, approachAngleDegrees))
        maxAlertsPerUpdate = minOf(5, maxOf(1, maxAlertsPerUpdate))
    }
}

/** Outer-ring choices. Finer near the rider, then 5 km steps out to 80 km. */
object AlertDistance {
    const val OUTER_MIN = 300.0
    const val OUTER_MAX = 80_000.0
    const val INNER_MIN = 50.0
    const val NEARBY_FLOOR_METERS = 15_000.0

    val outerStops: List<Double> = buildList {
        var meters = OUTER_MIN
        while (meters < 1_000) {
            add(meters)
            meters += 100
        }
        while (meters < 5_000) {
            add(meters)
            meters += 500
        }
        while (meters < 20_000) {
            add(meters)
            meters += 1_000
        }
        while (meters <= OUTER_MAX) {
            add(meters)
            meters += 5_000
        }
    }

    fun indexOf(meters: Double): Int {
        var best = 0
        var bestGap = Double.POSITIVE_INFINITY
        for (index in outerStops.indices) {
            val gap = abs(outerStops[index] - meters)
            if (gap < bestGap) {
                best = index
                bestGap = gap
            }
        }
        return best
    }

    /** Map and sync look at least 15 km, and at least as far as the outer ring. */
    fun nearbyRadius(outerMeters: Double): Double =
        maxOf(NEARBY_FLOOR_METERS, outerMeters.coerceIn(OUTER_MIN, OUTER_MAX))

    fun nearbyKilometers(outerMeters: Double): Int =
        (nearbyRadius(outerMeters) / 1_000.0).roundToInt()

    fun format(meters: Double): String {
        if (meters < 1_000) return "${meters.roundToInt()} 米"
        val tenths = (meters / 100.0).roundToInt()
        val whole = tenths / 10
        val fraction = tenths % 10
        return if (fraction == 0) "$whole 公里" else "$whole.$fraction 公里"
    }
}

@Serializable
data class EnforcementPoint(
    var id: String = "",
    var name: String = "",
    var type: PointType = PointType.OTHER,
    @kotlinx.serialization.SerialName("road_code") var roadCode: String = "",
    @kotlinx.serialization.SerialName("road_name") var roadName: String = "",
    var notes: String = "",
    var latitude: Double = 0.0,
    var longitude: Double = 0.0,
    @kotlinx.serialization.SerialName("fine_yuan") var fineYuan: Int? = null,
    @kotlinx.serialization.SerialName("penalty_points") var penaltyPoints: Int? = null,
    var violation: String = "",
    var source: PointSource = PointSource.MANUAL,
    var province: String = "",
    var city: String = "",
    @kotlinx.serialization.SerialName("created_at")
    @kotlinx.serialization.Serializable(with = IsoInstantSerializer::class)
    var createdAt: Instant = Instant.EPOCH,
    @kotlinx.serialization.SerialName("updated_at")
    @kotlinx.serialization.Serializable(with = IsoInstantSerializer::class)
    var updatedAt: Instant = Instant.EPOCH,
    @kotlinx.serialization.SerialName("is_community") var isCommunity: Boolean = false,
    @kotlinx.serialization.SerialName("community_source_count") var communitySourceCount: Int? = null,
    @kotlinx.serialization.SerialName("ocr_location") var ocrLocation: String = "",
    @kotlinx.serialization.SerialName("amap_name") var amapName: String = "",
    var located: Boolean = true,
    @kotlinx.serialization.SerialName("end_latitude") var endLatitude: Double? = null,
    @kotlinx.serialization.SerialName("end_longitude") var endLongitude: Double? = null,
    var stake: String = "",
    var direction: String = "",
    var confidence: String = "",
    @kotlinx.serialization.SerialName("upload_source") var uploadSource: String = "",
    @kotlinx.serialization.SerialName("ocr_text") var ocrText: String = "",
    var parsed: String = "",
) {
    val segment: Boolean get() = endLatitude != null && endLongitude != null
    val gcj02: CoordinateConverter.Coord
        get() = CoordinateConverter.wgs84ToGcj02(CoordinateConverter.Coord(latitude, longitude))
}

object AppIdentity {
    const val displayName = "小moto安全驾驶指南"
}
