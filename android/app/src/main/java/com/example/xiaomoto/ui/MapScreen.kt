package com.example.xiaomoto.ui

import android.Manifest
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Paint
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Bundle
import android.os.Looper
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.ui.graphics.Color
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
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
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import com.amap.api.maps.AMapOptions
import com.amap.api.maps.CameraUpdateFactory
import com.amap.api.maps.MapView
import com.amap.api.maps.MapsInitializer
import com.amap.api.maps.model.BitmapDescriptorFactory
import com.amap.api.maps.model.CameraPosition
import com.amap.api.maps.model.LatLng
import com.amap.api.maps.model.LatLngBounds
import com.amap.api.maps.model.MarkerOptions
import com.example.xiaomoto.MotoApplication
import com.example.xiaomoto.core.AlertDistance
import com.example.xiaomoto.core.EnforcementPoint
import com.example.xiaomoto.core.GeoMath
import com.example.xiaomoto.core.SyncNotice
import com.example.xiaomoto.core.PlacementStatus
import com.example.xiaomoto.core.penaltyLabel
import com.example.xiaomoto.core.roadLabel
import com.example.xiaomoto.core.PointType
import com.example.xiaomoto.map.MapDatum

private object MapCameraMemory {
    var latitude: Double? = null
    var longitude: Double? = null
    var zoom: Float = 4.2f
}

