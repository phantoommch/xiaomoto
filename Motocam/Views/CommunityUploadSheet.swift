import PhotosUI
import SwiftUI
import UIKit

struct CommunityUploadSheet: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(\.dismiss) private var dismiss

    var point: EnforcementPoint
    @State private var pickerItem: PhotosPickerItem?
    @State private var editorImage: EditableImage?
    @State private var isSending = false
    @State private var message: String?
    @State private var failed = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(point.name).font(.headline)
                    if !point.roadLabel.isEmpty {
                        Text(point.roadLabel).foregroundStyle(.secondary)
                    }
                    InlineNotice(text: "上传后会公开。服务器会去掉照片里的位置信息。")
                }
                if settings.communityBaseURL.trimmingCharacters(in: .whitespaces).isEmpty {
                    Section {
                        Text("还没有服务器地址。点位先留在本机。")
                            .foregroundStyle(.secondary)
                    }
                } else {
                    Section {
                        Button("只上传位置") { Task { await send(image: nil) } }
                            .disabled(isSending)
                        PhotosPicker(selection: $pickerItem, matching: .images) {
                            Label("选择截图并遮挡号牌", systemImage: "eye.slash")
                        }
                        .disabled(isSending)
                        if isSending {
                            ProgressView("正在提交")
                        }
                    }
                }
                if let message {
                    Section {
                        Text(message).foregroundStyle(failed ? Color.red : Color.primary)
                    }
                }
            }
            .navigationTitle("提交到社区")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("关闭") { dismiss() }
                }
            }
            .onChange(of: pickerItem) { _, item in
                guard let item else { return }
                Task { await load(item) }
            }
            .sheet(item: $editorImage) { box in
                BlurEditorView(image: box.image) { covered in
                    Task { await send(image: covered) }
                }
            }
        }
    }

    private func load(_ item: PhotosPickerItem) async {
        do {
            guard let data = try await item.loadTransferable(type: Data.self),
                  let image = UIImage(data: data) else { return }
            editorImage = EditableImage(image: image)
        } catch {
            failed = true
            message = "无法读取这张照片"
        }
    }

    private func send(image: UIImage?) async {
        guard SessionStore.shared.isLoggedIn else {
            SessionStore.shared.requireLogin(hint: LoginCopy.points)
            return
        }
        isSending = true
        failed = false
        defer { isSending = false }
        do {
            let jpeg = image.flatMap { ImageRedactor.jpegData(from: $0) }
            let result = try await CommunityClient.submit(
                baseURL: settings.communityBaseURL,
                deviceID: settings.deviceID,
                point: point,
                imageJPEG: jpeg
            )
            message = result.status == "approved" ? "已纳入公开点位。" : "已提交，等待审核后才会出现在其他骑手的地图上。"
            failed = false
        } catch {
            failed = true
            message = error.localizedDescription
        }
    }
}

struct EditableImage: Identifiable {
    let id = UUID()
    var image: UIImage
}
