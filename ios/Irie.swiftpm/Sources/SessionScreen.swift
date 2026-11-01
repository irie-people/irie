import SwiftUI

// One session's screen: its conversation, and at its foot the message box,
// or the needs card while the agent waits on you in its terminal, or why it
// can't take a message now.

@MainActor struct SessionScreen: View {
    @Environment(AppModel.self) private var app
    let ref: SessionRef
    @State private var model: ConversationModel?

    var body: some View {
        Group {
            if let model {
                SessionBody(model: model)
            } else {
                Theme.sidebar.ignoresSafeArea()
            }
        }
        .onAppear {
            if model == nil, let c = app.conn(ref.mid) { model = ConversationModel(conn: c, sid: ref.sid) }
            model?.start()
        }
        .onDisappear { model?.stop() }
    }
}

@MainActor private struct SessionBody: View {
    @Environment(AppModel.self) private var app
    @Environment(\.colorScheme) private var scheme
    @Bindable var model: ConversationModel
    @State private var details = false
    @State private var stuck = true
    @State private var fresh = false
    @State private var bottomVisible = true
    /// When the feed's frame changed (turning, the keyboard, the details),
    /// when its content's height did, and when its foot left the view.
    @State private var viewChangedAt = Date.distantPast
    @State private var contentChangedAt = Date.distantPast
    @State private var leftBottomAt = Date.distantPast
    @FocusState private var typing: Bool

    var body: some View {
        let blocks = model.blocks
        VStack(spacing: 0) {
            if details { DetailsPanel(model: model).transition(.move(edge: .top).combined(with: .opacity)) }
            ZStack(alignment: .bottom) {
                feed(blocks)
                if fresh && !stuck {
                    Button {
                        model.scrollRequest += 1
                    } label: {
                        Label("New", systemImage: "arrow.down")
                            .font(Theme.rounded(14.5, .semibold))
                            .padding(.horizontal, 15).frame(minHeight: 36)
                            .foregroundStyle(Theme.onPrimary)
                            .background(Theme.accent, in: Capsule())
                            .shadow(color: .black.opacity(0.16), radius: 12, y: 6)
                    }
                    .padding(.bottom, 12)
                    .transition(.scale(scale: 0.9).combined(with: .opacity))
                }
            }
        }
        .background(Theme.sidebar)
        .safeAreaInset(edge: .bottom, spacing: 0) { foot }
        .navigationBarTitleDisplayMode(.inline)
        .toolbarRole(.editor)
        .toolbar {
            ToolbarItem(placement: .principal) { title }
            ToolbarItem(placement: .topBarTrailing) { actions }
        }
        // The bar's colour by the scheme in use, so it follows a change of it
        // while the screen is open.
        .toolbarBackground(Theme.bar(scheme), for: .navigationBar)
        .toolbarBackground(.visible, for: .navigationBar)
        .toolbarColorScheme(.dark, for: .navigationBar)
        .onChange(of: model.mode) { model.modeChanged() }
        .onChange(of: model.row?.name) { if let n = model.row?.name { model.lastName = n } }
    }

    // MARK: The bar

    private var title: some View {
        let s = model.row
        let name = s?.name ?? (model.lastName.isEmpty ? "Session" : model.lastName)
        return Button {
            withAnimation(.snappy(duration: 0.28)) { details.toggle() }
        } label: {
            VStack(spacing: 1) {
                Text(name).font(Theme.rounded(17, .semibold)).foregroundStyle(Theme.ink).lineLimit(1)
                HStack(spacing: 5) {
                    if let s {
                        let look = Look(s)
                        StateMark(look: look, color: look.color(redOnDone: model.conn.redOnDone, session: s), size: 12)
                        Text(Look.label(s)).fontWeight(.semibold).foregroundStyle(look.textColor(redOnDone: model.conn.redOnDone, session: s))
                    } else {
                        Text("Closed")
                    }
                    Text("·")
                    Text(model.conn.m.name)
                }
                .font(.system(size: 13))
                .foregroundStyle(Theme.muted)
                .lineLimit(1)
            }
            .frame(maxWidth: 260)
        }
        .buttonStyle(.plain)
        .environment(\.colorScheme, .dark)
        .accessibilityLabel(s.map { "\($0.name), \(Look.label($0)), on \(model.conn.m.name)" } ?? name)
        .accessibilityHint(details ? "Hides the details" : "Shows the details")
    }

