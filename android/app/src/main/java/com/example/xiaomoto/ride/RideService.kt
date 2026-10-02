package com.example.xiaomoto.ride

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.media.AudioAttributes
import android.media.MediaPlayer
import android.net.Uri
import android.os.Build
import android.os.IBinder
import android.os.Looper
import android.os.VibrationEffect
import android.os.Vibrator
import android.os.VibratorManager
import android.provider.Settings
import android.speech.tts.TextToSpeech
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import com.example.xiaomoto.R
import com.example.xiaomoto.MotoApplication
import com.example.xiaomoto.core.AlertBand
import com.example.xiaomoto.core.AlertEngine
import com.example.xiaomoto.core.AppIdentity
import com.example.xiaomoto.core.ApproachRecord
import com.example.xiaomoto.core.PendingAlert
import com.example.xiaomoto.ui.MainActivity
import java.time.Instant
import java.util.Locale
import kotlinx.coroutines.flow.MutableStateFlow

data class RideNotice(val at: Instant, val text: String)

data class ActiveAlert(
    val id: String,
    val pointId: String,
    val title: String,
    val text: String,
)

object RideRuntime {
    val riding = MutableStateFlow(false)
    val lastError = MutableStateFlow<String?>(null)
    val recent = MutableStateFlow<List<RideNotice>>(emptyList())
    val chineseTts = MutableStateFlow<Boolean?>(null)
    val lastLocation = MutableStateFlow<Location?>(null)
    val activeAlert = MutableStateFlow<ActiveAlert?>(null)

    fun dismiss(id: String) {
        if (activeAlert.value?.id == id) activeAlert.value = null
    }

    fun acknowledge(context: android.content.Context, id: String) {
        dismiss(id)
        RideService.cancelAlert(context, id)
    }

    fun mute(context: android.content.Context, pointId: String, id: String) {
        if (pointId.isNotEmpty()) runCatching { MotoApplication.instance.settings.mute(pointId) }
        if (activeAlert.value?.id == id || activeAlert.value?.pointId == pointId) activeAlert.value = null
        RideService.cancelAlert(context, id)
    }
}

