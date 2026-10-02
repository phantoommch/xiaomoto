package com.example.xiaomoto.core

import java.time.Instant
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.util.UUID
import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.Json

class PointCodecException(val system: String) : IllegalArgumentException(system)

object IsoInstantSerializer : KSerializer<Instant> {
    private val formatter = DateTimeFormatter.ofPattern("yyyy-MM-dd'T'HH:mm:ss'Z'").withZone(ZoneOffset.UTC)
    override val descriptor: SerialDescriptor = PrimitiveSerialDescriptor("Instant", PrimitiveKind.STRING)
    override fun serialize(encoder: Encoder, value: Instant) {
        encoder.encodeString(formatter.format(value))
    }
    override fun deserialize(decoder: Decoder): Instant = Instant.parse(decoder.decodeString())
}

@Serializable
data class PointFile(
    val version: Int = 1,
    val coord: String = "wgs84",
    val points: List<EnforcementPoint> = emptyList(),
)

object PointCodec {
    private val json = Json {
        encodeDefaults = true
        explicitNulls = false
        prettyPrint = true
        ignoreUnknownKeys = true
    }

    fun encodeJSON(points: List<EnforcementPoint>): String {
        return json.encodeToString(PointFile.serializer(), PointFile(points = points))
    }

    fun decodeJSON(text: String): List<EnforcementPoint> {
        val file = json.decodeFromString(PointFile.serializer(), text)
        return applyCoordinateSystem(file.coord, file.points.map { point ->
            if (point.id.isEmpty()) point.copy(id = UUID.randomUUID().toString()) else point
        })
    }

    fun encodeGPX(points: List<EnforcementPoint>): String {
        val lines = mutableListOf(
            "<?xml version=\"1.0\" encoding=\"UTF-8\"?>",
            "<gpx version=\"1.1\" creator=\"motocam\" xmlns=\"http://www.topografix.com/GPX/1/1\" xmlns:motocam=\"urn:motocam:gpx:1\">",
            "  <metadata><name>${xmlEscape(AppIdentity.displayName)}</name></metadata>",
        )
        for (point in points) {
            lines += "  <wpt lat=\"%.7f\" lon=\"%.7f\">".format(point.latitude, point.longitude)
            lines += "    <name>${xmlEscape(point.name)}</name>"
            if (point.notes.isNotEmpty()) lines += "    <desc>${xmlEscape(point.notes)}</desc>"
            lines += "    <time>${IsoInstantSerializer.let { DateTimeFormatter.ofPattern("yyyy-MM-dd'T'HH:mm:ss'Z'").withZone(ZoneOffset.UTC).format(point.createdAt) }}</time>"
            lines += "    <extensions>"
            lines += "      <motocam:id>${xmlEscape(point.id)}</motocam:id>"
            lines += "      <motocam:type>${xmlEscape(point.type.toRaw())}</motocam:type>"
            lines += "      <motocam:source>${xmlEscape(point.source.toRaw())}</motocam:source>"
            lines += "      <motocam:roadCode>${xmlEscape(point.roadCode)}</motocam:roadCode>"
            lines += "      <motocam:roadName>${xmlEscape(point.roadName)}</motocam:roadName>"
            lines += "      <motocam:violation>${xmlEscape(point.violation)}</motocam:violation>"
            point.fineYuan?.let { lines += "      <motocam:fineYuan>$it</motocam:fineYuan>" }
            point.penaltyPoints?.let { lines += "      <motocam:penaltyPoints>$it</motocam:penaltyPoints>" }
            lines += "      <motocam:province>${xmlEscape(point.province)}</motocam:province>"
            lines += "      <motocam:city>${xmlEscape(point.city)}</motocam:city>"
            lines += "      <motocam:coord>wgs84</motocam:coord>"
            if (point.isCommunity) lines += "      <motocam:community>true</motocam:community>"
            lines += "    </extensions>"
            lines += "  </wpt>"
        }
        lines += "</gpx>"
        lines += ""
        return lines.joinToString("\n")
    }

