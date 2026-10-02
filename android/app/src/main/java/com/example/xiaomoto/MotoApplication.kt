package com.example.xiaomoto

import android.app.Application
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import com.example.xiaomoto.core.EnforcementPoint
import com.example.xiaomoto.data.PointRepository
import com.example.xiaomoto.data.SessionStore
import com.example.xiaomoto.data.SettingsStore
import com.example.xiaomoto.data.UploadQueue
import com.example.xiaomoto.ride.RideService
import java.io.File
import kotlinx.coroutines.flow.MutableStateFlow

class MotoApplication : Application() {
    lateinit var settings: SettingsStore
        private set
    lateinit var session: SessionStore
        private set
    lateinit var points: PointRepository
        private set
    lateinit var uploads: UploadQueue
        private set
    val placement = PlacementHold()

    override fun onCreate() {
        super.onCreate()
        instance = this
        settings = SettingsStore(this)
        session = SessionStore(this)
        val dir = File(filesDir, "xiaomoto")
        dir.mkdirs()
        points = PointRepository(dir)
        uploads = UploadQueue(dir, settings, points)
        if (!session.token.value.isNullOrBlank()) {
            RideService.ensureRunning(this)
            val base = settings.communityBaseUrl.value
            Thread { session.refresh(base) }.start()
        }
        val connectivity = getSystemService(ConnectivityManager::class.java)
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .build()
        connectivity.registerNetworkCallback(request, object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                Thread { uploads.flush() }.start()
            }
        })
    }

    companion object {
        lateinit var instance: MotoApplication
            private set
    }
}

class PlacementHold {
    val review = MutableStateFlow<List<EnforcementPoint>>(emptyList())
    var reviewImage: ByteArray? = null
}
