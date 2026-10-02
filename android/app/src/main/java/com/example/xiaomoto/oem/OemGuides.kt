package com.example.xiaomoto.oem

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.PowerManager
import android.provider.Settings

object OemGuides {
    fun batteryIntent(context: Context): Intent {
        return Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS).apply {
            data = Uri.parse("package:${context.packageName}")
        }
    }

    fun batteryListIntent(): Intent = Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)

    fun appDetails(context: Context): Intent {
        return Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS).apply {
            data = Uri.fromParts("package", context.packageName, null)
        }
    }

    fun isIgnoringBattery(context: Context): Boolean {
        val manager = context.getSystemService(PowerManager::class.java)
        return manager.isIgnoringBatteryOptimizations(context.packageName)
    }

    fun openStartupManager(context: Context): Boolean {
        val candidates = listOf(
            ComponentName("com.huawei.systemmanager", "com.huawei.systemmanager.startupmgr.ui.StartupNormalAppListActivity"),
            ComponentName("com.huawei.systemmanager", "com.huawei.systemmanager.optimize.process.ProtectActivity"),
            ComponentName("com.huawei.systemmanager", "com.huawei.systemmanager.appcontrol.activity.StartupAppControlActivity"),
            ComponentName("com.hihonor.systemmanager", "com.hihonor.systemmanager.startupmgr.ui.StartupNormalAppListActivity"),
            ComponentName("com.hihonor.systemmanager", "com.hihonor.systemmanager.optimize.process.ProtectActivity"),
            ComponentName("com.hihonor.systemmanager", "com.hihonor.systemmanager.appcontrol.activity.StartupAppControlActivity"),
        )
        for (component in candidates) {
            val intent = Intent().setComponent(component).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            try {
                context.startActivity(intent)
                return true
            } catch (_: Exception) {
                continue
            }
        }
        return false
    }

    const val startupInstructions = "没有自动打开手机管家。请手动进入：手机管家 → 应用启动管理（或 启动管理）→ 找到「小moto安全驾驶指南」→ 关闭自动管理，打开允许自启动、允许关联启动、允许后台活动。"

    const val locationInstructions = "定位请选「始终允许」。路径：设置 → 应用和服务 → 权限 → 位置信息 → 小moto安全驾驶指南 → 始终允许。只选「使用期间」时，锁屏后不会再算距离。"

    const val alertChannelInstructions = "请给「违法点提醒」打开「横幅通知」和「锁屏通知」。没打开的话，锁屏时按钮不会直接出现。"

    const val fullScreenInstructions = "还要允许全屏通知，锁屏时才会直接弹出带「我知道了」和「不再提醒」的卡片。"

    const val pureModeInstructions = "鸿蒙纯净模式会拦截侧载安装。安装时若提示风险，点开详情后选择仍要安装；或到 设置 → 系统和更新 → 纯净模式 关闭后再装。"
}