    fun decodeGPX(xml: String): List<EnforcementPoint> {
        val trimmed = xml.trim()
        if (!trimmed.contains("<gpx") && !trimmed.contains("<wpt")) throw PointCodecException("invalid")
        val expression = Regex("<wpt\\b([^>]*)>(.*?)</wpt>", setOf(RegexOption.DOT_MATCHES_ALL, RegexOption.IGNORE_CASE))
        return expression.findAll(trimmed).map { match ->
            val attrs = match.groupValues[1]
            val body = match.groupValues[2]
            val lat = doubleAttr("lat", attrs) ?: throw PointCodecException("invalid")
            val lon = doubleAttr("lon", attrs) ?: throw PointCodecException("invalid")
            val coord = tagText("coord", body) ?: "wgs84"
            val converted = applyCoordinateSystem(coord, listOf(EnforcementPoint(name = "tmp", latitude = lat, longitude = lon)))
            val created = tagText("time", body)?.let { runCatching { Instant.parse(it) }.getOrNull() } ?: Instant.now()
            val type = pointType(tagText("type", body)) ?: PointType.OTHER
            val source = pointSource(tagText("source", body)) ?: PointSource.MANUAL
            EnforcementPoint(
                id = tagText("id", body) ?: UUID.randomUUID().toString(),
                name = tagText("name", body) ?: "未命名",
                type = type,
                roadCode = tagText("roadCode", body) ?: "",
                roadName = tagText("roadName", body) ?: "",
                notes = tagText("desc", body) ?: "",
                latitude = converted[0].latitude,
                longitude = converted[0].longitude,
                fineYuan = tagText("fineYuan", body)?.toIntOrNull(),
                penaltyPoints = tagText("penaltyPoints", body)?.toIntOrNull(),
                violation = tagText("violation", body) ?: "",
                source = source,
                province = tagText("province", body) ?: "",
                city = tagText("city", body) ?: "",
                createdAt = created,
                updatedAt = created,
                isCommunity = tagText("community", body) == "true",
            )
        }.toList()
    }

    fun merging(existing: List<EnforcementPoint>, imported: List<EnforcementPoint>): List<EnforcementPoint> {
        val byId = existing.associateBy { it.id }.toMutableMap()
        val order = existing.map { it.id }.toMutableList()
        for (point in imported) {
            if (byId[point.id] == null) order += point.id
            byId[point.id] = point
        }
        return order.mapNotNull { byId[it] }
    }

    private fun applyCoordinateSystem(coord: String, points: List<EnforcementPoint>): List<EnforcementPoint> {
        return when (coord.trim().lowercase()) {
            "", "wgs84", "wgs-84" -> points
            "gcj02", "gcj-02" -> points.map { point ->
                val wgs = CoordinateConverter.gcj02ToWgs84(CoordinateConverter.Coord(point.latitude, point.longitude))
                point.copy(latitude = wgs.latitude, longitude = wgs.longitude)
            }
            else -> throw PointCodecException(coord)
        }
    }

    private fun xmlEscape(text: String): String {
        return text.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;").replace("\"", "&quot;").replace("'", "&apos;")
    }

    private fun xmlUnescape(text: String): String {
        return text.replace("&lt;", "<").replace("&gt;", ">").replace("&quot;", "\"").replace("&apos;", "'").replace("&amp;", "&")
    }

    private fun doubleAttr(name: String, attrs: String): Double? {
        val match = Regex("\\b$name\\s*=\\s*\"([^\"]+)\"").find(attrs) ?: return null
        return match.groupValues[1].toDoubleOrNull()
    }

    private fun tagText(name: String, body: String): String? {
        val pattern = "<(?:[A-Za-z0-9]+:)?$name>(.*?)</(?:[A-Za-z0-9]+:)?$name>"
        val match = Regex(pattern, RegexOption.DOT_MATCHES_ALL).find(body) ?: return null
        val value = xmlUnescape(match.groupValues[1]).trim()
        return value.ifEmpty { null }
    }
}

fun PointType.toRaw(): String = when (this) {
    PointType.BANNED_TOLL -> "banned_toll"
    PointType.BANNED_SEGMENT -> "banned_segment"
    PointType.CAMERA -> "camera"
    PointType.OTHER -> "other"
}

fun pointType(raw: String?): PointType? = when (raw) {
    "banned_toll" -> PointType.BANNED_TOLL
    "banned_segment" -> PointType.BANNED_SEGMENT
    "camera" -> PointType.CAMERA
    "other" -> PointType.OTHER
    else -> null
}

fun PointSource.toRaw(): String = when (this) {
    PointSource.MANUAL -> "manual"
    PointSource.SEARCH -> "search"
    PointSource.OCR -> "ocr"
    PointSource.COMMUNITY -> "community"
}

fun pointSource(raw: String?): PointSource? = when (raw) {
    "manual" -> PointSource.MANUAL
    "search" -> PointSource.SEARCH
    "ocr" -> PointSource.OCR
    "community" -> PointSource.COMMUNITY
    else -> null
}
