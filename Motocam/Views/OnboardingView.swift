import SwiftUI

struct OnboardingView: View {
    var body: some View {
        NavigationStack {
            VStack(alignment: .leading, spacing: 16) {
                Text("标记禁摩收费站、禁摩路段和抓拍点。骑行时在大约 2 公里和 500 米处，用语音、震动和通知提醒你不要驶入。")
                Text("开始骑行时，系统会请你允许「始终」定位，并允许通知。定位只在本机计算距离，不会上传行驶轨迹。通知标题是「\(AppIdentity.displayName)」。")
                    .foregroundStyle(.secondary)
                Text("违法截图只识别交警12123。")
                    .foregroundStyle(.secondary)
                Text("点位可能过时。现场的禁令标志和交警指挥优先。")
                    .foregroundStyle(.secondary)
                Spacer()
            }
            .padding(24)
            .navigationTitle(AppIdentity.displayName)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("开始使用") { dismiss() }
                }
            }
        }
    }

    @Environment(\.dismiss) private var dismiss
}
