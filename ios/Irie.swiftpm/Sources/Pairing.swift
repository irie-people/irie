import SwiftUI
import UIKit

// Pairing (docs/PHONE.md §3). A QR code or link from `irie remote pair`
// carries the machine's address, a code and the whole fingerprint of Irie's
// certificate authority, from a screen the person trusts, so the app pins
// that authority at once. An address and code typed by hand come with no
// fingerprint: the app reads the authority the machine answers with, shows
// the start of its fingerprint, and pins it only once the person says it
// matches what the machine shows. Either way it then sends the code, over
// a connection that trusts that authority alone.

@Observable @MainActor
final class PairFlow {
    enum Step: Equatable { case form, checking, confirm, pairing }

    var address = ""
    var code = ""
    var step: Step = .form
    var problem = ""
    var done = ""
    var scanning = false
    /// Pairing again with a machine this phone knows: its authority is pinned.
    var again: Machine?
    /// What the machine answered with, to confirm.
    var found: (url: URL, name: String, pin: String)?

    init(again: Machine? = nil) {
        self.again = again
    }

    static var device: String { UIDevice.current.model + " app" }

    /// Checks the address and code, then asks to confirm the machine's
    /// authority, or pairs at once with one already pinned.
    func submit(_ app: AppModel) async -> Machine? {
        problem = ""
        done = ""
        let c = code.filter(\.isNumber)
        if let m = again {
            guard c.count == 6 else {
                problem = "The code has 6 digits."
                return nil
            }
            found = (m.url, m.name, m.pin)
            return await pair(app)
        }
        guard let url = Address.normalize(address) else {
            problem = "That doesn’t look like an address."
            return nil
        }
        guard Address.isLocal(url) else {
            problem = "Irie answers only on your own network: use the machine’s .local name, or its address on this Wi-Fi."
            return nil
        }
        guard c.count == 6 else {
            problem = "The code has 6 digits."
            return nil
        }
        step = .checking
        let probe = Gateway(base: url, pin: nil)
        do {
            let info = try await probe.info()
            guard let pin = probe.delegate.seen else {
                throw APIError(code: "untrusted", message: "\(probe.host) didn’t send Irie’s certificate authority, so this phone can’t check it.")
            }
            found = (url, (info.name ?? "").isEmpty ? probe.host : info.name!, pin)
            step = .confirm
            Haptic.tap()
        } catch {
            problem = (error as? APIError)?.message ?? error.localizedDescription
            step = .form
            Haptic.error()
        }
        return nil
    }

    /// The person says the fingerprint matches: pins it and pairs.
    func pair(_ app: AppModel) async -> Machine? {
        guard let f = found else { return nil }
        step = .pairing
        problem = ""
        let g = Gateway(base: f.url, pin: f.pin)
        do {
            let r = try await g.pair(code: code.filter(\.isNumber), device: Self.device)
            let m = app.remember(url: f.url, pin: f.pin, result: r)
            step = .form
            found = nil
            address = ""
            code = ""
            done = "Paired with \(m.name)."
            return m
        } catch {
            problem = (error as? APIError)?.message ?? error.localizedDescription
            step = .form
            found = nil
            Haptic.error()
            return nil
        }
    }

    func mismatch() {
        let host = found.map { Gateway.host($0.url) } ?? "that address"
        found = nil
        step = .form
        problem = "Then don’t pair: something else on this network answered for \(host). Check the address, or scan the QR code on the machine."
        Haptic.error()
    }

    /// Pairs with what a link or a QR code says.
    static func pair(_ link: PairLink, _ app: AppModel) async throws -> Machine {
        let g = Gateway(base: link.base, pin: link.pin)
        let r = try await g.pair(code: link.code, device: device)
        return app.remember(url: link.base, pin: link.pin, result: r)
    }
}

/// The address and code to pair by, a QR scanner, and the fingerprint to
/// confirm. Used on the first screen, in Machines, and to pair again.
@MainActor struct PairForm: View {
    @Environment(AppModel.self) private var app
    @Bindable var flow: PairFlow
    var onPaired: (Machine) -> Void = { _ in }
    @FocusState private var focus: Field?
    @State private var linking: PairLink?

    enum Field { case address, code }

    var body: some View {
        Group {
            if flow.step == .confirm || (flow.step == .pairing && flow.again == nil), let f = flow.found {
                confirm(f)
            } else {
                form
            }
        }
        .fullScreenCover(isPresented: $flow.scanning) {
            ScannerScreen { text in scanned(text) }
        }
        .task {
            // Pairing again needs only the code: ready to type it.
            guard flow.again != nil else { return }
            try? await Task.sleep(nanoseconds: 450_000_000)
            focus = .code
        }
    }

