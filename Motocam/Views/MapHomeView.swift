import MapKit
import SwiftUI

struct MapHomeView: View {
    @Environment(PointStore.self) private var store
    @Environment(SettingsStore.self) private var settings
    @Environment(SessionStore.self) private var session
    @EnvironmentObject private var ride: RideController

    @State private var position: MapCameraPosition = .region(Self.openingRegion)
    @State private var selection: String?
    @State private var detail: MapSelection?
    @State private var editor: PointDraft?
    @State private var searchSeed: SearchSeed?
    @State private var showTicket = false
    @State private var upload: EnforcementPoint?
    @State private var showLegend = false
    @State private var syncMessage: String?
    @State private var syncAlert: String?
    @State private var isSyncing = false
    @State private var didCenterOnUser = false
    @State private var reviewPoints: [EnforcementPoint] = []
    @State private var reviewImage: Data?
    @State private var isUploading = false

    private static var didAutoUpdate = false

    var body: some View {
        NavigationStack {
            ZStack(alignment: .top) {
                map
                VStack(spacing: 10) {
                    Button {
                        guard session.isLoggedIn else {
                            session.requireLogin(hint: LoginCopy.points)
                            return
                        }
                        showTicket = true
                    } label: {
                        Text("违法点上传")
                            .font(.headline)
                            .frame(maxWidth: .infinity)
                            .padding(.vertical, 12)
                            .background(MotocamTheme.amber, in: RoundedRectangle(cornerRadius: 14))
                            .foregroundStyle(.white)
                    }
                    .buttonStyle(.plain)
                    HStack(spacing: 8) {
                        ridePill
                        Spacer()
                        control("magnifyingglass") { searchSeed = SearchSeed(query: "") }
                        Button {
                            guard session.isLoggedIn else {
                                session.requireLogin(hint: LoginCopy.points)
                                return
                            }
                            Task { await refresh(manual: true) }
                        } label: {
                            Text("更新")
                                .font(.subheadline.weight(.semibold))
                                .padding(.horizontal, 12)
                                .padding(.vertical, 8)
                                .background(.ultraThinMaterial, in: Capsule())
                        }
                        .buttonStyle(.plain)
                        .disabled(isSyncing)
                    }
                    if let syncMessage {
                        Text(syncMessage)
                            .font(.body)
                            .foregroundStyle(Color(red: 0.10, green: 0.09, blue: 0.08))
                            .padding(12)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(Color(red: 0.97, green: 0.96, blue: 0.93), in: RoundedRectangle(cornerRadius: 12))
                    }
                }
                .padding(.horizontal, 12)
                .padding(.top, 8)
            }
            .overlay(alignment: .bottom) {
                VStack(spacing: 8) {
                    if !reviewPoints.isEmpty {
                        reviewCard
                    } else if let nearest = nearestPoint {
                        nearestCard(nearest)
                    }
                }
                .padding(.horizontal, 12)
                .padding(.bottom, 8)
            }
            .alert("更新", isPresented: Binding(
                get: { syncAlert != nil },
                set: { if !$0 { syncAlert = nil } }
            )) {
                Button("知道了", role: .cancel) {}
            } message: {
                Text(syncAlert ?? "")
            }
            .navigationTitle(AppIdentity.displayName)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        showLegend.toggle()
                    } label: {
                        Image(systemName: "list.bullet")
                    }
                    .accessibilityLabel("图例")
                }
            }
            .sheet(item: $detail) { item in
                PointDetailSheet(
                    point: item.point,
                    distanceMeters: distance(to: item.point),
                    onEdit: {
                        detail = nil
                        editor = PointDraft.from(point: item.point)
                    },
                    onDelete: {
                        store.delete(id: item.point.id)
                        detail = nil
                    },
                    onUpload: {
                        let point = item.point
                        detail = nil
                        guard session.isLoggedIn else {
                            session.requireLogin(hint: LoginCopy.points)
                            return
                        }
                        upload = point
                    },
                    onSaveLocal: item.point.isCommunity ? {
                        var copy = item.point
                        copy.isCommunity = false
                        copy.source = .community
                        copy.id = UUID().uuidString
                        store.upsert(copy)
                        detail = nil
                    } : nil
                )
                .presentationDetents([.medium, .large])
            }
            .sheet(item: $editor) { draft in
                PointEditorSheet(draft: draft) { point in
                    var saved = point
                    if let existing = store.points.first(where: { $0.id == saved.id }) {
                        saved.createdAt = existing.createdAt
                    }
                    store.upsert(saved)
                }
            }
            .sheet(item: $searchSeed) { seed in
                SearchSheet(seed: seed) { draft in
                    editor = draft
                }
            }
            .sheet(isPresented: $showTicket) {
                TicketImportView { records, image in
                    Task { await placeTickets(records, imageJPEG: image) }
                }
            }
            .sheet(item: $upload) { point in
                CommunityUploadSheet(point: point)
            }
            .alert("图例", isPresented: $showLegend) {
                Button("知道了", role: .cancel) {}
            } message: {
                Text("红色禁摩收费站，橙色禁摩路段，紫色抓拍，灰色其他。青色「社」是已审核的社区点。长按地图可手动添加。")
            }
            .onAppear {
                UploadMonitor.start(settings: settings)
                ride.prepareMapLocation()
                autoUpdateIfNeeded()
            }
            .onChange(of: session.isLoggedIn) { _, loggedIn in
                if loggedIn { autoUpdateIfNeeded() }
            }
            .onChange(of: ride.lastLocation?.timestamp) { _, _ in
                guard let location = ride.lastLocation else { return }
                if !didCenterOnUser {
                    didCenterOnUser = true
                    position = .region(mapRegion(
                        latitude: location.coordinate.latitude,
                        longitude: location.coordinate.longitude,
                        radiusMeters: 15_000
                    ))
                }
            }
        }
    }

    private var map: some View {
        MapReader { proxy in
            Map(position: $position, selection: $selection) {
                ForEach(mapLocal.filter(\.located)) { point in
                    Marker(crowdTitle(point), coordinate: mapCoordinate(for: point))
                        .tint(MotocamTheme.color(for: point.type))
                        .tag("l:\(point.id)")
                }
                ForEach(store.communityPoints) { point in
                    Annotation(crowdTitle(point), coordinate: mapCoordinate(for: point)) {
                        CommunityPin()
                    }
                    .tag("c:\(point.id)")
                }
                UserAnnotation()
            }
            .mapControls {
                MapUserLocationButton()
                MapCompass()
                MapScaleView()
            }
            .simultaneousGesture(
                LongPressGesture(minimumDuration: 0.45)
                    .sequenced(before: DragGesture(minimumDistance: 0, coordinateSpace: .local))
                    .onEnded { value in
                        guard case .second(true, let drag) = value, let location = drag?.location else { return }
                        guard let coordinate = proxy.convert(location, from: .local) else { return }
                        let wgs = CoordinateConverter.gcj02ToWgs84(
                            CoordinateConverter.Coord(latitude: coordinate.latitude, longitude: coordinate.longitude)
                        )
                        editor = PointDraft.manual(latitude: wgs.latitude, longitude: wgs.longitude)
                    }
            )
            .onChange(of: selection) { _, tag in
                guard let tag, let item = lookup(tag) else { return }
                detail = item
                selection = nil
            }
        }
        .ignoresSafeArea(edges: .bottom)
    }

    private var ridePill: some View {
        Button {
            ride.toggleRide()
        } label: {
            Label(ride.isRiding ? "骑行中" : "开始骑行", systemImage: ride.isRiding ? "dot.radiowaves.left.and.right" : "play.fill")
                .font(.subheadline.weight(.semibold))
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(.ultraThinMaterial, in: Capsule())
        }
        .buttonStyle(.plain)
    }

    private func crowdTitle(_ point: EnforcementPoint) -> String {
        point.uploadSource == "user_upload" ? "\(point.name) 用户上传" : point.name
    }

    private func control(_ system: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: system)
                .font(.body.weight(.semibold))
                .frame(width: 36, height: 36)
                .background(.ultraThinMaterial, in: Circle())
        }
        .buttonStyle(.plain)
    }

    private func nearestCard(_ item: (point: EnforcementPoint, meters: Double)) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(item.meters < 2_000 ? "前方有记录点" : "最近的点")
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(crowdTitle(item.point))
                .font(.headline)
            Text("\(item.point.type.title) · \(formatMeters(item.meters))")
                .font(.subheadline)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 16))
        .onTapGesture {
            detail = MapSelection(point: item.point)
        }
    }

    private var reviewCard: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("请核对")
                .font(.headline)
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(reviewPoints) { point in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(crowdTitle(point)).font(.subheadline.weight(.semibold))
                            if !point.amapName.isEmpty {
                                Text(PlacementStatus.amapMatch(point.amapName))
                            }
                            Text("道路 \(roadText(point))")
                            Text(point.violation.isEmpty ? "未识别到违法行为" : point.violation)
                            Text(penaltyText(point))
                        }
                        .font(.footnote)
                    }
                }
            }
            .frame(maxHeight: 220)
            Button(isUploading ? "正在上传…" : "上传") {
                Task { await uploadReviewed() }
            }
            .buttonStyle(.borderedProminent)
            .disabled(isUploading)
            Button("暂不上传") {
                reviewPoints = []
            }
            .disabled(isUploading)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 16))
    }

    private var mapLocal: [EnforcementPoint] {
        var byID = Dictionary(uniqueKeysWithValues: store.points.map { ($0.id, $0) })
        for point in reviewPoints {
            byID[point.id] = point
        }
        return byID.values.sorted { $0.name < $1.name }
    }

    private var nearbyLocal: [EnforcementPoint] {
        store.points.filter(isWithinHomeRadius)
    }

    private var nearbyCommunity: [EnforcementPoint] {
        store.communityPoints.filter(isWithinHomeRadius)
    }

    private func isWithinHomeRadius(_ point: EnforcementPoint) -> Bool {
        guard let location = ride.lastLocation else { return false }
        return GeoMath.distanceMeters(
            location.coordinate.latitude,
            location.coordinate.longitude,
            point.latitude,
            point.longitude
        ) <= AlertDistance.nearbyRadius(settings.alerts.outerMeters)
    }

    private var nearestPoint: (point: EnforcementPoint, meters: Double)? {
        guard let location = ride.lastLocation else { return nil }
        let ranked = (nearbyLocal + nearbyCommunity).map { point in
            (point, GeoMath.distanceMeters(
                location.coordinate.latitude,
                location.coordinate.longitude,
                point.latitude,
                point.longitude
            ))
        }
        .filter { $0.1.isFinite }
        .sorted { $0.1 < $1.1 }
        return ranked.first
    }

    private func distance(to point: EnforcementPoint) -> Double? {
        guard let location = ride.lastLocation else { return nil }
        return GeoMath.distanceMeters(
            location.coordinate.latitude,
            location.coordinate.longitude,
            point.latitude,
            point.longitude
        )
    }

    private func lookup(_ tag: String) -> MapSelection? {
        if tag.hasPrefix("l:") {
            let id = String(tag.dropFirst(2))
            return store.points.first { $0.id == id }.map(MapSelection.init)
        }
        if tag.hasPrefix("c:") {
            let id = String(tag.dropFirst(2))
            return store.communityPoints.first { $0.id == id }.map(MapSelection.init)
        }
        return nil
    }

    private func mapCoordinate(for point: EnforcementPoint) -> CLLocationCoordinate2D {
        let gcj = point.gcj02
        return CLLocationCoordinate2D(latitude: gcj.latitude, longitude: gcj.longitude)
    }

    private func autoUpdateIfNeeded() {
        guard session.isLoggedIn else { return }
        guard !Self.didAutoUpdate else { return }
        Self.didAutoUpdate = true
        Task { await refresh(manual: false) }
    }

    private func refresh(manual: Bool) async {
        guard session.isLoggedIn else {
            if manual { session.requireLogin(hint: LoginCopy.points) }
            return
        }
        if manual {
            isSyncing = true
            syncMessage = "正在更新…"
        }
        defer { if manual { isSyncing = false } }
        _ = await UploadQueue.flush(settings: settings)
        let base = settings.communityBaseURL.trimmingCharacters(in: .whitespacesAndNewlines)
        if base.isEmpty {
            if manual {
                syncMessage = SyncNotice.noServer
                syncAlert = SyncNotice.noServer
            }
            return
        }
        do {
            let result = try await CommunityClient.sync(baseURL: base)
            store.replaceCommunity(result.points, removedIDs: result.removedIDs)
            settings.lastCommunitySync = result.serverTime
            let nearby: Int? = ride.lastLocation == nil
                ? nil
                : (store.points + store.communityPoints).filter(isWithinHomeRadius).count
            let message = SyncNotice.success(
                count: store.communityPoints.count,
                nearbyCount: nearby,
                nearbyKilometers: AlertDistance.nearbyKilometers(settings.alerts.outerMeters)
            )
            syncMessage = message
            if manual { syncAlert = message }
        } catch {
            if manual {
                let message = SyncNotice.failure(shortSyncReason(error))
                syncMessage = message
                syncAlert = message
                if case .server(let server) = error as? CommunityClientError, server.contains("未登录") {
                    session.requireLogin(hint: LoginCopy.points)
                }
            }
        }
    }

    private func shortSyncReason(_ error: Error) -> String {
        guard let community = error as? CommunityClientError else { return "网络不通" }
        switch community {
        case .notConfigured:
            return "还没有服务器地址"
        case .badURL:
            return "服务器地址无效"
        case .transport:
            return "网络不通"
        case .server(let message):
            let line = message.split(separator: "。").first.map(String.init) ?? message
            let trimmed = line.trimmingCharacters(in: .whitespacesAndNewlines)
            return trimmed.isEmpty ? "网络不通" : String(trimmed.prefix(24))
        }
    }

    private func placeTickets(_ records: [TicketRecord], imageJPEG: Data?) async {
        syncMessage = "正在标注…"
        let key = settings.effectiveAmapKey
        if key.isEmpty {
            syncMessage = "还没有高德 Web 服务 Key。"
            return
        }
        reviewImage = imageJPEG
        var placed: [EnforcementPoint] = []
        var missed = 0
        var searchError: String?
        for record in records {
            let location = record.location?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            if location.isEmpty { continue }
            let query = QueryCleaner.parse(location)
            do {
                if let hit = await Recog.locate(text: location, full: record.fullText.isEmpty ? location : record.fullText, key: key),
                   hit.latitude.isFinite {
                    let point = TicketPoint.fromLocated(hit, record: record)
                    if hit.exact { store.upsert(point) }
                    placed.append(point)
                } else {
                    var point = TicketPoint.unmatched(
                        location: location,
                        query: query,
                        violation: record.violation ?? "",
                        fineYuan: record.fineYuan,
                        penaltyPoints: record.penaltyPoints
                    )
                    point.ocrText = record.fullText
                    point.confidence = "none"
                    if !session.isLoggedIn {
                        session.requireLogin(hint: LoginCopy.points)
                    }
                    _ = await UploadQueue.upload(point, imageJPEG: imageJPEG, settings: settings)
                    missed += 1
                }
            } catch let error as AmapClientError {
                let stop: Bool
                switch error {
                case .missingKey:
                    stop = true
                case .service(let message):
                    stop = message.contains("Key") || message.contains("白名单") || message.contains("次数")
                case .badURL, .transport:
                    stop = false
                }
                if stop {
                    reviewPoints = placed
                    syncMessage = error.localizedDescription
                    focusReview()
                    return
                }
                searchError = error.localizedDescription
            } catch {
                searchError = "搜索失败"
            }
        }
        reviewPoints = placed
        if placed.isEmpty {
            syncMessage = missed > 0 ? PlacementStatus.notFound : (searchError ?? "没有可标注的地点")
        } else if missed > 0 {
            syncMessage = PlacementStatus.reviewPrompt(marked: placed.count) + "。" + PlacementStatus.notFound
            focusReview()
        } else {
            syncMessage = PlacementStatus.reviewPrompt(marked: placed.count)
            focusReview()
        }
    }

    private func uploadReviewed() async {
        let batch = reviewPoints
        guard !batch.isEmpty else { return }
        guard session.isLoggedIn else {
            session.requireLogin(hint: LoginCopy.points)
            return
        }
        isUploading = true
        defer { isUploading = false }
        var uploaded = 0
        for point in batch {
            if await UploadQueue.upload(point, imageJPEG: reviewImage, settings: settings) {
                uploaded += 1
            }
        }
        reviewPoints = []
        reviewImage = nil
        syncMessage = PlacementStatus.sentence(marked: batch.count, uploaded: uploaded)
    }

    private func focusReview() {
        guard let point = reviewPoints.first else { return }
        position = .region(mapRegion(
            latitude: point.latitude,
            longitude: point.longitude,
            radiusMeters: 3_000
        ))
    }

    private func roadText(_ point: EnforcementPoint) -> String {
        let road = [point.roadCode, point.roadName].filter { !$0.isEmpty }.joined(separator: " ")
        return road.isEmpty ? "未识别到道路" : road
    }

    private func penaltyText(_ point: EnforcementPoint) -> String {
        let fine = point.fineYuan.map { "罚款 \($0) 元" } ?? "未识别到罚款"
        let points = point.penaltyPoints.map { "记 \($0) 分" } ?? "未识别到记分"
        return "\(fine) · \(points)"
    }

    private func formatMeters(_ meters: Double) -> String {
        meters >= 1000 ? String(format: "%.1f 公里", meters / 1000) : "\(Int(meters.rounded())) 米"
    }

    private static var openingRegion: MKCoordinateRegion {
        let gcj = CoordinateConverter.wgs84ToGcj02(CoordinateConverter.Coord(latitude: 30.2, longitude: 114.5))
        return MKCoordinateRegion(
            center: CLLocationCoordinate2D(latitude: gcj.latitude, longitude: gcj.longitude),
            span: MKCoordinateSpan(latitudeDelta: 14, longitudeDelta: 14)
        )
    }
}

