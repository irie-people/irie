import SwiftUI

// The sessions: a card per session in the core's order, each machine a
// section, parked last; a state change never moves a card. Red when it
// needs you. A swipe parks or closes, a long press shows every action, and
// the task bar at the foot starts work on any machine.

@MainActor struct HomeScreen: View {
    @Environment(AppModel.self) private var app
    /// One clock for every card's age, which moves on every 30 s.
    @State private var now = Date()

    var body: some View {
        let loud = app.conns.reduce(0) { n, c in
            c.status == .unpaired ? n : n + c.sessions.values.filter { Look($0).loud }.count
        }
        List {
            if app.filter {
                HStack {
                    Text("Only the \(loud) that need\(loud == 1 ? "s" : "") you").foregroundStyle(Theme.muted)
                    Spacer()
                    Button("Show all") { app.filter = false }.foregroundStyle(Theme.accentInk).fontWeight(.semibold)
                }
                .font(.system(size: 14))
                .plainRow(EdgeInsets(top: 4, leading: 22, bottom: 4, trailing: 22))
            }
            ForEach(app.conns, id: \.mid) { c in
                MachineRows(c: c, multi: app.conns.count > 1)
            }
            EmptyHome()
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .contentMargins(.top, 10, for: .scrollContent)
        .contentMargins(.bottom, 14, for: .scrollContent)
        .environment(\.defaultMinListRowHeight, 1)
        .background(Theme.sidebar)
        .refreshable {
            app.kickAll()
            try? await Task.sleep(nanoseconds: 800_000_000)
        }
        .environment(\.now, now)
        .task {
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 30_000_000_000)
                now = Date()
            }
        }
        .safeAreaInset(edge: .top, spacing: 0) { MachineChips() }
        .safeAreaInset(edge: .bottom, spacing: 0) { TaskBar() }
        .navigationTitle("Irie")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                HStack(spacing: 8) {
                    SunMark().frame(width: 30, height: 30)
                    Text("Irie").font(Theme.rounded(26, .bold)).foregroundStyle(.white)
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("Irie")
                .accessibilityAddTraits(.isHeader)
            }
            ToolbarItem(placement: .principal) { Color.clear.frame(width: 1, height: 1) }
            ToolbarItem(placement: .topBarTrailing) { NeedsPill(count: loud) }
        }
        // The bar's ground is the chips' own, which runs up under it, so
        // the two read as one dark header.
        .toolbarBackground(.hidden, for: .navigationBar)
        .toolbarColorScheme(.dark, for: .navigationBar)
        .onChange(of: loud) { if loud == 0 { app.filter = false } }
    }
}

extension View {
    /// A list row with no separator or ground of its own.
    func plainRow(_ insets: EdgeInsets = EdgeInsets(top: 4, leading: 12, bottom: 4, trailing: 12)) -> some View {
        listRowInsets(insets)
            .listRowSeparator(.hidden)
            .listRowBackground(Color.clear)
    }
}

/// How many sessions need you, on every machine; a tap shows only them.
@MainActor private struct NeedsPill: View {
    @Environment(AppModel.self) private var app
    var count: Int

    var body: some View {
        if count > 0 {
            Button {
                Haptic.pick()
                withAnimation(.snappy) { app.filter.toggle() }
            } label: {
                HStack(spacing: 6) {
                    if !app.filter { StateMark(look: .needs, color: Theme.needs, size: 14) }
                    Text(app.filter ? "Show all" : "\(count) need\(count == 1 ? "s" : "") you")
                        .font(Theme.rounded(14.5, .semibold))
                        .foregroundStyle(app.filter ? Theme.ink : Theme.needs)
                }
                .padding(.leading, app.filter ? 13 : 10).padding(.trailing, 13)
                .frame(minHeight: 34)
                .background((app.filter ? Color.white.opacity(0.14) : Theme.needs.opacity(0.16)), in: Capsule())
                .overlay(Capsule().strokeBorder(app.filter ? Color.white.opacity(0.25) : Theme.needs.opacity(0.35), lineWidth: 1))
            }
            .buttonStyle(PressStyle(scale: 0.95))
            .environment(\.colorScheme, .dark)
            .accessibilityAddTraits(app.filter ? .isSelected : [])
        }
    }
}

