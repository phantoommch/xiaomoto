package com.example.xiaomoto.ui

import android.graphics.RectF
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.example.xiaomoto.MotoApplication
import com.example.xiaomoto.core.AmapRequest
import com.example.xiaomoto.core.EnforcementPoint
import com.example.xiaomoto.core.PlaceCandidate
import com.example.xiaomoto.core.PlaceRanker
import com.example.xiaomoto.core.PointSource
import com.example.xiaomoto.core.PointType
import com.example.xiaomoto.core.QueryCleaner
import com.example.xiaomoto.core.RankedPlace
import com.example.xiaomoto.core.TicketParser
import com.example.xiaomoto.core.TicketRecord
import com.example.xiaomoto.net.AmapClient
import com.example.xiaomoto.ocr.ChineseOcr
import java.time.Instant
import java.util.UUID
import kotlin.math.max
import kotlin.math.min

@Composable
fun PointEditorDialog(initial: EnforcementPoint, onDismiss: () -> Unit, onSave: (EnforcementPoint) -> Unit) {
    var name by remember { mutableStateOf(initial.name) }
    var type by remember { mutableStateOf(initial.type) }
    var roadCode by remember { mutableStateOf(initial.roadCode) }
    var roadName by remember { mutableStateOf(initial.roadName) }
    var notes by remember { mutableStateOf(initial.notes) }
    var fine by remember { mutableStateOf(initial.fineYuan?.toString().orEmpty()) }
    var points by remember { mutableStateOf(initial.penaltyPoints?.toString().orEmpty()) }
    var violation by remember { mutableStateOf(initial.violation) }
    var menu by remember { mutableStateOf(false) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (initial.name.isBlank() && initial.id.isBlank()) "添加点位" else "编辑点位") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                OutlinedTextField(name, { name = it }, label = { Text("名称") }, modifier = Modifier.fillMaxWidth())
                TextButton(onClick = { menu = true }) { Text(type.title) }
                DropdownMenu(menu, { menu = false }) {
                    PointType.entries.forEach { item ->
                        DropdownMenuItem(text = { Text(item.title) }, onClick = { type = item; menu = false })
                    }
                }
                OutlinedTextField(roadCode, { roadCode = it }, label = { Text("公路编号，如 G25") }, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(roadName, { roadName = it }, label = { Text("路名") }, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(notes, { notes = it }, label = { Text("备注") }, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(violation, { violation = it }, label = { Text("违法行为") }, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(fine, { fine = it }, label = { Text("罚款（元）") }, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(points, { points = it }, label = { Text("记分") }, modifier = Modifier.fillMaxWidth())
                Text("%.5f, %.5f".format(initial.latitude, initial.longitude))
            }
        },
        confirmButton = {
            TextButton(onClick = {
                if (name.isBlank()) return@TextButton
                val now = Instant.now()
                onSave(
                    initial.copy(
                        id = initial.id.ifBlank { UUID.randomUUID().toString() },
                        name = name.trim(),
                        type = type,
                        roadCode = roadCode.trim(),
                        roadName = roadName.trim(),
                        notes = notes.trim(),
                        violation = violation.trim(),
                        fineYuan = fine.toIntOrNull(),
                        penaltyPoints = points.toIntOrNull(),
                        source = if (initial.id.isBlank()) PointSource.MANUAL else initial.source,
                        createdAt = if (initial.createdAt == Instant.EPOCH) now else initial.createdAt,
                        updatedAt = now,
                    ),
                )
            }) { Text("保存") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

@Composable
fun DetailDialog(
    point: EnforcementPoint,
    onDismiss: () -> Unit,
    onEdit: () -> Unit,
    onDelete: () -> Unit,
    onUpload: () -> Unit,
    onSaveLocal: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(point.name) },
        text = {
            Text(
                listOf(
                    point.type.title,
                    listOf(point.roadCode, point.roadName).filter { it.isNotBlank() }.joinToString(" "),
                    point.violation,
                    point.fineYuan?.let { "罚款 $it 元" },
                    point.penaltyPoints?.let { "记 $it 分" },
                    "来源 ${point.source.title}",
                    if (point.uploadSource == "user_upload") "用户上传" else null,
                    point.notes,
                ).filter { !it.isNullOrBlank() }.joinToString("\n"),
            )
        },
        confirmButton = {
            Row {
                if (!point.isCommunity) {
                    TextButton(onClick = onEdit) { Text("编辑") }
                    TextButton(onClick = onUpload) { Text("上报") }
                    TextButton(onClick = onDelete) { Text("删除") }
                } else {
                    TextButton(onClick = onSaveLocal) { Text("存到本机") }
                }
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("关闭") } },
    )
}

@Composable
fun SearchDialog(seed: String, onDismiss: () -> Unit, onPick: (EnforcementPoint) -> Unit) {
    val app = MotoApplication.instance
    var keywords by remember { mutableStateOf(seed) }
    var city by remember { mutableStateOf("") }
    var results by remember { mutableStateOf<List<RankedPlace>>(emptyList()) }
    var message by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("搜索地点") },
        text = {
            Column {
                OutlinedTextField(keywords, { keywords = it }, label = { Text("名称或罚单地点") }, modifier = Modifier.fillMaxWidth())
                OutlinedTextField(city, { city = it }, label = { Text("城市，可选") }, modifier = Modifier.fillMaxWidth())
                if (busy) LinearProgressIndicator(Modifier.fillMaxWidth())
                message?.let { Text(it) }
                LazyColumn(Modifier.height(240.dp)) {
                    items(results, key = { it.place.id }) { ranked ->
                        TextButton(onClick = { onPick(ranked.toPoint(QueryCleaner.parse(keywords))) }) {
                            Column {
                                Text(ranked.place.name)
                                Text(ranked.reasons.joinToString("；"), style = androidx.compose.material3.MaterialTheme.typography.bodySmall)
                            }
                        }
                    }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = {
                val parsed = QueryCleaner.parse(keywords)
                busy = true
                message = null
                Thread {
                    try {
                        val key = app.settings.effectiveAmapKey()
                        val places = AmapClient.search(key, parsed.keywords, city, AmapRequest.types(parsed))
                        val located = if (QueryCleaner.needsRoadGeocode(parsed)) {
                            runCatching { AmapClient.geocode(key, QueryCleaner.locatePlan(parsed).geocodeAddress) }.getOrDefault(emptyList())
                        } else {
                            emptyList()
                        }
                        val ranked = PlaceRanker.rank(located + places, parsed, city.ifBlank { null })
                        android.os.Handler(android.os.Looper.getMainLooper()).post {
                            results = ranked
                            busy = false
                            if (ranked.isEmpty()) message = "没有结果"
                            else if (ranked.first().score < 20) message = "匹配分偏低，请核对公路编号。"
                        }
                    } catch (error: Exception) {
                        android.os.Handler(android.os.Looper.getMainLooper()).post {
                            busy = false
                            message = error.message
                        }
                    }
                }.start()
            }) { Text("搜索") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("关闭") } },
    )
}

private fun RankedPlace.toPoint(query: com.example.xiaomoto.core.ParsedQuery): EnforcementPoint {
    val codes = QueryCleaner.roadCodes("${place.name} ${place.address} ${place.province}")
    val now = Instant.now()
    return EnforcementPoint(
        id = UUID.randomUUID().toString(),
        name = place.name,
        type = if (query.wantsToll || place.typeCode.startsWith("1802")) PointType.BANNED_TOLL else PointType.OTHER,
        roadCode = query.roadCode?.takeIf { codes.contains(it) } ?: codes.firstOrNull().orEmpty(),
        roadName = query.roadName?.takeIf { "${place.name}${place.address}".contains(it) }.orEmpty(),
        latitude = place.wgs84.latitude,
        longitude = place.wgs84.longitude,
        source = PointSource.SEARCH,
        province = place.province,
        city = place.city,
        createdAt = now,
        updatedAt = now,
        notes = reasons.joinToString("；"),
    )
}

private const val unrecognized = "不是交警12123违法截图，不予识别。"

@Composable
fun TicketDialog(onDismiss: () -> Unit, onRecognized: (List<com.example.xiaomoto.core.TicketRecord>, ByteArray?) -> Unit) {
    val context = LocalContext.current
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri: Uri? ->
        if (uri == null) return@rememberLauncherForActivityResult
        busy = true
        error = null
        val bitmap = runCatching { ImageTools.decode(context, uri) }.getOrElse {
            busy = false
            error = it.message ?: "无法读取图片"
            return@rememberLauncherForActivityResult
        }
        Thread {
            val outcome = runCatching { ChineseOcr.recognize(context.applicationContext, bitmap) }
            val jpeg = runCatching { ImageTools.jpeg(bitmap) }.getOrNull()
            android.os.Handler(android.os.Looper.getMainLooper()).post {
                busy = false
                bitmap.recycle()
                outcome.fold(
                    onSuccess = { text ->
                        val parsed = TicketParser.parse(text)
                        if (text.isBlank() || parsed.records.isEmpty()) {
                            error = unrecognized
                        } else {
                            onRecognized(parsed.records, jpeg)
                        }
                    },
                    onFailure = { error = "识别失败，不予识别。" },
                )
            }
        }.start()
    }
    AlertDialog(
        onDismissRequest = { if (!busy) onDismiss() },
        title = { Text("违法点上传") },
        text = {
            Column {
                Text("只识别交警12123违法截图。")
                Button(onClick = { picker.launch("image/*") }, enabled = !busy, modifier = Modifier.padding(top = 8.dp)) { Text("从相册选择") }
                if (busy) LinearProgressIndicator(Modifier.fillMaxWidth().padding(top = 8.dp))
                error?.let { Text(it, modifier = Modifier.padding(top = 8.dp)) }
            }
        },
        confirmButton = {},
        dismissButton = { TextButton(onClick = onDismiss, enabled = !busy) { Text("关闭") } },
    )
}

@Composable
fun UploadDialog(point: EnforcementPoint, onDismiss: () -> Unit, onDone: (String) -> Unit) {
    val context = LocalContext.current
    val app = MotoApplication.instance
    var blurSource by remember { mutableStateOf<android.graphics.Bitmap?>(null) }
    var message by remember { mutableStateOf<String?>(null) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri: Uri? ->
        if (uri == null) return@rememberLauncherForActivityResult
        blurSource = runCatching { ImageTools.decode(context, uri) }.getOrElse {
            message = it.message
            null
        }
    }
    if (blurSource != null) {
        BlurEditor(blurSource!!) { jpeg ->
            blurSource = null
            if (jpeg == null) return@BlurEditor
            send(app, point, jpeg, onDone) { message = it }
        }
        return
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("提交到社区") },
        text = {
            Column {
                Text(point.name)
                Text("可以只上传位置，或先框住号牌再传照片。照片会重新压成 JPEG，不保留拍摄位置。上传后会公开。")
                message?.let { Text(it) }
            }
        },
        confirmButton = {
            Column {
                TextButton(onClick = { send(app, point, null, onDone) { message = it } }) { Text("只上传位置") }
                TextButton(onClick = { picker.launch("image/*") }) { Text("选择照片并打码") }
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消") } },
    )
}

private fun send(
    app: MotoApplication,
    point: EnforcementPoint,
    jpeg: ByteArray?,
    onDone: (String) -> Unit,
    onError: (String) -> Unit,
) {
    Thread {
        try {
            val json = com.example.xiaomoto.net.CommunityClient.submit(
                app.settings.communityBaseUrl.value,
                app.settings.deviceId,
                point,
                jpeg,
            )
            val status = json.optString("status")
            android.os.Handler(android.os.Looper.getMainLooper()).post {
                onDone(if (status == "pending") "已提交，等待审核。" else "已提交（$status）。")
            }
        } catch (error: Exception) {
            android.os.Handler(android.os.Looper.getMainLooper()).post { onError(error.message ?: "上传失败") }
        }
    }.start()
}

@Composable
private fun BlurEditor(bitmap: android.graphics.Bitmap, onDone: (ByteArray?) -> Unit) {
    var drag by remember { mutableStateOf<RectF?>(null) }
    var start by remember { mutableStateOf(Offset.Zero) }
    var size by remember { mutableStateOf(IntSize.Zero) }
    Dialog(onDismissRequest = { onDone(null) }, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Column(Modifier.fillMaxSize().padding(12.dp)) {
            Text("用手指框住号牌。完成后照片会打码，并去掉位置信息。")
            Image(
                bitmap = bitmap.asImageBitmap(),
                contentDescription = "待打码照片",
                modifier = Modifier
                    .weight(1f)
                    .fillMaxWidth()
                    .onSizeChanged { size = it }
                    .pointerInput(bitmap) {
                        detectDragGestures(
                            onDragStart = { offset ->
                                start = offset
                                drag = RectF(offset.x, offset.y, offset.x, offset.y)
                            },
                            onDrag = { change, _ ->
                                val end = change.position
                                drag = RectF(min(start.x, end.x), min(start.y, end.y), max(start.x, end.x), max(start.y, end.y))
                            },
                        )
                    },
                contentScale = ContentScale.Fit,
            )
            Canvas(Modifier.fillMaxWidth().height(0.dp)) {}
            drag?.let { rect ->
                Text("已框选 ${rect.width().toInt()} × ${rect.height().toInt()}")
            }
            Row {
                Button(onClick = { onDone(null) }) { Text("取消") }
                Button(
                    onClick = {
                        val rect = drag ?: return@Button
                        val mapped = ImageTools.mapRect(size.width.toFloat(), size.height.toFloat(), bitmap.width, bitmap.height, rect)
                        if (mapped.width() < 4 || mapped.height() < 4) return@Button
                        onDone(ImageTools.jpeg(ImageTools.pixelate(bitmap, mapped)))
                    },
                    enabled = drag != null,
                ) { Text("完成") }
            }
        }
    }
}
