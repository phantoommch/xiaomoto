package com.example.xiaomoto.core

import java.time.Instant
import java.time.ZoneId
import java.time.ZonedDateTime

data class TicketRecord(
    val location: String?,
    val happenedAt: Instant?,
    val violation: String?,
    val fineYuan: Int?,
    val penaltyPoints: Int?,
    val fullText: String = "",
) {
    fun penaltyLabel(): String {
        val fine = fineYuan?.let { "罚款 $it 元" } ?: "未识别到罚款"
        val points = penaltyPoints?.let { "记 $it 分" } ?: "未识别到记分"
        return "$fine · $points"
    }
}

enum class TicketStyle {
    /** 未处理列表：没有「违法地点」字段，桩号以 G 或 S 开头，一张图可以有多条。 */
    LIST,
    /** 违法详情：有「违法地点」字段，上面是抓拍照片，一张图只有一条。 */
    DETAIL,
}

data class TicketFields(
    val location: String?,
    val happenedAt: Instant?,
    val violation: String?,
    val fineYuan: Int?,
    val penaltyPoints: Int?,
    val rawText: String,
    val records: List<TicketRecord> = emptyList(),
    val style: TicketStyle? = null,
)

object TicketParser {
    private val labels = listOf(
        "违法地点", "违法时间", "违法行为", "违法车辆", "号牌号码",
        "罚款金额", "记分", "罚款",
    )
    private val valueLabels = setOf("违法地点", "违法时间", "违法行为")

    fun parse(text: String): TicketFields {
        val normalized = collapseHanSpaces(TextNormalize.halfwidth(text)).replace("\r\n", "\n")
        val lines = normalized.split('\n').map { it.trim() }.filter { it.isNotEmpty() }
        val values = mutableMapOf<String, String>()
        var index = 0
        while (index < lines.size) {
            val line = lines[index]
            val label = matchingLabel(line)
            if (label != null) {
                val inline = inlineValue(label, line)
                if (inline.isNotEmpty()) {
                    values[label] = inline
                } else if (label in valueLabels && index + 1 < lines.size && matchingLabel(lines[index + 1]) == null) {
                    values[label] = lines[index + 1]
                    index += 1
                }
            }
            index += 1
        }
        val flat = lines.joinToString(" ")
        val labeledLocation = cleaned(values["违法地点"])
        // 两种版式互斥。详情页一旦读到「违法地点」，就只保留这一条，不再按列表去拆卡片。
        val style: TicketStyle?
        val records = if (labeledLocation != null) {
            style = TicketStyle.DETAIL
            listOf(
                TicketRecord(
                    location = labeledLocation,
                    happenedAt = values["违法时间"]?.let(::parseDate),
                    violation = cleaned(values["违法行为"]),
                    fineYuan = parseFine(flat),
                    penaltyPoints = parsePoints(flat),
                    fullText = normalized.trim(),
                ),
            )
        } else {
            val blocks = roadBlocks(lines).filter { it.fineYuan != null || it.penaltyPoints != null }
            val sms = if (blocks.isEmpty()) smsRecord(normalized, flat) else null
            style = when {
                blocks.isNotEmpty() -> TicketStyle.LIST
                sms != null -> TicketStyle.DETAIL
                else -> null
            }
            if (sms != null && blocks.isEmpty()) listOf(sms) else blocks
        }
        val first = records.firstOrNull()
        return TicketFields(
            location = first?.location,
            happenedAt = first?.happenedAt,
            violation = first?.violation,
            fineYuan = first?.fineYuan,
            penaltyPoints = first?.penaltyPoints,
            rawText = normalized.trim(),
            records = records,
            style = style,
        )
    }

    /**
     * 交管未处理列表没有「违法地点」标签。每一条是违法行为、公路桩号、时间和罚款记分。
     * 列表里有几条就返回几条，不再只留禁令那一条。
     */
    private fun roadBlocks(lines: List<String>): List<TicketRecord> {
        val cards = mutableListOf<MutableList<String>>()
        for (line in lines) {
            if (cards.isEmpty() || isViolationTitle(line)) cards.add(mutableListOf(line)) else cards.last().add(line)
        }
        return cards.flatMap { card ->
            val locations = card.mapNotNull { placeLine(it) }.distinct()
            if (locations.isEmpty()) return@flatMap emptyList()
            val text = card.joinToString("\n")
            locations.map { location ->
                TicketRecord(
                    location = location,
                    happenedAt = card.firstNotNullOfOrNull { parseDate(it) },
                    violation = card.firstOrNull { isViolationTitle(it) },
                    fineYuan = parseFine(text),
                    penaltyPoints = parsePoints(text),
                    fullText = lines.joinToString("\n"),
                )
            }
        }
    }

