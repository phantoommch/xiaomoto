package com.example.xiaomoto.core

import java.io.File
import java.io.InputStream
import java.security.MessageDigest

data class AppRelease(
    val versionCode: Int,
    val versionName: String,
    val notes: String,
    val url: String,
    val sha256: String,
    val size: Long,
    val force: Boolean,
) {
    companion object {
        fun isNewer(remote: Int, local: Int): Boolean = remote > local

        fun currentVersionLine(code: Int): String = "当前版本：第 $code 版"

        fun upToDateLine(code: Int): String = "已是最新版本（第 $code 版）"

        fun updateAvailableLine(remote: Int, local: Int): String = "有新版本：第 $remote 版（当前第 $local 版）"

        fun sha256Hex(file: File): String = file.inputStream().use { sha256Hex(it) }

        fun sha256Hex(input: InputStream): String {
            val digest = MessageDigest.getInstance("SHA-256")
            val buffer = ByteArray(64 * 1024)
            while (true) {
                val n = input.read(buffer)
                if (n < 0) break
                digest.update(buffer, 0, n)
            }
            return digest.digest().joinToString("") { "%02x".format(it) }
        }
    }
}