/// Each machine's chip, under the bar; a tap shows the machines.
@MainActor private struct MachineChips: View {
    @Environment(AppModel.self) private var app

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(app.conns, id: \.mid) { c in
                    Button {
                        app.sheet = .machines
                    } label: {
                        HStack(spacing: 7) {
                            StatusDot(status: c.status)
                            Text(c.m.name).font(Theme.rounded(14.5, .semibold)).foregroundStyle(Theme.ink)
                            if c.status != .connected {
                                Text(c.status.words).font(.system(size: 13, weight: .medium)).foregroundStyle(Theme.muted)
                            }
                        }
                        .padding(.leading, 11).padding(.trailing, 13)
                        .frame(minHeight: 34)
                        .background(Color.white.opacity(0.1), in: Capsule())
                        .overlay(Capsule().strokeBorder(Color.white.opacity(0.14), lineWidth: 1))
                    }
                    .buttonStyle(PressStyle(scale: 0.96))
                    .accessibilityLabel("\(c.m.name): \(c.status.words). Shows the machines.")
                }
                Button {
                    app.sheet = .machines
                } label: {
                    Image(systemName: "desktopcomputer").font(.system(size: 16, weight: .medium)).foregroundStyle(Theme.muted)
                        .padding(.horizontal, 11).frame(minHeight: 34)
                        .background(Color.white.opacity(0.1), in: Capsule())
                        .overlay(Capsule().strokeBorder(Color.white.opacity(0.14), lineWidth: 1))
                }
                .buttonStyle(PressStyle(scale: 0.96))
                .accessibilityLabel("Machines")
            }
            .padding(.horizontal, 16)
            .padding(.top, 4).padding(.bottom, 12)
        }
        .environment(\.colorScheme, .dark)
        .background {
            UnevenRoundedRectangle(bottomLeadingRadius: 24, bottomTrailingRadius: 24, style: .continuous)
                .fill(Theme.bar)
                .ignoresSafeArea(edges: .top)
        }
        .overlay(alignment: .bottom) {
            UnevenRoundedRectangle(bottomLeadingRadius: 24, bottomTrailingRadius: 24, style: .continuous)
                .stroke(Theme.barEdge, lineWidth: 0.5).padding(.top, -1)
        }
    }
}

/// One machine's rows: its head, a note when it can't be reached, its
/// cards, Parked and Closed.
@MainActor private struct MachineRows: View {
    @Environment(AppModel.self) private var app
    let c: Conn
    let multi: Bool