    /** 短信截图没有「违法地点」，数字还可能被换行拆开。交警或 12123 才认，避免把随手写的桩号当罚单。 */
    private fun smsRecord(normalized: String, flat: String): TicketRecord? {
        if (!normalized.contains("交警") && !normalized.contains("12123")) return null
        val parsed = Recog.parse(normalized)
        if (parsed.stakes.isEmpty() && parsed.glued.isEmpty() && parsed.facility == null) return null
        return TicketRecord(
            location = parsed.norm,
            happenedAt = parseDate(flat),
            violation = null,
            fineYuan = parseFine(flat),
            penaltyPoints = parsePoints(flat),
            fullText = normalized.trim(),
        )
    }

    private fun placeLine(line: String): String? {
        stakeLine(line)?.let { return it }
        val trimmed = line.trim()
        if (trimmed.length !in 4..40) return null
        if (!Regex("隧道|收费站|服务区|停车区|大桥|枢纽|互通").containsMatchIn(trimmed)) return null
        if (matchingLabel(trimmed) != null) return null
        return trimmed
    }

    private fun stakeLine(line: String): String? {
        val trimmed = line.trim()
        if (!Regex("(?i)^[GS]\\d{1,4}").containsMatchIn(trimmed)) return null
        if (!Regex("(?i)高速|公里|千米|KM").containsMatchIn(trimmed)) return null
        return trimmed
    }

    private fun isViolationTitle(line: String): Boolean {
        if (matchingLabel(line) != null) return false
        return line.contains("禁令") || line.contains("驾驶") || line.contains("超速")
    }

    fun chinaZone(): ZoneId = ZoneId.of("Asia/Shanghai")

    fun formatWhen(instant: Instant): String {
        return java.time.format.DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm").withZone(chinaZone()).format(instant)
    }

    /** OCR often inserts a space between every Chinese character. */
    private fun collapseHanSpaces(text: String): String {
        return text.replace(Regex("(?<=[\\u4e00-\\u9fff])[ \\t]+(?=[\\u4e00-\\u9fff])"), "")
    }

    private fun matchingLabel(line: String): String? {
        val trimmed = line.trim()
        for (label in labels.sortedByDescending { it.length }) {
            if (trimmed == label || trimmed.startsWith(label)) {
                val rest = trimmed.drop(label.length)
                if (rest.isEmpty() || rest.startsWith(":") || rest.startsWith(" ") || rest.startsWith("\t")) {
                    return label
                }
                val first = rest.firstOrNull()
                if ((label == "罚款" || label == "记分" || label == "罚款金额") && first != null && first.isDigit()) {
                    return label
                }
            }
        }
        return null
    }

    private fun inlineValue(label: String, line: String): String {
        val range = line.indexOf(label)
        if (range < 0) return ""
        var rest = line.substring(range + label.length)
        while (rest.isNotEmpty() && (rest[0] == ':' || rest[0].isWhitespace())) {
            rest = rest.drop(1)
        }
        return rest.trim()
    }

    private fun cleaned(value: String?): String? {
        val trimmed = value?.trim().orEmpty()
        return trimmed.ifEmpty { null }
    }

    private fun parseFine(text: String): Int? {
        return firstInt("罚款金额\\s*:?\\s*(\\d+)", text)
            ?: firstInt("罚款\\s*:?\\s*(\\d+)\\s*元", text)
    }

    private fun parsePoints(text: String): Int? {
        return firstInt("记分\\s*:?\\s*(\\d+)", text)
            ?: firstInt("记\\s*(\\d+)\\s*分", text)
            ?: firstInt("扣\\s*(\\d+)\\s*分", text)
    }

    private fun parseDate(text: String): Instant? {
        val match = Regex("(\\d{4})[-/.年](\\d{1,2})[-/.月](\\d{1,2})日?\\s*(\\d{1,2}):(\\d{2})(?::(\\d{2}))?").find(text)
            ?: return null
        val year = match.groupValues[1].toInt()
        val month = match.groupValues[2].toInt()
        val day = match.groupValues[3].toInt()
        val hour = match.groupValues[4].toInt()
        val minute = match.groupValues[5].toInt()
        val second = match.groupValues[6].toIntOrNull() ?: 0
        return ZonedDateTime.of(year, month, day, hour, minute, second, 0, chinaZone()).toInstant()
    }

    private fun firstInt(pattern: String, text: String): Int? {
        return Regex(pattern).find(text)?.groupValues?.getOrNull(1)?.toIntOrNull()
    }
}
