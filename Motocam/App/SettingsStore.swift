import Foundation
import Observation

@Observable
final class SettingsStore {
    private enum Key {
        static let alerts = "settings.alerts"
        static let deviceID = "settings.deviceID"
        static let lastSync = "settings.lastCommunitySync"
        static let riding = "settings.ridingEnabled"
        static let onboarding = "settings.onboardingSeen"
        static let muted = "settings.mutedPointIDs"
    }

    var alerts: AlertSettings {
        didSet { persistLockedAlerts() }
    }

    /// Compiled-in server. A previously saved address is ignored.
    var communityBaseURL: String { Self.bundleValue("CommunityAPIBaseURL") }

    var lastCommunitySync: String? {
        didSet { defaults.set(lastCommunitySync, forKey: Key.lastSync) }
    }

    private(set) var deviceID: String
    private(set) var ridingEnabled: Bool
    private(set) var mutedPointIDs: Set<String>
    private(set) var hasSeenOnboarding: Bool
    private(set) var amapKeyOverride: String?
    private(set) var builtInAmapKey: String
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard, loadKeychain: Bool = true) {
        self.defaults = defaults
        if let data = defaults.data(forKey: Key.alerts),
           let decoded = try? JSONDecoder().decode(AlertSettings.self, from: data) {
            alerts = Self.locked(decoded)
        } else {
            alerts = Self.locked(AlertSettings())
        }
        lastCommunitySync = defaults.string(forKey: Key.lastSync)
        if let existing = defaults.string(forKey: Key.deviceID), UUID(uuidString: existing) != nil {
            deviceID = existing
        } else {
            let newID = UUID().uuidString
            deviceID = newID
            defaults.set(newID, forKey: Key.deviceID)
        }
        ridingEnabled = defaults.bool(forKey: Key.riding)
        mutedPointIDs = Set(defaults.stringArray(forKey: Key.muted) ?? [])
        hasSeenOnboarding = defaults.bool(forKey: Key.onboarding)
        builtInAmapKey = Self.bundleValue("AMapWebKey")
        amapKeyOverride = loadKeychain ? KeychainStore.load(account: KeychainStore.amapAccount) : nil
    }

    var effectiveAmapKey: String { builtInAmapKey }

    var hasAmapKey: Bool { !effectiveAmapKey.isEmpty }

    var amapKeySource: String {
        if let amapKeyOverride, !amapKeyOverride.isEmpty { return "本机覆盖" }
        if !builtInAmapKey.isEmpty { return "工程内置" }
        return "未配置"
    }

    func markOnboardingSeen() {
        hasSeenOnboarding = true
        defaults.set(true, forKey: Key.onboarding)
    }

    func setRidingEnabled(_ enabled: Bool) {
        ridingEnabled = enabled
        defaults.set(enabled, forKey: Key.riding)
    }

    func mute(_ id: String) {
        guard !id.isEmpty else { return }
        mutedPointIDs.insert(id)
        defaults.set(Array(mutedPointIDs), forKey: Key.muted)
    }

    func clearMuted() {
        mutedPointIDs = []
        defaults.removeObject(forKey: Key.muted)
    }

    func setAmapKeyOverride(_ value: String) throws {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty {
            clearAmapKeyOverride()
            return
        }
        try KeychainStore.save(trimmed, account: KeychainStore.amapAccount)
        amapKeyOverride = trimmed
    }

    func clearAmapKeyOverride() {
        KeychainStore.delete(account: KeychainStore.amapAccount)
        amapKeyOverride = nil
    }

    private func persistLockedAlerts() {
        let locked = Self.locked(alerts)
        if locked != alerts {
            alerts = locked
            return
        }
        persistAlerts()
    }

    private static func locked(_ alerts: AlertSettings) -> AlertSettings {
        alerts
    }

    private func persistAlerts() {
        if let data = try? JSONEncoder().encode(alerts) {
            defaults.set(data, forKey: Key.alerts)
        }
    }

    private static func bundleValue(_ key: String) -> String {
        let raw = (Bundle.main.object(forInfoDictionaryKey: key) as? String) ?? ""
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty || trimmed.contains("$(") { return "" }
        return trimmed
    }
}