    var body: some View {
        let (live, parked) = c.rows
        let pick: ([Session]) -> [Session] = { list in app.filter ? list.filter { Look($0).loud } : list }
        let unpaired = c.status == .unpaired
        let liveShown = unpaired ? [] : pick(live)
        let parkedShown = unpaired ? [] : pick(parked)
        let open = app.filter || (app.parkedOpen[c.mid] ?? false)
        let parkedLoud = parked.filter { Look($0).loud }.count
        let closed = app.filter || unpaired ? [] : c.closed
        let closedOpen = app.closedOpen[c.mid] ?? false
        let hidden = app.filter && liveShown.isEmpty && parkedShown.isEmpty
        let stale = c.status != .connected

        if !hidden {
            if multi {
                HStack(spacing: 8) {
                    StatusDot(status: c.status)
                    Text(c.m.name).font(Theme.rounded(15, .bold)).foregroundStyle(Theme.ink)
                    Spacer()
                    Text("\(c.sessions.count)").font(Theme.rounded(13, .semibold).monospacedDigit()).foregroundStyle(Theme.muted)
                }
                .plainRow(EdgeInsets(top: 14, leading: 22, bottom: 4, trailing: 22))
                .accessibilityAddTraits(.isHeader)
            }
            if unpaired {
                Banner(text: "\(c.m.name) no longer knows this phone.", loud: true) {
                    Button("Pair again") { app.sheet = .again(c.m) }.buttonStyle(PrimaryButton(small: true))
                }
                .plainRow()
            } else if c.status == .offline && c.loaded {
                Banner(text: "Can’t reach \(c.m.name). This is what it said \(Fmt.ageSpoken(c.seen)).") {
                    Button("Retry") { c.kick() }.buttonStyle(SecondaryButton(small: true))
                }
                .plainRow()
            }
            ForEach(liveShown) { s in
                CardRow(c: c, s: s).opacity(stale ? 0.62 : 1)
            }
            if !parkedShown.isEmpty {
                Button {
                    withAnimation(.snappy) {
                        app.parkedOpen[c.mid] = !(app.parkedOpen[c.mid] ?? false)
                        Keep.set("irie.parkedOpen", app.parkedOpen)
                    }
                } label: {
                    HStack(spacing: 7) {
                        Image(systemName: "chevron.right").font(.system(size: 12, weight: .bold)).foregroundStyle(Theme.muted)
                            .rotationEffect(.degrees(open ? 90 : 0))
                        Text("Parked").foregroundStyle(parkedLoud > 0 ? Theme.needs : Theme.ink)
                        if parkedLoud > 0 && !open {
                            Text("· \(parkedLoud) need\(parkedLoud == 1 ? "s" : "") you").font(.system(size: 13.5)).foregroundStyle(Theme.needs)
                        }
                        Spacer()
                        Text("\(parkedShown.count)").font(Theme.rounded(13, .semibold).monospacedDigit()).foregroundStyle(Theme.muted)
                    }
                    .font(Theme.rounded(14.5, .bold))
                    .padding(.horizontal, 10).frame(minHeight: 44)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .plainRow(EdgeInsets(top: 6, leading: 12, bottom: 0, trailing: 12))
                .accessibilityValue(open ? "Open" : "Closed")
                if open {
                    ForEach(parkedShown) { s in
                        CardRow(c: c, s: s).opacity(stale ? 0.62 : 1)
                    }
                }
            }
            if !closed.isEmpty {
                Button {
                    withAnimation(.snappy) { app.closedOpen[c.mid] = !closedOpen }
                } label: {
                    HStack(spacing: 7) {
                        Image(systemName: "chevron.right").font(.system(size: 12, weight: .bold)).foregroundStyle(Theme.muted)
                            .rotationEffect(.degrees(closedOpen ? 90 : 0))
                        Text("Closed").foregroundStyle(Theme.ink)
                        Spacer()
                        Text("\(closed.count)").font(Theme.rounded(13, .semibold).monospacedDigit()).foregroundStyle(Theme.muted)
                    }
                    .font(Theme.rounded(14.5, .bold))
                    .padding(.horizontal, 10).frame(minHeight: 44)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .plainRow(EdgeInsets(top: 0, leading: 12, bottom: 0, trailing: 12))
                .accessibilityValue(closedOpen ? "Open" : "Closed")
                if closedOpen {
                    ForEach(closed) { x in
                        HStack(spacing: 10) {
                            VStack(alignment: .leading, spacing: 1) {
                                Text(x.name).font(Theme.rounded(15, .semibold)).foregroundStyle(Theme.muted).lineLimit(1)
                                Text((x.auto == true ? "Closed itself " : "Closed ") + Fmt.ageSpoken(x.closed))
                                    .font(.system(size: 12.5)).foregroundStyle(Theme.muted)
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                            Button("Reopen") { Task { await app.reopen(c, x.id) } }.buttonStyle(SecondaryButton(small: true))
                        }
                        .padding(.leading, 14).padding(.trailing, 10).padding(.vertical, 9)
                        .background(Theme.panel, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                        .plainRow(EdgeInsets(top: 3, leading: 12, bottom: 3, trailing: 12))
                    }
                }
            }
        }
    }
}

/// A session's card in the list, with its swipe actions and long press.
@MainActor private struct CardRow: View {
    @Environment(AppModel.self) private var app
    let c: Conn
    let s: Session

    var body: some View {
        Button {
            app.open(c, s.id)
        } label: {
            SessionCard(c: c, s: s)
        }
        .buttonStyle(PressStyle(scale: 0.985))
        .plainRow()
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if c.can("close") {
                Button {
                    Task { await app.close(c, s) }
                } label: { Label("Close", systemImage: "xmark") }
                    .tint(Theme.needs)
            }
            if c.can("park") {
                Button {
                    Task { await app.setParked(c, s, !s.isParked) }
                } label: { Label(s.isParked ? "Unpark" : "Park", systemImage: s.isParked ? "tray.and.arrow.up" : "tray.and.arrow.down") }
                    .tint(Theme.accentFill)
            }
        }
        .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: 16, style: .continuous))
        .contextMenu {
            SessionActions(conn: c, s: s)
        }
    }
}

/// One session's card: its mark, name, age, what it does or needs, where it
/// runs and its helpers.
@MainActor struct SessionCard: View {
    @Environment(\.now) private var now
    let c: Conn
    let s: Session

