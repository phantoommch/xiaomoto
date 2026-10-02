package com.example.xiaomoto.ui

import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import com.example.xiaomoto.ride.ActiveAlert
import com.example.xiaomoto.ride.RideRuntime
import com.example.xiaomoto.ride.RideService

/** Lock-screen card launched by the alert's full-screen intent. Both buttons are on the card. */
class AlertCardActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O_MR1) {
            setShowWhenLocked(true)
            setTurnScreenOn(true)
        }
        val activity = this
        val fallback = ActiveAlert(
            id = intent.getStringExtra(RideService.EXTRA_TAG).orEmpty(),
            pointId = intent.getStringExtra(RideService.EXTRA_POINT_ID).orEmpty(),
            title = intent.getStringExtra(RideService.EXTRA_TITLE).orEmpty().ifBlank { "违法点" },
            text = intent.getStringExtra(RideService.EXTRA_TEXT).orEmpty(),
        )
        setContent {
            val live by RideRuntime.activeAlert.collectAsStateCompat()
            var seen by remember { mutableStateOf(live != null) }
            LaunchedEffect(live) {
                if (live != null) seen = true
                else if (seen) activity.finish()
            }
            val current = live ?: fallback.takeIf { it.id.isNotEmpty() }
            if (current == null) {
                LaunchedEffect(Unit) { activity.finish() }
                return@setContent
            }
            XiaoMotoTheme {
                AlertPromptScreen(
                    alert = current,
                    onAck = {
                        RideRuntime.acknowledge(activity, current.id)
                        activity.finish()
                    },
                    onMute = {
                        RideRuntime.mute(activity, current.pointId, current.id)
                        activity.finish()
                    },
                )
            }
        }
    }
}