func mapRegion(latitude: Double, longitude: Double, radiusMeters: Double) -> MKCoordinateRegion {
    let gcj = CoordinateConverter.wgs84ToGcj02(CoordinateConverter.Coord(latitude: latitude, longitude: longitude))
    let latitudeDelta = (radiusMeters * 2) / 111_320
    let cosine = max(0.2, abs(cos(gcj.latitude * .pi / 180)))
    return MKCoordinateRegion(
        center: CLLocationCoordinate2D(latitude: gcj.latitude, longitude: gcj.longitude),
        span: MKCoordinateSpan(latitudeDelta: latitudeDelta, longitudeDelta: latitudeDelta / cosine)
    )
}

struct MapSelection: Identifiable {
    var id: String { (point.isCommunity ? "c:" : "l:") + point.id }
    var point: EnforcementPoint
}

struct CommunityPin: View {
    var body: some View {
        VStack(spacing: 0) {
            Text("社")
                .font(.caption.bold())
                .foregroundStyle(.white)
                .frame(width: 28, height: 28)
                .background(MotocamTheme.community, in: Circle())
                .overlay(Circle().stroke(Color.white, lineWidth: 2))
            Image(systemName: "arrowtriangle.down.fill")
                .font(.system(size: 8))
                .foregroundStyle(MotocamTheme.community)
                .offset(y: -3)
        }
        .accessibilityLabel("社区点")
    }
}