    var body: some View {
        let look = Look(s)
        let loud = look.loud
        let quiet = look.quiet || (look == .done && s.isParked)
        let working = look == .working
        let loose = s.loose != nil && !loud
        let detail = statusLine(look)
        let meta = [(s.repo ?? s.cwd ?? s.worktree).map(Fmt.base) ?? "", s.branch ?? "",
                    (s.profile ?? "").isEmpty ? (s.model ?? "") : "+" + s.profile!].filter { !$0.isEmpty }.joined(separator: " · ")
        let helpers = Array((s.subagents ?? []).prefix(2))
        let more = (s.subagents ?? []).count - helpers.count
        let tint = AgentColor.colors(s)
        HStack(alignment: .top, spacing: 11) {
            StateMark(look: look, color: look.color(redOnDone: c.redOnDone, session: s), size: 18)
                .padding(.top, 2)
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(s.name)
                        .font(Theme.rounded(16.5, quiet ? .medium : .semibold))
                        .foregroundStyle(quiet ? Theme.muted : Theme.ink)
                        .lineLimit(1)
                    if s.isPromoted {
                        Text("▲").font(.system(size: 11)).foregroundStyle(Theme.accentInk).accessibilityLabel("Promoted")
                    }
                    Spacer(minLength: 4)
                    Text(Fmt.age(s.created, now: now)).font(.system(size: 13).monospacedDigit()).foregroundStyle(Theme.muted)
                }
                Text(detail)
                    .font(.system(size: 14.5, weight: loud ? .semibold : .regular))
                    .foregroundStyle(loud ? Theme.needs : loose ? Theme.accentInk : working ? Theme.ink.opacity(0.82) : Theme.muted)
                    .lineLimit(2)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if !meta.isEmpty {
                    Text(meta).font(.system(size: 12.5)).foregroundStyle(Theme.muted).lineLimit(1).padding(.top, 1)
                }
                if !helpers.isEmpty {
                    VStack(alignment: .leading, spacing: 1) {
                        ForEach(helpers) { a in
                            (Text("↳ ") + Text(a.type ?? "Helper").fontWeight(.semibold) + Text(" " + (a.detail ?? "")))
                                .font(.system(size: 13))
                                .foregroundStyle(a.needsYou == true ? Theme.needs : working ? tint.ink : Theme.muted)
                                .lineLimit(1)
                        }
                        if more > 0 {
                            Text("and \(more) more helper\(more == 1 ? "" : "s")").font(.system(size: 13)).foregroundStyle(Theme.muted)
                        }
                    }
                    .padding(.top, 3)
                }
            }
        }
        .padding(.leading, 13).padding(.trailing, 14).padding(.vertical, 12)
        .background {
            let shape = RoundedRectangle(cornerRadius: 16, style: .continuous)
            if loud {
                shape.fill(Theme.needsTint)
            } else if working {
                shape.fill(LinearGradient(stops: [.init(color: tint.wash, location: 0), .init(color: tint.wash2, location: 0.8)],
                                          startPoint: .leading, endPoint: .trailing))
            } else {
                shape.fill(Theme.card)
            }
        }
        .overlay(RoundedRectangle(cornerRadius: 16, style: .continuous).strokeBorder(loud ? Theme.needs.opacity(0.45) : .clear, lineWidth: 1))
        .shadow(color: Color(red: 30 / 255, green: 43 / 255, blue: 34 / 255).opacity(0.07), radius: 1, y: 1)
        .contentShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([s.name, Look.label(s), detail != Look.label(s) ? detail : "", s.isPromoted ? "promoted" : "",
                             "started " + Fmt.ageSpoken(s.created, now: now)].filter { !$0.isEmpty }.joined(separator: ", "))
        .accessibilityHint("Opens its conversation")
    }

    /// The second line: what it needs, what it does, or how it ended.
    private func statusLine(_ look: Look) -> String {
        let d = s.detail ?? ""
        switch look {
        case .starting: return "Starting…"
        case .frozen: return s.freezing == true ? "Freezing…" : Look.label(s)
        case .paused, .pausing: return Look.label(s)
        case .done, .idle:
            if !d.isEmpty { return d }
            if let l = s.loose, !l.words.isEmpty { return l.words }
            return Look.label(s)
        default: return d.isEmpty ? Look.label(s) : d
        }
    }
}

