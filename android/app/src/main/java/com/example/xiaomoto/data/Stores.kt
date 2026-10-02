package com.example.xiaomoto.data

import android.content.Context
import com.example.xiaomoto.BuildConfig
import com.example.xiaomoto.core.AlertDistance
import com.example.xiaomoto.core.AlertSettings
import com.example.xiaomoto.core.ApproachRecord
import com.example.xiaomoto.core.EnforcementPoint
import com.example.xiaomoto.core.PointCodec
import com.example.xiaomoto.core.PointCodecException
import java.io.File
import java.util.UUID
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

class SettingsStore(context: Context) {
    private val prefs = context.getSharedPreferences("xiaomoto", Context.MODE_PRIVATE)

    private val _outer = MutableStateFlow(prefs.getFloat(KEY_OUTER, 2000f).toDouble())
    private val _inner = MutableStateFlow(prefs.getFloat(KEY_INNER, 500f).toDouble())
    private val _cooldownMin = MutableStateFlow(prefs.getInt(KEY_COOLDOWN, 30))
    private val _approach = MutableStateFlow(prefs.getBoolean(KEY_APPROACH, false))
    private val _amap = MutableStateFlow(prefs.getString(KEY_AMAP, "").orEmpty())
    private val _androidKey = MutableStateFlow(prefs.getString(KEY_ANDROID_KEY, "").orEmpty())
    private val _base = MutableStateFlow(prefs.getString(KEY_BASE, null) ?: BuildConfig.COMMUNITY_API_BASE_URL)
    private val _seen = MutableStateFlow(prefs.getBoolean(KEY_SEEN, false))
    private val _privacy = MutableStateFlow(prefs.getBoolean(KEY_PRIVACY, false))
    private val _riding = MutableStateFlow(prefs.getBoolean(KEY_RIDING, false))
    private val _muted = MutableStateFlow(prefs.getStringSet(KEY_MUTED, emptySet()).orEmpty().toSet())

    val outerMeters: StateFlow<Double> = _outer
    val innerMeters: StateFlow<Double> = _inner
    val cooldownMinutes: StateFlow<Int> = _cooldownMin
    val onlyWhenApproaching: StateFlow<Boolean> = _approach
    val amapOverride: StateFlow<String> = _amap
    val androidMapKey: StateFlow<String> = _androidKey
    val communityBaseUrl: StateFlow<String> = _base
    val onboardingSeen: StateFlow<Boolean> = _seen
    val amapPrivacyAgreed: StateFlow<Boolean> = _privacy
    val ridingEnabled: StateFlow<Boolean> = _riding
    val mutedIds: StateFlow<Set<String>> = _muted

    val deviceId: String = prefs.getString(KEY_DEVICE, null) ?: UUID.randomUUID().toString().also {
        prefs.edit().putString(KEY_DEVICE, it).apply()
    }

    fun effectiveAmapKey(): String = BuildConfig.AMAP_WEB_KEY.trim()

    fun effectiveAndroidMapKey(): String = BuildConfig.AMAP_ANDROID_KEY.trim()

    fun alertSettings(): AlertSettings = AlertSettings(
        outerMeters = _outer.value,
        innerMeters = _inner.value,
    )

    fun mute(id: String) {
        if (id.isEmpty()) return
        val next = _muted.value + id
        _muted.value = next
        prefs.edit().putStringSet(KEY_MUTED, next.toHashSet()).apply()
    }

    fun clearMuted() {
        _muted.value = emptySet()
        prefs.edit().remove(KEY_MUTED).apply()
    }

    fun loadApproach(): Map<String, ApproachRecord> {
        val raw = prefs.getString(KEY_APPROACH_STATE, null) ?: return emptyMap()
        return runCatching { approachJson.decodeFromString<Map<String, ApproachRecord>>(raw) }.getOrDefault(emptyMap())
    }

    fun saveApproach(value: Map<String, ApproachRecord>) {
        prefs.edit().putString(KEY_APPROACH_STATE, approachJson.encodeToString(value)).apply()
    }

