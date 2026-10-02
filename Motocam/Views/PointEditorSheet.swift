import SwiftUI

struct PointDraft: Identifiable, Equatable {
    var id: String
    var isNew: Bool
    var name: String
    var type: PointType
    var roadCode: String
    var roadName: String
    var notes: String
    var latitude: Double
    var longitude: Double
    var fineYuan: String
    var penaltyPoints: String
    var violation: String
    var source: PointSource
    var province: String
    var city: String

    static func manual(latitude: Double, longitude: Double) -> PointDraft {
        PointDraft(
            id: UUID().uuidString,
            isNew: true,
            name: "",
            type: .bannedToll,
            roadCode: "",
            roadName: "",
            notes: "",
            latitude: latitude,
            longitude: longitude,
            fineYuan: "",
            penaltyPoints: "",
            violation: "",
            source: .manual,
            province: "",
            city: ""
        )
    }

    static func from(point: EnforcementPoint) -> PointDraft {
        PointDraft(
            id: point.id,
            isNew: false,
            name: point.name,
            type: point.type,
            roadCode: point.roadCode,
            roadName: point.roadName,
            notes: point.notes,
            latitude: point.latitude,
            longitude: point.longitude,
            fineYuan: point.fineYuan.map(String.init) ?? "",
            penaltyPoints: point.penaltyPoints.map(String.init) ?? "",
            violation: point.violation,
            source: point.source,
            province: point.province,
            city: point.city
        )
    }

    func makePoint(existing: EnforcementPoint?) -> EnforcementPoint? {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, latitude >= -90, latitude <= 90, longitude >= -180, longitude <= 180 else { return nil }
        return EnforcementPoint(
            id: id,
            name: trimmed,
            type: type,
            roadCode: roadCode.trimmingCharacters(in: .whitespacesAndNewlines).uppercased(),
            roadName: roadName.trimmingCharacters(in: .whitespacesAndNewlines),
            notes: notes.trimmingCharacters(in: .whitespacesAndNewlines),
            latitude: latitude,
            longitude: longitude,
            fineYuan: Int(fineYuan.trimmingCharacters(in: .whitespaces)),
            penaltyPoints: Int(penaltyPoints.trimmingCharacters(in: .whitespaces)),
            violation: violation.trimmingCharacters(in: .whitespacesAndNewlines),
            source: source,
            province: province,
            city: city,
            createdAt: existing?.createdAt ?? Date(),
            updatedAt: Date(),
            isCommunity: false,
            communitySourceCount: existing?.communitySourceCount
        )
    }
}

struct PointEditorSheet: View {
    @Environment(\.dismiss) private var dismiss
    @State var draft: PointDraft
    var onSave: (EnforcementPoint) -> Void

    var body: some View {
        NavigationStack {
            Form {
                Section("地点") {
                    TextField("名称，例如赤石收费站", text: $draft.name)
                    Picker("类型", selection: $draft.type) {
                        ForEach(PointType.allCases) { type in
                            Text(type.title).tag(type)
                        }
                    }
                    TextField("公路编号，例如 G25", text: $draft.roadCode)
                        .textInputAutocapitalization(.characters)
                    TextField("路名，例如长深高速", text: $draft.roadName)
                    TextField("省份", text: $draft.province)
                    TextField("城市", text: $draft.city)
                }
                Section("处罚信息") {
                    TextField("罚款（元）", text: $draft.fineYuan)
                        .keyboardType(.numberPad)
                    TextField("记分", text: $draft.penaltyPoints)
                        .keyboardType(.numberPad)
                    TextField("违法行为", text: $draft.violation, axis: .vertical)
                        .lineLimit(2...4)
                }
                Section("备注") {
                    TextField("现场标志、方向、容易误入的匝道", text: $draft.notes, axis: .vertical)
                        .lineLimit(3...6)
                }
                Section("坐标（WGS-84）") {
                    LabeledContent("纬度", value: String(format: "%.6f", draft.latitude))
                    LabeledContent("经度", value: String(format: "%.6f", draft.longitude))
                    LabeledContent("来源", value: draft.source.title)
                }
            }
            .navigationTitle(draft.isNew ? "添加点位" : "编辑点位")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("取消") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("保存") { save() }
                        .disabled(draft.name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
        }
    }

    private func save() {
        guard let point = draft.makePoint(existing: nil) else { return }
        onSave(point)
        dismiss()
    }
}
