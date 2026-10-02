package com.example.xiaomoto.core

object SyncNotice {
    const val noneNearby = "附近 15 公里内暂时没有点位，缩小地图可以看到其他地方的点。"
    const val noServer = "还没有服务器地址"

    fun nearbyLine(kilometers: Int): String =
        "附近 $kilometers 公里内暂时没有点位，缩小地图可以看到其他地方的点。"

    fun success(count: Int, nearbyCount: Int?, nearbyKilometers: Int = 15): String {
        val base = "已更新，共 $count 个点位"
        val line = if (nearbyKilometers == 15) noneNearby else nearbyLine(nearbyKilometers)
        return if (nearbyCount == 0) "$base。$line" else base
    }

    fun failure(reason: String): String {
        val text = reason.trim().ifEmpty { "网络不通" }
        return "更新失败：$text"
    }
}
