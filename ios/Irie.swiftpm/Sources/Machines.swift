import SwiftUI

// Machines: each paired machine, whether it answers, its Irie and plan use;
// forget one, and add one with a QR code or its address and a code.

@MainActor struct MachinesSheet: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var flow = PairFlow()
    @State private var forgetting: String?

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    ForEach(app.conns, id: \.mid) { c in
                        MachineCard(c: c, forgetting: $forgetting)
                    }
                    Label("Notifications: use the Home Screen web app for now.", systemImage: "bell.slash")
                        .font(.system(size: 13.5)).foregroundStyle(Theme.muted)
                        .padding(.horizontal, 6).padding(.vertical, 2)
                    Text("Add a machine").font(Theme.rounded(15, .bold)).foregroundStyle(Theme.muted)
                        .padding(.horizontal, 4).padding(.top, 8)
                    PairForm(flow: flow)
                }
                .padding(16)
                .frame(maxWidth: 560)
                .frame(maxWidth: .infinity)
            }
            .scrollDismissesKeyboard(.interactively)
            .background(Theme.sidebar)
            .navigationTitle("Machines")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() }.fontWeight(.semibold) }
            }
        }
        .presentationDragIndicator(.visible)
    }
}

@MainActor private struct MachineCard: View {
    @Environment(AppModel.self) private var app
    let c: Conn
    @Binding var forgetting: String?

    private static let os = ["darwin": "macOS", "linux": "Linux", "windows": "Windows"]

    var body: some View {
        let i = c.info
        // The release, without the build's hash after the +.
        let version = (i?.version ?? c.version ?? "").split(separator: "+").first.map(String.init) ?? ""
        let meta = [version.isEmpty ? "" : "Irie " + version, i?.os.map { Self.os[$0] ?? $0 } ?? "", c.m.host].filter { !$0.isEmpty }.joined(separator: " · ")
        let plan = c.header?.plan
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 9) {
                StatusDot(status: c.status)
                Text(c.m.name).font(Theme.rounded(17, .bold)).foregroundStyle(Theme.ink).lineLimit(1)
                Spacer()
                Text(c.status.words).font(.system(size: 13.5, weight: .semibold))
                    .foregroundStyle(c.status == .connected ? Theme.done : c.status == .unpaired ? Theme.needs : Theme.muted)
            }
            Text(meta).font(.system(size: 13.5)).foregroundStyle(Theme.muted).padding(.leading, 17)
            if let plan, plan.fiveHour != nil || plan.sevenDay != nil {
                VStack(spacing: 6) {
                    if let w = plan.fiveHour { Meter(label: "5 hours", window: w) }
                    if let w = plan.sevenDay { Meter(label: "Week", window: w) }
                }
                .padding(.leading, 17).padding(.top, 4).padding(.bottom, 2)
                .accessibilityElement(children: .contain)
                .accessibilityLabel("Claude plan use")
            }
            if c.status == .unpaired {
                Banner(text: "It no longer knows this phone.", loud: true) {
                    Button("Pair again") { app.sheet = .again(c.m) }.buttonStyle(PrimaryButton(small: true))
                }
            }
            if forgetting == c.mid {
                VStack(alignment: .leading, spacing: 10) {
                    Text("Forget \(c.m.name)? This phone signs out of it. A new code pairs them again.")
                        .font(.system(size: 14.5)).foregroundStyle(Theme.ink)
                        .fixedSize(horizontal: false, vertical: true)
                    HStack(spacing: 8) {
                        Spacer()
                        Button("Cancel") { forgetting = nil }.buttonStyle(SecondaryButton(small: true))
                        Button("Forget") {
                            forgetting = nil
                            Task { await app.forget(c) }
                        }
                        .font(Theme.rounded(14.5, .semibold))
                        .foregroundStyle(Theme.onNeeds)
                        .padding(.horizontal, 14).frame(minHeight: 36)
                        .background(Theme.needs, in: Capsule())
                    }
                }
                .padding(.horizontal, 12).padding(.vertical, 10)
                .background(Theme.needsTint, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            } else {
                HStack(spacing: 6) {
                    Spacer()
                    if c.status == .offline {
                        Button("Retry") { c.kick() }.buttonStyle(SecondaryButton(small: true))
                    }
                    Button("Forget") { forgetting = c.mid }.buttonStyle(SecondaryButton(small: true, danger: true))
                }
            }
        }
        .padding(.leading, 16).padding(.trailing, 14).padding(.top, 14).padding(.bottom, 10)
        .background(Theme.card, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
        .shadow(color: .black.opacity(0.05), radius: 1, y: 1)
    }
}

/// One window of the Claude plan's use.
@MainActor private struct Meter: View {
    var label: String
    var window: Plan.Window

    var body: some View {
        let used = max(0, min(100, window.used ?? 0))
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 9) {
                Text(label).foregroundStyle(Theme.muted).frame(width: 62, alignment: .leading)
                GeometryReader { g in
                    ZStack(alignment: .leading) {
                        Capsule().fill(Theme.panel).overlay(Capsule().strokeBorder(Theme.edge, lineWidth: 0.5))
                        Capsule().fill(used >= 95 ? Theme.needs : used >= 80 ? Theme.paused : Theme.accentFill)
                            .frame(width: g.size.width * used / 100)
                    }
                }
                .frame(height: 7)
                Text("\(Int(used.rounded()))%").fontWeight(.semibold).monospacedDigit().frame(width: 38, alignment: .trailing)
            }
            if let r = window.resetsAt.real {
                Text("resets " + Fmt.clock(r)).font(.system(size: 12)).foregroundStyle(Theme.muted).padding(.leading, 71)
            }
        }
        .font(.system(size: 13))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(label): \(Int(used.rounded()))% used")
    }
}
