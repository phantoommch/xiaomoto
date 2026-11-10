package com.example.xiaomoto.core

import java.net.URI

object APIPath {
    const val passwordLogin = "v1/hajimi/auth/login"
    const val passwordLoginAlias = "v1/auth/login"
    const val passwordRegister = "v1/hajimi/auth/register"
    const val emailCode = "v1/hajimi/auth/email/code"
    const val emailVerify = "v1/hajimi/auth/email/verify"
    const val passwordReset = "v1/hajimi/auth/password/reset"
    const val requestCode = "v1/auth/request-code"
    const val verify = "v1/auth/verify"
    const val logout = "v1/auth/logout"
    const val me = "v1/me"
    const val acceptTerms = "v1/legal/accept"
    const val terms = "v1/legal/terms"
    const val points = "v1/points"
    const val reports = "v1/reports"
    const val myReports = "v1/me/reports"
    const val appLatestAndroid = "v1/app/latest?platform=android"
}

/**
 * Join a community base URL and a root-relative API path.
 * `https://host`, `https://host/v1`, and `https://host/v1/hajimi` all resolve
 * a `v1/...` path onto the origin. A non-`/v1` mount prefix is kept.
 * An already doubled `v1/v1/...` path is not rewritten.
 */
object APIURL {
    fun resolve(base: String, path: String): String? {
        val raw = base.trim().trimEnd('/')
        if (raw.isEmpty() || raw.contains("$(")) return null
        val baseURI = runCatching { URI(raw) }.getOrNull() ?: return null
        val scheme = baseURI.scheme?.lowercase() ?: return null
        if (scheme != "http" && scheme != "https") return null
        if (baseURI.host.isNullOrEmpty() || baseURI.userInfo != null) return null
        val relative = split(path) ?: return null
        val baseParts = segments(baseURI.path)
        val merged = if (relative.segments.firstOrNull() == "v1" && (baseParts.isEmpty() || baseParts.first() == "v1")) {
            relative.segments
        } else {
            baseParts + relative.segments
        }
        if (merged.isEmpty()) return null
        val port = if (baseURI.port == -1) "" else ":${baseURI.port}"
        val query = relative.query?.let { "?$it" }.orEmpty()
        return "$scheme://${baseURI.host}$port/${merged.joinToString("/")}$query"
    }

    private data class Relative(val segments: List<String>, val query: String?)

    private fun split(path: String): Relative? {
        var raw = path.trim()
        if (raw.isEmpty() || raw.contains("://") || raw.contains("..")) return null
        while (raw.startsWith("/")) raw = raw.drop(1)
        val halves = raw.split("?", limit = 2)
        val query = if (halves.size == 2) halves[1] else null
        if (query != null && (query.isEmpty() || query.contains(" "))) return null
        val parts = segments(halves[0])
        if (parts.isEmpty() || parts.any { it.contains(" ") }) return null
        return Relative(parts, query)
    }

    private fun segments(path: String?): List<String> {
        if (path.isNullOrEmpty()) return emptyList()
        return path.split("/").filter { it.isNotEmpty() && it != "." }
    }
}
