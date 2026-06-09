// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "MeeptMenuBar",
    platforms: [.macOS(.v13)],
    products: [.executable(name: "MeeptMenuBar", targets: ["MeeptMenuBar"])],
    dependencies: [
        .package(url: "https://github.com/Flight-School/AnyCodable", from: "0.6.7"),
    ],
    targets: [
        .executableTarget(
            name: "MeeptMenuBar",
            dependencies: [
                .product(name: "AnyCodable", package: "AnyCodable"),
            ],
            path: "MeeptMenuBar"
        )
    ]
)