@Composable
fun MapScreen() {
    val app = MotoApplication.instance
    val context = LocalContext.current
    val local by app.points.local.collectAsStateCompat()
    val community by app.points.community.collectAsStateCompat()
    val privacy by app.settings.amapPrivacyAgreed.collectAsStateCompat()
    val outerMeters by app.settings.outerMeters.collectAsStateCompat()
    val mapKey = app.settings.effectiveAndroidMapKey()
    var user by remember { mutableStateOf<Location?>(null) }
    var coarse by remember { mutableStateOf(false) }
    var editor by remember { mutableStateOf<EnforcementPoint?>(null) }
    var detail by remember { mutableStateOf<EnforcementPoint?>(null) }
    var showSearch by remember { mutableStateOf(false) }
    var searchSeed by remember { mutableStateOf("") }
    var showTicket by remember { mutableStateOf(false) }
    var upload by remember { mutableStateOf<EnforcementPoint?>(null) }
    var status by remember { mutableStateOf<String?>(null) }
    var mapView by remember { mutableStateOf<MapView?>(null) }
    var wantCenter by remember { mutableStateOf(false) }
    var askPrivacy by remember { mutableStateOf(!privacy) }
    var uploading by remember { mutableStateOf(false) }
    val review by app.placement.review.collectAsStateCompat()

    var locationGate by remember { mutableIntStateOf(0) }
    val permission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {
        locationGate += 1
        if (it) wantCenter = true
    }

    fun center(location: Location) {
        val map = mapView?.map ?: return
        frameMap(map, location.latitude, location.longitude, 15_000.0)
        wantCenter = false
        if (coarse || location.provider == LocationManager.NETWORK_PROVIDER) {
            status = "卫星定位没开，这是网络定位，精度较差。"
        }
    }

    LaunchedEffect(Unit) {
        val granted = ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
        if (!granted) {
            wantCenter = true
            permission.launch(Manifest.permission.ACCESS_FINE_LOCATION)
        } else {
            wantCenter = true
        }
    }

    LaunchedEffect(user, wantCenter, mapView) {
        val location = user
        if (wantCenter && location != null && mapView?.map != null) center(location)
    }

    LaunchedEffect(Unit) {
        if (LaunchSync.done) return@LaunchedEffect
        LaunchSync.done = true
        refresh(app, context, announce = false, latitude = null, longitude = null) { status = it }
    }

    LaunchedEffect(review) {
        if (review.isNotEmpty()) {
            val point = review.first()
            mapView?.map?.let { frameMap(it, point.latitude, point.longitude, 3_000.0) }
        }
    }

    DisposableEffect(locationGate) {
        val manager = context.getSystemService(LocationManager::class.java)
        val listener = LocationListener { location ->
            coarse = location.provider == LocationManager.NETWORK_PROVIDER
            user = location
        }
        val granted = ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
        if (granted) {
            val gpsOn = runCatching { manager.isProviderEnabled(LocationManager.GPS_PROVIDER) }.getOrDefault(false)
            val networkOn = runCatching { manager.isProviderEnabled(LocationManager.NETWORK_PROVIDER) }.getOrDefault(false)
            val provider = when {
                gpsOn -> LocationManager.GPS_PROVIDER
                networkOn -> LocationManager.NETWORK_PROVIDER
                else -> null
            }
            if (provider != null) {
                runCatching {
                    manager.requestLocationUpdates(provider, 2_000L, 5f, listener, Looper.getMainLooper())
                    manager.getLastKnownLocation(provider)?.let { listener.onLocationChanged(it) }
                }
            }
        }
        onDispose { runCatching { manager.removeUpdates(listener) } }
    }

    Box(Modifier.fillMaxSize()) {
        val hereForMap = user
        val shown = (local + community + review).filter { it.located }.distinctBy { it.id }
        val nearby = if (hereForMap == null) {
            emptyList()
        } else {
            (local + community).filter { point ->
                GeoMath.distanceMeters(hereForMap.latitude, hereForMap.longitude, point.latitude, point.longitude) <=
                    AlertDistance.nearbyRadius(outerMeters)
            }
        }
        if (privacy) {
            AmapCanvas(
                mapKey = mapKey,
                points = shown,
                user = user,
                onMap = { mapView = it },
                onMarker = { detail = it },
                onMapClick = null,
            )
        }
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Button(onClick = { showTicket = true }, modifier = Modifier.fillMaxWidth()) { Text("违法点上传") }
            Row(
                Modifier.horizontalScroll(rememberScrollState()),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Button(onClick = { showSearch = true }) { Text("搜索") }
                Button(onClick = {
                    val granted = ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
                    if (!granted) {
                        wantCenter = true
                        permission.launch(Manifest.permission.ACCESS_FINE_LOCATION)
                        return@Button
                    }
                    val known = user ?: lastLocation(context)?.also { (location, network) ->
                        coarse = network
                        user = location
                    }?.first
                    if (known == null) {
                        wantCenter = true
                        status = if (gpsOrNetworkEnabled(context)) "正在定位…" else "请先打开系统定位。"
                    } else {
                        wantCenter = true
                        center(known)
                    }
                }) { Text("当前位置") }
                Button(onClick = {
                    val known = user ?: lastLocation(context)?.first
                    refresh(app, context, announce = true, known?.latitude, known?.longitude) { status = it }
                }) { Text("更新") }
            }
            if (!privacy) {
                Card {
                    Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text("地图还没打开。需要先同意高德地图隐私说明。")
                        Button(onClick = { askPrivacy = true }) { Text("查看说明") }
                    }
                }
            } else if (mapKey.isBlank()) {
                Card {
                    Text(
                        "还没有高德 Android 平台 Key，底图会鉴权失败。包名 com.example.xiaomoto。Web 服务 Key 不能用来显示地图。",
                        Modifier.padding(12.dp),
                    )
                }
            }
            val nearest = if (hereForMap == null || nearby.isEmpty()) {
                null
            } else {
                val point = nearby.minBy { GeoMath.distanceMeters(hereForMap.latitude, hereForMap.longitude, it.latitude, it.longitude) }
                point to GeoMath.distanceMeters(hereForMap.latitude, hereForMap.longitude, point.latitude, point.longitude)
            }
            if (nearest != null) {
                Card {
                    Text("${nearest.first.name} · ${formatMeters(nearest.second)}", Modifier.padding(12.dp))
                }
            }
            status?.let {
                Card(
                    colors = CardDefaults.cardColors(
                        containerColor = Color(0xFFF7F4EE),
                        contentColor = Color(0xFF1A1814),
                    ),
                ) {
                    Text(
                        it,
                        Modifier.padding(12.dp),
                        style = MaterialTheme.typography.bodyMedium,
                        color = Color(0xFF1A1814),
                    )
                }
            }
        }
        if (review.isNotEmpty()) {
            Card(Modifier.align(Alignment.BottomCenter).padding(12.dp)) {
                Column(Modifier.padding(12.dp).heightIn(max = 280.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("请核对", style = MaterialTheme.typography.titleMedium)
                    review.forEach { point ->
                        Column {
                            Text(point.name, style = MaterialTheme.typography.titleSmall)
                            if (point.amapName.isNotBlank()) Text(PlacementStatus.amapMatch(point.amapName))
                            Text("道路 ${roadLabel(point)}")
                            Text(point.violation.ifBlank { "未识别到违法行为" })
                            Text(penaltyLabel(point))
                        }
                    }
                    Button(
                        onClick = {
                            uploading = true
                            val batch = review
                            val main = android.os.Handler(Looper.getMainLooper())
                            Thread {
                                var uploaded = 0
                                val image = app.placement.reviewImage
                                batch.forEach { point ->
                                    if (app.uploads.submitOrQueue(point, image)) uploaded += 1
                                }
                                main.post {
                                    app.placement.reviewImage = null
                                    app.placement.review.value = emptyList()
                                    status = PlacementStatus.sentence(batch.size, uploaded)
                                    uploading = false
                                }
                            }.start()
                        },
                        enabled = !uploading,
                        modifier = Modifier.fillMaxWidth(),
                    ) { Text(if (uploading) "正在上传…" else "上传") }
                    TextButton(onClick = { app.placement.review.value = emptyList() }, enabled = !uploading) { Text("暂不上传") }
                }
            }
        }
    }

    if (askPrivacy && !privacy) {
        AlertDialog(
            onDismissRequest = { askPrivacy = false },
            title = { Text("高德地图隐私提示") },
            text = {
                Text("地图由高德提供。高德地图 SDK 会读取设备信息、位置和网络，用来显示底图。点位仍保存在本机，按 WGS-84 存储，画到地图上时转成 GCJ-02。本应用不把点位上传给高德。")
            },
            confirmButton = {
                TextButton(onClick = {
                    app.settings.setAmapPrivacyAgreed(true)
                    askPrivacy = false
                }) { Text("同意并显示地图") }
            },
            dismissButton = {
                TextButton(onClick = { askPrivacy = false }) { Text("暂不使用地图") }
            },
        )
    }

    editor?.let { draft ->
        PointEditorDialog(draft, onDismiss = { editor = null }) { saved ->
            app.points.upsert(saved)
            editor = null
        }
    }
    detail?.let { point ->
        DetailDialog(
            point = point,
            onDismiss = { detail = null },
            onEdit = { editor = point; detail = null },
            onDelete = { app.points.delete(point.id); detail = null },
            onUpload = { upload = point; detail = null },
            onSaveLocal = {
                if (point.isCommunity) {
                    app.points.upsert(
                        point.copy(
                            id = java.util.UUID.randomUUID().toString(),
                            isCommunity = false,
                            source = com.example.xiaomoto.core.PointSource.COMMUNITY,
                        ),
                    )
                }
                detail = null
            },
        )
    }
    if (showSearch) {
        SearchDialog(searchSeed, onDismiss = { showSearch = false }) { point ->
            app.points.upsert(point)
            showSearch = false
        }
    }
    if (showTicket) {
        TicketDialog(onDismiss = { showTicket = false }) { records, jpeg ->
            showTicket = false
            app.placement.reviewImage = jpeg
            status = "正在标注…"
            val main = android.os.Handler(Looper.getMainLooper())
            Thread {
                val batch = TicketPlacer.place(app, records, jpeg)
                main.post {
                    app.placement.review.value = batch.placed
                    status = batch.notice
                }
            }.start()
        }
    }
    upload?.let { point ->
        UploadDialog(point, onDismiss = { upload = null }) { message ->
            status = message
            upload = null
        }
    }
}

