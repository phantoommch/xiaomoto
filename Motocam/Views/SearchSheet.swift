import SwiftUI

struct SearchSeed: Identifiable {
    let id = UUID()
    var query: String
    var city: String = ""
    var violation: String = ""
    var fineYuan: Int?
    var penaltyPoints: Int?
    var source: PointSource = .search
}

struct SearchSheet: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(\.dismiss) private var dismiss

    var seed: SearchSeed
    var onPick: (PointDraft) -> Void

    @State private var keywords: String
    @State private var city: String
    @State private var results: [RankedPlace] = []
    @State private var parsed: ParsedQuery?
    @State private var isSearching = false
    @State private var errorText: String?

    init(seed: SearchSeed, onPick: @escaping (PointDraft) -> Void) {
        self.seed = seed
        self.onPick = onPick
        _keywords = State(initialValue: seed.query)
        _city = State(initialValue: seed.city)
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    TextField("收费站或路段名称", text: $keywords)
                    TextField("城市或省份（可选，只影响排序）", text: $city)
                    Button {
                        Task { await runSearch() }
                    } label: {
                        if isSearching {
                            ProgressView()
                        } else {
                            Text("搜索")
                        }
                    }
                    .disabled(isSearching || keywords.trimmingCharacters(in: .whitespaces).isEmpty)
                } footer: {
                    if let parsed {
                        Text(summary(parsed))
                    } else {
                        Text("会去掉进口、出口、内广场这类罚单用词，再用公路编号给结果排序。同名收费站不会自动保存，需要你确认。")
                    }
                }
                if let errorText {
                    Section {
                        Text(errorText).foregroundStyle(.red)
                    }
                }
                if let top = results.first, top.score < 40 {
                    Section {
                        InlineNotice(text: "最高结果和罚单里的公路编号对不上，请逐条核对，不要直接相信第一条。", systemImage: "exclamationmark.triangle")
                    }
                }
                Section("候选") {
                    if results.isEmpty && !isSearching {
                        Text("还没有结果")
                            .foregroundStyle(.secondary)
                    }
                    ForEach(results) { item in
                        Button {
                            onPick(draft(from: item))
                            dismiss()
                        } label: {
                            VStack(alignment: .leading, spacing: 4) {
                                HStack {
                                    Text(item.place.name)
                                        .font(.body.weight(.semibold))
                                        .foregroundStyle(.primary)
                                    Spacer()
                                    Text("\(item.score)")
                                        .font(.caption.monospacedDigit())
                                        .foregroundStyle(.secondary)
                                }
                                Text([item.place.province, item.place.city, item.place.address].filter { !$0.isEmpty }.joined(separator: " "))
                                    .font(.subheadline)
                                    .foregroundStyle(.secondary)
                                    .lineLimit(2)
                                Text(item.reasons.joined(separator: " · "))
                                    .font(.caption)
                                    .foregroundStyle(item.score < 0 ? Color.red : Color.secondary)
                            }
                            .padding(.vertical, 4)
                        }
                    }
                }
            }
            .navigationTitle("搜索地点")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("关闭") { dismiss() }
                }
            }
            .task { await runSearch() }
        }
    }

    private func summary(_ query: ParsedQuery) -> String {
        var parts = ["搜索词 \(query.keywords)"]
        if let code = query.roadCode { parts.append("编号 \(code)") }
        if let name = query.roadName { parts.append(name) }
        return parts.joined(separator: " · ")
    }

    private func runSearch() async {
        let query = QueryCleaner.parse(keywords)
        parsed = query
        guard !query.keywords.isEmpty else { return }
        isSearching = true
        errorText = nil
        defer { isSearching = false }
        do {
            let places = try await AmapClient.search(
                key: settings.effectiveAmapKey,
                keywords: query.keywords,
                city: city,
                types: AmapRequest.types(for: query)
            )
            var located: [PlaceCandidate] = []
            if QueryCleaner.needsRoadGeocode(query) {
                located = await AmapClient.geocode(
                    key: settings.effectiveAmapKey,
                    address: QueryCleaner.locatePlan(query).geocodeAddress
                )
            }
            results = PlaceRanker.rank(located + places, query: query, cityHint: city)
            if results.isEmpty {
                errorText = "没有找到地点。可以换一个更短的名称，或清空城市后再搜。"
            }
        } catch {
            results = []
            errorText = error.localizedDescription
        }
    }

    private func draft(from item: RankedPlace) -> PointDraft {
        let query = parsed ?? QueryCleaner.parse(keywords)
        let wgs = item.place.wgs84
        let toll = query.wantsToll || item.place.typeCode.hasPrefix("1802")
        return PointDraft(
            id: UUID().uuidString,
            isNew: true,
            name: item.place.name,
            type: toll ? .bannedToll : .other,
            roadCode: query.roadCode ?? "",
            roadName: query.roadName ?? "",
            notes: item.place.address,
            latitude: wgs.latitude,
            longitude: wgs.longitude,
            fineYuan: seed.fineYuan.map(String.init) ?? "",
            penaltyPoints: seed.penaltyPoints.map(String.init) ?? "",
            violation: seed.violation,
            source: seed.source,
            province: item.place.province,
            city: item.place.city
        )
    }
}
