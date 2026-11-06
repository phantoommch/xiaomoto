import UIKit
import Vision

enum TicketOCRError: LocalizedError {
    case unreadable
    case empty

    var errorDescription: String? {
        switch self {
        case .unreadable: return "这张图片无法读取。"
        case .empty: return "没有识别到文字。可以改用下方的文字粘贴。"
        }
    }
}

private final class ResumeBox: @unchecked Sendable {
    private let lock = NSLock()
    private var done = false
    private let continuation: CheckedContinuation<[VNRecognizedTextObservation], Error>

    init(_ continuation: CheckedContinuation<[VNRecognizedTextObservation], Error>) {
        self.continuation = continuation
    }

    func resume(returning value: [VNRecognizedTextObservation]) {
        lock.lock()
        defer { lock.unlock() }
        guard !done else { return }
        done = true
        continuation.resume(returning: value)
    }

    func resume(throwing error: Error) {
        lock.lock()
        defer { lock.unlock() }
        guard !done else { return }
        done = true
        continuation.resume(throwing: error)
    }
}

enum TicketOCR {
    static func recognize(_ image: UIImage) async throws -> String {
        let upright = ImageRedactor.normalized(image)
        guard let cgImage = upright.cgImage else { throw TicketOCRError.unreadable }
        let observations: [VNRecognizedTextObservation] = try await withCheckedThrowingContinuation { continuation in
            let once = ResumeBox(continuation)
            let request = VNRecognizeTextRequest { request, error in
                if let error {
                    once.resume(throwing: error)
                    return
                }
                let lines = (request.results as? [VNRecognizedTextObservation]) ?? []
                once.resume(returning: lines)
            }
            request.recognitionLevel = .accurate
            request.recognitionLanguages = ["zh-Hans", "en-US"]
            request.usesLanguageCorrection = false
            let handler = VNImageRequestHandler(cgImage: cgImage, orientation: .up, options: [:])
            DispatchQueue.global(qos: .userInitiated).async {
                do {
                    try handler.perform([request])
                } catch {
                    once.resume(throwing: error)
                }
            }
        }
        let text = joinLines(observations)
        guard !text.isEmpty else { throw TicketOCRError.empty }
        guard OCRText.evidence(text) == 0, let bigger = magnified(upright) else { return text }
        let extra = (try? await recognizeObservations(bigger)).map(joinLines) ?? ""
        return OCRText.merge(text, extra)
    }

    /// A phone screenshot's burned-in caption is only a few dozen pixels tall.
    /// A second pass at 2x is used only when the first pass saw no pile number.
    private static func magnified(_ image: UIImage, maxSide: CGFloat = 2800) -> UIImage? {
        let longest = max(image.size.width, image.size.height)
        guard longest > 0, longest < maxSide else { return nil }
        let scale = min(CGFloat(2), maxSide / longest)
        guard scale > 1.05 else { return nil }
        let size = CGSize(width: floor(image.size.width * scale), height: floor(image.size.height * scale))
        let format = UIGraphicsImageRendererFormat.default()
        format.scale = 1
        format.opaque = true
        return UIGraphicsImageRenderer(size: size, format: format).image { _ in
            image.draw(in: CGRect(origin: .zero, size: size))
        }
    }

    private static func recognizeObservations(_ image: UIImage) async throws -> [VNRecognizedTextObservation] {
        guard let cgImage = image.cgImage else { throw TicketOCRError.unreadable }
        return try await withCheckedThrowingContinuation { continuation in
            let once = ResumeBox(continuation)
            let request = VNRecognizeTextRequest { request, error in
                if let error {
                    once.resume(throwing: error)
                    return
                }
                once.resume(returning: (request.results as? [VNRecognizedTextObservation]) ?? [])
            }
            request.recognitionLevel = .accurate
            request.recognitionLanguages = ["zh-Hans", "en-US"]
            request.usesLanguageCorrection = false
            let handler = VNImageRequestHandler(cgImage: cgImage, orientation: .up, options: [:])
            DispatchQueue.global(qos: .userInitiated).async {
                do { try handler.perform([request]) } catch { once.resume(throwing: error) }
            }
        }
    }

    static func joinLines(_ observations: [VNRecognizedTextObservation]) -> String {
        let sorted = observations.sorted { lhs, rhs in
            if abs(lhs.boundingBox.midY - rhs.boundingBox.midY) > 0.015 {
                return lhs.boundingBox.midY > rhs.boundingBox.midY
            }
            return lhs.boundingBox.minX < rhs.boundingBox.minX
        }
        var rows: [[VNRecognizedTextObservation]] = []
        for observation in sorted {
            if let last = rows.last, abs(last[0].boundingBox.midY - observation.boundingBox.midY) < 0.015 {
                rows[rows.count - 1].append(observation)
            } else {
                rows.append([observation])
            }
        }
        return rows.map { row in
            row.sorted { $0.boundingBox.minX < $1.boundingBox.minX }
                .compactMap { $0.topCandidates(1).first?.string }
                .joined(separator: " ")
        }
        .filter { !$0.isEmpty }
        .joined(separator: "\n")
    }
}
