import SwiftUI
import UIKit

// Irie for iPhone, the native app: every session on each machine this phone
// is paired with, through that machine's gateway (docs/PHONE.md, the web
// app in internal/remote/web it follows). The core still runs every agent
// on its machine, so nothing stops when the phone sleeps or leaves.

@main @MainActor
struct IrieApp: App {
    @State private var app = AppModel()

    init() {
        // Bars without the hairline under them: the dark bars run on into
        // the machines' chips and the pairing hero, as in the web app.
        let a = UINavigationBarAppearance()
        a.configureWithOpaqueBackground()
        a.backgroundColor = UIColor(Theme.sidebar)
        a.shadowColor = .clear
        let bar = UINavigationBar.appearance()
        bar.standardAppearance = a
        bar.scrollEdgeAppearance = a
        bar.compactAppearance = a
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(app)
                .tint(Theme.accent)
        }
    }
}

@MainActor struct RootView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.scenePhase) private var phase

    var body: some View {
        @Bindable var app = app
        ZStack(alignment: .bottom) {
            if app.machines.isEmpty {
                FirstPair()
            } else {
                NavigationStack(path: $app.path) {
                    HomeScreen()
                        .navigationDestination(for: SessionRef.self) { SessionScreen(ref: $0) }
                }
            }
            ToastStack().padding(.bottom, 84)
        }
        .sheet(item: $app.sheet) { sheet in
            switch sheet {
            case .machines: MachinesSheet()
            case .link(let link): LinkPairSheet(link: link)
            case .again(let m): AgainSheet(machine: m)
            }
        }
        .confirmationDialog(app.choice?.choice.question ?? "", isPresented: Binding(get: { app.choice != nil }, set: { if !$0 { app.choice = nil } }),
                            titleVisibility: .visible, presenting: app.choice) { p in
            ForEach(p.choice.options) { o in
                Button(o.label + (o.recommended == true ? " (recommended)" : ""), role: o.key == "cancel" ? .cancel : nil) {
                    Task { await app.answer(p, o.key) }
                }
            }
        } message: { p in
            Text(p.choice.options.compactMap { o in (o.detail ?? "").isEmpty ? nil : "\(o.label): \(o.detail!)" }.joined(separator: "\n"))
        }
        .confirmationDialog("Close “\(app.closing?.session.name ?? "")”?", isPresented: Binding(get: { app.closing != nil }, set: { if !$0 { app.closing = nil } }),
                            titleVisibility: .visible, presenting: app.closing) { p in
            Button("Close", role: .destructive) { Task { await app.close(p.conn, p.session) } }
        } message: { _ in
            Text("Its agent stops and its card goes. Undo brings it back.")
        }
        .alert("Rename", isPresented: Binding(get: { app.renaming != nil }, set: { if !$0 { app.renaming = nil } }), presenting: app.renaming) { p in
            TextField("Name", text: $app.renameText)
            Button("Rename") { Task { await app.rename(p, to: app.renameText) } }
            Button("Cancel", role: .cancel) {}
        }
        .onOpenURL { url in
            guard url.scheme?.lowercased() == "irie" else { return }
            if let link = PairLink(url) {
                app.sheet = .link(link)
            } else {
                app.toast("That pairing link isn’t complete. Open the one `irie remote pair` shows.", error: true)
            }
        }
        .onChange(of: phase) { _, p in
            switch p {
            case .active: app.cameBack()
            case .background: app.wentAway()
            default: break
            }
        }
    }
}
