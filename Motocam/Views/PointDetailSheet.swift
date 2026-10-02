import SwiftUI

struct PointDetailSheet: View {
    var point: EnforcementPoint
    var distanceMeters: Double?
    var onEdit: () -> Void
    var onDelete: () -> Void
    var onUpload: () -> Void
    var onSaveLocal: (() -> Void)?

    var body: some View {
        NavigationStack {
            List {
                Section {
                    HStack {
                        TypeBadge(type: point.type)
                        if point.isCommunity {
                            Text("社区")
                                .font(.caption.weight(.semibold))
                                .padding(.horizontal, 8)
                                .padding(.vertical, 3)
                                .foregroundStyle(.white)
                                .background(MotocamTheme.community, in: Capsule())
                        }
                        Spacer()
                        Text(point.source.title)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                        if point.uploadSource == "user_upload" {
                            Text("用户上传")
                                .font(.caption.weight(.semibold))
                                .padding(.horizontal, 8)
                                .padding(.vertical, 3)
                                .foregroundStyle(.white)
                                .background(Color.orange, in: Capsule())
                        }
                    }
                    Text(point.name)
                        .font(.title3.weight(.semibold))
                    if !point.roadLabel.isEmpty {
                        LabeledContent("道路", value: point.roadLabel)
                    }
                    if let distanceMeters {
                        LabeledContent("距离", value: formatDistance(distanceMeters))
                    }
                }
                Section("处罚") {
                    LabeledContent("罚款", value: point.fineYuan.map { "\($0) 元" } ?? "未记录")
                    LabeledContent("记分", value: point.penaltyPoints.map { "\($0) 分" } ?? "未记录")
                    if !point.violation.isEmpty {
                        Text(point.violation)
                    }
                }
                if !point.notes.isEmpty || !point.province.isEmpty || !point.city.isEmpty {
                    Section("说明") {
                        if !point.province.isEmpty || !point.city.isEmpty {
                            LabeledContent("地区", value: [point.province, point.city].filter { !$0.isEmpty }.joined(separator: " "))
                        }
                        if !point.notes.isEmpty {
                            Text(point.notes)
                        }
                    }
                }
                if point.isCommunity {
                    Section {
                        Text("已通过审核后公开。\(point.communitySourceCount.map { "有 \($0) 条相互印证的提交。" } ?? "")现场标志优先于地图标记。")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                        if let onSaveLocal {
                            Button("收入本地点位", action: onSaveLocal)
                        }
                    }
                }
                Section {
                    Button("提交到社区", action: onUpload)
                    if !point.isCommunity {
                        Button("编辑", action: onEdit)
                        Button("删除", role: .destructive, action: onDelete)
                    }
                }
                Section("坐标") {
                    LabeledContent("WGS-84", value: String(format: "%.6f, %.6f", point.latitude, point.longitude))
                    let gcj = point.gcj02
                    LabeledContent("地图 GCJ-02", value: String(format: "%.6f, %.6f", gcj.latitude, gcj.longitude))
                }
            }
            .navigationTitle("点位详情")
            .navigationBarTitleDisplayMode(.inline)
        }
    }

    private func formatDistance(_ meters: Double) -> String {
        if meters >= 1000 {
            return String(format: "%.1f 公里", meters / 1000)
        }
        return "\(Int(meters.rounded())) 米"
    }
}
