import AVFoundation
import SwiftUI
import UIKit

// A QR scanner for the pairing code `irie remote pair` shows. The Camera
// app reads it too and opens the link in Irie; this saves the trip.

@MainActor struct ScannerScreen: View {
    /// What was read; a problem to show, or nil once it is taken.
    var onCode: (String) -> String?
    @Environment(\.dismiss) private var dismiss
    @State private var access: Access = .asking
    @State private var problem = ""

    enum Access { case asking, granted, denied, none }

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            switch access {
            case .granted:
                CameraView { text in
                    if let p = onCode(text) {
                        problem = p
                        Haptic.error()
                    }
                }
                .ignoresSafeArea()
                RoundedRectangle(cornerRadius: 28, style: .continuous)
                    .strokeBorder(.white.opacity(0.85), lineWidth: 3)
                    .frame(width: 250, height: 250)
                    .accessibilityHidden(true)
            case .asking:
                ProgressView().tint(.white)
            case .denied:
                message("Irie can’t use the camera", "Allow it in Settings › Irie › Camera, or type the machine’s address and code instead.", settings: true)
            case .none:
                message("No camera here", "Open the pairing link instead, or type the machine’s address and code.", settings: false)
            }
            VStack {
                HStack {
                    Spacer()
                    Button {
                        dismiss()
                    } label: {
                        Image(systemName: "xmark").font(.system(size: 17, weight: .bold)).foregroundStyle(.white)
                            .frame(width: 44, height: 44).background(.white.opacity(0.18), in: Circle())
                    }
                    .accessibilityLabel("Close")
                }
                .padding(16)
                Spacer()
                if access == .granted {
                    VStack(spacing: 6) {
                        Text("Scan the code for the app built in Xcode").font(Theme.rounded(17, .semibold))
                        Text(problem.isEmpty ? "`irie remote pair` shows it on the machine." : problem)
                            .font(.system(size: 14.5, weight: problem.isEmpty ? .regular : .semibold))
                            .foregroundStyle(problem.isEmpty ? .white.opacity(0.8) : Color(red: 1, green: 0.6, blue: 0.53))
                            .multilineTextAlignment(.center)
                    }
                    .foregroundStyle(.white)
                    .padding(.horizontal, 20).padding(.vertical, 14)
                    .background(.black.opacity(0.55), in: RoundedRectangle(cornerRadius: 20, style: .continuous))
                    .padding(20)
                }
            }
        }
        .task { await ask() }
    }

    private func message(_ title: String, _ text: String, settings: Bool) -> some View {
        VStack(spacing: 10) {
            Image(systemName: "camera").font(.system(size: 40)).foregroundStyle(.white.opacity(0.7))
            Text(title).font(Theme.rounded(20, .bold)).foregroundStyle(.white)
            Text(text).font(.system(size: 15.5)).foregroundStyle(.white.opacity(0.75)).multilineTextAlignment(.center).frame(maxWidth: 320)
            if settings, let url = URL(string: UIApplication.openSettingsURLString) {
                Button("Open Settings") { UIApplication.shared.open(url) }.buttonStyle(PrimaryButton(small: true)).padding(.top, 6)
            }
        }
        .padding(28)
    }

    private func ask() async {
        guard AVCaptureDevice.default(for: .video) != nil else {
            access = .none
            return
        }
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized: access = .granted
        case .notDetermined: access = await AVCaptureDevice.requestAccess(for: .video) ? .granted : .denied
        default: access = .denied
        }
    }
}

@MainActor private struct CameraView: UIViewControllerRepresentable {
    var onCode: (String) -> Void

    func makeUIViewController(context: Context) -> CameraController {
        let c = CameraController()
        c.onCode = onCode
        return c
    }

    func updateUIViewController(_ c: CameraController, context: Context) {
        c.onCode = onCode
    }
}

final class CameraController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var onCode: ((String) -> Void)?
    private let session = AVCaptureSession()
    private let queue = DispatchQueue(label: "io.github.irie-people.irie.camera")
    private var preview: AVCaptureVideoPreviewLayer?
    private var last = ""
    private var lastAt = Date.distantPast

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        guard let device = AVCaptureDevice.default(for: .video),
              let input = try? AVCaptureDeviceInput(device: device), session.canAddInput(input) else { return }
        session.addInput(input)
        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else { return }
        session.addOutput(output)
        output.setMetadataObjectsDelegate(self, queue: .main)
        output.metadataObjectTypes = [.qr]
        let layer = AVCaptureVideoPreviewLayer(session: session)
        layer.videoGravity = .resizeAspectFill
        view.layer.addSublayer(layer)
        preview = layer
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        preview?.frame = view.bounds
    }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        let s = session
        queue.async { if !s.isRunning { s.startRunning() } }
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        let s = session
        queue.async { if s.isRunning { s.stopRunning() } }
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection) {
        guard let text = (objects.first as? AVMetadataMachineReadableCodeObject)?.stringValue else { return }
        // The same code is read many times a second: take it once.
        if text == last, Date().timeIntervalSince(lastAt) < 2.5 { return }
        last = text
        lastAt = Date()
        onCode?(text)
    }
}
