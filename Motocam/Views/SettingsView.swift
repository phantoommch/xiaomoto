import SwiftUI

struct SettingsView: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(SessionStore.self) private var session
    @Environment(\.openURL) private var openURL

    @State private var outerMeters = 2_000.0
    @State private var innerMeters = 500.0
    @State private var status: String?
    @State private var accountTerms = ""
    @State private var showAccountTerms = false
    @State private var confirmDelete = false

    var body: some View {
        NavigationStack {
            Form {
                Section("提醒距离") {
                    VStack(alignment: .leading) {
                        Text("外圈 \(AlertDistance.format(outerMeters))")
                        Slider(
                            value: Binding(
                                get: { Double(AlertDistance.index(of: outerMeters)) },
                                set: { newValue in
                                    let stops = AlertDistance.outerStops
                                    let index = min(max(0, Int(newValue.rounded())), stops.count - 1)
                                    outerMeters = stops[index]
                                }
                            ),
                            in: 0...Double(AlertDistance.outerStops.count - 1),
                            step: 1
                        )
                    }
                    VStack(alignment: .leading) {
                        Text("内圈 \(Int(innerMeters)) 米")
                        Slider(value: $innerMeters, in: 50...2_000)
                    }
                    Text("靠近时每个圈提醒一次。离开这段路之后，下次再靠近还会提醒。点「不再提醒」的点位不再出声。")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    Button("已关闭提醒的点位（\(settings.mutedPointIDs.count)个）· 全部恢复") {
                        settings.clearMuted()
                    }
                }
                Section("账号") {
                    if session.isLoggedIn {
                        Text(session.email.isEmpty ? "已登录" : session.email)
                        Button("用户协议与免责声明") {
                            Task {
                                accountTerms = await TermsLoader.load(baseURL: settings.communityBaseURL).markdown
                                showAccountTerms = true
                            }
                        }
                        Button("退出登录") {
                            Task {
                                try? await AuthClient.logout(baseURL: settings.communityBaseURL)
                                session.signOutLocal()
                            }
                        }
                        Button("注销账号", role: .destructive) {
                            confirmDelete = true
                        }
                    } else {
                        Button("注册 / 登录") {
                            session.requireLogin(hint: "")
                        }
                    }
                }
                if let storeURL = AppUpdateLink.appStoreURL() {
                    Section {
                        Button("检查更新") { openURL(storeURL) }
                    }
                }
                if let status {
                    Section {
                        Text(status)
                    }
                }
            }
            .safeAreaInset(edge: .bottom) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(AppUpdateLink.currentVersionLine(AppUpdateClient.localVersionCode))
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    if !AppUpdateClient.localVersionName.isEmpty {
                        Text(AppUpdateClient.localVersionName)
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 20)
                .padding(.bottom, 8)
            }
            .onAppear(perform: loadDistances)
            .onChange(of: outerMeters) { _, value in
                save(\.outerMeters, value)
            }
            .onChange(of: innerMeters) { _, value in
                save(\.innerMeters, value)
            }
            .navigationTitle("设置")
            .sheet(isPresented: $showAccountTerms) {
                TermsReaderView(markdown: accountTerms) { showAccountTerms = false }
            }
            .alert("注销账号", isPresented: $confirmDelete) {
                Button("注销", role: .destructive) {
                    Task {
                        do {
                            try await AuthClient.deleteAccount(baseURL: settings.communityBaseURL)
                            session.signOutLocal()
                            status = "账号已注销。"
                        } catch {
                            status = error.localizedDescription
                        }
                    }
                }
                Button("取消", role: .cancel) {}
            } message: {
                Text("注销后这台手机上的登录会被清掉。已经公开的违法点还会留在地图上，但不再和这个邮箱关联。")
            }
        }
    }

    private func loadDistances() {
        outerMeters = settings.alerts.outerMeters
        innerMeters = settings.alerts.innerMeters
    }

    private func save(_ keyPath: WritableKeyPath<AlertSettings, Double>, _ value: Double) {
        guard abs(settings.alerts[keyPath: keyPath] - value) > 0.5 else { return }
        var copy = settings.alerts
        copy[keyPath: keyPath] = value
        settings.alerts = copy
    }
}
