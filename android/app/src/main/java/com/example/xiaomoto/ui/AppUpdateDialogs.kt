package com.example.xiaomoto.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import com.example.xiaomoto.BuildConfig
import com.example.xiaomoto.core.AppRelease
import com.example.xiaomoto.net.AppUpdateController
import com.example.xiaomoto.net.UpdateUi

@Composable
fun AppUpdateHost() {
    val context = LocalContext.current
    val ui by AppUpdateController.ui.collectAsStateCompat()
    LaunchedEffect(Unit) { AppUpdateController.maybeDaily(context) }
    when (val state = ui) {
        UpdateUi.Idle -> Unit
        is UpdateUi.Offer -> AlertDialog(
            onDismissRequest = { AppUpdateController.later() },
            title = {
                Text(AppRelease.updateAvailableLine(state.release.versionCode, BuildConfig.VERSION_CODE))
            },
            text = { Text(state.release.notes.ifBlank { "有一个新版本可以安装。" }) },
            confirmButton = {
                TextButton(onClick = { AppUpdateController.startDownload(context) }) { Text("立即更新") }
            },
            dismissButton = if (state.release.force) {
                null
            } else {
                { TextButton(onClick = { AppUpdateController.later() }) { Text("以后再说") } }
            },
        )
        is UpdateUi.Progress -> AlertDialog(
            onDismissRequest = {},
            title = { Text("正在下载") },
            text = {
                Column {
                    LinearProgressIndicator(
                        progress = { state.fraction.coerceIn(0f, 1f) },
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Text("${(state.fraction.coerceIn(0f, 1f) * 100).toInt()}%", Modifier.padding(top = 8.dp))
                }
            },
            confirmButton = {},
        )
        is UpdateUi.AllowInstall -> AlertDialog(
            onDismissRequest = { if (!state.force) AppUpdateController.ui.value = UpdateUi.Idle },
            title = { Text("允许安装应用") },
            text = { Text("安装更新需要允许本应用安装应用。接下来系统会让你打开这个开关。") },
            confirmButton = {
                TextButton(onClick = { AppUpdateController.openInstallSettings(context) }) { Text("去设置") }
            },
            dismissButton = if (state.force) {
                null
            } else {
                { TextButton(onClick = { AppUpdateController.ui.value = UpdateUi.Idle }) { Text("取消") } }
            },
        )
        is UpdateUi.Notice -> AlertDialog(
            onDismissRequest = { AppUpdateController.dismissNotice() },
            title = { Text("更新没有完成") },
            text = { Text(state.text) },
            confirmButton = {
                TextButton(onClick = { AppUpdateController.dismissNotice() }) { Text("知道了") }
            },
        )
    }
}
