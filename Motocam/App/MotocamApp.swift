import SwiftUI

final class MotocamAppDelegate: NSObject, UIApplicationDelegate {
    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        RideController.shared?.restoreIfNeeded()
        return true
    }
}

@main
struct MotocamApp: App {
    @UIApplicationDelegateAdaptor(MotocamAppDelegate.self) private var appDelegate
    @State private var store: PointStore
    @State private var settings: SettingsStore
    @StateObject private var ride: RideController

    init() {
        let store = PointStore()
        let settings = SettingsStore()
        let ride = RideController()
        ride.attach(store: store, settings: settings)
        _store = State(initialValue: store)
        _settings = State(initialValue: settings)
        _ride = StateObject(wrappedValue: ride)
    }

    var body: some Scene {
        WindowGroup {
            RootTabView()
                .environment(store)
                .environment(settings)
                .environment(SessionStore.shared)
                .environmentObject(ride)
                .tint(MotocamTheme.amber)
                .onAppear {
                    ride.attach(store: store, settings: settings)
                    ride.restoreIfNeeded()
                }
        }
    }
}
