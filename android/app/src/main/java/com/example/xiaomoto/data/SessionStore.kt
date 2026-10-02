package com.example.xiaomoto.data

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import com.example.xiaomoto.net.AuthClient
import com.example.xiaomoto.net.AuthException
import kotlinx.coroutines.flow.MutableStateFlow

enum class SessionGate {
    Ready,
    Terms,
}

class SessionStore(context: Context) {
    private val prefs = open(context.applicationContext)
    val token = MutableStateFlow(prefs.getString(KEY_TOKEN, null)?.takeIf { it.isNotBlank() })
    val email = MutableStateFlow(prefs.getString(KEY_EMAIL, "").orEmpty())
    val acceptedTerms = MutableStateFlow(prefs.getString(KEY_TERMS, "").orEmpty())
    val currentTerms = MutableStateFlow(acceptedTerms.value.ifBlank { "1.0" })
    val gate = MutableStateFlow(SessionGate.Ready)

    fun save(token: String, email: String, termsVersion: String) {
        prefs.edit()
            .putString(KEY_TOKEN, token)
            .putString(KEY_EMAIL, email)
            .putString(KEY_TERMS, termsVersion)
            .apply()
        this.token.value = token
        this.email.value = email
        this.acceptedTerms.value = termsVersion
        this.currentTerms.value = termsVersion
        gate.value = SessionGate.Ready
    }

    fun markTerms(version: String) {
        prefs.edit().putString(KEY_TERMS, version).apply()
        acceptedTerms.value = version
        currentTerms.value = version
        gate.value = SessionGate.Ready
    }

    fun clear() {
        prefs.edit().clear().apply()
        token.value = null
        email.value = ""
        acceptedTerms.value = ""
        gate.value = SessionGate.Ready
    }

    fun refresh(baseUrl: String) {
        val current = token.value ?: return
        try {
            val me = AuthClient.me(baseUrl, current)
            prefs.edit()
                .putString(KEY_EMAIL, me.email)
                .putString(KEY_TERMS, me.termsVersion)
                .apply()
            email.value = me.email
            acceptedTerms.value = me.termsVersion
            currentTerms.value = me.termsCurrent.ifBlank { "1.0" }
            gate.value = if (me.termsAccepted) SessionGate.Ready else SessionGate.Terms
        } catch (error: AuthException) {
            if (error.code == 401) clear()
        } catch (_: Exception) {
        }
    }

    private fun open(context: Context): SharedPreferences {
        return try {
            val masterKey = MasterKey.Builder(context)
                .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
                .build()
            EncryptedSharedPreferences.create(
                context,
                "xiaomoto.session",
                masterKey,
                EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
                EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
            )
        } catch (_: Exception) {
            context.getSharedPreferences("xiaomoto.session.plain", Context.MODE_PRIVATE)
        }
    }

    private companion object {
        const val KEY_TOKEN = "token"
        const val KEY_EMAIL = "email"
        const val KEY_TERMS = "terms"
    }
}
