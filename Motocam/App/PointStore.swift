import Foundation
import Network
import Observation

enum AppFiles {
    static func directory() throws -> URL {
        let base = try FileManager.default.url(
            for: .applicationSupportDirectory,
            in: .userDomainMask,
            appropriateFor: nil,
            create: true
        )
        let folder = base.appendingPathComponent("Motocam", isDirectory: true)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        return folder
    }
}

@Observable
final class PointStore {
    private(set) var points: [EnforcementPoint] = []
    private(set) var communityPoints: [EnforcementPoint] = []
    var lastError: String?

    private let persist: Bool
    private let directory: URL?

    init(persist: Bool = true) {
        self.persist = persist
        if persist {
            directory = try? AppFiles.directory()
            load()
        } else {
            directory = nil
        }
    }

    var alertPoints: [EnforcementPoint] { points.filter(\.located) + communityPoints }

    func upsert(_ point: EnforcementPoint) {
        if let index = points.firstIndex(where: { $0.id == point.id }) {
            points[index] = point
        } else {
            points.append(point)
        }
        saveLocal()
    }

    func delete(id: String) {
        points.removeAll { $0.id == id }
        saveLocal()
    }

    func replaceCommunity(_ incoming: [EnforcementPoint], removedIDs: [String]) {
        let removed = Set(removedIDs)
        var byID: [String: EnforcementPoint] = [:]
        for point in incoming where !removed.contains(point.id) {
            byID[point.id] = point
        }
        communityPoints = byID.values.sorted { $0.name < $1.name }
        saveCommunity()
    }

    func clearCommunity() {
        communityPoints = []
        saveCommunity()
    }

    @discardableResult
    func importPayload(_ data: Data) throws -> Int {
        let text = String(data: data, encoding: .utf8) ?? ""
        let imported: [EnforcementPoint]
        if text.trimmingCharacters(in: .whitespacesAndNewlines).hasPrefix("<") || text.contains("<gpx") || text.contains("<wpt") {
            imported = try PointCodec.decodeGPX(text)
        } else {
            imported = try PointCodec.decodeJSON(data)
        }
        let localized = imported.map { point -> EnforcementPoint in
            var copy = point
            copy.isCommunity = false
            return copy
        }
        points = PointCodec.merging(existing: points, imported: localized)
        saveLocal()
        return localized.count
    }

    func exportJSON(includeCommunity: Bool) throws -> Data {
        try PointCodec.encodeJSON(exportPoints(includeCommunity: includeCommunity))
    }

    func exportGPX(includeCommunity: Bool) -> String {
        PointCodec.encodeGPX(exportPoints(includeCommunity: includeCommunity))
    }

    private func exportPoints(includeCommunity: Bool) -> [EnforcementPoint] {
        includeCommunity ? points + communityPoints : points
    }

    private func load() {
        guard let directory else { return }
        points = (try? PointCodec.decodeJSON(Data(contentsOf: directory.appendingPathComponent("points.json")))) ?? []
        communityPoints = (try? PointCodec.decodeJSON(Data(contentsOf: directory.appendingPathComponent("community.json")))) ?? []
    }

    private func saveLocal() {
        guard persist, let directory else { return }
        do {
            let data = try PointCodec.encodeJSON(points)
            try data.write(to: directory.appendingPathComponent("points.json"), options: .atomic)
            lastError = nil
        } catch {
            lastError = "本地点位保存失败"
        }
    }

    private func saveCommunity() {
        guard persist, let directory else { return }
        do {
            let data = try PointCodec.encodeJSON(communityPoints)
            try data.write(to: directory.appendingPathComponent("community.json"), options: .atomic)
        } catch {
            lastError = "社区缓存保存失败"
        }
    }
}

enum UploadQueue {
    private static let fileName = "upload-queue.json"
    private static let lock = NSLock()
    private static var flushing = false

    static func upload(_ point: EnforcementPoint, imageJPEG: Data? = nil, settings: SettingsStore) async -> Bool {
        await submitOrQueue(point, imageJPEG: imageJPEG, settings: settings)
    }

