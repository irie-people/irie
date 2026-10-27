// swift-tools-version:5.9

// The native Mac app for Irie (docs/MAC.md). It is a client of the core's
// socket, like the sidebar, and shows the shown agent's live terminal.
import Foundation
import PackageDescription

// IRIE_MAC_NO_SWIFTTERM=1 builds with Apple's frameworks only: the app then
// opens the agent's terminal in Ghostty instead of drawing it itself.
let withSwiftTerm = ProcessInfo.processInfo.environment["IRIE_MAC_NO_SWIFTTERM"] != "1"

let package = Package(
    name: "Irie",
    platforms: [.macOS(.v14)],
    dependencies: withSwiftTerm ? [
        // v1.18.0 is the newest release whose manifest builds with Swift 5.9.
        .package(url: "https://github.com/migueldeicaza/SwiftTerm", exact: "1.18.0"),
    ] : [],
    targets: [
        .executableTarget(
            name: "Irie",
            dependencies: withSwiftTerm ? [.product(name: "SwiftTerm", package: "SwiftTerm")] : [],
            path: "Sources/Irie"
        ),
    ]
)
