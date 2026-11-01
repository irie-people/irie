// swift-tools-version: 5.9

// Irie for iPhone, the native app (docs/PHONE.md, ios/README.md). Open this
// folder in Xcode 15 or later, pick your team under Signing & Capabilities,
// and run it on your iPhone.
import AppleProductTypes
import PackageDescription

let package = Package(
    name: "Irie",
    platforms: [.iOS("17.0")],
    products: [
        .iOSApplication(
            name: "Irie",
            targets: ["Irie"],
            bundleIdentifier: "io.github.irie-people.irie",
            displayVersion: "0.1.0",
            bundleVersion: "1",
            appIcon: .asset("AppIcon"),
            accentColor: .asset("AccentColor"),
            supportedDeviceFamilies: [.phone],
            supportedInterfaceOrientations: [.portrait, .landscapeLeft, .landscapeRight],
            capabilities: [
                .camera(purposeString: "Irie reads the QR code your machine shows, to pair with it."),
                .localNetwork(purposeString: "Irie talks to your machines over your own network, as the web app does."),
            ],
            additionalInfoPlistContentFilePath: "Info.plist"
        ),
    ],
    targets: [
        .executableTarget(name: "Irie", path: "Sources"),
    ]
)
