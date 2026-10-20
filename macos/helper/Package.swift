// swift-tools-version:5.9
import PackageDescription

// tolk-helper：Tolk 在 macOS 上的原生小助手（协议见 PROTOCOL.md）。
// - TolkHelperCore：协议编解码、语言标签映射等纯逻辑（有单元测试）。
// - tolk-helper：可执行文件，AppKit / SwiftUI / Vision / Translation 相关的全部 UI 与系统调用。
let package = Package(
    name: "tolk-helper",
    platforms: [.macOS(.v13)],
    products: [
        .executable(name: "tolk-helper", targets: ["tolk-helper"])
    ],
    targets: [
        .target(name: "TolkHelperCore"),
        .executableTarget(
            name: "tolk-helper",
            dependencies: ["TolkHelperCore"],
            swiftSettings: [
                // Translation.framework 只有较新的系统才有：不让编译器自动强链接，下面改成弱链接，
                // 低版本系统启动时不会因为缺这个库而起不来（apple.* 在低版本里本来就不开放）。
                .unsafeFlags(["-Xfrontend", "-disable-autolink-framework", "-Xfrontend", "Translation"])
            ],
            linkerSettings: [
                .unsafeFlags(["-Xlinker", "-weak_framework", "-Xlinker", "Translation"])
            ]
        ),
        .testTarget(
            name: "TolkHelperCoreTests",
            dependencies: ["TolkHelperCore"]
        )
    ]
)
