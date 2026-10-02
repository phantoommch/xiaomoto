import UIKit

enum ImageRedactor {
    /// Redraws the image so camera EXIF, including GPS, is not carried forward.
    static func flattened(_ image: UIImage, maxSide: CGFloat = 1920) -> UIImage {
        let upright = normalized(image)
        let size = fitted(upright.size, maxSide: maxSide)
        let format = UIGraphicsImageRendererFormat.default()
        format.scale = 1
        format.opaque = true
        let renderer = UIGraphicsImageRenderer(size: size, format: format)
        return renderer.image { _ in
            upright.draw(in: CGRect(origin: .zero, size: size))
        }
    }

    static func jpegData(from image: UIImage, quality: CGFloat = 0.82) -> Data? {
        flattened(image).jpegData(compressionQuality: quality)
    }

    /// Pixelates `rect` in the image's pixel space (the image should already be `.up`).
    static func cover(image: UIImage, rect: CGRect) -> UIImage {
        let bounds = CGRect(origin: .zero, size: image.size)
        let region = rect.integral.intersection(bounds)
        guard region.width >= 4, region.height >= 4 else { return image }
        let format = UIGraphicsImageRendererFormat.default()
        format.scale = 1
        format.opaque = true
        let renderer = UIGraphicsImageRenderer(size: image.size, format: format)
        return renderer.image { context in
            image.draw(in: bounds)
            let block = max(8, min(region.width, region.height) / 10)
            let small = CGSize(width: max(1, region.width / block), height: max(1, region.height / block))
            let tinyFormat = UIGraphicsImageRendererFormat.default()
            tinyFormat.scale = 1
            tinyFormat.opaque = true
            let tiny = UIGraphicsImageRenderer(size: small, format: tinyFormat).image { _ in
                image.draw(in: CGRect(
                    x: -region.minX * small.width / region.width,
                    y: -region.minY * small.height / region.height,
                    width: image.size.width * small.width / region.width,
                    height: image.size.height * small.height / region.height
                ))
            }
            context.cgContext.interpolationQuality = .none
            tiny.draw(in: region)
            UIColor.black.withAlphaComponent(0.28).setFill()
            context.fill(region)
        }
    }

    static func normalized(_ image: UIImage) -> UIImage {
        if image.imageOrientation == .up, image.cgImage != nil { return image }
        let format = UIGraphicsImageRendererFormat.default()
        format.scale = image.scale
        format.opaque = true
        return UIGraphicsImageRenderer(size: image.size, format: format).image { _ in
            image.draw(in: CGRect(origin: .zero, size: image.size))
        }
    }

    private static func fitted(_ size: CGSize, maxSide: CGFloat) -> CGSize {
        let longest = max(size.width, size.height)
        guard longest > maxSide, longest > 0 else { return size }
        let scale = maxSide / longest
        return CGSize(width: floor(size.width * scale), height: floor(size.height * scale))
    }
}
