package com.example.xiaomoto.map

import com.example.xiaomoto.core.CoordinateConverter

/**
 * Amap's 3D map is always GCJ-02. Storage stays WGS-84; convert only when
 * drawing or when reading a camera position back from the map.
 */
object MapDatum {
    fun toMap(latitude: Double, longitude: Double): CoordinateConverter.Coord {
        return CoordinateConverter.wgs84ToGcj02(CoordinateConverter.Coord(latitude, longitude))
    }

    fun fromMap(latitude: Double, longitude: Double): CoordinateConverter.Coord {
        return CoordinateConverter.gcj02ToWgs84(CoordinateConverter.Coord(latitude, longitude))
    }
}