    @ViewBuilder
    private var actions: some View {
        if let s = model.row {
            Menu {
                SessionActions(conn: model.conn, s: s, inSession: true)
            } label: {
                Image(systemName: "ellipsis.circle").font(.system(size: 19, weight: .medium)).foregroundStyle(.white)
            }
            .accessibilityLabel("Actions for this session")
        }
    }

    // MARK: The feed

    private func feed(_ blocks: [Block]) -> some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 10) {
                    if model.more {
                        Text("Earlier messages are in its terminal.")
                            .font(.system(size: 13)).foregroundStyle(Theme.muted)
                            .frame(maxWidth: .infinity).padding(.bottom, 4)
                    }
                    ForEach(blocks) { b in
                        BlockView(block: b, model: model)
                    }
                    if blocks.isEmpty && model.needs == nil {
                        EmptyConversation(model: model)
                    }
                    Color.clear.frame(height: 1).id("bottom")
                        .onAppear {
                            bottomVisible = true
                            stuck = true
                            fresh = false
                        }
                        .onDisappear {
                            bottomVisible = false
                            leftBottomAt = Date()
                            // Turning the phone, the keyboard, a reset or rows
                            // that grow move it too: only a scroll of the
                            // person's own unpins.
                            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) {
                                guard !bottomVisible else { return }
                                let moved = Date().timeIntervalSince(viewChangedAt) < 0.8
                                    || abs(leftBottomAt.timeIntervalSince(contentChangedAt)) < 0.5
                                if !moved {
                                    stuck = false
                                } else if stuck {
                                    proxy.scrollTo("bottom", anchor: .bottom)
                                }
                            }
                        }
                }
                .padding(.horizontal, 14).padding(.vertical, 14)
                .frame(maxWidth: 760)
                .frame(maxWidth: .infinity)
                .background {
                    // The conversation as it grows, and as its rows settle
                    // their height: held at its foot while you are there.
                    GeometryReader { g in
                        Color.clear.onChange(of: g.size.height) {
                            contentChangedAt = Date()
                            if stuck && (bottomVisible || Date().timeIntervalSince(leftBottomAt) < 0.5) {
                                proxy.scrollTo("bottom", anchor: .bottom)
                            }
                        }
                    }
                }
            }
            .defaultScrollAnchor(.bottom)
            // Made again once the conversation has come, so its first
            // layout starts from the newest item rather than scrolling
            // there through rows not measured yet.
            .id(model.loaded)
            .scrollDismissesKeyboard(.interactively)
            .background {
                GeometryReader { g in
                    Color.clear.onChange(of: g.size) {
                        viewChangedAt = Date()
                        if stuck { proxy.scrollTo("bottom", anchor: .bottom) }
                    }
                }
            }
            .onChange(of: model.resets) {
                viewChangedAt = Date()
                if stuck { proxy.scrollTo("bottom", anchor: .bottom) }
            }
            .onChange(of: blocks.last?.id) {
                if stuck {
                    proxy.scrollTo("bottom", anchor: .bottom)
                } else if !blocks.isEmpty {
                    withAnimation(.snappy) { fresh = true }
                }
            }
            .onChange(of: model.items.last) { if stuck { proxy.scrollTo("bottom", anchor: .bottom) } }
            .onChange(of: model.scrollRequest) {
                withAnimation(.snappy) { proxy.scrollTo("bottom", anchor: .bottom) }
                fresh = false
            }
        }
    }

    // MARK: The foot

    @ViewBuilder
    private var foot: some View {
        VStack(spacing: 0) {
            Rectangle().fill(Theme.edge).frame(height: 0.5)
            Group {
                switch model.mode {
                case .compose: composer
                case .needs: NeedsCard(model: model)
                case .gone:
                    note("This session was closed.", loud: false) {
                        Button("Reopen") { Task { await app.reopen(model.conn, model.sid) } }.buttonStyle(PrimaryButton(small: true))
                    }
                case .ended:
                    let s = model.row
                    let failed = s?.state == "failed"
                    note(failed ? ((s?.detail).map { "It failed: \($0)" } ?? "It failed to start.")
                         : ((s?.exitCode ?? 0) != 0 ? "It ended with an error (exit code \(s?.exitCode ?? 0))." : "It has ended."), loud: failed) { EmptyView() }
                case .early:
                    let s = model.row
                    note(s?.state == "stalled" ? ((s?.detail).map { "It hasn’t started: \($0)" } ?? "It hasn’t started.")
                         : "Starting… It takes messages once it’s ready.", loud: false) { EmptyView() }
                case .frozen:
                    note("Frozen. Thaw it to send a message.", loud: false) {
                        Button("Thaw") {
                            Task { await app.op(model.conn, "thaw", ["session": model.sid]) }
                        }
                        .buttonStyle(PrimaryButton(small: true))
                    }
                }
            }
            .padding(.horizontal, 10).padding(.top, 8).padding(.bottom, 10)
        }
        .background(Theme.sidebar)
    }

    private var composer: some View {
        VStack(alignment: .leading, spacing: 6) {
            if !model.sendProblem.isEmpty {
                Text(model.sendProblem).font(.system(size: 14, weight: .medium)).foregroundStyle(Theme.needs).padding(.horizontal, 8)
            }
            HStack(alignment: .bottom, spacing: 8) {
                TextField("Message \(model.row?.name ?? "it")…", text: $model.draft, axis: .vertical)
                    .lineLimit(1...6)
                    .font(.system(size: 16))
                    .focused($typing)
                    .padding(.vertical, 6)
                    .accessibilityLabel("Message \(model.row?.name ?? "it")")
                    .onChange(of: model.draft) { if !model.sendProblem.isEmpty { model.sendProblem = "" } }
                SendButton(enabled: !model.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !model.sending, busy: model.sending, label: "Send") {
                    Task { await model.send() }
                }
            }
            .padding(.leading, 15).padding(.trailing, 5).padding(.vertical, 5)
            .background(Theme.field, in: RoundedRectangle(cornerRadius: 23, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 23, style: .continuous).strokeBorder(typing ? Theme.accent : Theme.edge, lineWidth: 1))
        }
    }

    private func note(_ text: String, loud: Bool, @ViewBuilder button: () -> some View) -> some View {
        HStack(spacing: 12) {
            Text(text).font(.system(size: 15)).foregroundStyle(loud ? Theme.needs : Theme.muted)
                .frame(maxWidth: .infinity, alignment: .leading)
                .fixedSize(horizontal: false, vertical: true)
            button()
        }
        .padding(.leading, 16).padding(.trailing, 8).padding(.vertical, 8)
        .frame(minHeight: 50)
        .background(loud ? Theme.needsTint : Theme.panel, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 18, style: .continuous).strokeBorder(loud ? Theme.needs.opacity(0.35) : Theme.edge, lineWidth: 1))
    }
}

