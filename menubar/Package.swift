// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "MeeptMenuBar",
    platforms: [.macOS(.v13)],
    products: [.executable(name: "MeeptMenuBar", targets: ["MeeptMenuBar"])],
    targets: [
        .executableTarget(
            name: "MeeptMenuBar",
            path: "MeeptMenuBar"
        )
    ]
)
