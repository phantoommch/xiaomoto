package com.example.xiaomoto.net

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.provider.Settings
import androidx.core.content.FileProvider
import com.example.xiaomoto.BuildConfig
import com.example.xiaomoto.MotoApplication
import com.example.xiaomoto.core.AppRelease
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject

class AppUpdateException(message: String) : Exception(message)

sealed interface UpdateUi {
    data object Idle : UpdateUi
    data class Offer(val release: AppRelease) : UpdateUi
    data class Progress(val fraction: Float) : UpdateUi
    data class AllowInstall(val file: File, val force: Boolean) : UpdateUi
    data class Notice(val text: String) : UpdateUi
}

object AppUpdateController {
    val ui = MutableStateFlow<UpdateUi>(UpdateUi.Idle)
    val settingsStatus = MutableStateFlow<String?>(null)

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val gate = Any()
    private var running = false
    private const val DAY_MS = 24L * 60 * 60 * 1000
    private const val PREFS = "xiaomoto"
    private const val KEY_LAST = "lastUpdateCheck"

    fun maybeDaily(context: Context) {
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val last = prefs.getLong(KEY_LAST, 0L)
        if (System.currentTimeMillis() - last < DAY_MS) return
        check(context, manual = false)
    }

    fun check(context: Context, manual: Boolean) {
        synchronized(gate) {
            if (running) return
            running = true
        }
        val base = MotoApplication.instance.settings.communityBaseUrl.value.trim().trimEnd('/')
        if (base.isEmpty()) {
            synchronized(gate) { running = false }
            if (manual) settingsStatus.value = "还没有服务器地址"
            return
        }
        if (manual) settingsStatus.value = "正在检查更新…"
        val appContext = context.applicationContext
        scope.launch {
            try {
                val release = withContext(Dispatchers.IO) { AppUpdateClient.latest(base) }
                rememberCheck(appContext)
                val local = BuildConfig.VERSION_CODE
                if (release != null && AppRelease.isNewer(release.versionCode, local)) {
                    if (manual) settingsStatus.value = AppRelease.updateAvailableLine(release.versionCode, local)
                    ui.value = UpdateUi.Offer(release)
                } else if (manual) {
                    settingsStatus.value = AppRelease.upToDateLine(local)
                }
            } catch (_: Exception) {
                if (manual) settingsStatus.value = "检查更新失败"
            } finally {
                synchronized(gate) { running = false }
            }
        }
    }

    fun later() {
        val offer = ui.value as? UpdateUi.Offer ?: return
        if (offer.release.force) return
        ui.value = UpdateUi.Idle
    }

    fun dismissNotice() {
        if (ui.value is UpdateUi.Notice) ui.value = UpdateUi.Idle
    }

    fun startDownload(context: Context) {
        val offer = ui.value as? UpdateUi.Offer ?: return
        val appContext = context.applicationContext
        val release = offer.release
        ui.value = UpdateUi.Progress(0f)
        scope.launch {
            try {
                val file = withContext(Dispatchers.IO) {
                    AppUpdateClient.download(release, appContext.cacheDir) { fraction ->
                        ui.value = UpdateUi.Progress(fraction)
                    }
                }
                if (appContext.packageManager.canRequestPackageInstalls()) {
                    ui.value = UpdateUi.Idle
                    AppUpdateClient.install(appContext, file)
                } else {
                    ui.value = UpdateUi.AllowInstall(file, release.force)
                }
            } catch (error: Exception) {
                ui.value = UpdateUi.Notice(error.message ?: "下载失败")
            }
        }
    }

    fun openInstallSettings(context: Context) {
        val intent = Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}"))
        intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        context.startActivity(intent)
    }

    fun onHostResume(context: Context) {
        val pending = ui.value as? UpdateUi.AllowInstall ?: return
        if (!context.packageManager.canRequestPackageInstalls()) return
        ui.value = UpdateUi.Idle
        AppUpdateClient.install(context, pending.file)
    }

    private fun rememberCheck(context: Context) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .edit()
            .putLong(KEY_LAST, System.currentTimeMillis())
            .apply()
    }
}