/// What the list says when it has no card to show.
@MainActor private struct EmptyHome: View {
    @Environment(AppModel.self) private var app

    var body: some View {
        let all = app.conns
        let any = all.contains { c in
            c.status != .unpaired && (!c.sessions.isEmpty || !c.closed.isEmpty)
        }
        if !any && !app.filter {
            let names = all.map(\.m.name).joined(separator: " and ")
            VStack(spacing: 6) {
                SunMark().frame(width: 56, height: 56)
                if all.allSatisfy({ $0.status == .unpaired }) {
                    title("Not paired")
                    text("Pair again with a new code from the machine.")
                } else if all.contains(where: { $0.loaded && $0.status == .connected }) {
                    title("Nothing running yet")
                    (Text("Type a task below, like ") + Text("“@parser fix the flaky timeout test”").foregroundColor(Theme.ink).fontWeight(.medium)
                        + Text(". Irie starts an agent on it, in its own worktree."))
                        .font(.system(size: 15.5)).foregroundStyle(Theme.muted).multilineTextAlignment(.center).frame(maxWidth: 320)
                } else if all.contains(where: { $0.status == .connecting && !$0.loaded }) {
                    title("Connecting to \(names)…")
                } else {
                    title("Can’t reach \(names)")
                    text("Is it awake, and is this phone on the same Wi-Fi?")
                    Button("Try again") { app.kickAll() }.buttonStyle(SecondaryButton())
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.horizontal, 32).padding(.vertical, 48)
            .plainRow()
        }
    }

    private func title(_ s: String) -> some View {
        Text(s).font(Theme.rounded(20, .bold)).foregroundStyle(Theme.ink).multilineTextAlignment(.center).padding(.top, 10)
    }

    private func text(_ s: String) -> some View {
        Text(s).font(.system(size: 15.5)).foregroundStyle(Theme.muted).multilineTextAlignment(.center).frame(maxWidth: 320).padding(.bottom, 10)
    }
}


private struct NowKey: EnvironmentKey {
    static let defaultValue = Date()
}

extension EnvironmentValues {
    /// The time the list's ages are measured from.
    var now: Date {
        get { self[NowKey.self] }
        set { self[NowKey.self] = newValue }
    }
}