// MARK: - Details

@MainActor private struct DetailsPanel: View {
    let model: ConversationModel

    var body: some View {
        ScrollView {
            if let s = model.row {
                Grid(alignment: .leadingFirstTextBaseline, horizontalSpacing: 14, verticalSpacing: 6) {
                    ForEach(rows(s), id: \.0) { k, v, mono in
                        GridRow {
                            Text(k).foregroundStyle(Theme.muted)
                            Text(v)
                                .font(mono ? Theme.mono(13) : .system(size: 14))
                                .foregroundStyle(Theme.ink)
                                .textSelection(.enabled)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                    }
                }
                .font(.system(size: 14))
                .padding(.horizontal, 18).padding(.vertical, 12)
            } else {
                Text("This session was closed.").font(.system(size: 14)).foregroundStyle(Theme.muted).padding(16)
            }
        }
        .frame(maxHeight: 300)
        .fixedSize(horizontal: false, vertical: true)
        .background(Theme.panel)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.edge).frame(height: 0.5) }
    }

    private func rows(_ s: Session) -> [(String, String, Bool)] {
        var out: [(String, String, Bool)] = []
        func add(_ k: String, _ v: String?, _ mono: Bool = false) {
            if let v, !v.isEmpty { out.append((k, v, mono)) }
        }
        add("State", Look.label(s) + ((s.reason ?? "").isEmpty ? "" : " (\(s.reason!))"))
        add("Doing", s.detail)
        add("Call", s.command, true)
        if let l = s.loose { add("Not landed", l.words) }
        add("Repo", s.repo, true)
        add("Branch", s.branch, true)
        add((s.worktree ?? "").isEmpty ? "Folder" : "Worktree", (s.worktree ?? "").isEmpty ? s.cwd : s.worktree, true)
        let agent = [s.profile.map { "+" + $0 }, s.model].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
        let routed = s.routed == true ? " (auto chose it\((s.taskClass ?? "").isEmpty ? "" : " for a \(s.taskClass!) task"))" : ""
        add("Agent", agent.isEmpty && routed.isEmpty ? nil : agent + routed)
        if let t = s.tokens {
            add("Tokens", "\(Fmt.count(t.total)): \(Fmt.count(t.input ?? 0)) in, \(Fmt.count(t.output ?? 0)) out, \(Fmt.count(t.cacheRead ?? 0)) cache read, \(Fmt.count(t.cacheWrite ?? 0)) cache write")
        }
        if let c = s.costUSD { add("Cost", String(format: "$%.2f at list price", c)) }
        if let c = s.context { add("Context", "\(Int(c.rounded()))% full") }
        if let m = s.memory, m > 0 { add("Memory", "\(Int((Double(m) / 1_048_576).rounded())) MB") }
        if let subs = s.subagents, !subs.isEmpty {
            add("Helpers", subs.map { "\($0.type ?? "Helper")\(($0.detail ?? "").isEmpty ? "" : " — " + $0.detail!)" }.joined(separator: "\n"))
        }
        if let w = s.warnings, !w.isEmpty { add("Warnings", w.joined(separator: "\n")) }
        add("Rated", s.rated)
        if let t = s.created.real { add("Started", Fmt.clock(t)) }
        add("Session", s.id, true)
        return out
    }
}

