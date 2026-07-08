// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "Callwire",
    platforms: [.macOS(.v12)],
    products: [
        .library(name: "Callwire", targets: ["Callwire"]),
    ],
    targets: [
        // C core (vendored copy of ../c — see Sources/CCallwire/README for why
        // this isn't a symlink: SPM/git package publishing can drop symlinks,
        // same pitfall hit earlier with the npm package's README.md).
        .target(
            name: "CCallwire",
            path: "Sources/CCallwire",
            publicHeadersPath: "include"
        ),
        .target(
            name: "Callwire",
            dependencies: ["CCallwire"],
            path: "Sources/Callwire"
        ),
        .testTarget(
            name: "CallwireTests",
            dependencies: ["Callwire"],
            path: "Tests/CallwireTests"
        ),
    ]
)
