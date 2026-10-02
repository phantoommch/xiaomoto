package com.example.xiaomoto.ui

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.waitForUpOrCancellation
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.layout.widthIn
import androidx.compose.ui.Alignment
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.ui.graphics.Color
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import com.example.xiaomoto.BuildConfig
import com.example.xiaomoto.MotoApplication
import com.example.xiaomoto.core.AlertDistance
import com.example.xiaomoto.core.AppRelease
import com.example.xiaomoto.net.AppUpdateController
import com.example.xiaomoto.oem.OemGuides
import com.example.xiaomoto.ride.ActiveAlert
import com.example.xiaomoto.ride.RideRuntime
import com.example.xiaomoto.ride.RideService
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import kotlin.math.roundToInt
import kotlinx.coroutines.withTimeoutOrNull

@Composable
fun RideScreen() {
    val context = LocalContext.current
    val riding by RideRuntime.riding.collectAsStateCompat()
    val error by RideRuntime.lastError.collectAsStateCompat()
    val recent by RideRuntime.recent.collectAsStateCompat()
    val tts by RideRuntime.chineseTts.collectAsStateCompat()
    val fine = ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
    val background = ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_BACKGROUND_LOCATION) == PackageManager.PERMISSION_GRANTED
    val notifications = if (Build.VERSION.SDK_INT < 33) true else {
        ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED
    }
    val askFine = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { }
    val askBackground = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { }
    val askNotifications = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { }
    var channelPrompt by remember { mutableStateOf(false) }
    var fullScreenPrompt by remember { mutableStateOf(false) }
    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(if (riding) "正在用 GPS 监听前方点位" else "骑行提醒未开启", style = MaterialTheme.typography.titleMedium)
        Text("靠近时每个圈提醒一次。离开这段路之后，下次再靠近还会提醒。点「不再提醒」的点位不再出声。")
        if (tts == false) {
            Card(Modifier.padding(top = 8.dp)) {
                Text(
                    "这台手机没有可用的中文语音引擎。华为和荣耀出厂经常不带中文 TTS。提醒会改成提示音，并把整句写在通知里。可到系统设置 → 智慧助手或语音数据里安装中文语音；装好后重新开始骑行。",
                    Modifier.padding(12.dp),
                )
            }
        }
        Button(
            onClick = {
                if (riding) {
                    RideService.stop(context)
                    return@Button
                }
                if (!fine) {
                    askFine.launch(arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION))
                    return@Button
                }
                if (!background) {
                    askBackground.launch(Manifest.permission.ACCESS_BACKGROUND_LOCATION)
                    return@Button
                }
                if (!notifications && Build.VERSION.SDK_INT >= 33) {
                    askNotifications.launch(Manifest.permission.POST_NOTIFICATIONS)
                }
                RideService.prepareNotifications(context)
                val prominent = RideService.alertsAreProminent(context)
                val fullScreen = RideService.canUseFullScreenIntent(context)
                RideService.start(context)
                when {
                    !prominent -> channelPrompt = true
                    !fullScreen -> fullScreenPrompt = true
                    !OemGuides.isIgnoringBattery(context) -> {
                        runCatching { context.startActivity(OemGuides.batteryIntent(context)) }
                    }
                }
            },
            modifier = Modifier.padding(top = 12.dp),
        ) { Text(if (riding) "结束骑行" else "开始骑行") }
        Text(if (fine) "定位：已允许精确定位" else "定位：尚未允许")
        Text(if (background) "后台定位：已允许（请确认为始终允许）" else "后台定位：未允许。锁屏后需要「始终允许」。")
        Text(OemGuides.locationInstructions, style = MaterialTheme.typography.bodySmall)
        Text("华为和荣耀还要在应用启动管理里允许自启动和后台活动，否则锁屏后提醒会被关掉。", style = MaterialTheme.typography.bodySmall)
        Text(OemGuides.alertChannelInstructions, style = MaterialTheme.typography.bodySmall)
        Button(onClick = {
            RideService.prepareNotifications(context)
            RideService.openAlertSettings(context)
        }) { Text("打开「违法点提醒」设置") }
        if (Build.VERSION.SDK_INT >= 34) {
            Text(OemGuides.fullScreenInstructions, style = MaterialTheme.typography.bodySmall)
            Button(onClick = { RideService.openFullScreenSettings(context) }) { Text("允许全屏通知") }
        }
        Button(onClick = { context.startActivity(OemGuides.appDetails(context)) }) { Text("打开应用权限设置") }
        Button(onClick = {
            try {
                context.startActivity(OemGuides.batteryIntent(context))
            } catch (_: Exception) {
                context.startActivity(OemGuides.batteryListIntent())
            }
        }) { Text(if (OemGuides.isIgnoringBattery(context)) "电池优化已忽略" else "请求忽略电池优化") }
        Button(onClick = {
            if (!OemGuides.openStartupManager(context)) {
                android.widget.Toast.makeText(context, OemGuides.startupInstructions, android.widget.Toast.LENGTH_LONG).show()
            }
        }) { Text("打开自启动 / 后台活动") }
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        Text("最近提醒", Modifier.padding(top = 12.dp), style = MaterialTheme.typography.titleSmall)
        if (recent.isEmpty()) Text("还没有触发提醒")
        recent.forEach { item ->
            val clock = item.at.atZone(ZoneId.systemDefault()).format(DateTimeFormatter.ofPattern("HH:mm:ss"))
            Text("$clock  ${item.text}")
        }
    }
    if (channelPrompt) {
        AlertDialog(
            onDismissRequest = { channelPrompt = false },
            title = { Text("锁屏提醒") },
            text = { Text(OemGuides.alertChannelInstructions) },
            confirmButton = {
                TextButton(onClick = {
                    channelPrompt = false
                    RideService.openAlertSettings(context)
                }) { Text("去打开") }
            },
            dismissButton = {
                TextButton(onClick = { channelPrompt = false }) { Text("知道了") }
            },
        )
    }
    if (fullScreenPrompt) {
        AlertDialog(
            onDismissRequest = { fullScreenPrompt = false },
            title = { Text("锁屏提醒") },
            text = { Text(OemGuides.fullScreenInstructions) },
            confirmButton = {
                TextButton(onClick = {
                    fullScreenPrompt = false
                    RideService.openFullScreenSettings(context)
                }) { Text("去打开") }
            },
            dismissButton = {
                TextButton(onClick = { fullScreenPrompt = false }) { Text("知道了") }
            },
        )
    }
}