    static func saveAndUpload(_ point: EnforcementPoint, store: PointStore, settings: SettingsStore) async -> Bool {
        store.upsert(point)
        return await submitOrQueue(point, imageJPEG: nil, settings: settings)
    }

    static func flush(settings: SettingsStore) async -> Int {
        lock.lock()
        if flushing {
            lock.unlock()
            return 0
        }
        flushing = true
        lock.unlock()
        defer {
            lock.lock()
            flushing = false
            lock.unlock()
        }
        guard SessionToken.current()?.isEmpty == false else { return 0 }
        let pending = load()
        var uploaded = 0
        for point in pending {
            let base = settings.communityBaseURL.trimmingCharacters(in: .whitespacesAndNewlines)
            if base.isEmpty { break }
            do {
                _ = try await CommunityClient.submit(
                    baseURL: base,
                    deviceID: settings.deviceID,
                    point: point,
                    imageJPEG: loadImage(point.id)
                )
                remove(point.id)
                uploaded += 1
            } catch {
                enqueue(point)
                break
            }
        }
        return uploaded
    }

    private static func submitOrQueue(_ point: EnforcementPoint, imageJPEG: Data?, settings: SettingsStore) async -> Bool {
        if let imageJPEG, !imageJPEG.isEmpty {
            storeImage(point.id, imageJPEG)
        }
        if SessionToken.current()?.isEmpty != false {
            enqueue(point)
            return false
        }
        let base = settings.communityBaseURL.trimmingCharacters(in: .whitespacesAndNewlines)
        if base.isEmpty {
            enqueue(point)
            return false
        }
        do {
            _ = try await CommunityClient.submit(
                baseURL: base,
                deviceID: settings.deviceID,
                point: point,
                imageJPEG: imageJPEG ?? loadImage(point.id)
            )
            remove(point.id)
            return true
        } catch {
            enqueue(point)
            return false
        }
    }

    private static func enqueue(_ point: EnforcementPoint) {
        lock.lock()
        defer { lock.unlock() }
        var next = loadUnlocked()
        next.removeAll { $0.id == point.id }
        next.append(point)
        writeUnlocked(next)
    }

    private static func remove(_ id: String) {
        lock.lock()
        defer { lock.unlock() }
        writeUnlocked(loadUnlocked().filter { $0.id != id })
        if let file = imageURL(id) {
            try? FileManager.default.removeItem(at: file)
        }
    }

    private static func imageURL(_ id: String) -> URL? {
        guard let directory = try? AppFiles.directory() else { return nil }
        return directory.appendingPathComponent("upload-images", isDirectory: true).appendingPathComponent("\(id).jpg")
    }

    private static func storeImage(_ id: String, _ data: Data) {
        guard let file = imageURL(id) else { return }
        try? FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
        try? data.write(to: file, options: .atomic)
    }

    private static func loadImage(_ id: String) -> Data? {
        guard let file = imageURL(id) else { return nil }
        return try? Data(contentsOf: file)
    }

    private static func load() -> [EnforcementPoint] {
        lock.lock()
        defer { lock.unlock() }
        return loadUnlocked()
    }

    private static func loadUnlocked() -> [EnforcementPoint] {
        guard let directory = try? AppFiles.directory() else { return [] }
        let url = directory.appendingPathComponent(fileName)
        guard let data = try? Data(contentsOf: url) else { return [] }
        return (try? PointCodec.decodeJSON(data)) ?? []
    }

    private static func writeUnlocked(_ points: [EnforcementPoint]) {
        guard let directory = try? AppFiles.directory() else { return }
        guard let data = try? PointCodec.encodeJSON(points) else { return }
        try? data.write(to: directory.appendingPathComponent(fileName), options: .atomic)
    }
}

enum UploadMonitor {
    private static var monitor: NWPathMonitor?
    private static var started = false

    static func start(settings: SettingsStore) {
        guard !started else { return }
        started = true
        let monitor = NWPathMonitor()
        self.monitor = monitor
        monitor.pathUpdateHandler = { path in
            guard path.status == .satisfied else { return }
            Task { await UploadQueue.flush(settings: settings) }
        }
        monitor.start(queue: DispatchQueue(label: "motocam.upload"))
    }
}
