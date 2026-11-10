package com.example.xiaomoto.net

import com.example.xiaomoto.core.AmapRequest
import com.example.xiaomoto.core.CoordinateConverter
import com.example.xiaomoto.core.EnforcementPoint
import com.example.xiaomoto.core.PlaceCandidate
import com.example.xiaomoto.core.PointType
import com.example.xiaomoto.core.PointSource
import com.example.xiaomoto.core.pointType
import com.example.xiaomoto.core.toRaw
import java.io.BufferedReader
import java.io.OutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.time.Instant
import java.util.UUID
import org.json.JSONArray
import org.json.JSONObject

class AmapException(message: String) : Exception(message)

object AmapClient {
    fun search(key: String, keywords: String, city: String?, types: String?, limitToCity: Boolean = false): List<PlaceCandidate> {
        val trimmed = key.trim()
        if (trimmed.isEmpty()) throw AmapException("还没有高德 Web 服务 Key。")
        val url = AmapRequest.placeTextURL(trimmed, keywords, city, types, limitToCity)
        val text = httpGet(url)
        val root = JSONObject(text)
        if (root.optString("status") != "1") {
            throw AmapException(explain(root.optString("info"), root.optString("infocode")))
        }
        val pois = root.optJSONArray("pois") ?: JSONArray()
        return buildList {
            for (i in 0 until pois.length()) {
                candidate(pois.getJSONObject(i))?.let { add(it) }
            }
        }
    }

    fun geocode(key: String, address: String, city: String? = null): List<PlaceCandidate> {
        val trimmed = key.trim()
        if (trimmed.isEmpty() || address.isBlank()) return emptyList()
        val cityQuery = city?.trim().orEmpty().let { if (it.isEmpty()) "" else "&city=${encode(it)}" }
        val url = "https://restapi.amap.com/v3/geocode/geo?key=${encode(trimmed)}&address=${encode(address)}$cityQuery"
        val root = JSONObject(httpGet(url))
        if (root.optString("status") != "1") return emptyList()
        val rows = root.optJSONArray("geocodes") ?: return emptyList()
        return buildList {
            for (i in 0 until rows.length()) {
                geocodeCandidate(rows.getJSONObject(i))?.let { add(it) }
            }
        }.distinctBy { "${it.gcjLatitude},${it.gcjLongitude}" }
    }

    private fun candidate(poi: JSONObject): PlaceCandidate? {
        val location = poi.optString("location")
        val parts = location.split(",")
        if (parts.size != 2) return null
        val longitude = parts[0].toDoubleOrNull() ?: return null
        val latitude = parts[1].toDoubleOrNull() ?: return null
        val name = poi.opt("name").asText().trim()
        if (name.isEmpty()) return null
        return PlaceCandidate(
            id = poi.opt("id").asText().ifBlank { UUID.randomUUID().toString() },
            name = name,
            address = poi.opt("address").asText(),
            province = poi.opt("pname").asText(),
            city = poi.opt("cityname").asText(),
            district = poi.opt("adname").asText(),
            typeCode = poi.opt("typecode").asText(),
            typeName = poi.opt("type").asText(),
            gcjLatitude = latitude,
            gcjLongitude = longitude,
        )
    }

    private fun geocodeCandidate(row: JSONObject): PlaceCandidate? {
        val parts = row.optString("location").split(",")
        if (parts.size != 2) return null
        val longitude = parts[0].toDoubleOrNull() ?: return null
        val latitude = parts[1].toDoubleOrNull() ?: return null
        val name = row.opt("formatted_address").asText().trim()
        if (name.isEmpty()) return null
        return PlaceCandidate(
            id = "geo-$longitude,$latitude",
            name = name,
            address = name,
            province = row.opt("province").asText(),
            city = row.opt("city").asText(),
            district = row.opt("district").asText(),
            typeCode = "190301",
            typeName = row.opt("level").asText().ifBlank { "道路" },
            gcjLatitude = latitude,
            gcjLongitude = longitude,
        )
    }

    private fun encode(value: String): String = java.net.URLEncoder.encode(value, Charsets.UTF_8.name())

    private fun explain(info: String, code: String): String = when (info) {
        "INVALID_USER_KEY" -> "高德 Key 无效。"
        "USERKEY_PLAT_NOMATCH" -> "这个 Key 不是 Web 服务类型。请使用 Web 服务 Key，不要用 Android 或 iOS SDK Key。"
        "INVALID_USER_IP", "INVALID_USER_IP_ACCESS" -> "当前网络 IP 不在 Key 白名单。手机直连时要放行出口 IP，或暂时关闭 IP 限制。"
        "DAILY_QUERY_OVER_LIMIT" -> "高德 Key 今日调用次数已用完。"
        else -> "高德没有返回结果（${info.ifBlank { code.ifBlank { "未知错误" } }}）。"
    }