// MARK: - Blocks

@MainActor private struct BlockView: View {
    let block: Block
    let model: ConversationModel

    var body: some View {
        switch block.kind {
        case .user(let pending): UserBubble(item: block.items[0], pending: pending, key: block.id, model: model)
        case .text:
            MarkdownView(block.items[0].text ?? "")
                .textSelection(.enabled)
        case .note:
            HStack(spacing: 10) {
                Rectangle().fill(Theme.edge).frame(height: 0.5)
                Text(block.items[0].text ?? "").font(.system(size: 12.5)).foregroundStyle(Theme.muted)
                    .multilineTextAlignment(.center).layoutPriority(1)
                Rectangle().fill(Theme.edge).frame(height: 0.5)
            }
            .padding(.vertical, 6)
        case .time(let t):
            Text(Fmt.clock(t)).font(.system(size: 12, weight: .semibold)).foregroundStyle(Theme.muted)
                .frame(maxWidth: .infinity).padding(.top, 8)
        case .group: GroupCall(block: block, model: model)
        case .agent(let running, let live): HelperCall(item: block.items[0], running: running, live: live, model: model)
        case .needItem: ToolCall(item: block.items[0], needs: true, model: model)
        case .tool: ToolCall(item: block.items[0], needs: false, model: model)
        }
    }
}

@MainActor private struct UserBubble: View {
    let item: Item
    let pending: Bool
    let key: String
    let model: ConversationModel

