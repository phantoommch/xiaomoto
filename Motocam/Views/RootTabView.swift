import SwiftUI

struct RootTabView: View {
    private static var rideSymbol: String {
        if #available(iOS 18.0, *) { return "motorcycle" }
        return "scooter"
    }

    @Environment(SettingsStore.self) private var settings
    @Environment(SessionStore.self) private var session
    @EnvironmentObject private var ride: RideController
    @State private var showOnboarding = false

    var body: some View {
        TabView {
            MapHomeView()
                .tabItem { Label("地图", systemImage: "map") }
            RideDashboardView()
                .tabItem { Label("骑行", systemImage: Self.rideSymbol) }
            SettingsView()
                .tabItem { Label("设置", systemImage: "gearshape") }
        }
        .sheet(isPresented: $showOnboarding, onDismiss: {
            settings.markOnboardingSeen()
        }) {
            OnboardingView()
        }
        .onAppear {
            showOnboarding = !settings.hasSeenOnboarding
        }
        .task {
            await session.refresh(baseURL: settings.communityBaseURL)
        }
        .sheet(isPresented: Binding(
            get: { session.showLogin },
            set: { session.showLogin = $0 }
        )) {
            LoginView(hint: session.loginHint)
        }
        .sheet(isPresented: Binding(
            get: { session.needsReaccept && session.isLoggedIn },
            set: { _ in }
        )) {
            TermsReacceptView()
        }
        .overlay {
            if let alert = ride.activeAlert {
                ZStack {
                    Color.black.opacity(0.45)
                        .ignoresSafeArea()
                    VStack(alignment: .leading, spacing: 16) {
                        Text(alert.title)
                            .font(.title2.bold())
                        Text(alert.text)
                            .font(.body)
                        Button {
                            ride.acknowledgeActive()
                        } label: {
                            Text("我知道了")
                                .font(.title3.weight(.semibold))
                                .frame(maxWidth: .infinity)
                                .padding(.vertical, 8)
                        }
                        .buttonStyle(.borderedProminent)
                        .controlSize(.large)
                        Button(role: .destructive) {
                            ride.muteActive()
                        } label: {
                            Text("不再提醒")
                                .font(.title3.weight(.semibold))
                                .frame(maxWidth: .infinity)
                                .padding(.vertical, 8)
                        }
                        .buttonStyle(.bordered)
                        .controlSize(.large)
                    }
                    .padding(24)
                    .frame(maxWidth: 420)
                    .background(Color(red: 0.97, green: 0.96, blue: 0.93), in: RoundedRectangle(cornerRadius: 16))
                    .foregroundStyle(Color(red: 0.10, green: 0.09, blue: 0.08))
                    .padding(.horizontal, 24)
                }
            }
        }
    }
}
