package com.example.xiaomoto.ui

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Place
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.TwoWheeler
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import com.example.xiaomoto.core.AppIdentity
import com.example.xiaomoto.net.AppUpdateController
import com.example.xiaomoto.ride.RideService

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent { XiaoMotoTheme { AppScaffold() } }
    }

    override fun onResume() {
        super.onResume()
        if (com.example.xiaomoto.MotoApplication.instance.session.token.value.isNullOrBlank()) return
        AppUpdateController.onHostResume(this)
        RideService.ensureRunning(this)
    }
}

private data class Tab(val route: String, val label: String, val icon: androidx.compose.ui.graphics.vector.ImageVector)

@Composable
private fun AppScaffold() {
    val app = com.example.xiaomoto.MotoApplication.instance
    val token by app.session.token.collectAsStateCompat()
    val gate by app.session.gate.collectAsStateCompat()
    if (token.isNullOrBlank()) {
        LoginScreen()
    } else if (gate == com.example.xiaomoto.data.SessionGate.Terms) {
        TermsAcceptScreen()
    } else {
        MainScaffold()
    }
}

@Composable
private fun MainScaffold() {
    val nav = rememberNavController()
    val tabs = listOf(
        Tab("map", "地图", Icons.Filled.Place),
        Tab("ride", "骑行", Icons.Filled.TwoWheeler),
        Tab("settings", "设置", Icons.Filled.Settings),
    )
    val backStack by nav.currentBackStackEntryAsState()
    val current = backStack?.destination?.route ?: "map"
    Box(Modifier.fillMaxSize()) {
    Scaffold(
        bottomBar = {
            NavigationBar {
                tabs.forEach { tab ->
                    NavigationBarItem(
                        selected = current == tab.route,
                        onClick = {
                            nav.navigate(tab.route) {
                                popUpTo(nav.graph.findStartDestination().id) { saveState = true }
                                launchSingleTop = true
                                restoreState = true
                            }
                        },
                        icon = { Icon(tab.icon, contentDescription = tab.label) },
                        label = { Text(tab.label) },
                    )
                }
            }
        },
    ) { padding ->
        NavHost(nav, startDestination = "map", modifier = Modifier.padding(padding)) {
            composable("map") { MapScreen() }
            composable("ride") { RideScreen() }
            composable("settings") { SettingsScreen() }
        }
        OnboardingDialog()
        AppUpdateHost()
    }
    ForegroundAlertOverlay()
    }
}

@Composable
private fun OnboardingDialog() {
    val app = com.example.xiaomoto.MotoApplication.instance
    val seen by app.settings.onboardingSeen.collectAsStateCompat()
    if (seen) return
    androidx.compose.material3.AlertDialog(
        onDismissRequest = { app.settings.markOnboardingSeen() },
        title = { Text(AppIdentity.displayName) },
        text = {
            Text("标记禁摩收费站、禁摩路段和抓拍点。骑行时在大约 2 公里和 500 米处用语音、震动和通知提醒。定位只在本机算距离。地图不能长按添加。华为和荣耀请另外允许始终定位、关闭电池优化，并在手机管家里允许自启动。")
        },
        confirmButton = {
            androidx.compose.material3.TextButton(onClick = { app.settings.markOnboardingSeen() }) {
                Text("开始使用")
            }
        },
    )
}