    private var form: some View {
        VStack(alignment: .leading, spacing: 10) {
            if flow.again == nil {
                Button {
                    flow.problem = ""
                    flow.scanning = true
                } label: {
                    Label("Scan the QR code", systemImage: "qrcode.viewfinder")
                }
                .buttonStyle(PrimaryButton(wide: true))
                .disabled(linking != nil)
                if let l = linking {
                    HStack(spacing: 8) {
                        ProgressView().controlSize(.small)
                        Text("Pairing with \(l.name)…").foregroundStyle(Theme.muted)
                    }
                    .font(.system(size: 14.5))
                }
                Text("Or type its address and code")
                    .font(Theme.rounded(14, .semibold)).foregroundStyle(Theme.muted)
                    .padding(.top, 8).padding(.leading, 4)
                TextField("10.0.0.7 or studio.local", text: $flow.address)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.URL)
                    .textContentType(.URL)
                    .submitLabel(.next)
                    .focused($focus, equals: .address)
                    .onSubmit { focus = .code }
                    .fieldStyle()
                    .accessibilityLabel("Address")
            }
            HStack(spacing: 8) {
                TextField("Code", text: $flow.code)
                    .keyboardType(.numberPad)
                    .textContentType(.oneTimeCode)
                    .multilineTextAlignment(.center)
                    .font(Theme.rounded(17, .semibold).monospacedDigit())
                    .kerning(flow.code.isEmpty ? 0 : 4)
                    .focused($focus, equals: .code)
                    .fieldStyle()
                    .onChange(of: flow.code) {
                        let v = String(flow.code.filter(\.isNumber).prefix(6))
                        if v != flow.code { flow.code = v }
                        flow.problem = ""
                    }
                    .accessibilityLabel("Pairing code")
                Button {
                    focus = nil
                    Task { if let m = await flow.submit(app) { onPaired(m) } }
                } label: {
                    if flow.step == .checking || flow.step == .pairing {
                        ProgressView().tint(Theme.onPrimary)
                    } else {
                        Text(flow.again == nil ? "Add" : "Pair")
                    }
                }
                .buttonStyle(PrimaryButton())
                .frame(minWidth: 96)
                .disabled(flow.step != .form || flow.code.count != 6 || (flow.again == nil && flow.address.trimmingCharacters(in: .whitespaces).isEmpty))
            }
            if !flow.problem.isEmpty {
                Text(flow.problem).font(.system(size: 14, weight: .medium)).foregroundStyle(Theme.needs)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if !flow.done.isEmpty {
                Label(flow.done, systemImage: "checkmark.circle.fill").font(.system(size: 14.5, weight: .semibold)).foregroundStyle(Theme.done)
            }
            (Text("On the machine, run ") + Text("irie remote pair").font(Theme.mono(13)) + Text(". It shows a QR code and a code that work for 10 minutes."))
                .font(.system(size: 13.5)).foregroundStyle(Theme.muted)
                .padding(.horizontal, 4).padding(.top, 2)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private func confirm(_ f: (url: URL, name: String, pin: String)) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Is this \(f.name)?").font(Theme.rounded(20, .bold)).foregroundStyle(Theme.ink)
            (Text("\(Gateway.host(f.url)) answered with this certificate authority. Check it against the fingerprint ")
                + Text("irie remote pair").font(Theme.mono(13.5))
                + Text(" shows on the machine (its SHA-256 begins …):"))
                .font(.system(size: 15)).foregroundStyle(Theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            Text(Trust.start(f.pin))
                .font(.system(size: 21, weight: .semibold, design: .monospaced))
                .foregroundStyle(Theme.ink)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 14)
                .background(Theme.card, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
                .accessibilityLabel("Fingerprint: " + Trust.start(f.pin).map { String($0) }.joined(separator: " "))
            Button {
                Task { if let m = await flow.pair(app) { onPaired(m) } }
            } label: {
                if flow.step == .pairing { ProgressView().tint(Theme.onPrimary) } else { Text("It matches: pair") }
            }
            .buttonStyle(PrimaryButton(wide: true))
            .disabled(flow.step == .pairing)
            Button("It doesn’t match") { flow.mismatch() }
                .buttonStyle(SecondaryButton(danger: true))
                .frame(maxWidth: .infinity)
                .disabled(flow.step == .pairing)
            if !flow.problem.isEmpty {
                Text(flow.problem).font(.system(size: 14, weight: .medium)).foregroundStyle(Theme.needs)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    /// What the scanner read: a pairing link pairs, anything else is said.
    private func scanned(_ text: String) -> String? {
        if let url = URL(string: text.trimmingCharacters(in: .whitespacesAndNewlines)), let link = PairLink(url) {
            flow.scanning = false
            linking = link
            Task {
                do {
                    let m = try await PairFlow.pair(link, app)
                    flow.done = "Paired with \(m.name)."
                    onPaired(m)
                } catch {
                    flow.problem = (error as? APIError)?.message ?? error.localizedDescription
                    Haptic.error()
                }
                linking = nil
            }
            return nil
        }
        if text.hasPrefix("http://") || text.hasPrefix("https://") {
            return "That’s the code for the web app. Scan the other one, for the app built in Xcode."
        }
        return "That isn’t a pairing code from Irie."
    }
}

extension View {
    func fieldStyle(height: CGFloat = 46) -> some View {
        font(.system(size: 16))
            .padding(.horizontal, 14)
            .frame(minHeight: height)
            .background(Theme.field, in: RoundedRectangle(cornerRadius: 13, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 13, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
    }
}

/// The first screen, before any machine is paired.
@MainActor struct FirstPair: View {
    @State private var flow = PairFlow()

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 0) {
                    VStack(spacing: 8) {
                        SunMark().frame(width: 64, height: 64)
                        Text("Pair with your machine").font(Theme.rounded(26, .bold)).foregroundStyle(Theme.ink)
                            .multilineTextAlignment(.center).padding(.top, 8)
                        Text("Irie talks to your machine directly, over your Wi-Fi.")
                            .font(.system(size: 16)).foregroundStyle(Theme.muted).multilineTextAlignment(.center).frame(maxWidth: 330)
                    }
                    .padding(.horizontal, 28).padding(.top, 18).padding(.bottom, 30)
                    .frame(maxWidth: .infinity)
                    .environment(\.colorScheme, .dark)
                    .background {
                        UnevenRoundedRectangle(bottomLeadingRadius: 30, bottomTrailingRadius: 30, style: .continuous)
                            .fill(Theme.bar)
                            .padding(.top, -600)
                    }
                    PairForm(flow: flow)
                        .padding(.horizontal, 24).padding(.top, 26).padding(.bottom, 24)
                        .frame(maxWidth: 520)
                }
            }
            .scrollDismissesKeyboard(.interactively)
            .background(Theme.sidebar)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .principal) { Color.clear.frame(width: 1, height: 1) } }
            .toolbarBackground(.hidden, for: .navigationBar)
            .toolbarColorScheme(.dark, for: .navigationBar)
        }
    }
}

