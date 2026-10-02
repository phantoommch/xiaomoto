import SwiftUI

struct RideDashboardView: View {
    @Environment(SettingsStore.self) private var settings
    @EnvironmentObject private var ride: RideController

    var body: some View {
        NavigationStack {
            List {
                Section {
                    VStack(alignment: .leading, spacing: 8) {
                        Text(ride.isRiding ? "正在监听前方点位" : "骑行提醒未开启")
                            .font(.title3.weight(.semibold))
                        Text("靠近时每个圈提醒一次。离开这段路之后，下次再靠近还会提醒。点「不再提醒」的点位不再出声。锁屏时会弹出系统通知。")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                        Button(ride.isRiding ? "结束骑行" : "开始骑行") {
                            ride.toggleRide()
                        }
                        .buttonStyle(.borderedProminent)
                        .padding(.top, 4)
                    }
                    .padding(.vertical, 4)
                }
                Section("权限") {
                    LabeledContent("定位", value: ride.authorizationLabel)
                    LabeledContent("通知", value: ride.notificationsGranted ? "已允许" : "未确认")
                    Text("锁屏提醒的标题是点位名称，下面有「我知道了」和「不再提醒」。")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    if ride.authorization != .authorizedAlways {
                        Text("锁屏后要能提醒，定位必须选「始终」。现在不是「始终」，锁屏或切走后就不会再算距离。")
                            .font(.footnote)
                            .foregroundStyle(.red)
                        Button("打开系统设置") { ride.openSystemSettings() }
                    }
                    if let lastError = ride.lastError {
                        Text(lastError).foregroundStyle(.red)
                    }
                }
                Section("本次距离") {
                    LabeledContent("外圈", value: AlertDistance.format(settings.alerts.outerMeters))
                    LabeledContent("内圈", value: "\(Int(settings.alerts.innerMeters)) 米")
                    Text("靠近时每个圈提醒一次。离开这段路之后，下次再靠近还会提醒。")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    if let location = ride.lastLocation {
                        LabeledContent("最近定位", value: location.timestamp.formatted(date: .omitted, time: .standard))
                    }
                }
                Section("最近提醒") {
                    if ride.recentAlerts.isEmpty {
                        Text("还没有触发提醒")
                            .foregroundStyle(.secondary)
                    }
                    ForEach(ride.recentAlerts) { item in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(item.at.formatted(date: .omitted, time: .standard))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            Text(item.text)
                        }
                    }
                }
            }
            .navigationTitle("骑行")
        }
    }
}