@Composable
private fun AmapCanvas(
    mapKey: String,
    points: List<EnforcementPoint>,
    user: Location?,
    onMap: (MapView) -> Unit,
    onMarker: (EnforcementPoint) -> Unit,
    onMapClick: ((com.amap.api.maps.model.LatLng) -> Unit)?,
) {
    val context = LocalContext.current
    val view = remember(mapKey) {
        MapsInitializer.updatePrivacyShow(context, true, true)
        MapsInitializer.updatePrivacyAgree(context, true)
        if (mapKey.isNotBlank()) MapsInitializer.setApiKey(mapKey)
        val savedLat = MapCameraMemory.latitude
        val savedLon = MapCameraMemory.longitude
        val start = if (savedLat != null && savedLon != null) {
            LatLng(savedLat, savedLon)
        } else {
            val china = MapDatum.toMap(35.0, 104.0)
            LatLng(china.latitude, china.longitude)
        }
        val options = AMapOptions().camera(CameraPosition.fromLatLngZoom(start, MapCameraMemory.zoom))
        MapView(context, options).apply { onCreate(Bundle()) }
    }
    DisposableEffect(view) {
        view.onResume()
        onMap(view)
        onDispose {
            runCatching {
                val camera = view.map?.cameraPosition
                if (camera != null) {
                    MapCameraMemory.latitude = camera.target.latitude
                    MapCameraMemory.longitude = camera.target.longitude
                    MapCameraMemory.zoom = camera.zoom
                }
            }
            view.onPause()
            view.onDestroy()
        }
    }
    AndroidView(
        modifier = Modifier.fillMaxSize(),
        factory = { view },
        update = { mapView ->
            val map = mapView.map ?: return@AndroidView
            map.uiSettings.isMyLocationButtonEnabled = false
            map.uiSettings.isZoomGesturesEnabled = true
            map.uiSettings.isScrollGesturesEnabled = true
            map.setOnMapLongClickListener(null)
            if (onMapClick == null) map.setOnMapClickListener(null) else map.setOnMapClickListener { onMapClick(it) }
            map.setOnMarkerClickListener { marker ->
                val id = marker.`object` as? String ?: return@setOnMarkerClickListener true
                points.firstOrNull { it.id == id }?.let(onMarker)
                true
            }
            map.clear()
            points.forEach { point ->
                val shown = MapDatum.toMap(point.latitude, point.longitude)
                map.addMarker(
                    MarkerOptions()
                        .position(LatLng(shown.latitude, shown.longitude))
                        .title(if (point.uploadSource == "user_upload") "${point.name} 用户上传" else point.name)
                        .anchor(0.5f, 0.5f)
                        .draggable(false)
                        .icon(BitmapDescriptorFactory.fromBitmap(pinBitmap(if (point.isCommunity) 0xFF00897B.toInt() else color(point.type), point.isCommunity))),
                )?.setObject(point.id)
            }
            user?.let { location ->
                val shown = MapDatum.toMap(location.latitude, location.longitude)
                map.addMarker(
                    MarkerOptions()
                        .position(LatLng(shown.latitude, shown.longitude))
                        .title("我的位置")
                        .anchor(0.5f, 0.5f)
                        .icon(BitmapDescriptorFactory.fromBitmap(pinBitmap(0xFF1E88E5.toInt(), false))),
                )
            }
        },
    )
}