@Composable
fun ForegroundAlertOverlay() {
    val alert by RideRuntime.activeAlert.collectAsStateCompat()
    val current = alert ?: return
    val context = LocalContext.current
    Box(
        Modifier
            .fillMaxSize()
            .background(Color(0x99000000))
            .clickable(
                indication = null,
                interactionSource = remember { MutableInteractionSource() },
            ) {},
        contentAlignment = Alignment.Center,
    ) {
        AlertPromptScreen(
            alert = current,
            onAck = { RideRuntime.acknowledge(context, current.id) },
            onMute = { RideRuntime.mute(context, current.pointId, current.id) },
        )
    }
}

@Composable
fun AlertPromptScreen(alert: ActiveAlert, onAck: () -> Unit, onMute: () -> Unit) {
    Card(
        modifier = Modifier
            .padding(24.dp)
            .widthIn(max = 420.dp)
            .fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = Color(0xFFF7F4EE),
            contentColor = Color(0xFF1A1814),
        ),
    ) {
        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(alert.title, style = MaterialTheme.typography.headlineSmall, color = Color(0xFF1A1814))
            Text(alert.text, style = MaterialTheme.typography.bodyLarge, color = Color(0xFF1A1814))
            Button(onClick = onAck, modifier = Modifier.fillMaxWidth()) {
                Text("我知道了", style = MaterialTheme.typography.titleMedium)
            }
            Button(
                onClick = onMute,
                modifier = Modifier.fillMaxWidth(),
                colors = ButtonDefaults.buttonColors(containerColor = Color(0xFF8C2F2F), contentColor = Color.White),
            ) {
                Text("不再提醒", style = MaterialTheme.typography.titleMedium)
            }
        }
    }
}

