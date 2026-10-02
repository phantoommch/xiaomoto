package com.example.xiaomoto.core

import kotlin.math.abs
import kotlin.math.round
import kotlinx.serialization.Serializable

enum class AlertBand { OUTER, INNER }

data class PendingAlert(
    val point: EnforcementPoint,
    val band: AlertBand,
    val distanceMeters: Double,
    val spokenText: String,
    val noticeText: String,
)

/** One pass of a point. Rings arm again only after the rider leaves the outer ring. */
@Serializable
data class ApproachRecord(
    val outerFired: Boolean = false,
    val innerFired: Boolean = false,
    val lastDistanceMeters: Double? = null,
)

object AlertEngine {
    const val REARM_MARGIN_METERS = 300.0
    const val AHEAD_DEGREES = 90.0
    const val MIN_COURSE_SPEED_MPS = 2.0
    private const val DECREASE_METERS = 1.0

    /**
     * Each ring fires once per approach. A point re-arms after the rider is
     * farther than the outer ring plus [REARM_MARGIN_METERS]. Alerts only
     * when the point is ahead: course within 90 degrees, or, when the course
     * cannot be trusted, when distance is decreasing.
     */
    fun evaluate(
        latitude: Double,
        longitude: Double,
        headingDegrees: Double?,
        speedMps: Double?,
        points: List<EnforcementPoint>,
        settings: AlertSettings,
        approach: Map<String, ApproachRecord>,
        mutedIds: Set<String> = emptySet(),
    ): Pair<List<PendingAlert>, Map<String, ApproachRecord>> {
        settings.normalize()
        val alerts = mutableListOf<PendingAlert>()
        val next = approach.toMutableMap()
        val seen = mutableSetOf<String>()
        val rearmAt = settings.outerMeters + REARM_MARGIN_METERS
        val ordered = deduped(points).mapNotNull { point ->
            val distance = GeoMath.distanceMeters(latitude, longitude, point.latitude, point.longitude)
            if (!distance.isFinite()) null else point to distance
        }.sortedBy { it.second }
        for ((point, distance) in ordered) {
            seen += point.id
            if (point.id in mutedIds || !point.located) {
                next.remove(point.id)
                continue
            }
            if (point.segment) {
                val (segDist, frac) = GeoMath.alongSegment(
                    latitude, longitude, point.latitude, point.longitude, point.endLatitude!!, point.endLongitude!!,
                )
                val previous = next[point.id] ?: ApproachRecord()
                if (segDist > rearmAt) {
                    next.remove(point.id)
                    continue
                }
                var record = previous.copy(lastDistanceMeters = segDist)
                if (!record.outerFired && segDist <= settings.outerMeters && frac < 0.25 && alerts.size < settings.maxAlertsPerUpdate) {
                    val line = "前方进入区间测速，${spokenPlace(point)}，请注意平均车速"
                    alerts += PendingAlert(point, AlertBand.OUTER, segDist, "注意，$line。", withCrowd(line, point))
                    record = record.copy(outerFired = true)
                }
                if (!record.innerFired && segDist < 400 && frac in 0.05..0.95 && alerts.size < settings.maxAlertsPerUpdate) {
                    val line = "已进入区间测速，请控制平均车速"
                    alerts += PendingAlert(point, AlertBand.INNER, segDist, "注意，$line。", withCrowd(line, point))
                    record = record.copy(innerFired = true, outerFired = true)
                }
                next[point.id] = record
                continue
            }
            val previous = next[point.id] ?: ApproachRecord()
            if (distance > rearmAt) {
                next.remove(point.id)
                continue
            }
            if (distance > settings.outerMeters) {
                next[point.id] = previous.copy(lastDistanceMeters = distance)
                continue
            }
            val band = if (distance <= settings.innerMeters) AlertBand.INNER else AlertBand.OUTER
            val already = if (band == AlertBand.INNER) previous.innerFired else previous.outerFired
            var record = previous.copy(lastDistanceMeters = distance)
            val ahead = isAhead(latitude, longitude, point, headingDegrees, speedMps, distance, previous.lastDistanceMeters)
            if (!already && ahead && alerts.size < settings.maxAlertsPerUpdate) {
                alerts += PendingAlert(point, band, distance, speech(point, distance), notice(point, distance))
                record = if (band == AlertBand.INNER) {
                    record.copy(innerFired = true, outerFired = true)
                } else {
                    record.copy(outerFired = true)
                }
            }
            next[point.id] = record
        }
        next.keys.retainAll(seen)
        return alerts to next
    }

    fun stampKey(pointId: String, band: AlertBand): String {
        val raw = if (band == AlertBand.OUTER) "outer" else "inner"
        return "$pointId|$raw"
    }

    fun alertTitle(point: EnforcementPoint): String {
        val name = point.name.trim()
        val head = listOf(point.roadCode, point.roadName).filter { it.isNotBlank() && !name.contains(it) }
        val parts = head + listOf(name).filter { it.isNotBlank() }
        val title = if (parts.isEmpty()) "点位" else parts.joinToString(" ")
        return if (point.uploadSource == "user_upload") "$title 用户上传" else title
    }

    fun withCrowd(line: String, point: EnforcementPoint): String {
        if (point.uploadSource != "user_upload" || line.contains("用户上传")) return line
        return "$line，用户上传"
    }

