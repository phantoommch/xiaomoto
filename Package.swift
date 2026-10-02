// swift-tools-version:5.9
import PackageDescription

// Core logic only. The iOS app is the Xcode project; this package exists so
// `swift test` can run the same unit tests on macOS or Linux.
let package = Package(
    name: "MotocamCore",
    platforms: [
        .macOS(.v13),
        .iOS(.v17),
    ],
    products: [
        .library(name: "MotocamCore", targets: ["MotocamCore"]),
    ],
    targets: [
        .target(name: "MotocamCore", path: "Motocam/Core"),
        .testTarget(
            name: "MotocamCoreTests",
            dependencies: ["MotocamCore"],
            path: "MotocamTests",
            exclude: ["Fixtures"]
        ),
    ]
)
