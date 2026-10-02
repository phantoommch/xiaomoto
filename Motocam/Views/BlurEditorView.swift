import SwiftUI
import UIKit

struct BlurEditorView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var image: UIImage
    @State private var history: [UIImage] = []
    @State private var didCover = false
    var onDone: (UIImage) -> Void

    init(image: UIImage, onDone: @escaping (UIImage) -> Void) {
        _image = State(initialValue: ImageRedactor.flattened(image))
        self.onDone = onDone
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 12) {
                Text("用手指框住号牌。完成后照片会打码，并去掉拍摄位置等信息。")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                GeometryReader { proxy in
                    let display = aspectFit(imageSize: image.size, in: proxy.size)
                    ZStack {
                        Color.black
                        Image(uiImage: image)
                            .resizable()
                            .frame(width: display.width, height: display.height)
                    }
                    .frame(width: proxy.size.width, height: proxy.size.height)
                    .contentShape(Rectangle())
                    .gesture(
                        DragGesture(minimumDistance: 8)
                            .onEnded { value in
                                let rect = CGRect(
                                    x: min(value.startLocation.x, value.location.x),
                                    y: min(value.startLocation.y, value.location.y),
                                    width: abs(value.location.x - value.startLocation.x),
                                    height: abs(value.location.y - value.startLocation.y)
                                )
                                apply(viewRect: rect, display: display)
                            }
                    )
                }
                .background(Color.black.opacity(0.9))
                .clipShape(RoundedRectangle(cornerRadius: 12))
                HStack {
                    Button("撤销") { undo() }
                        .disabled(history.isEmpty)
                    Spacer()
                    Button("完成") {
                        onDone(image)
                        dismiss()
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(!didCover)
                }
            }
            .padding()
            .navigationTitle("遮挡号牌")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("取消") { dismiss() }
                }
            }
        }
    }

    private func apply(viewRect: CGRect, display: CGRect) {
        let intersection = viewRect.intersection(display)
        guard intersection.width > 8, intersection.height > 8 else { return }
        let scaleX = image.size.width / display.width
        let scaleY = image.size.height / display.height
        let pixel = CGRect(
            x: (intersection.minX - display.minX) * scaleX,
            y: (intersection.minY - display.minY) * scaleY,
            width: intersection.width * scaleX,
            height: intersection.height * scaleY
        )
        history.append(image)
        image = ImageRedactor.cover(image: image, rect: pixel)
        didCover = true
    }

    private func undo() {
        guard let previous = history.popLast() else { return }
        image = previous
        didCover = !history.isEmpty
    }

    private func aspectFit(imageSize: CGSize, in container: CGSize) -> CGRect {
        guard imageSize.width > 0, imageSize.height > 0 else { return .zero }
        let scale = min(container.width / imageSize.width, container.height / imageSize.height)
        let size = CGSize(width: imageSize.width * scale, height: imageSize.height * scale)
        return CGRect(
            x: (container.width - size.width) / 2,
            y: (container.height - size.height) / 2,
            width: size.width,
            height: size.height
        )
    }
}
