import PhotosUI
import SwiftUI
import UIKit

struct TicketImportView: View {
    @Environment(\.dismiss) private var dismiss
    var onRecognized: ([TicketRecord], Data?) -> Void

    @State private var pickerItem: PhotosPickerItem?
    @State private var preview: UIImage?
    @State private var isReading = false
    @State private var errorText: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    PhotosPicker(selection: $pickerItem, matching: .images) {
                        Label("从相册选择违法截图", systemImage: "text.viewfinder")
                    }
                    .disabled(isReading)
                    if isReading {
                        ProgressView("正在识别文字")
                    }
                    if let preview {
                        Image(uiImage: preview)
                            .resizable()
                            .scaledToFit()
                            .frame(maxHeight: 180)
                            .clipShape(RoundedRectangle(cornerRadius: 8))
                    }
                    if let errorText {
                        Text(errorText).foregroundStyle(.red)
                    }
                } footer: {
                    Text("只识别交警12123违法截图。")
                }
            }
            .navigationTitle("违法点上传")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("关闭") { dismiss() }
                        .disabled(isReading)
                }
            }
            .onChange(of: pickerItem) { _, item in
                guard let item else { return }
                Task { await load(item) }
            }
        }
    }

    private func load(_ item: PhotosPickerItem) async {
        isReading = true
        errorText = nil
        defer { isReading = false }
        do {
            guard let data = try await item.loadTransferable(type: Data.self),
                  let image = UIImage(data: data) else {
                throw TicketOCRError.unreadable
            }
            preview = image
            let rawText = try await TicketOCR.recognize(image)
            let fields = TicketParser.parse(rawText)
            if fields.records.isEmpty {
                errorText = "不是交警12123违法截图，不予识别。"
            } else {
                let records = fields.records
                let jpeg = image.jpegData(compressionQuality: 0.82)
                dismiss()
                onRecognized(records, jpeg)
            }
        } catch {
            errorText = "识别失败，不予识别。"
        }
    }
}
