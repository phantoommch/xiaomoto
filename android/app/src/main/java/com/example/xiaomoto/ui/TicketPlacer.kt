package com.example.xiaomoto.ui

import com.example.xiaomoto.MotoApplication
import com.example.xiaomoto.core.PlacementStatus
import com.example.xiaomoto.core.QueryCleaner
import com.example.xiaomoto.core.Recog
import com.example.xiaomoto.core.TicketPoint
import com.example.xiaomoto.core.TicketRecord

data class PlaceBatch(
    val placed: List<com.example.xiaomoto.core.EnforcementPoint>,
    val notice: String,
)

object TicketPlacer {
    fun place(app: MotoApplication, records: List<TicketRecord>, imageJpeg: ByteArray?): PlaceBatch {
        val key = app.settings.effectiveAmapKey()
        if (key.isBlank()) {
            return PlaceBatch(emptyList(), "还没有高德 Web 服务 Key。")
        }
        val placed = mutableListOf<com.example.xiaomoto.core.EnforcementPoint>()
        var missed = 0
        var searchError: String? = null
        for (record in records) {
            val location = record.location?.trim().orEmpty()
            if (location.isEmpty()) continue
            val hit = try {
                Recog.locate(location, record.fullText.ifBlank { location }, key)
            } catch (error: Exception) {
                val message = error.message.orEmpty()
                if (message.contains("Key") || message.contains("白名单") || message.contains("次数")) {
                    return PlaceBatch(placed, message)
                }
                searchError = message.ifBlank { "搜索失败" }
                continue
            }
            if (hit == null || !hit.latitude.isFinite()) {
                val query = QueryCleaner.parse(location)
                val point = TicketPoint.unmatched(location, query, record.violation.orEmpty(), record.fineYuan, record.penaltyPoints)
                    .copy(ocrText = record.fullText, confidence = "none")
                app.uploads.submitOrQueue(point, imageJpeg)
                missed += 1
                continue
            }
            val point = TicketPoint.fromLocated(hit, record)
            if (hit.exact) app.points.upsert(point)
            placed += point
        }
        val notice = when {
            placed.isNotEmpty() && missed > 0 -> PlacementStatus.reviewPrompt(placed.size) + "。" + PlacementStatus.notFound
            missed > 0 -> PlacementStatus.notFound
            searchError != null -> searchError
            placed.isEmpty() -> "没有可标注的地点"
            else -> PlacementStatus.reviewPrompt(placed.size)
        }
        return PlaceBatch(placed, notice)
    }
}
