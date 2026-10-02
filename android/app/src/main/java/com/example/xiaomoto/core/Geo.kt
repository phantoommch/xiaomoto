package com.example.xiaomoto.core

import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.asin
import kotlin.math.atan2
import kotlin.math.cos
import kotlin.math.sin
import kotlin.math.sqrt

/**
 * Persisted coordinates are WGS-84. Amap Web Service and the Amap 3D map
 * are GCJ-02. Convert at the edges; do not store GCJ-02.
 *
 * Outside longitude 72.004–137.8347 and latitude 0.8293–55.8271 the transform
 * is identity. Hong Kong, Macau, and Taiwan sit inside that rectangle and can
 * be shifted.
 */
object CoordinateConverter {
    data class Coord(val latitude: Double, val longitude: Double)

    private const val a = 6378245.0
    private const val ee = 0.00669342162296594323

    fun isInChinaBoundingBox(latitude: Double, longitude: Double): Boolean {
        return longitude >= 72.004 && longitude <= 137.8347 && latitude >= 0.8293 && latitude <= 55.8271
    }

    fun wgs84ToGcj02(coord: Coord): Coord {
        if (!isInChinaBoundingBox(coord.latitude, coord.longitude)) return coord
        var dLat = transformLatitude(coord.longitude - 105.0, coord.latitude - 35.0)
        var dLon = transformLongitude(coord.longitude - 105.0, coord.latitude - 35.0)
        val radLat = coord.latitude / 180.0 * PI
        var magic = sin(radLat)
        magic = 1 - ee * magic * magic
        val sqrtMagic = sqrt(magic)
        dLat = (dLat * 180.0) / ((a * (1 - ee)) / (magic * sqrtMagic) * PI)
        dLon = (dLon * 180.0) / (a / sqrtMagic * cos(radLat) * PI)
        return Coord(coord.latitude + dLat, coord.longitude + dLon)
    }

    fun gcj02ToWgs84(coord: Coord): Coord {
        if (!isInChinaBoundingBox(coord.latitude, coord.longitude)) return coord
        var guess = coord
        repeat(12) {
            val forward = wgs84ToGcj02(guess)
            guess = Coord(
                guess.latitude + (coord.latitude - forward.latitude),
                guess.longitude + (coord.longitude - forward.longitude),
            )
        }
        return guess
    }

    private fun transformLatitude(x: Double, y: Double): Double {
        var ret = -100.0 + 2.0 * x + 3.0 * y + 0.2 * y * y + 0.1 * x * y + 0.2 * sqrt(abs(x))
        ret += (20.0 * sin(6.0 * x * PI) + 20.0 * sin(2.0 * x * PI)) * 2.0 / 3.0
        ret += (20.0 * sin(y * PI) + 40.0 * sin(y / 3.0 * PI)) * 2.0 / 3.0
        ret += (160.0 * sin(y / 12.0 * PI) + 320 * sin(y * PI / 30.0)) * 2.0 / 3.0
        return ret
    }

    private fun transformLongitude(x: Double, y: Double): Double {
        var ret = 300.0 + x + 2.0 * y + 0.1 * x * x + 0.1 * x * y + 0.1 * sqrt(abs(x))
        ret += (20.0 * sin(6.0 * x * PI) + 20.0 * sin(2.0 * x * PI)) * 2.0 / 3.0
        ret += (20.0 * sin(x * PI) + 40.0 * sin(x / 3.0 * PI)) * 2.0 / 3.0
        ret += (150.0 * sin(x / 12.0 * PI) + 300.0 * sin(x / 30.0 * PI)) * 2.0 / 3.0
        return ret
    }
}

object GeoMath {
    const val earthRadiusMeters = 6_371_000.0

    fun distanceMeters(aLat: Double, aLon: Double, bLat: Double, bLon: Double): Double {
        if (!aLat.isFinite() || !aLon.isFinite() || !bLat.isFinite() || !bLon.isFinite()) return Double.POSITIVE_INFINITY
        val lat1 = aLat * PI / 180
        val lat2 = bLat * PI / 180
        val dLat = (bLat - aLat) * PI / 180
        val dLon = (bLon - aLon) * PI / 180
        val h = sin(dLat / 2) * sin(dLat / 2) + cos(lat1) * cos(lat2) * sin(dLon / 2) * sin(dLon / 2)
        return 2 * earthRadiusMeters * asin(minOf(1.0, sqrt(maxOf(0.0, h))))
    }

    fun bearingDegrees(aLat: Double, aLon: Double, bLat: Double, bLon: Double): Double {
        val lat1 = aLat * PI / 180
        val lat2 = bLat * PI / 180
        val dLon = (bLon - aLon) * PI / 180
        val y = sin(dLon) * cos(lat2)
        val x = cos(lat1) * sin(lat2) - sin(lat1) * cos(lat2) * cos(dLon)
        val degrees = atan2(y, x) * 180 / PI
        return (degrees + 360).mod(360.0)
    }

    fun angleDelta(a: Double, b: Double): Double {
        val raw = abs(a - b).mod(360.0)
        return if (raw > 180) 360 - raw else raw
    }

    /** Distance to the segment and the fraction along it, 0 at the start and 1 at the end. */
    fun alongSegment(lat: Double, lon: Double, aLat: Double, aLon: Double, bLat: Double, bLon: Double): Pair<Double, Double> {
        val mean = (aLat + bLat) / 2
        val kx = 111_320.0 * kotlin.math.cos(mean * PI / 180)
        val ky = 111_320.0
        val abx = (bLon - aLon) * kx
        val aby = (bLat - aLat) * ky
        val apx = (lon - aLon) * kx
        val apy = (lat - aLat) * ky
        val ab2 = abx * abx + aby * aby
        if (ab2 < 1) return distanceMeters(lat, lon, aLat, aLon) to 0.0
        val t = ((apx * abx + apy * aby) / ab2).coerceIn(0.0, 1.0)
        val lonC = aLon + (bLon - aLon) * t
        val latC = aLat + (bLat - aLat) * t
        return distanceMeters(lat, lon, latC, lonC) to t
    }

    fun offset(latitude: Double, longitude: Double, northMeters: Double, eastMeters: Double): CoordinateConverter.Coord {
        val dLat = northMeters / 111_320
        val cosLat = cos(latitude * PI / 180)
        val dLon = eastMeters / (111_320 * maxOf(0.2, abs(cosLat)))
        return CoordinateConverter.Coord(latitude + dLat, longitude + dLon)
    }
}

object TextNormalize {
    fun halfwidth(text: String): String {
        val out = StringBuilder()
        text.forEach { ch ->
            val value = ch.code
            when {
                value in 0xFF01..0xFF5E -> out.append((value - 0xFEE0).toChar())
                value == 0x3000 -> out.append(' ')
                else -> out.append(ch)
            }
        }
        return out.toString()
    }

    fun collapseSpaces(text: String): String {
        return text.split(Regex("\\s+")).filter { it.isNotEmpty() }.joinToString(" ")
    }

    fun regionKey(text: String): String {
        var value = collapseSpaces(halfwidth(text)).trim()
        val suffixes = listOf("特别行政区", "自治州", "地区", "省", "市", "区", "县")
        var changed = true
        while (changed) {
            changed = false
            for (suffix in suffixes) {
                if (value.endsWith(suffix) && value.length > suffix.length + 1) {
                    value = value.dropLast(suffix.length)
                    changed = true
                    break
                }
            }
        }
        return value
    }
}
