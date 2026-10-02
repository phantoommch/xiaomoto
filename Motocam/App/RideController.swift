import AudioToolbox
import AVFoundation
import CoreLocation
import Foundation
import UIKit
import UserNotifications

struct RideAlertRecord: Identifiable, Equatable {
    var id = UUID()
    var at: Date
    var text: String
}

struct ActiveRideAlert: Identifiable, Equatable {
    var id: String
    var pointId: String
    var title: String
    var text: String
}

final class RideController: NSObject, ObservableObject, CLLocationManagerDelegate, AVSpeechSynthesizerDelegate, UNUserNotificationCenterDelegate {
    static let categoryID = "RIDE_ALERT"
    static let actionAck = "ACK"
    static let actionMute = "MUTE"
    static let ridingDefaultsKey = "settings.ridingEnabled"

    static weak var shared: RideController?

    @Published var isRiding = false
    @Published var authorization: CLAuthorizationStatus = .notDetermined
    @Published var notificationsGranted = false
    @Published var lastLocation: CLLocation?
    @Published var lastError: String?
    @Published var recentAlerts: [RideAlertRecord] = []
    @Published var activeAlert: ActiveRideAlert?

    private let manager = CLLocationManager()
    private let synthesizer = AVSpeechSynthesizer()
    private var store: PointStore?
    private var settings: SettingsStore?
    private var approach: [String: ApproachRecord] = [:]
    private var alertClosestMeters = Double.greatestFiniteMagnitude
    private var currentUtterance: AVSpeechUtterance?
    private var pendingStart = false
    private var pendingMapLocation = false
    private var backgroundActivity: CLBackgroundActivitySession?
    private let approachKey = "ride.approach"

    override init() {
        super.init()
        Self.shared = self
        manager.delegate = self
        manager.activityType = .automotiveNavigation
        manager.desiredAccuracy = kCLLocationAccuracyNearestTenMeters
        manager.distanceFilter = 50
        manager.pausesLocationUpdatesAutomatically = false
        synthesizer.delegate = self
        authorization = manager.authorizationStatus
        loadApproach()
        registerNotificationCategory()
    }

    func attach(store: PointStore, settings: SettingsStore) {
        self.store = store
        self.settings = settings
    }

    func prepareMapLocation() {
        authorization = manager.authorizationStatus
        switch authorization {
        case .authorizedAlways, .authorizedWhenInUse:
            manager.startUpdatingLocation()
            manager.requestLocation()
        case .notDetermined:
            pendingMapLocation = true
            manager.requestWhenInUseAuthorization()
        default:
            break
        }
    }

    /// Safe to call more than once. Reads the riding flag from UserDefaults when
    /// settings have not been attached yet, so a background location launch can
    /// resume before the SwiftUI view appears.
    func restoreIfNeeded() {
        let enabled = settings?.ridingEnabled ?? UserDefaults.standard.bool(forKey: Self.ridingDefaultsKey)
        guard enabled, !isRiding else { return }
        beginRide(requestPermission: false)
    }

    func toggleRide() {
        if isRiding {
            stopRide()
        } else {
            beginRide(requestPermission: true)
        }
    }

