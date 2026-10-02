package com.example.xiaomoto.ui

import android.os.Handler
import android.os.Looper
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import com.example.xiaomoto.MotoApplication
import com.example.xiaomoto.net.AuthClient
import com.example.xiaomoto.net.AuthException
import com.example.xiaomoto.ride.RideService
import kotlinx.coroutines.delay

@Composable
fun LoginScreen() {
    val context = LocalContext.current
    val app = MotoApplication.instance
    var email by remember { mutableStateOf("") }
    var code by remember { mutableStateOf("") }
    var agreed by remember { mutableStateOf(false) }
    var termsVersion by remember { mutableStateOf("1.0") }
    var termsText by remember { mutableStateOf<String?>(null) }
    var message by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var cooldown by remember { mutableIntStateOf(0) }
    val main = remember { Handler(Looper.getMainLooper()) }

    LaunchedEffect(cooldown) {
        if (cooldown <= 0) return@LaunchedEffect
        delay(1_000)
        cooldown -= 1
    }

    fun showTerms() {
        if (busy) return
        busy = true
        Thread {
            val loaded = loadTerms(app.settings.communityBaseUrl.value, context.assets)
            main.post {
                termsVersion = loaded.version
                termsText = loaded.markdown
                busy = false
            }
        }.start()
    }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("注册 / 登录", style = MaterialTheme.typography.headlineSmall)
        Text("用邮箱收取 6 位激活码。新用户和回来的用户都用这一页。")
        OutlinedTextField(
            email,
            { email = it.trim() },
            modifier = Modifier.fillMaxWidth(),
            singleLine = true,
            label = { Text("邮箱") },
        )
        Button(
            onClick = {
                val address = email.trim()
                if (!address.contains("@") || busy || cooldown > 0) return@Button
                busy = true
                message = null
                Thread {
                    val text = try {
                        val loaded = loadTerms(app.settings.communityBaseUrl.value, context.assets)
                        AuthClient.requestCode(app.settings.communityBaseUrl.value, address)
                        main.post { termsVersion = loaded.version }
                        "激活码已发送，请查看邮箱。"
                    } catch (error: AuthException) {
                        error.message ?: "获取激活码失败"
                    } catch (error: Exception) {
                        error.message ?: "网络不通"
                    }
                    main.post {
                        message = text
                        busy = false
                        if (text.startsWith("激活码已发送")) cooldown = 60
                    }
                }.start()
            },
            enabled = !busy && cooldown == 0 && email.contains("@"),
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text(if (cooldown > 0) "$cooldown 秒后可重发" else "获取激活码")
        }
        OutlinedTextField(
            code,
            { code = it.filter(Char::isDigit).take(6) },
            modifier = Modifier.fillMaxWidth(),
            singleLine = true,
            label = { Text("6 位激活码") },
        )
        Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(agreed, { agreed = it })
            Text("我已阅读并同意")
            TextButton(onClick = { showTerms() }) { Text("《用户协议与免责声明》") }
        }
        Button(
            onClick = {
                if (!agreed || code.length != 6 || busy) return@Button
                busy = true
                message = null
                val address = email.trim()
                Thread {
                    val failure = try {
                        val loaded = loadTerms(app.settings.communityBaseUrl.value, context.assets)
                        val session = AuthClient.verify(
                            app.settings.communityBaseUrl.value,
                            address,
                            code,
                            loaded.version,
                        )
                        app.session.save(session.token, session.email, session.termsVersion)
                        RideService.ensureRunning(context.applicationContext)
                        null
                    } catch (error: AuthException) {
                        error.message ?: "登录失败"
                    } catch (error: Exception) {
                        error.message ?: "网络不通"
                    }
                    main.post {
                        message = failure
                        busy = false
                    }
                }.start()
            },
            enabled = agreed && code.length == 6 && !busy,
            modifier = Modifier.fillMaxWidth(),
        ) { Text("注册 / 登录") }
        message?.let { Text(it) }
    }
    termsText?.let { text ->
        TermsDialog(text) { termsText = null }
    }
}

@Composable
fun TermsAcceptScreen() {
    val context = LocalContext.current
    val app = MotoApplication.instance
    var markdown by remember { mutableStateOf("正在加载协议…") }
    var version by remember { mutableStateOf(app.session.currentTerms.value) }
    var agreed by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val main = remember { Handler(Looper.getMainLooper()) }
    LaunchedEffect(Unit) {
        Thread {
            val loaded = loadTerms(app.settings.communityBaseUrl.value, context.assets)
            main.post {
                markdown = loaded.markdown
                version = loaded.version
            }
        }.start()
    }
    Column(
        Modifier
            .fillMaxSize()
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("请重新同意用户协议", style = MaterialTheme.typography.titleLarge)
        Text("协议已更新到 $version。同意后才能继续使用。")
        Text(
            markdown,
            Modifier
                .weight(1f)
                .verticalScroll(rememberScrollState()),
        )
        Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(agreed, { agreed = it })
            Text("我已阅读并同意《用户协议与免责声明》")
        }
        Button(
            onClick = {
                val token = app.session.token.value ?: return@Button
                busy = true
                Thread {
                    val failure = try {
                        AuthClient.acceptTerms(app.settings.communityBaseUrl.value, token, version)
                        app.session.markTerms(version)
                        null
                    } catch (error: Exception) {
                        error.message ?: "没能保存同意记录"
                    }
                    main.post {
                        message = failure
                        busy = false
                    }
                }.start()
            },
            enabled = agreed && !busy,
            modifier = Modifier.fillMaxWidth(),
        ) { Text("同意并继续") }
        message?.let { Text(it) }
    }
}

@Composable
fun TermsDialog(markdown: String, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("用户协议与免责声明") },
        text = {
            Text(
                markdown,
                Modifier
                    .heightIn(max = 420.dp)
                    .verticalScroll(rememberScrollState()),
            )
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text("关闭") } },
    )
}

private data class LoadedTerms(val version: String, val markdown: String)

private fun loadTerms(baseUrl: String, assets: android.content.res.AssetManager): LoadedTerms {
    val remote = runCatching { AuthClient.terms(baseUrl) }.getOrNull()
    if (remote != null && remote.markdown.isNotBlank()) {
        return LoadedTerms(remote.version, remote.markdown)
    }
    val bundled = runCatching {
        assets.open("legal/terms.md").bufferedReader().use { it.readText() }
    }.getOrDefault("暂时无法加载用户协议。请稍后再试。")
    return LoadedTerms(remote?.version ?: "1.0", bundled)
}