private object LaunchSync {
    var done = false
}

private fun refresh(
    app: MotoApplication,
    context: android.content.Context,
    announce: Boolean,
    latitude: Double?,
    longitude: Double?,
    onStatus: (String) -> Unit,
) {
    if (announce) onStatus("正在更新…")
    val main = android.os.Handler(Looper.getMainLooper())
    fun publish(text: String, toast: Boolean) {
        main.post {
            onStatus(text)
            if (toast) Toast.makeText(context, text, Toast.LENGTH_LONG).show()
        }
    }
    Thread {
        app.uploads.flush()
        val base = app.settings.communityBaseUrl.value.trim()
        if (base.isEmpty()) {
            if (announce) publish(SyncNotice.noServer, true)
            return@Thread
        }
        try {
            val sync = com.example.xiaomoto.net.CommunityClient.sync(base)
            app.points.applyCommunity(sync.points, sync.removedIds)
            val count = app.points.community.value.size
            val radius = AlertDistance.nearbyRadius(app.settings.outerMeters.value)
            val nearby = if (latitude != null && longitude != null) {
                (app.points.local.value + app.points.community.value).count { point ->
                    GeoMath.distanceMeters(latitude, longitude, point.latitude, point.longitude) <= radius
                }
            } else {
                null
            }
            publish(SyncNotice.success(count, nearby, AlertDistance.nearbyKilometers(app.settings.outerMeters.value)), announce)
        } catch (error: Exception) {
            if (announce) publish(SyncNotice.failure(shortSyncReason(error)), true)
        }
    }.start()
}

private fun shortSyncReason(error: Exception): String {
    val raw = error.message?.trim().orEmpty()
    if (raw.any { it.code > 127 }) return raw.substringBefore('。').take(24)
    return when (error) {
        is java.net.UnknownHostException -> "连不上服务器"
        is java.net.SocketTimeoutException -> "连接超时"
        else -> "网络不通"
    }
}