@Composable
fun SettingsScreen() {
    val context = LocalContext.current
    val app = MotoApplication.instance
    val outer by app.settings.outerMeters.collectAsStateCompat()
    val inner by app.settings.innerMeters.collectAsStateCompat()
    val updateStatus by AppUpdateController.settingsStatus.collectAsStateCompat()
    val muted by app.settings.mutedIds.collectAsStateCompat()
    val accountEmail by app.session.email.collectAsStateCompat()
    var showServer by remember { mutableStateOf(false) }
    var serverField by remember { mutableStateOf("") }
    var status by remember { mutableStateOf<String?>(null) }
    var showTerms by remember { mutableStateOf(false) }
    var termsBody by remember { mutableStateOf("") }
    var confirmDelete by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxSize()) {
    Column(
        Modifier
            .weight(1f)
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            "设置",
            style = MaterialTheme.typography.titleLarge,
            modifier = Modifier.pointerInput(Unit) {
                awaitEachGesture {
                    awaitFirstDown(requireUnconsumed = false)
                    val released = withTimeoutOrNull(3_000) { waitForUpOrCancellation() }
                    if (released == null) {
                        serverField = app.settings.communityBaseUrl.value
                        showServer = true
                    }
                }
            },
        )
        Text("提醒距离", style = MaterialTheme.typography.titleMedium)
        val outerStops = AlertDistance.outerStops
        Text("外圈 ${AlertDistance.format(outer)}")
        Slider(
            AlertDistance.indexOf(outer).toFloat(),
            { raw ->
                val next = outerStops[raw.roundToInt().coerceIn(0, outerStops.lastIndex)]
                if (next != outer) app.settings.setOuter(next)
            },
            valueRange = 0f..outerStops.lastIndex.toFloat(),
            steps = outerStops.size - 2,
        )
        Text("内圈 ${inner.toInt()} 米")
        Slider(inner.toFloat(), { app.settings.setInner(it.toDouble()) }, valueRange = 50f..2000f)
        Text("靠近时每个圈提醒一次。离开这段路之后，下次再靠近还会提醒。点「不再提醒」的点位不再出声。")
        TextButton(onClick = { app.settings.clearMuted() }) {
            Text("已关闭提醒的点位（${muted.size}个）· 全部恢复")
        }
        Text("账号", style = MaterialTheme.typography.titleMedium)
        Text(accountEmail.ifBlank { "已登录" })
        TextButton(onClick = {
            Thread {
                val loaded = runCatching {
                    com.example.xiaomoto.net.AuthClient.terms(app.settings.communityBaseUrl.value).markdown
                }.getOrElse {
                    context.assets.open("legal/terms.md").bufferedReader().use { it.readText() }
                }
                android.os.Handler(android.os.Looper.getMainLooper()).post {
                    termsBody = loaded
                    showTerms = true
                }
            }.start()
        }) { Text("用户协议与免责声明") }
        TextButton(onClick = {
            val token = app.session.token.value
            val base = app.settings.communityBaseUrl.value
            if (!token.isNullOrBlank()) {
                Thread {
                    runCatching { com.example.xiaomoto.net.AuthClient.logout(base, token) }
                    android.os.Handler(android.os.Looper.getMainLooper()).post { app.session.clear() }
                }.start()
            } else {
                app.session.clear()
            }
        }) { Text("退出登录") }
        TextButton(onClick = { confirmDelete = true }) { Text("注销账号") }
        Button(onClick = { AppUpdateController.check(context, manual = true) }) { Text("检查更新") }
        updateStatus?.let { Text(it) }
        status?.let { Text(it) }
    }
        Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp)) {
            Text(
                AppRelease.currentVersionLine(BuildConfig.VERSION_CODE),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                BuildConfig.VERSION_NAME,
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
    if (showServer) {
        AlertDialog(
            onDismissRequest = { showServer = false },
            title = { Text("服务器地址") },
            text = {
                OutlinedTextField(
                    serverField,
                    { serverField = it },
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("https://") },
                )
            },
            confirmButton = {
                TextButton(onClick = {
                    app.settings.setCommunityBaseUrl(serverField)
                    Thread { app.uploads.flush() }.start()
                    status = "地址已保存。"
                    showServer = false
                }) { Text("保存") }
            },
            dismissButton = {
                TextButton(onClick = { showServer = false }) { Text("取消") }
            },
        )
    }
    if (showTerms) {
        TermsDialog(termsBody.ifBlank { "正在加载协议…" }) { showTerms = false }
    }
    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text("注销账号") },
            text = { Text("注销后这台手机上的登录会被清掉。已经公开的违法点还会留在地图上，但不再和这个邮箱关联。") },
            confirmButton = {
                TextButton(onClick = {
                    confirmDelete = false
                    val token = app.session.token.value
                    val base = app.settings.communityBaseUrl.value
                    if (token.isNullOrBlank()) {
                        app.session.clear()
                        return@TextButton
                    }
                    Thread {
                        val failure = runCatching {
                            com.example.xiaomoto.net.AuthClient.deleteAccount(base, token)
                        }.exceptionOrNull()
                        android.os.Handler(android.os.Looper.getMainLooper()).post {
                            if (failure == null) {
                                app.session.clear()
                            } else {
                                status = failure.message ?: "注销失败"
                            }
                        }
                    }.start()
                }) { Text("注销") }
            },
            dismissButton = {
                TextButton(onClick = { confirmDelete = false }) { Text("取消") }
            },
        )
    }
}