class RideService : Service(), TextToSpeech.OnInitListener, LocationListener {
    private var tts: TextToSpeech? = null
    private var chineseReady = false
    private var player: MediaPlayer? = null
    private val approach = mutableMapOf<String, ApproachRecord>()
    private var closestMeters = Double.POSITIVE_INFINITY
    private lateinit var locations: LocationManager

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        running = true
        locations = getSystemService(LocationManager::class.java)
        prepareNotifications(this)
        approach.clear()
        approach.putAll(MotoApplication.instance.settings.loadApproach())
        tts = TextToSpeech(this, this)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        running = true
        getSystemService(NotificationManager::class.java).cancel(NOTE_RECOVERY)
        startForeground(NOTE_RIDE, rideNotification("正在用 GPS 监听前方禁摩点"), ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION)
        if (intent?.action == ACTION_STOP) {
            userStopped = true
            shutdown()
            return START_NOT_STICKY
        }
        userStopped = false
        beginUpdates()
        return START_STICKY
    }

    override fun onInit(status: Int) {
        val engine = tts
        val ready = status == TextToSpeech.SUCCESS && engine != null && chineseAvailable(engine)
        chineseReady = ready
        RideRuntime.chineseTts.value = ready
        if (ready) {
            engine?.setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_ASSISTANCE_NAVIGATION_GUIDANCE)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH)
                    .build(),
            )
        }
    }

    override fun onLocationChanged(location: Location) {
        RideRuntime.lastLocation.value = location
        val app = MotoApplication.instance
        val heading = if (location.hasBearing() && location.bearing >= 0f) location.bearing.toDouble() else null
        val speed = if (location.hasSpeed()) location.speed.toDouble() else null
        val result = AlertEngine.evaluate(
            latitude = location.latitude,
            longitude = location.longitude,
            headingDegrees = heading,
            speedMps = speed,
            points = app.points.alertPoints(),
            settings = app.settings.alertSettings(),
            approach = approach,
            mutedIds = app.settings.mutedIds.value,
        )
        approach.clear()
        approach.putAll(result.second)
        app.settings.saveApproach(approach)
        for (alert in result.first) deliver(alert)
        dismissIfPassed(location, app.points.alertPoints())
    }

    override fun onTaskRemoved(rootIntent: Intent?) {
        // Swiping the app away should not end an active ride.
    }

    override fun onDestroy() {
        running = false
        if (::locations.isInitialized) locations.removeUpdates(this)
        tts?.shutdown()
        player?.release()
        runCatching { MotoApplication.instance.settings.saveApproach(approach) }
        val keepRiding = !userStopped && runCatching { MotoApplication.instance.settings.ridingEnabled.value }.getOrDefault(false)
        if (keepRiding) postRecovery(this)
        RideRuntime.riding.value = false
        super.onDestroy()
    }

    private fun beginUpdates() {
        RideRuntime.lastError.value = null
        if (!locations.isProviderEnabled(LocationManager.GPS_PROVIDER)) {
            RideRuntime.lastError.value = "GPS 没有打开。请在系统定位里打开 GPS，华为/荣耀不要只用网络定位。"
        }
        val fine = checkSelfPermission(android.Manifest.permission.ACCESS_FINE_LOCATION) == android.content.pm.PackageManager.PERMISSION_GRANTED
        if (!fine) {
            RideRuntime.lastError.value = "没有定位权限，骑行提醒已停止。"
            userStopped = true
            shutdown()
            return
        }
        MotoApplication.instance.settings.setRiding(true)
        RideRuntime.riding.value = true
        locations.requestLocationUpdates(LocationManager.GPS_PROVIDER, 2_000L, 30f, this, Looper.getMainLooper())
    }

    private fun deliver(alert: PendingAlert) {
        val title = AlertEngine.alertTitle(alert.point)
        val tag = AlertEngine.stampKey(alert.point.id, alert.band)
        val text = alert.noticeText
        val shortText = AlertEngine.shortNotice(alert.distanceMeters)
        closestMeters = alert.distanceMeters
        RideRuntime.activeAlert.value = ActiveAlert(tag, alert.point.id, title, text)
        val next = listOf(RideNotice(Instant.now(), text)) + RideRuntime.recent.value
        RideRuntime.recent.value = next.take(20)
        vibrate()
        if (chineseReady) {
            tts?.speak(alert.spokenText, TextToSpeech.QUEUE_ADD, null, java.util.UUID.randomUUID().toString())
        } else {
            playClip(alert.band)
        }
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val ack = actionIntent(ACTION_ACK, alert.point.id, tag, 0x51)
        val mute = actionIntent(ACTION_MUTE, alert.point.id, tag, 0x52)
        val card = PendingIntent.getActivity(
            this,
            noteId(tag),
            Intent(this, com.example.xiaomoto.ui.AlertCardActivity::class.java)
                .putExtra(EXTRA_POINT_ID, alert.point.id)
                .putExtra(EXTRA_TAG, tag)
                .putExtra(EXTRA_TITLE, title)
                .putExtra(EXTRA_TEXT, text)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val builder = NotificationCompat.Builder(this, CHANNEL_ALERT)
            .setSmallIcon(R.drawable.ic_stat)
            .setContentTitle(title)
            .setContentText(shortText)
            .setStyle(NotificationCompat.BigTextStyle().bigText(text).setBigContentTitle(title))
            .setPriority(NotificationCompat.PRIORITY_MAX)
            .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
            .setCategory(NotificationCompat.CATEGORY_ALARM)
            .setAutoCancel(true)
            .setTimeoutAfter(120_000)
            .setContentIntent(open)
            .setOnlyAlertOnce(false)
            .addAction(0, "我知道了", ack)
            .addAction(0, "不再提醒", mute)
        if (canUseFullScreenIntent(this)) {
            builder.setFullScreenIntent(card, true)
        }
        getSystemService(NotificationManager::class.java).notify(tag, noteId(tag), builder.build())
        getSystemService(NotificationManager::class.java).notify(NOTE_RIDE, rideNotification(shortText))
    }

    private fun dismissIfPassed(location: Location, points: List<com.example.xiaomoto.core.EnforcementPoint>) {
        val active = RideRuntime.activeAlert.value ?: return
        val point = points.find { it.id == active.pointId } ?: return
        val current = com.example.xiaomoto.core.GeoMath.distanceMeters(
            location.latitude, location.longitude, point.latitude, point.longitude,
        )
        if (!current.isFinite()) return
        if (current < closestMeters) closestMeters = current
        val heading = if (location.hasBearing() && location.bearing >= 0f) location.bearing.toDouble() else null
        val speed = if (location.hasSpeed()) location.speed.toDouble() else null
        if (!AlertEngine.hasPassed(location.latitude, location.longitude, heading, speed, point, closestMeters, current)) return
        RideRuntime.activeAlert.value = null
        closestMeters = Double.POSITIVE_INFINITY
        cancelAlert(this, active.id)
    }

    private fun actionIntent(action: String, pointId: String, tag: String, salt: Int): PendingIntent {
        val intent = Intent(this, AlertActionReceiver::class.java)
            .setAction(action)
            .putExtra(EXTRA_POINT_ID, pointId)
            .putExtra(EXTRA_TAG, tag)
        val code = (tag.hashCode() xor salt) and 0x7fffffff
        return PendingIntent.getBroadcast(
            this,
            if (code == 0) salt else code,
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }

    private fun playClip(band: AlertBand) {
        player?.release()
        val created = MediaPlayer.create(this, clipId(band)) ?: return
        created.setAudioAttributes(
            AudioAttributes.Builder()
                .setUsage(AudioAttributes.USAGE_ASSISTANCE_NAVIGATION_GUIDANCE)
                .setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION)
                .build(),
        )
        created.setOnCompletionListener { it.release() }
        player = created
        created.start()
    }

    private fun clipId(band: AlertBand): Int = if (band == AlertBand.INNER) R.raw.alert_inner else R.raw.alert_outer

    private fun vibrate() {
        val effect = VibrationEffect.createWaveform(longArrayOf(0, 180, 80, 180), -1)
        if (Build.VERSION.SDK_INT >= 31) {
            getSystemService(VibratorManager::class.java).defaultVibrator.vibrate(effect)
        } else {
            @Suppress("DEPRECATION")
            (getSystemService(Context.VIBRATOR_SERVICE) as Vibrator).vibrate(effect)
        }
    }

    private fun shutdown() {
        MotoApplication.instance.settings.setRiding(false)
        RideRuntime.riding.value = false
        RideRuntime.activeAlert.value = null
        if (::locations.isInitialized) locations.removeUpdates(this)
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    private fun rideNotification(text: String): Notification {
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val stop = PendingIntent.getService(
            this, 1, Intent(this, RideService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        return NotificationCompat.Builder(this, CHANNEL_RIDE)
            .setSmallIcon(R.drawable.ic_stat)
            .setContentTitle(AppIdentity.displayName)
            .setContentText(text)
            .setOngoing(true)
            .setContentIntent(open)
            .addAction(0, "结束骑行", stop)
            .build()
    }

    companion object {
        const val ACTION_STOP = "com.example.xiaomoto.STOP_RIDE"
        const val ACTION_ACK = "com.example.xiaomoto.ALERT_ACK"
        const val ACTION_MUTE = "com.example.xiaomoto.ALERT_MUTE"
        const val EXTRA_POINT_ID = "pointId"
        const val EXTRA_TAG = "tag"
        const val EXTRA_TITLE = "title"
        const val EXTRA_TEXT = "text"
        const val CHANNEL_ALERT = "alerts_v3"
        const val CHANNEL_ALERT_NAME = "违法点提醒"
        private const val CHANNEL_RIDE = "ride"
        private val LEGACY_ALERT_CHANNELS = listOf("alerts", "alerts_v2")
        private const val NOTE_RIDE = 7
        private const val NOTE_RECOVERY = 9

        @Volatile var running: Boolean = false
        @Volatile var userStopped: Boolean = false

        fun noteId(tag: String): Int {
            val mixed = tag.hashCode() and 0x7fffffff
            return if (mixed == 0) 1 else mixed
        }

        fun start(context: Context) {
            userStopped = false
            ContextCompat.startForegroundService(context, Intent(context, RideService::class.java))
        }

        fun stop(context: Context) {
            userStopped = true
            context.startService(Intent(context, RideService::class.java).setAction(ACTION_STOP))
        }

        fun ensureRunning(context: Context) {
            val riding = runCatching { MotoApplication.instance.settings.ridingEnabled.value }.getOrDefault(false)
            if (!riding || running) return
            runCatching { start(context) }
        }

        fun cancelAlert(context: Context, tag: String) {
            context.getSystemService(NotificationManager::class.java).cancel(tag, noteId(tag))
        }

        fun prepareNotifications(context: Context) {
            val manager = context.getSystemService(NotificationManager::class.java)
            val ride = NotificationChannel(CHANNEL_RIDE, "骑行中", NotificationManager.IMPORTANCE_LOW)
            ride.description = "锁屏后仍在监听前方禁摩点"
            val sound = Uri.parse("android.resource://${context.packageName}/${R.raw.alert_outer}")
            val audio = AudioAttributes.Builder()
                .setUsage(AudioAttributes.USAGE_NOTIFICATION)
                .setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION)
                .build()
            val alert = NotificationChannel(CHANNEL_ALERT, CHANNEL_ALERT_NAME, NotificationManager.IMPORTANCE_HIGH)
            alert.description = "靠近违法点时弹出，锁屏也能看到两个按钮"
            alert.lockscreenVisibility = Notification.VISIBILITY_PUBLIC
            alert.enableVibration(true)
            alert.vibrationPattern = longArrayOf(0, 180, 80, 180)
            alert.setSound(sound, audio)
            manager.createNotificationChannel(ride)
            manager.createNotificationChannel(alert)
            for (legacy in LEGACY_ALERT_CHANNELS) manager.deleteNotificationChannel(legacy)
        }

        fun canUseFullScreenIntent(context: Context): Boolean {
            if (Build.VERSION.SDK_INT < 34) return true
            return context.getSystemService(NotificationManager::class.java).canUseFullScreenIntent()
        }

        fun openFullScreenSettings(context: Context) {
            if (Build.VERSION.SDK_INT < 34) return
            val intent = Intent(Settings.ACTION_MANAGE_APP_USE_FULL_SCREEN_INTENT).apply {
                data = Uri.parse("package:${context.packageName}")
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            runCatching { context.startActivity(intent) }
        }

        fun alertsAreProminent(context: Context): Boolean {
            val manager = context.getSystemService(NotificationManager::class.java)
            if (!manager.areNotificationsEnabled()) return false
            val channel = manager.getNotificationChannel(CHANNEL_ALERT) ?: return false
            if (channel.importance < NotificationManager.IMPORTANCE_HIGH) return false
            return channel.lockscreenVisibility != Notification.VISIBILITY_SECRET
        }

        fun openAlertSettings(context: Context) {
            val channel = Intent(Settings.ACTION_CHANNEL_NOTIFICATION_SETTINGS).apply {
                putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
                putExtra(Settings.EXTRA_CHANNEL_ID, CHANNEL_ALERT)
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            val opened = runCatching {
                context.startActivity(channel)
                true
            }.getOrDefault(false)
            if (opened) return
            val app = Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS).apply {
                putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            runCatching { context.startActivity(app) }
        }

        private fun postRecovery(context: Context) {
            prepareNotifications(context)
            val open = PendingIntent.getForegroundService(
                context,
                9,
                Intent(context, RideService::class.java),
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
            )
            val notification = NotificationCompat.Builder(context, CHANNEL_ALERT)
                .setSmallIcon(R.drawable.ic_stat)
                .setContentTitle(AppIdentity.displayName)
                .setContentText("骑行提醒被系统停止，点此恢复")
                .setPriority(NotificationCompat.PRIORITY_MAX)
                .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
                .setCategory(NotificationCompat.CATEGORY_ALARM)
                .setContentIntent(open)
                .setAutoCancel(true)
                .build()
            context.getSystemService(NotificationManager::class.java).notify(NOTE_RECOVERY, notification)
        }
    }
}

class AlertActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent?) {
        if (intent == null) return
        val pointId = intent.getStringExtra(RideService.EXTRA_POINT_ID).orEmpty()
        val tag = intent.getStringExtra(RideService.EXTRA_TAG).orEmpty()
        if (tag.isNotEmpty()) RideService.cancelAlert(context, tag)
        if (intent.action == RideService.ACTION_MUTE && pointId.isNotEmpty()) {
            runCatching { MotoApplication.instance.settings.mute(pointId) }
        }
        val active = RideRuntime.activeAlert.value
        if (active != null && (active.id == tag || (intent.action == RideService.ACTION_MUTE && active.pointId == pointId))) {
            RideRuntime.activeAlert.value = null
        }
    }
}

private fun chineseAvailable(engine: TextToSpeech): Boolean {
    val result = engine.setLanguage(Locale.SIMPLIFIED_CHINESE)
    if (result == TextToSpeech.LANG_MISSING_DATA || result == TextToSpeech.LANG_NOT_SUPPORTED) return false
    return result >= TextToSpeech.LANG_AVAILABLE
}