    var body: some View {
        let text = item.text ?? ""
        let long = text.split(separator: "\n", omittingEmptySubsequences: false).count > 12 || text.count > 1400
        let open = model.expanded.contains(key)
        var note = ""
        if pending { note = item.queued == true ? "It reads this at its next step" : "On its way" } else if item.queued == true { note = "Sent while it worked" }
        if item.from == "parent" { note = "From its session" }
        return VStack(alignment: .trailing, spacing: 4) {
            Text(text)
                .font(.system(size: 16))
                .lineSpacing(2)
                .foregroundStyle(pending ? Theme.ink : Theme.onAccent)
                .lineLimit(long && !open ? 12 : nil)
                .textSelection(.enabled)
                .padding(.horizontal, 14).padding(.vertical, 9)
                .background {
                    let shape = UnevenRoundedRectangle(topLeadingRadius: 20, bottomLeadingRadius: 20, bottomTrailingRadius: 6, topTrailingRadius: 20, style: .continuous)
                    if pending {
                        shape.strokeBorder(Theme.accentFill, lineWidth: 1.5)
                    } else {
                        shape.fill(Theme.accentFill)
                    }
                }
                .opacity(pending ? 0.62 : 1)
            if long {
                Button(open ? "Show less" : "Show all") {
                    if open { model.expanded.remove(key) } else { model.expanded.insert(key) }
                }
                .font(Theme.rounded(14, .semibold)).foregroundStyle(Theme.accentInk)
            }
            if !note.isEmpty {
                HStack(spacing: 4) {
                    if pending { Image(systemName: "clock").font(.system(size: 11)) }
                    Text(note)
                }
                .font(.system(size: 12.5)).foregroundStyle(Theme.muted)
            }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .padding(.leading, 40)
        .padding(.top, 6).padding(.bottom, 2)
    }
}

@MainActor private struct Changes: View {
    var added: Int?
    var removed: Int?

    var body: some View {
        if (added ?? 0) > 0 || (removed ?? 0) > 0 {
            HStack(spacing: 5) {
                if let a = added, a > 0 { Text("+\(a)").foregroundStyle(Theme.done) }
                if let r = removed, r > 0 { Text("−\(r)").foregroundStyle(Theme.needs) }
            }
            .font(Theme.mono(13).weight(.semibold))
        }
    }
}

@MainActor private struct Chevron: View {
    var open: Bool

    var body: some View {
        Image(systemName: "chevron.right").font(.system(size: 11, weight: .bold)).foregroundStyle(Theme.muted)
            .rotationEffect(.degrees(open ? 90 : 0))
    }
}

@MainActor private struct Mono: View {
    var text: String
    var error = false

    var body: some View {
        Text(text)
            .font(Theme.mono(12.5))
            .lineSpacing(2)
            .foregroundStyle(error ? Theme.needs : Theme.ink)
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 11).padding(.vertical, 9)
            .background(error ? Theme.needsTint : Theme.panel, in: RoundedRectangle(cornerRadius: 11, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 11, style: .continuous).strokeBorder(error ? Theme.needs.opacity(0.3) : Theme.edge, lineWidth: 0.5))
    }
}

private func callButton(_ open: Bool, needs: Bool = false, failed: Bool = false, helper: Bool = false, @ViewBuilder _ content: () -> some View) -> some View {
    HStack(spacing: 9) { content() }
        .font(.system(size: 14.5))
        .padding(.leading, 12).padding(.trailing, 10).padding(.vertical, 8)
        .frame(minHeight: 42)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(needs ? Theme.needsTint : helper ? Theme.panel : Theme.card, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(failed ? Theme.needs.opacity(0.55) : Theme.edge, lineWidth: 0.5))
        .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
}

@MainActor private struct ToolCall: View {
    let item: Item
    let needs: Bool
    let model: ConversationModel