private fun lastLocation(context: android.content.Context): Pair<Location, Boolean>? {
    if (ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_FINE_LOCATION) != PackageManager.PERMISSION_GRANTED) return null
    val manager = context.getSystemService(LocationManager::class.java)
    val gps = runCatching {
        if (manager.isProviderEnabled(LocationManager.GPS_PROVIDER)) manager.getLastKnownLocation(LocationManager.GPS_PROVIDER) else null
    }.getOrNull()
    if (gps != null) return gps to false
    val network = runCatching {
        if (manager.isProviderEnabled(LocationManager.NETWORK_PROVIDER)) manager.getLastKnownLocation(LocationManager.NETWORK_PROVIDER) else null
    }.getOrNull()
    if (network != null) return network to true
    return null
}

private fun gpsOrNetworkEnabled(context: android.content.Context): Boolean {
    val manager = context.getSystemService(LocationManager::class.java)
    val gps = runCatching { manager.isProviderEnabled(LocationManager.GPS_PROVIDER) }.getOrDefault(false)
    val network = runCatching { manager.isProviderEnabled(LocationManager.NETWORK_PROVIDER) }.getOrDefault(false)
    return gps || network
}

private fun color(type: PointType): Int = when (type) {
    PointType.BANNED_TOLL -> 0xFFE53935.toInt()
    PointType.BANNED_SEGMENT -> 0xFFFB8C00.toInt()
    PointType.CAMERA -> 0xFF8E24AA.toInt()
    PointType.OTHER -> 0xFF757575.toInt()
}

private fun pinBitmap(color: Int, community: Boolean): Bitmap {
    val bitmap = Bitmap.createBitmap(72, 72, Bitmap.Config.ARGB_8888)
    val canvas = Canvas(bitmap)
    val paint = Paint(Paint.ANTI_ALIAS_FLAG)
    paint.color = color
    canvas.drawCircle(36f, 36f, 28f, paint)
    if (community) {
        paint.color = 0xFFFFFFFF.toInt()
        paint.textSize = 28f
        paint.textAlign = Paint.Align.CENTER
        canvas.drawText("社", 36f, 46f, paint)
    }
    return bitmap
}

internal fun formatMeters(meters: Double): String {
    return if (meters >= 1000) "约 %.1f 公里".format(meters / 1000) else "约 ${meters.toInt()} 米"
}

private fun frameMap(map: com.amap.api.maps.AMap, latitude: Double, longitude: Double, radiusMeters: Double) {
    val bounds = radiusBounds(latitude, longitude, radiusMeters)
    val moved = runCatching {
        map.moveCamera(CameraUpdateFactory.newLatLngBounds(bounds, 24))
    }.isSuccess
    if (!moved) {
        val shown = MapDatum.toMap(latitude, longitude)
        map.moveCamera(CameraUpdateFactory.newLatLngZoom(LatLng(shown.latitude, shown.longitude), zoomForRadius(latitude, radiusMeters)))
    }
}

private fun radiusBounds(latitude: Double, longitude: Double, radiusMeters: Double): LatLngBounds {
    val corners = listOf(
        GeoMath.offset(latitude, longitude, radiusMeters, 0.0),
        GeoMath.offset(latitude, longitude, -radiusMeters, 0.0),
        GeoMath.offset(latitude, longitude, 0.0, radiusMeters),
        GeoMath.offset(latitude, longitude, 0.0, -radiusMeters),
    )
    val builder = LatLngBounds.builder()
    corners.forEach { corner ->
        val shown = MapDatum.toMap(corner.latitude, corner.longitude)
        builder.include(LatLng(shown.latitude, shown.longitude))
    }
    return builder.build()
}

private fun zoomForRadius(latitude: Double, radiusMeters: Double): Float {
    val metersPerPixel = (radiusMeters * 2) / 900.0
    val cosine = kotlin.math.cos(latitude * Math.PI / 180.0).coerceAtLeast(0.2)
    val zoom = kotlin.math.ln(156543.03392 * cosine / metersPerPixel) / kotlin.math.ln(2.0)
    return zoom.toFloat().coerceIn(3f, 18f)
}