object AppUpdateClient {
    fun latest(baseUrl: String): AppRelease? {
        val connection = open(baseUrl.trimEnd('/') + "/v1/app/latest?platform=android")
        try {
            val code = connection.responseCode
            val stream = if (code in 200..299) connection.inputStream else connection.errorStream
            val body = stream?.bufferedReader()?.use { it.readText() }.orEmpty()
            if (code == 401) {
                ApiAuth.noteStatus(401)
                throw AppUpdateException("未登录")
            }
            if (code == 404 || body.isBlank()) return null
            if (code !in 200..299) throw AppUpdateException("检查更新失败")
            val json = JSONObject(body)
            if (!json.has("versionCode")) return null
            return AppRelease(
                versionCode = json.getInt("versionCode"),
                versionName = json.optString("versionName"),
                notes = json.optString("notes"),
                url = json.optString("url"),
                sha256 = json.optString("sha256"),
                size = json.optLong("size"),
                force = json.optBoolean("force"),
            )
        } finally {
            connection.disconnect()
        }
    }

    fun download(release: AppRelease, cacheDir: File, onProgress: (Float) -> Unit): File {
        if (release.url.isBlank() || release.sha256.isBlank()) throw AppUpdateException("下载失败")
        val dir = File(cacheDir, "updates").apply { mkdirs() }
        val dest = File(dir, "xiaomoto-update.apk")
        val part = File(dir, "xiaomoto-update.apk.part")
        val connection = open(release.url)
        try {
            val code = connection.responseCode
            if (code == 401) {
                ApiAuth.noteStatus(401)
                throw AppUpdateException("未登录")
            }
            if (code !in 200..299) throw AppUpdateException("下载失败")
            val total = connection.contentLengthLong.let { if (it > 0) it else release.size }
            connection.inputStream.use { input ->
                part.outputStream().use { output ->
                    val buffer = ByteArray(64 * 1024)
                    var got = 0L
                    while (true) {
                        val n = input.read(buffer)
                        if (n < 0) break
                        output.write(buffer, 0, n)
                        got += n
                        if (total > 0) onProgress((got.toFloat() / total.toFloat()).coerceIn(0f, 1f))
                    }
                }
            }
        } catch (error: AppUpdateException) {
            part.delete()
            throw error
        } catch (_: Exception) {
            part.delete()
            throw AppUpdateException("下载失败")
        } finally {
            connection.disconnect()
        }
        val digest = AppRelease.sha256Hex(part)
        if (!digest.equals(release.sha256, ignoreCase = true)) {
            part.delete()
            throw AppUpdateException("安装包校验失败")
        }
        if (dest.exists() && !dest.delete()) throw AppUpdateException("下载失败")
        if (!part.renameTo(dest)) {
            part.copyTo(dest, overwrite = true)
            part.delete()
        }
        onProgress(1f)
        return dest
    }

    fun install(context: Context, file: File) {
        val uri = FileProvider.getUriForFile(context, context.packageName + ".fileprovider", file)
        val intent = Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(uri, "application/vnd.android.package-archive")
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        val resolvers = context.packageManager.queryIntentActivities(intent, PackageManager.MATCH_DEFAULT_ONLY)
        for (info in resolvers) {
            context.grantUriPermission(info.activityInfo.packageName, uri, Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        context.startActivity(intent)
    }

    private fun open(url: String): HttpURLConnection {
        return (URL(url).openConnection() as HttpURLConnection).apply {
            connectTimeout = 20_000
            readTimeout = 120_000
            instanceFollowRedirects = true
            setRequestProperty("User-Agent", "XiaoMoto/1.0")
            ApiAuth.apply(this)
        }
    }
}