    private fun httpGet(url: String): String {
        val connection = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 20_000
            readTimeout = 20_000
            setRequestProperty("User-Agent", "XiaoMoto/1.0")
        }
        try {
            val stream = if (connection.responseCode in 200..299) connection.inputStream else connection.errorStream
            val body = stream?.bufferedReader()?.use(BufferedReader::readText).orEmpty()
            if (connection.responseCode !in 200..299) throw AmapException("搜索失败：HTTP ${connection.responseCode}")
            return body
        } catch (error: AmapException) {
            throw error
        } catch (error: Exception) {
            throw AmapException("搜索失败：${error.message ?: "网络错误"}")
        } finally {
            connection.disconnect()
        }
    }
}

data class CommunitySync(val points: List<EnforcementPoint>, val removedIds: List<String>, val serverTime: String)

class CommunityException(message: String) : Exception(message)

object CommunityClient {
    private const val PAGE_LIMIT = 5000

    fun submit(baseUrl: String, deviceId: String, point: EnforcementPoint, imageJpeg: ByteArray?): JSONObject {
        val url = endpoint(baseUrl, "v1/reports")
        val connection = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            connectTimeout = 40_000
            readTimeout = 40_000
            setRequestProperty("User-Agent", "XiaoMoto/1.0")
            setRequestProperty("X-Device-Id", deviceId)
            ApiAuth.apply(this)
            doOutput = true
        }
        try {
            if (imageJpeg == null) {
                connection.setRequestProperty("Content-Type", "application/json; charset=utf-8")
                connection.outputStream.use { it.write(reportJson(point).toString().toByteArray()) }
            } else {
                val boundary = "xiaomoto-${UUID.randomUUID()}"
                connection.setRequestProperty("Content-Type", "multipart/form-data; boundary=$boundary")
                connection.outputStream.use { multipart(it, point, imageJpeg, boundary) }
            }
            return readJson(connection)
        } finally {
            connection.disconnect()
        }
    }

    /**
     * Downloads every public point. No bounding box.
     * Pages at 5000, which is the hard cap of the server build already online.
     * A newer server may return the whole set in one page; a full page is followed
     * with since=last updated_at. Unknown query keys are ignored by the old server.
     */
    fun sync(baseUrl: String): CommunitySync {
        val byID = linkedMapOf<String, EnforcementPoint>()
        val removed = linkedSetOf<String>()
        var since: String? = null
        var serverTime = ""
        repeat(40) {
            val page = fetchPage(baseUrl, since)
            if (page.serverTime.isNotBlank()) serverTime = page.serverTime
            removed += page.removedIds
            var added = 0
            for (point in page.points) {
                if (byID.put(point.id, point) == null) added += 1
            }
            val cursor = page.lastUpdatedAt
            if (page.points.size < PAGE_LIMIT || added == 0 || cursor.isNullOrBlank() || cursor == since) {
                val points = byID.values.filter { it.id !in removed }
                return CommunitySync(points, removed.toList(), serverTime)
            }
            since = cursor
        }
        val points = byID.values.filter { it.id !in removed }
        return CommunitySync(points, removed.toList(), serverTime)
    }

    private data class Page(
        val points: List<EnforcementPoint>,
        val removedIds: List<String>,
        val serverTime: String,
        val lastUpdatedAt: String?,
    )

    private fun fetchPage(baseUrl: String, since: String?): Page {
        val root = endpoint(baseUrl, "v1/points")
        val query = buildString {
            append("limit=").append(PAGE_LIMIT)
            if (!since.isNullOrBlank()) {
                append("&since=").append(java.net.URLEncoder.encode(since, Charsets.UTF_8.name()))
            }
        }
        val connection = (URL("$root?$query").openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 30_000
            readTimeout = 30_000
            setRequestProperty("User-Agent", "XiaoMoto/1.0")
            ApiAuth.apply(this)
        }
        try {
            val json = readJson(connection)
            val points = mutableListOf<EnforcementPoint>()
            val array = json.optJSONArray("points") ?: JSONArray()
            var lastUpdatedAt: String? = null
            for (i in 0 until array.length()) {
                val remote = array.getJSONObject(i)
                points += mapPoint(remote)
                val stamp = remote.optString("updated_at")
                if (stamp.isNotBlank()) lastUpdatedAt = stamp
            }
            val removed = mutableListOf<String>()
            val removedArray = json.optJSONArray("removed_ids") ?: JSONArray()
            for (i in 0 until removedArray.length()) removed += removedArray.getString(i)
            return Page(points, removed, json.optString("server_time"), lastUpdatedAt)
        } finally {
            connection.disconnect()
        }
    }

    private fun mapPoint(remote: JSONObject): EnforcementPoint {
        var latitude = remote.getDouble("latitude")
        var longitude = remote.getDouble("longitude")
        val system = remote.optString("coord", "wgs84").lowercase()
        if (system == "gcj02" || system == "gcj-02") {
            val wgs = CoordinateConverter.gcj02ToWgs84(CoordinateConverter.Coord(latitude, longitude))
            latitude = wgs.latitude
            longitude = wgs.longitude
        }
        val updated = runCatching { Instant.parse(remote.optString("updated_at")) }.getOrElse { Instant.now() }
        return EnforcementPoint(
            id = remote.getString("id"),
            name = remote.optString("name"),
            type = pointType(remote.optString("type")) ?: PointType.OTHER,
            roadCode = remote.optString("road_code"),
            roadName = remote.optString("road_name"),
            notes = remote.optString("notes"),
            latitude = latitude,
            longitude = longitude,
            fineYuan = remote.optIntOrNull("fine_yuan"),
            penaltyPoints = remote.optIntOrNull("penalty_points"),
            violation = remote.optString("violation"),
            source = PointSource.COMMUNITY,
            uploadSource = remote.optString("source"),
            createdAt = updated,
            updatedAt = updated,
            isCommunity = true,
            communitySourceCount = remote.optIntOrNull("source_count"),
            endLatitude = remote.optDoubleOrNull("end_latitude"),
            endLongitude = remote.optDoubleOrNull("end_longitude"),
            stake = remote.optString("stake"),
            direction = remote.optString("direction"),
        )
    }

    private fun reportJson(point: EnforcementPoint): JSONObject {
        val json = JSONObject()
        json.put("name", point.name)
        json.put("type", point.type.toRaw())
        json.put("road_code", point.roadCode)
        json.put("road_name", point.roadName)
        json.put("notes", point.notes)
        json.put("ocr_location", point.ocrLocation)
        json.put("amap_name", point.amapName)
        json.put("ocr_text", point.ocrText)
        json.put("parsed", point.parsed)
        json.put("confidence", point.confidence)
        json.put("stake", point.stake)
        json.put("direction", point.direction)
        point.endLatitude?.let { json.put("end_latitude", it) }
        point.endLongitude?.let { json.put("end_longitude", it) }
        if (point.located) {
            json.put("latitude", point.latitude)
            json.put("longitude", point.longitude)
        } else {
            json.put("located", false)
            json.put("latitude", JSONObject.NULL)
            json.put("longitude", JSONObject.NULL)
        }
        json.put("violation", point.violation)
        point.fineYuan?.let { json.put("fine_yuan", it) }
        point.penaltyPoints?.let { json.put("penalty_points", it) }
        return json
    }

    private fun multipart(out: OutputStream, point: EnforcementPoint, image: ByteArray, boundary: String) {
        fun field(name: String, value: String) {
            out.write("--$boundary\r\nContent-Disposition: form-data; name=\"$name\"\r\n\r\n$value\r\n".toByteArray())
        }
        field("name", point.name)
        field("type", point.type.toRaw())
        field("road_code", point.roadCode)
        field("road_name", point.roadName)
        field("notes", point.notes)
        field("ocr_location", point.ocrLocation)
        field("amap_name", point.amapName)
        field("ocr_text", point.ocrText)
        field("parsed", point.parsed)
        field("confidence", point.confidence)
        field("stake", point.stake)
        field("direction", point.direction)
        point.endLatitude?.let { field("end_latitude", it.toString()) }
        point.endLongitude?.let { field("end_longitude", it.toString()) }
        if (point.located) {
            field("latitude", point.latitude.toString())
            field("longitude", point.longitude.toString())
        } else {
            field("located", "0")
        }
        field("violation", point.violation)
        point.fineYuan?.let { field("fine_yuan", it.toString()) }
        point.penaltyPoints?.let { field("penalty_points", it.toString()) }
        out.write("--$boundary\r\nContent-Disposition: form-data; name=\"image\"; filename=\"report.jpg\"\r\nContent-Type: image/jpeg\r\n\r\n".toByteArray())
        out.write(image)
        out.write("\r\n--$boundary--\r\n".toByteArray())
    }

    private fun endpoint(raw: String, path: String): String {
        var trimmed = raw.trim()
        if (trimmed.isEmpty()) throw CommunityException("还没有填写社区服务器地址。不填也可以离线使用本机点位。")
        while (trimmed.endsWith("/")) trimmed = trimmed.dropLast(1)
        if (!trimmed.startsWith("http://") && !trimmed.startsWith("https://")) {
            throw CommunityException("社区服务器地址无效。")
        }
        return "$trimmed/$path"
    }

    private fun readJson(connection: HttpURLConnection): JSONObject {
        val code = connection.responseCode
        val stream = if (code in 200..299) connection.inputStream else connection.errorStream
        val body = stream?.bufferedReader()?.use(BufferedReader::readText).orEmpty()
        if (code !in 200..299) {
            ApiAuth.noteStatus(code)
            val message = runCatching { JSONObject(body).optString("error") }.getOrNull().orEmpty()
            throw CommunityException(message.ifBlank { "服务器返回 $code" })
        }
        return try {
            JSONObject(body)
        } catch (_: Exception) {
            throw CommunityException("服务器响应无法解析")
        }
    }
}

private fun Any?.asText(): String = when (this) {
    null, JSONObject.NULL -> ""
    is String -> this
    is JSONArray -> ""
    else -> toString()
}

private fun JSONObject.optIntOrNull(name: String): Int? {
    if (!has(name) || isNull(name)) return null
    return optInt(name)
}

private fun JSONObject.optDoubleOrNull(name: String): Double? {
    if (!has(name) || isNull(name)) return null
    val value = optDouble(name)
    return value.takeIf { it.isFinite() }
}