    func acknowledgeActive() {
        guard let alert = activeAlert else { return }
        activeAlert = nil
        UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [alert.id])
    }

    func muteActive() {
        guard let alert = activeAlert else { return }
        settings?.mute(alert.pointId)
        activeAlert = nil
        UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [alert.id])
    }

    func openSystemSettings() {
        guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
        UIApplication.shared.open(url)
    }

    var authorizationLabel: String {
        switch authorization {
        case .authorizedAlways: return "始终允许"
        case .authorizedWhenInUse: return "仅使用期间"
        case .denied: return "已拒绝"
        case .restricted: return "受限制"
        case .notDetermined: return "尚未选择"
        @unknown default: return "未知"
        }
    }

    private func beginRide(requestPermission: Bool) {
        lastError = nil
        authorization = manager.authorizationStatus
        switch authorization {
        case .authorizedAlways, .authorizedWhenInUse:
            startUpdates()
        case .notDetermined:
            guard requestPermission else { return }
            pendingStart = true
            manager.requestAlwaysAuthorization()
        default:
            lastError = "定位权限未开启。请允许「始终」，锁屏后才能继续提醒。"
        }
    }

    private func startUpdates() {
        isRiding = true
        settings?.setRidingEnabled(true)
        registerNotificationCategory()
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .timeSensitive]) { [weak self] granted, _ in
            DispatchQueue.main.async { self?.notificationsGranted = granted }
        }
        if manager.authorizationStatus == .authorizedAlways {
            manager.allowsBackgroundLocationUpdates = true
            manager.showsBackgroundLocationIndicator = true
            manager.startMonitoringSignificantLocationChanges()
            if backgroundActivity == nil {
                backgroundActivity = CLBackgroundActivitySession()
            }
        } else {
            lastError = "当前只允许使用期间定位。锁屏后提醒会停止，请在系统设置中改为「始终」。"
        }
        manager.startUpdatingLocation()
    }

    private func stopRide() {
        isRiding = false
        pendingStart = false
        settings?.setRidingEnabled(false)
        manager.stopUpdatingLocation()
        manager.stopMonitoringSignificantLocationChanges()
        manager.allowsBackgroundLocationUpdates = false
        backgroundActivity?.invalidate()
        backgroundActivity = nil
        activeAlert = nil
        stopSpeech()
    }

    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        DispatchQueue.main.async {
            self.authorization = manager.authorizationStatus
            if self.pendingStart {
                self.pendingStart = false
                self.beginRide(requestPermission: false)
            } else if self.pendingMapLocation {
                self.pendingMapLocation = false
                self.prepareMapLocation()
            }
        }
    }

    func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        DispatchQueue.main.async { self.handle(locations) }
    }

    func locationManager(_ manager: CLLocationManager, didFailWithError error: Error) {
        DispatchQueue.main.async { self.lastError = "定位失败：\(error.localizedDescription)" }
    }

    private func handle(_ locations: [CLLocation]) {
        guard let location = locations.last else { return }
        lastLocation = location
        guard isRiding, let store, let settings else { return }
        let heading: Double? = location.course >= 0 ? location.course : nil
        let speed: Double? = location.speed >= 0 ? location.speed : nil
        let result = AlertEngine.evaluate(
            latitude: location.coordinate.latitude,
            longitude: location.coordinate.longitude,
            headingDegrees: heading,
            speedMps: speed,
            points: store.alertPoints,
            settings: settings.alerts,
            approach: approach,
            mutedIDs: settings.mutedPointIDs
        )
        approach = result.approach
        saveApproach()
        for alert in result.alerts {
            deliver(alert)
        }
        dismissIfPassed(location, points: store.alertPoints)
    }

    private func deliver(_ alert: PendingAlert) {
        let tag = AlertEngine.stampKey(alert.point.id, alert.band)
        let title = AlertEngine.alertTitle(alert.point)
        alertClosestMeters = alert.distanceMeters
        activeAlert = ActiveRideAlert(id: tag, pointId: alert.point.id, title: title, text: alert.noticeText)
        let record = RideAlertRecord(at: Date(), text: alert.noticeText)
        recentAlerts.insert(record, at: 0)
        if recentAlerts.count > 20 { recentAlerts.removeLast(recentAlerts.count - 20) }
        speak(alert.spokenText)
        AudioServicesPlaySystemSound(kSystemSoundID_Vibrate)
        let generator = UINotificationFeedbackGenerator()
        generator.notificationOccurred(.warning)
        let content = UNMutableNotificationContent()
        content.title = title
        content.body = alert.noticeText
        content.sound = .default
        content.categoryIdentifier = Self.categoryID
        content.userInfo = ["pointId": alert.point.id, "tag": tag]
        content.interruptionLevel = .timeSensitive
        let request = UNNotificationRequest(identifier: tag, content: content, trigger: nil)
        UNUserNotificationCenter.current().add(request)
    }

    private func dismissIfPassed(_ location: CLLocation, points: [EnforcementPoint]) {
        guard let active = activeAlert,
              let point = points.first(where: { $0.id == active.pointId }) else { return }
        let current = GeoMath.distanceMeters(
            location.coordinate.latitude,
            location.coordinate.longitude,
            point.latitude,
            point.longitude
        )
        guard current.isFinite else { return }
        if current < alertClosestMeters { alertClosestMeters = current }
        let heading: Double? = location.course >= 0 ? location.course : nil
        let speed: Double? = location.speed >= 0 ? location.speed : nil
        guard AlertEngine.hasPassed(
            latitude: location.coordinate.latitude,
            longitude: location.coordinate.longitude,
            headingDegrees: heading,
            speedMps: speed,
            point: point,
            closestMeters: alertClosestMeters,
            currentMeters: current
        ) else { return }
        let identifier = active.id
        activeAlert = nil
        alertClosestMeters = .greatestFiniteMagnitude
        UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [identifier])
    }

    private func speak(_ text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        let session = AVAudioSession.sharedInstance()
        try? session.setCategory(.playback, mode: .spokenAudio, options: [.duckOthers])
        try? session.setActive(true)
        let utterance = AVSpeechUtterance(string: trimmed)
        utterance.voice = AVSpeechSynthesisVoice(language: "zh-CN")
        utterance.rate = AVSpeechUtteranceDefaultSpeechRate * 0.95
        currentUtterance = utterance
        if synthesizer.isSpeaking {
            synthesizer.stopSpeaking(at: .immediate)
        }
        synthesizer.speak(utterance)
    }

    func speechSynthesizer(_ synthesizer: AVSpeechSynthesizer, didFinish utterance: AVSpeechUtterance) {
        finishSpeech(utterance)
    }

    func speechSynthesizer(_ synthesizer: AVSpeechSynthesizer, didCancel utterance: AVSpeechUtterance) {
        finishSpeech(utterance)
    }

    private func finishSpeech(_ utterance: AVSpeechUtterance) {
        guard utterance === currentUtterance else { return }
        currentUtterance = nil
        guard !synthesizer.isSpeaking else { return }
        try? AVAudioSession.sharedInstance().setActive(false, options: [.notifyOthersOnDeactivation])
    }

    private func stopSpeech() {
        currentUtterance = nil
        if synthesizer.isSpeaking {
            synthesizer.stopSpeaking(at: .immediate)
        }
        try? AVAudioSession.sharedInstance().setActive(false, options: [.notifyOthersOnDeactivation])
    }

    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .sound, .list])
    }

    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let action = response.actionIdentifier
        let content = response.notification.request.content
        let pointID = content.userInfo["pointId"] as? String
        let tag = response.notification.request.identifier
        DispatchQueue.main.async {
            if action == Self.actionMute, let pointID, !pointID.isEmpty {
                self.settings?.mute(pointID)
                if self.activeAlert?.pointId == pointID {
                    self.activeAlert = nil
                }
            } else if self.activeAlert?.id == tag {
                self.activeAlert = nil
            }
            completionHandler()
        }
    }

    private func registerNotificationCategory() {
        let ack = UNNotificationAction(identifier: Self.actionAck, title: "我知道了", options: [])
        let mute = UNNotificationAction(identifier: Self.actionMute, title: "不再提醒", options: [.destructive])
        let category = UNNotificationCategory(
            identifier: Self.categoryID,
            actions: [ack, mute],
            intentIdentifiers: [],
            options: []
        )
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        center.setNotificationCategories([category])
    }

    private func loadApproach() {
        guard let data = UserDefaults.standard.data(forKey: approachKey),
              let decoded = try? JSONDecoder().decode([String: ApproachRecord].self, from: data) else { return }
        approach = decoded
    }

    private func saveApproach() {
        if let data = try? JSONEncoder().encode(approach) {
            UserDefaults.standard.set(data, forKey: approachKey)
        }
    }
}
