// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "MonacoCore",
    platforms: [
        .macOS(.v13),
        .iOS(.v17),
    ],
    products: [
        .library(name: "MonacoCore", targets: ["MonacoCore"]),
    ],
    targets: [
        .target(name: "MonacoCore"),
        .testTarget(
            name: "MonacoCoreTests",
            dependencies: ["MonacoCore"],
            resources: [
                .process("Fixtures"),
            ]
        ),
    ]
)