/// A pairing link opened from the camera or another app: one tap to pair.
@MainActor struct LinkPairSheet: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    let link: PairLink
    @State private var busy = false
    @State private var problem = ""

    var body: some View {
        VStack(spacing: 14) {
            SunMark().frame(width: 56, height: 56).padding(.top, 28)
            Text("Pair with \(link.name)?").font(Theme.rounded(24, .bold)).foregroundStyle(Theme.ink).multilineTextAlignment(.center)
            Text("Irie on \(Gateway.host(link.base)), with the code and certificate fingerprint from its pairing link.")
                .font(.system(size: 15)).foregroundStyle(Theme.muted).multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            Text(Trust.start(link.pin))
                .font(.system(size: 15, weight: .semibold, design: .monospaced)).foregroundStyle(Theme.muted)
            if !problem.isEmpty {
                Text(problem).font(.system(size: 14.5, weight: .medium)).foregroundStyle(Theme.needs)
                    .multilineTextAlignment(.center).fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 8)
            Button {
                Task { await pair() }
            } label: {
                if busy { ProgressView().tint(Theme.onPrimary) } else { Text(problem.isEmpty ? "Pair" : "Try again") }
            }
            .buttonStyle(PrimaryButton(wide: true))
            .disabled(busy)
            Button("Not now") { dismiss() }
                .font(Theme.rounded(15, .semibold)).foregroundStyle(Theme.accentInk)
                .frame(minHeight: 44)
        }
        .padding(.horizontal, 24).padding(.bottom, 12)
        .frame(maxWidth: 520)
        .frame(maxWidth: .infinity)
        .background(Theme.sidebar)
        .presentationDetents([.medium])
        .presentationDragIndicator(.visible)
    }

    private func pair() async {
        busy = true
        problem = ""
        do {
            let m = try await PairFlow.pair(link, app)
            dismiss()
            app.toast("Paired with \(m.name).")
        } catch {
            problem = (error as? APIError)?.message ?? error.localizedDescription
            Haptic.error()
        }
        busy = false
    }
}

/// Pairing again with a machine that no longer knows this phone.
@MainActor struct AgainSheet: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var flow: PairFlow

    init(machine: Machine) {
        _flow = State(initialValue: PairFlow(again: machine))
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    Text("It no longer knows this phone. A new code pairs them again.")
                        .font(.system(size: 16)).foregroundStyle(Theme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                    PairForm(flow: flow) { m in
                        dismiss()
                        app.toast("Paired with \(m.name).")
                    }
                }
                .padding(20)
            }
            .background(Theme.sidebar)
            .navigationTitle("Pair again with \(flow.again?.name ?? "it")")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Not now") { dismiss() } } }
        }
    }
}