    fun setOuter(meters: Double) {
        val next = meters.coerceIn(AlertDistance.OUTER_MIN, AlertDistance.OUTER_MAX)
        put(_outer, next) { putFloat(KEY_OUTER, it.toFloat()) }
    }
    fun setInner(meters: Double) = put(_inner, meters) { putFloat(KEY_INNER, it.toFloat()) }
    fun setCooldownMinutes(minutes: Int) {
        val clamped = minutes.coerceIn(1, 360)
        _cooldownMin.value = clamped
        prefs.edit().putInt(KEY_COOLDOWN, clamped).apply()
    }
    fun setApproach(value: Boolean) {
        _approach.value = value
        prefs.edit().putBoolean(KEY_APPROACH, value).apply()
    }
    fun setAmapOverride(value: String) {
        _amap.value = value.trim()
        prefs.edit().putString(KEY_AMAP, _amap.value).apply()
    }
    fun setAndroidMapKey(value: String) {
        _androidKey.value = value.trim()
        prefs.edit().putString(KEY_ANDROID_KEY, _androidKey.value).apply()
    }
    fun setAmapPrivacyAgreed(value: Boolean) {
        _privacy.value = value
        prefs.edit().putBoolean(KEY_PRIVACY, value).apply()
    }
    fun setCommunityBaseUrl(value: String) {
        _base.value = value.trim()
        prefs.edit().putString(KEY_BASE, _base.value).apply()
    }
    fun markOnboardingSeen() {
        _seen.value = true
        prefs.edit().putBoolean(KEY_SEEN, true).apply()
    }
    fun setRiding(value: Boolean) {
        _riding.value = value
        prefs.edit().putBoolean(KEY_RIDING, value).apply()
    }

    private fun <T> put(flow: MutableStateFlow<T>, value: T, write: android.content.SharedPreferences.Editor.(T) -> Unit) {
        flow.value = value
        prefs.edit().apply { write(value) }.apply()
    }

    private companion object {
        const val KEY_OUTER = "outer"
        const val KEY_INNER = "inner"
        const val KEY_COOLDOWN = "cooldownMin"
        const val KEY_APPROACH = "approach"
        const val KEY_AMAP = "amap"
        const val KEY_ANDROID_KEY = "amapAndroid"
        const val KEY_BASE = "base"
        const val KEY_SEEN = "seen"
        const val KEY_PRIVACY = "amapPrivacy"
        const val KEY_RIDING = "riding"
        const val KEY_DEVICE = "device"
        const val KEY_MUTED = "mutedPointIds"
        const val KEY_APPROACH_STATE = "approachState"
    }

    private val approachJson = Json { ignoreUnknownKeys = true; encodeDefaults = true }
}

class PointRepository(dir: File) {
    private val localFile = File(dir, "points.json")
    private val communityFile = File(dir, "community.json")
    private val localFlow = MutableStateFlow(read(localFile))
    private val communityFlow = MutableStateFlow(read(communityFile))

    val local: StateFlow<List<EnforcementPoint>> = localFlow
    val community: StateFlow<List<EnforcementPoint>> = communityFlow

    fun alertPoints(): List<EnforcementPoint> = localFlow.value.filter { it.located } + communityFlow.value

    @Synchronized
    fun upsert(point: EnforcementPoint) {
        val next = PointCodec.merging(localFlow.value, listOf(point))
        localFlow.value = next
        write(localFile, next)
    }

    @Synchronized
    fun delete(id: String) {
        val next = localFlow.value.filterNot { it.id == id }
        localFlow.value = next
        write(localFile, next)
    }

    @Synchronized
    fun importText(text: String): Int {
        val imported = when {
            text.trimStart().startsWith("{") || text.contains("\"points\"") -> PointCodec.decodeJSON(text)
            text.contains("<gpx") || text.contains("<wpt") -> PointCodec.decodeGPX(text)
            else -> throw PointCodecException("invalid")
        }
        val next = PointCodec.merging(localFlow.value, imported)
        localFlow.value = next
        write(localFile, next)
        return imported.size
    }

    fun exportJSON(includeCommunity: Boolean): String {
        val points = if (includeCommunity) alertPoints() else localFlow.value
        return PointCodec.encodeJSON(points)
    }

    fun exportGPX(includeCommunity: Boolean): String {
        val points = if (includeCommunity) alertPoints() else localFlow.value
        return PointCodec.encodeGPX(points)
    }

    @Synchronized
    fun applyCommunity(points: List<EnforcementPoint>, removedIds: List<String>) {
        val removed = removedIds.toSet()
        val next = points.filterNot { it.id in removed }
        communityFlow.value = next
        write(communityFile, next)
    }

    private fun read(file: File): List<EnforcementPoint> {
        if (!file.exists()) return emptyList()
        return runCatching { PointCodec.decodeJSON(file.readText()) }.getOrDefault(emptyList())
    }

    private fun write(file: File, points: List<EnforcementPoint>) {
        file.parentFile?.mkdirs()
        file.writeText(PointCodec.encodeJSON(points))
    }
}