    /** Short line for a collapsed heads-up, so the two actions stay on screen. */
    fun shortNotice(distanceMeters: Double): String = "前方${spokenDistance(distanceMeters)}"

    /**
     * The rider has gone past the point that raised the current alert.
     * With a trusted course, "past" means more than 90° behind. Otherwise the
     * distance has grown past the closest point of this approach.
     */
    fun hasPassed(
        latitude: Double,
        longitude: Double,
        headingDegrees: Double?,
        speedMps: Double?,
        point: EnforcementPoint,
        closestMeters: Double,
        currentMeters: Double,
    ): Boolean {
        if (courseTrusted(headingDegrees, speedMps)) {
            val bearing = GeoMath.bearingDegrees(latitude, longitude, point.latitude, point.longitude)
            return GeoMath.angleDelta(headingDegrees!!, bearing) > AHEAD_DEGREES
        }
        return currentMeters > closestMeters + 60.0
    }

    fun deduped(points: List<EnforcementPoint>): List<EnforcementPoint> {
        val kept = mutableListOf<EnforcementPoint>()
        for (point in points) {
            val index = kept.indexOfFirst {
                !it.segment && !point.segment &&
                    GeoMath.distanceMeters(it.latitude, it.longitude, point.latitude, point.longitude) <= 80
            }
            if (index >= 0) kept[index] = preferred(kept[index], point) else kept += point
        }
        return kept
    }

    fun speech(point: EnforcementPoint, distanceMeters: Double): String {
        return sentence(spokenDistance(distanceMeters), spokenPlace(point), point)
    }

    /** Notification and on-screen card. Road numbers stay digits, like G15. */
    fun notice(point: EnforcementPoint, distanceMeters: Double): String {
        val text = sentence(spokenDistance(distanceMeters), displayPlace(point), point)
        if (point.uploadSource != "user_upload") return text
        return text.removeSuffix("。") + "，用户上传。"
    }

    private fun sentence(distance: String, place: String, point: EnforcementPoint): String {
        val kind = when (point.type) {
            PointType.BANNED_TOLL -> "禁止摩托车进入"
            PointType.BANNED_SEGMENT -> "是禁摩路段"
            PointType.CAMERA -> "有抓拍摄像头"
            PointType.OTHER -> "请注意"
        }
        return "注意，前方$distance，$place，$kind。"
    }

    private fun displayPlace(point: EnforcementPoint): String {
        val name = point.name.trim()
        val code = point.roadCode.trim()
        val roadName = point.roadName.trim()
        val covered = name.isNotEmpty() &&
            (code.isEmpty() || name.contains(code)) &&
            (roadName.isEmpty() || name.contains(roadName))
        if (covered) return name
        val road = listOf(code, roadName).filter { it.isNotEmpty() }.joinToString(" ")
        if (road.isEmpty()) return name
        if (name.isEmpty()) return road
        return "$road，$name"
    }

    private fun spokenPlace(point: EnforcementPoint): String {
        val road = spokenRoad(point)
        return if (road.isEmpty()) point.name else "$road，${point.name}"
    }

    fun spokenRoadCode(code: String): String {
        val digits = mapOf(
            '0' to "零", '1' to "一", '2' to "二", '3' to "三", '4' to "四",
            '5' to "五", '6' to "六", '7' to "七", '8' to "八", '9' to "九",
        )
        return buildString {
            for (character in code) append(digits[character] ?: character.toString())
        }
    }

    private fun isAhead(
        latitude: Double,
        longitude: Double,
        point: EnforcementPoint,
        heading: Double?,
        speedMps: Double?,
        distance: Double,
        lastDistance: Double?,
    ): Boolean {
        if (courseTrusted(heading, speedMps)) {
            val bearing = GeoMath.bearingDegrees(latitude, longitude, point.latitude, point.longitude)
            return GeoMath.angleDelta(heading!!, bearing) <= AHEAD_DEGREES
        }
        val last = lastDistance ?: return false
        return distance < last - DECREASE_METERS
    }

    private fun courseTrusted(heading: Double?, speedMps: Double?): Boolean {
        if (heading == null || !heading.isFinite() || heading < 0.0) return false
        if (speedMps != null && speedMps < MIN_COURSE_SPEED_MPS) return false
        return true
    }

    private fun preferred(a: EnforcementPoint, b: EnforcementPoint): EnforcementPoint {
        if (a.isCommunity != b.isCommunity) return if (a.isCommunity) b else a
        return if ((a.communitySourceCount ?: 0) >= (b.communitySourceCount ?: 0)) a else b
    }

    private fun spokenDistance(meters: Double): String {
        if (meters >= 1000) {
            val km = meters / 1000
            if (abs(km - round(km)) < 0.15) return "约${round(km).toInt()}公里"
            return "约${"%.1f".format(java.util.Locale.US, km)}公里"
        }
        val rounded = (round(meters / 50.0) * 50).toInt()
        return "约${maxOf(50, rounded)}米"
    }

    private fun spokenRoad(point: EnforcementPoint): String {
        val parts = mutableListOf<String>()
        if (point.roadCode.isNotEmpty()) parts += spokenRoadCode(point.roadCode)
        if (point.roadName.isNotEmpty()) parts += point.roadName
        return parts.joinToString("")
    }
}