    var body: some View {
        let open = model.expanded.contains(item.id)
        let phrase = needs ? (item.phrase ?? item.text ?? "Needs you") : (item.phrase ?? item.tool ?? "A tool call")
        VStack(alignment: .leading, spacing: 6) {
            Button {
                withAnimation(.snappy(duration: 0.22)) { model.expanded.formSymmetricDifference([item.id]) }
            } label: {
                callButton(open, needs: needs, failed: item.state == "error") {
                    if needs {
                        StateMark(look: .needs, color: Theme.needs, size: 17)
                    } else {
                        CallMark(state: item.state)
                    }
                    Text(phrase).lineLimit(1).truncationMode(.tail)
                        .foregroundStyle(needs ? Theme.needs : Theme.ink).fontWeight(needs ? .semibold : .regular)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    Changes(added: item.added, removed: item.removed)
                    if let ms = item.ms, ms > 0 { Text(Fmt.took(ms)).font(.system(size: 13).monospacedDigit()).foregroundStyle(Theme.muted) }
                    Chevron(open: open)
                }
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(phrase), \(needs ? "needs you" : word(item.state))")
            if open {
                VStack(alignment: .leading, spacing: 6) {
                    if let d = item.detail, !d.isEmpty { Mono(text: d) }
                    if let o = item.output, !o.isEmpty { Mono(text: o, error: true) }
                    if (item.detail ?? "").isEmpty && (item.output ?? "").isEmpty {
                        Text(item.tool ?? "No details").font(.system(size: 13.5)).foregroundStyle(Theme.muted)
                    }
                }
                .padding(.leading, 28)
            }
        }
    }
}

private func word(_ state: String?) -> String {
    ["ok": "done", "error": "failed", "stopped": "stopped"][state ?? ""] ?? "running"
}

@MainActor private struct GroupCall: View {
    let block: Block
    let model: ConversationModel

    var body: some View {
        let items = block.items
        let open = model.expanded.contains(block.id)
        let files = Swift.Set(items.compactMap { $0.detail.flatMap { $0.isEmpty ? nil : Fmt.base($0) } })
        let one = files.count == 1 ? files.first : nil
        let kind = items[0].group ?? ""
        let phrase: String = {
            switch kind {
            case "edit": return one.map { "Edited \($0) \(items.count) times" } ?? "Edited \(files.count) files"
            case "read": return one.map { "Read \($0) \(items.count) times" } ?? "Read \(files.count) files"
            default: return "Searched \(items.count) times"
            }
        }()
        let added = items.reduce(0) { $0 + ($1.added ?? 0) }
        let removed = items.reduce(0) { $0 + ($1.removed ?? 0) }
        let ms = items.reduce(Int64(0)) { $0 + ($1.ms ?? 0) }
        return VStack(alignment: .leading, spacing: 4) {
            Button {
                withAnimation(.snappy(duration: 0.22)) { model.expanded.formSymmetricDifference([block.id]) }
            } label: {
                callButton(open) {
                    CallMark(state: "ok")
                    Text(phrase).lineLimit(1).foregroundStyle(Theme.ink).frame(maxWidth: .infinity, alignment: .leading)
                    Changes(added: added, removed: removed)
                    if ms > 0 { Text(Fmt.took(ms)).font(.system(size: 13).monospacedDigit()).foregroundStyle(Theme.muted) }
                    Chevron(open: open)
                }
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(phrase), done")
            if open {
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(items) { it in SubCall(item: it, model: model) }
                }
                .padding(.leading, 28)
            }
        }
    }
}

@MainActor private struct SubCall: View {
    let item: Item
    let model: ConversationModel

    var body: some View {
        let open = model.expanded.contains(item.id)
        VStack(alignment: .leading, spacing: 2) {
            Button {
                withAnimation(.snappy(duration: 0.2)) { model.expanded.formSymmetricDifference([item.id]) }
            } label: {
                HStack(spacing: 8) {
                    CallMark(state: item.state, size: 15)
                    Text(item.phrase ?? item.tool ?? "A tool call").lineLimit(1).foregroundStyle(Theme.ink)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    Changes(added: item.added, removed: item.removed)
                    if let ms = item.ms, ms > 0 { Text(Fmt.took(ms)).font(.system(size: 12.5).monospacedDigit()).foregroundStyle(Theme.muted) }
                }
                .font(.system(size: 14))
                .padding(.horizontal, 8).padding(.vertical, 6).frame(minHeight: 40)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if open {
                Group {
                    if let d = item.detail, !d.isEmpty { Mono(text: d) } else {
                        Text(item.tool ?? "No details").font(.system(size: 13.5)).foregroundStyle(Theme.muted)
                    }
                }
                .padding(.leading, 31).padding(.bottom, 6)
            }
        }
    }
}

@MainActor private struct HelperCall: View {
    let item: Item
    let running: Bool
    let live: String
    let model: ConversationModel

