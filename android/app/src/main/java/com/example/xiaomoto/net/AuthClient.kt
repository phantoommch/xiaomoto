package com.example.xiaomoto.net

import com.example.xiaomoto.BuildConfig
import com.example.xiaomoto.MotoApplication
import com.example.xiaomoto.core.APIPath
import com.example.xiaomoto.core.APIURL
import java.io.BufferedReader
import java.net.HttpURLConnection
import java.net.URL
import org.json.JSONObject

class AuthException(message: String, val code: Int) : Exception(message)

object ApiAuth {
    fun apply(connection: HttpURLConnection) {
        connection.setRequestProperty("X-App-Build", BuildConfig.VERSION_CODE.toString())
        connection.setRequestProperty("User-Agent", "XiaoMoto/1.0")
        val token = runCatching { MotoApplication.instance.session.token.value }.getOrNull()
        if (!token.isNullOrBlank()) {
            connection.setRequestProperty("Authorization", "Bearer $token")
        }
    }

    fun noteStatus(code: Int) {
        if (code == 401) {
            runCatching { MotoApplication.instance.session.clear() }
        }
    }
}

object AuthClient {
    data class TermsDoc(val version: String, val title: String, val markdown: String)
    data class Me(
        val email: String,
        val termsVersion: String,
        val termsCurrent: String,
        val termsAccepted: Boolean,
    )
    data class Session(val token: String, val email: String, val termsVersion: String)
    data class MineReport(
        val id: String,
        val name: String,
        val status: String,
        val roadCode: String,
        val stake: String,
    )

    fun myReports(baseUrl: String, token: String): List<MineReport> {
        val json = call(baseUrl, APIPath.myReports, "GET", null, token)
        val rows = json.optJSONArray("reports") ?: return emptyList()
        return buildList {
            for (index in 0 until rows.length()) {
                val row = rows.optJSONObject(index) ?: continue
                add(
                    MineReport(
                        id = row.optString("id"),
                        name = row.optString("name"),
                        status = row.optString("status"),
                        roadCode = row.optString("road_code"),
                        stake = row.optString("stake"),
                    ),
                )
            }
        }
    }

    fun requestCode(baseUrl: String, email: String) {
        val json = call(baseUrl, APIPath.requestCode, "POST", JSONObject().put("email", email), null)
        if (json.optBoolean("ok")) return
        throw AuthException(json.optString("error").ifBlank { "获取激活码失败" }, 200)
    }

    fun verify(baseUrl: String, email: String, code: String, termsVersion: String): Session {
        val body = JSONObject()
            .put("email", email)
            .put("code", code)
            .put("termsVersion", termsVersion)
        val json = call(baseUrl, APIPath.verify, "POST", body, null)
        val token = json.optString("token")
        if (token.isBlank()) throw AuthException("登录没有返回凭证", 200)
        val user = json.optJSONObject("user")
        return Session(
            token = token,
            email = user?.optString("email").orEmpty().ifBlank { email.trim() },
            termsVersion = user?.optString("termsVersion").orEmpty().ifBlank { termsVersion },
        )
    }

    fun me(baseUrl: String, token: String): Me {
        val json = call(baseUrl, APIPath.me, "GET", null, token)
        return Me(
            email = json.optString("email"),
            termsVersion = json.optString("termsVersion"),
            termsCurrent = json.optString("termsCurrent"),
            termsAccepted = json.optBoolean("termsAccepted"),
        )
    }

    fun logout(baseUrl: String, token: String) {
        call(baseUrl, APIPath.logout, "POST", null, token)
    }

    fun deleteAccount(baseUrl: String, token: String) {
        call(baseUrl, APIPath.me, "DELETE", null, token)
    }

    fun acceptTerms(baseUrl: String, token: String, version: String) {
        call(baseUrl, APIPath.acceptTerms, "POST", JSONObject().put("version", version), token)
    }

    fun terms(baseUrl: String): TermsDoc {
        val json = call(baseUrl, APIPath.terms, "GET", null, null)
        return TermsDoc(
            version = json.optString("version").ifBlank { "1.0" },
            title = json.optString("title").ifBlank { "用户协议与免责声明" },
            markdown = json.optString("markdown"),
        )
    }

    private fun call(baseUrl: String, path: String, method: String, body: JSONObject?, token: String?): JSONObject {
        val url = endpoint(baseUrl, path)
        val connection = (URL(url).openConnection() as HttpURLConnection).apply {
            requestMethod = method
            connectTimeout = 20_000
            readTimeout = 20_000
            setRequestProperty("Accept", "application/json")
            ApiAuth.apply(this)
            if (!token.isNullOrBlank()) {
                setRequestProperty("Authorization", "Bearer $token")
            }
            if (body != null) {
                doOutput = true
                setRequestProperty("Content-Type", "application/json; charset=utf-8")
            }
        }
        try {
            if (body != null) {
                connection.outputStream.use { it.write(body.toString().toByteArray()) }
            }
            val code = connection.responseCode
            val stream = if (code in 200..299) connection.inputStream else connection.errorStream
            val text = stream?.bufferedReader()?.use(BufferedReader::readText).orEmpty()
            val json = runCatching { JSONObject(text) }.getOrElse { JSONObject() }
            if (code == 401) {
                ApiAuth.noteStatus(401)
                throw AuthException(json.optString("error").ifBlank { "未登录" }, code)
            }
            if (code !in 200..299) {
                throw AuthException(json.optString("error").ifBlank { "服务器返回 $code" }, code)
            }
            return json
        } catch (error: AuthException) {
            throw error
        } catch (error: Exception) {
            throw AuthException(error.message ?: "网络不通", 0)
        } finally {
            connection.disconnect()
        }
    }

    private fun endpoint(raw: String, path: String): String {
        if (raw.trim().isEmpty()) throw AuthException("还没有服务器地址", 0)
        return APIURL.resolve(raw, path) ?: throw AuthException("服务器地址无效", 0)
    }
}