    var body: some View {
        let open = model.expanded.contains(item.id)
        let task = item.text ?? item.phrase ?? ""
        VStack(alignment: .leading, spacing: 4) {
            Button {
                withAnimation(.snappy(duration: 0.22)) { model.expanded.formSymmetricDifference([item.id]) }
            } label: {
                callButton(open, helper: true) {
                    Text("↳").fontWeight(.bold).foregroundStyle(Theme.muted)
                    (Text(item.agentType ?? "Helper").fontWeight(.semibold).foregroundColor(Theme.ink)
                        + Text(" — " + task).foregroundColor(Theme.muted))
                        .lineLimit(1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    CallMark(state: running ? "running" : item.state)
                    if let ms = item.ms, ms > 0, !running { Text(Fmt.took(ms)).font(.system(size: 13).monospacedDigit()).foregroundStyle(Theme.muted) }
                }
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Helper \(item.agentType ?? ""): \(task), \(word(running ? "running" : item.state))")
            if running, !live.isEmpty {
                Text(live).font(.system(size: 13)).foregroundStyle(Theme.muted).lineLimit(1).padding(.leading, 36)
            }
            if open {
                VStack(alignment: .leading, spacing: 6) {
                    if let n = item.tools, n > 0 { Text(n == 1 ? "1 tool call" : "\(n) tool calls").font(.system(size: 13.5)).foregroundStyle(Theme.muted) }
                    if let d = item.detail, !d.isEmpty { Mono(text: d) }
                }
                .padding(.leading, 28)
            }
        }
    }
}

@MainActor private struct EmptyConversation: View {
    let model: ConversationModel

    var body: some View {
        let e = model.empty
        VStack(spacing: 6) {
            SunMark().frame(width: 56, height: 56)
            Text(e.title).font(Theme.rounded(20, .bold)).foregroundStyle(Theme.ink).multilineTextAlignment(.center).padding(.top, 10)
            if !e.text.isEmpty {
                Text(e.text).font(.system(size: 15.5)).foregroundStyle(Theme.muted).multilineTextAlignment(.center)
                    .frame(maxWidth: 320).padding(.bottom, 10)
            }
            if e.terminal {
                Button {
                    model.toggleTerminal()
                } label: {
                    Label(model.showTerm ? "Hide the terminal" : "Show the terminal", systemImage: "terminal")
                }
                .buttonStyle(SecondaryButton())
                if model.showTerm { TerminalText(model: model).padding(.top, 10) }
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.horizontal, 18).padding(.vertical, 40)
    }
}

/// What the agent's terminal shows, as text.
@MainActor struct TerminalText: View {
    let model: ConversationModel

    var body: some View {
        let text: String = {
            guard let s = model.screen else { return model.screenProblem.isEmpty ? "Reading its screen…" : model.screenProblem }
            let lines = (s.lines ?? []).joined(separator: "\n")
            return lines.isEmpty ? " " : lines
        }()
        ScrollViewReader { proxy in
            ScrollView([.horizontal, .vertical]) {
                Text(text)
                    .font(Theme.mono(11.5))
                    .lineSpacing(2)
                    .foregroundStyle(Theme.termInk)
                    .textSelection(.enabled)
                    .fixedSize()
                    .padding(.horizontal, 12).padding(.vertical, 10)
                    .id("term")
            }
            .defaultScrollAnchor(.bottomLeading)
            .onChange(of: text) { proxy.scrollTo("term", anchor: .bottomLeading) }
        }
        .frame(maxWidth: .infinity, minHeight: 120, maxHeight: 240)
        .background(Theme.termBack, in: RoundedRectangle(cornerRadius: 13, style: .continuous))
        .accessibilityLabel("Its terminal")
    }
}
