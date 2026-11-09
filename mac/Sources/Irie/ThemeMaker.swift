import AppKit
import SwiftUI

/// Make a Theme (View > Theme > Make a Theme…): a theme of your own, from
/// three colours or every one, in light and dark, drawn as you go and worn
/// by Irie's window meanwhile; and the window's layouts, to save and come
/// back to (View > Layout). Your themes and layouts are files (Looks.swift).
@MainActor
struct ThemeMaker: View {
    static let windowID = "make-theme"
    static let size = CGSize(width: 980, height: 720)

    @Environment(Store.self) private var store
    @AppStorage(Palette.defaultsKey) private var theme = Palette.standard.id
    @State private var page: Page
    @State private var draft = ThemeDraft(new: Palette.standard, name: "My Theme")
    /// The set Every Colour shows: light or dark.
    @State private var dark = false
    @State private var everyColour = false
    @State private var problem: String?
    @State private var deleting = false
    @State private var trying: Task<Void, Never>?
    @State private var layoutName = ""
    @State private var withTheme = false
    @FocusState private var naming: Bool

    enum Page: String, CaseIterable, Identifiable {
        case colours, layouts
        var id: String { rawValue }
        var title: String { self == .colours ? "Colours" : "Layouts" }
    }

    /// What it opens on when no menu asked: `--snapshot` draws a theme of
    /// the fixture's own being changed, with every colour open.
    private let opening: LookBook.Request?

    init(page: Page = .colours, opening: LookBook.Request? = nil, everyColour: Bool = false) {
        _page = State(initialValue: page)
        _everyColour = State(initialValue: everyColour)
        self.opening = opening
    }

    private var looks: LookBook { LookBook.shared }

    var body: some View {
        VStack(spacing: 0) {
            Picker("Page", selection: $page) {
                ForEach(Page.allCases) { p in Text(p.title).tag(p) }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .frame(width: 240)
            .padding(.vertical, 10)
            Divider()
            switch page {
            case .colours: colours
            case .layouts: layouts
            }
        }
        .frame(minWidth: 900, minHeight: 620)
        // The window's own colours, not the theme's: a theme being made
        // never leaves its controls hard to read.
        .background(Color(nsColor: .windowBackgroundColor))
        .tint(Theme.accent)
        .onAppear {
            if let r = looks.request ?? opening {
                take(r)
            } else {
                draft = fresh(Palette.named(Palette.chosen), name: newName())
            }
        }
        .onChange(of: looks.request) { if let r = looks.request { take(r) } }
        .onChange(of: draft) { tryOn() }
        .onChange(of: page) { tryOn() }
        // Closed, the window wears the theme in use again; a theme not
        // saved is let go.
        .onDisappear {
            trying?.cancel()
            looks.tryOn(nil)
        }
    }

    /// Opens on what the menu or Settings asked for.
    private func take(_ r: LookBook.Request) {
        looks.request = nil
        problem = nil
        deleting = false
        switch r {
        case .new:
            page = .colours
            draft = fresh(Palette.named(Palette.chosen), name: newName())
        case let .edit(id):
            page = .colours
            draft = looks.theme(id).map(ThemeDraft.init(editing:)) ?? fresh(Palette.named(Palette.chosen), name: newName())
        case let .layouts(saving):
            page = .layouts
            if saving { naming = true }
        }
    }

    /// A new theme starting from a palette.
    private func fresh(_ p: Palette, name: String) -> ThemeDraft {
        ThemeDraft(new: p, name: name, basedOn: looks.theme(p.id)?.basedOn)
    }

    /// "My Theme", or "My Theme 2" and on while that is taken.
    private func newName() -> String {
        let names = Set(Palette.choices.map { $0.name.lowercased() })
        var name = "My Theme"
        var n = 2
        while names.contains(name.lowercased()) {
            name = "My Theme \(n)"
            n += 1
        }
        return name
    }

    /// The window wears the theme being made, a moment after the last
    /// change, while the Colours page shows and it differs from the theme
    /// in use; so a colour dragged round the wheel redraws the window a few
    /// times, not at every step.
    private func tryOn() {
        trying?.cancel()
        let chosen = Palette.named(Palette.chosen)
        let p = draft.palette
        let wear = page == .colours && (p.light != chosen.light || p.dark != chosen.dark)
        trying = Task { @MainActor in
            try? await Task.sleep(nanoseconds: 250_000_000)
            guard !Task.isCancelled else { return }
            LookBook.shared.tryOn(wear ? p : nil)
        }
    }

    // MARK: Colours

    private var colours: some View {
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                ScrollView {
                    colourControls
                        .padding(18)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .frame(width: 370)
                Divider()
                ScrollView {
                    VStack(alignment: .leading, spacing: 10) {
                        sketchLabel("Light")
                        ThemeSketch(palette: draft.palette, dark: false)
                        sketchLabel("Dark")
                            .padding(.top, 6)
                        ThemeSketch(palette: draft.palette, dark: true)
                    }
                    .padding(18)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color(nsColor: .underPageBackgroundColor))
            }
            Divider()
            colourFooter
        }
    }

    private func sketchLabel(_ s: String) -> some View {
        Text(s)
            .font(Theme.font(12, .semibold))
            .foregroundStyle(Color.secondary)
    }

    private var colourControls: some View {
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 6) {
                Text(draft.editing == nil ? "Make a theme" : "Change “\(looks.theme(draft.editing ?? "")?.name ?? draft.name)”")
                    .font(Theme.font(19, .bold))
                    .foregroundStyle(Color.primary)
                Text("Pick three colours and Irie works out the rest, light and dark, keeping every word easy to read. Irie’s window wears the theme while you make it.")
                    .font(.system(size: 12))
                    .foregroundStyle(Color.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Grid(alignment: .leading, horizontalSpacing: 10, verticalSpacing: 10) {
                GridRow {
                    Text("Name").gridColumnAlignment(.trailing)
                    TextField("Name", text: $draft.name)
                        .textFieldStyle(.roundedBorder)
                        .labelsHidden()
                }
                GridRow {
                    Text("Start from")
                    Picker("Start from", selection: Binding(get: { draft.start.id }, set: { restart(from: $0) })) {
                        ForEach(Palette.all) { p in Text(p.name).tag(p.id) }
                        if !looks.themes.isEmpty {
                            Divider()
                            ForEach(looks.themes) { t in Text(t.name).tag(t.id) }
                        }
                    }
                    .labelsHidden()
                }
            }
            .font(.system(size: 13))
            .foregroundStyle(Color.primary)
            section("Your colours") {
                pickRow("Accent", "The selected card’s edge, focus rings, the start button and the waves.", \.accent)
                pickRow("Tint", "The hue of the grounds, the cards and the text. A grey keeps them grey.", \.tint)
                pickRow("Sun", "The sun of Irie’s mark.", \.sun)
                if draft.light.accent != draft.picks.accent, !draft.byHand.contains("light.accent") {
                    Text("In light mode the accent is a little deeper than picked, so it stands out at 3:1 on every ground.")
                        .font(.system(size: 11))
                        .foregroundStyle(Color.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            DisclosureGroup(isExpanded: $everyColour) {
                everyColourList
                    .padding(.top, 8)
            } label: {
                Text("Every colour")
                    .font(Theme.font(13, .semibold))
                    .foregroundStyle(Color.primary)
            }
            contrast
        }
    }

    private func section(_ title: String, @ViewBuilder _ content: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(title)
                .font(Theme.font(13, .semibold))
                .foregroundStyle(Color.primary)
            content()
        }
    }

    /// One of the three picks: its well, its name and use, and its value
    /// to type.
    private func pickRow(_ title: String, _ use: String, _ key: WritableKeyPath<ThemeDraft.Picks, UInt32>) -> some View {
        let value = Binding<UInt32>(get: { draft.picks[keyPath: key] }, set: { v in
            draft.picks[keyPath: key] = v
            draft.rederive()
        })
        return HStack(alignment: .top, spacing: 10) {
            ColorPicker(title, selection: colorBinding(value), supportsOpacity: false)
                .labelsHidden()
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.system(size: 13, weight: .medium)).foregroundStyle(Color.primary)
                Text(use).font(.system(size: 11)).foregroundStyle(Color.secondary).fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 4)
            HexField(value: value)
        }
    }

    private func colorBinding(_ v: Binding<UInt32>) -> Binding<Color> {
        Binding(get: { Color(hex: v.wrappedValue) }, set: { c in
            let hex = c.srgbHex
            if hex != v.wrappedValue { v.wrappedValue = hex }
        })
    }

    /// Every colour of the light or the dark set, by group, each to pick or
    /// type; one set by hand keeps it when the picks above change, until
    /// its ↺ gives it back to them.
    private var everyColourList: some View {
        VStack(alignment: .leading, spacing: 12) {
            Picker("Set", selection: $dark) {
                Text("Light").tag(false)
                Text("Dark").tag(true)
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            ForEach(ColorRole.Kind.allCases, id: \.self) { kind in
                VStack(alignment: .leading, spacing: 6) {
                    Text(kind.title.uppercased())
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundStyle(Color.secondary)
                    ForEach(ColorRole.all.filter { $0.kind == kind }) { role in roleRow(role) }
                }
            }
        }
    }

    private func roleRow(_ role: ColorRole) -> some View {
        let mode = dark ? "dark" : "light"
        let byHand = draft.byHand.contains("\(mode).\(role.key)")
        let value = Binding<UInt32>(get: { draft[dark][keyPath: role.path] }, set: { v in
            draft[dark][keyPath: role.path] = v
            draft.byHand.insert("\(mode).\(role.key)")
        })
        return HStack(spacing: 8) {
            ColorPicker(role.title, selection: colorBinding(value), supportsOpacity: false)
                .labelsHidden()
            Text(role.title)
                .font(.system(size: 12))
                .foregroundStyle(Color.primary)
                .lineLimit(1)
            Spacer(minLength: 4)
            if byHand {
                Button {
                    draft.byHand.remove("\(mode).\(role.key)")
                    draft.rederive()
                } label: {
                    Image(systemName: "arrow.uturn.backward")
                }
                .buttonStyle(.borderless)
                .help("Let the picks above set it again")
            }
            HexField(value: value)
        }
    }

    /// Whether each colour drawn on a ground reads on every one: text at
    /// 4.5:1 and the accent at 3:1 (WCAG 2.2), as Irie's own palettes do.
    @ViewBuilder private var contrast: some View {
        let clashes = ThemeMath.clashes(draft.light, dark: false) + ThemeMath.clashes(draft.dark, dark: true)
        VStack(alignment: .leading, spacing: 8) {
            if clashes.isEmpty {
                Label("Easy to read: text keeps 4.5:1 and the accent 3:1 on every ground, light and dark.", systemImage: "checkmark.circle.fill")
                    .symbolRenderingMode(.multicolor)
                    .foregroundStyle(Color.secondary)
            } else {
                Label(clashes.count == 1 ? "One colour is hard to read on a ground:" : "\(clashes.count) colours are hard to read on a ground:",
                      systemImage: "exclamationmark.triangle.fill")
                    .symbolRenderingMode(.multicolor)
                    .foregroundStyle(Color.primary)
                ForEach(clashes.prefix(4)) { c in
                    Text("\(c.title) · \(String(format: "%.1f", c.ratio)):1 of \(String(format: "%.1f", c.want))")
                        .font(.system(size: 11))
                        .foregroundStyle(Color.secondary)
                }
                if clashes.count > 4 {
                    Text("and \(clashes.count - 4) more")
                        .font(.system(size: 11))
                        .foregroundStyle(Color.secondary)
                }
                Button("Make Them Readable") {
                    draft.light = ThemeMath.fixed(draft.light)
                    draft.dark = ThemeMath.fixed(draft.dark)
                }
                .help("Deepens or lightens each such colour as little as gives it its contrast, keeping its hue")
            }
        }
        .font(.system(size: 12))
        .fixedSize(horizontal: false, vertical: true)
    }

    private var colourFooter: some View {
        HStack(spacing: 10) {
            if let id = draft.editing {
                if deleting {
                    Text("Move “\(looks.theme(id)?.name ?? draft.name)” to the Trash?")
                        .font(.system(size: 12))
                        .foregroundStyle(Color.primary)
                    Button("Move to Trash", role: .destructive) { delete(id) }
                    Button("Keep It") { deleting = false }
                } else {
                    Button("Delete…") { deleting = true }
                }
            }
            Button("Show in Finder") {
                if let id = draft.editing, let t = looks.theme(id) {
                    NSWorkspace.shared.activateFileViewerSelecting([t.file])
                } else {
                    try? FileManager.default.createDirectory(at: LookFiles.themes, withIntermediateDirectories: true)
                    NSWorkspace.shared.activateFileViewerSelecting([LookFiles.themes])
                }
            }
            .help("Each theme is a JSON file you can copy, share or edit")
            Spacer()
            if let problem {
                Text(problem)
                    .font(.system(size: 12))
                    .foregroundStyle(Color(nsColor: .systemRed))
                    .lineLimit(2)
            }
            Button("Start Over") { restart(from: draft.start.id) }
                .disabled(draft.isUnchanged)
            if draft.editing != nil {
                Button("Save as a New Theme") { save(asNew: true) }
                    .disabled(draft.trimmedName.isEmpty)
            }
            Button(draft.editing == nil ? "Save and Use" : "Save") { save(asNew: false) }
                .keyboardShortcut(.defaultAction)
                .disabled(draft.trimmedName.isEmpty)
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 12)
    }

    /// Starts again from a palette: Irie's, or one of yours. A theme of
    /// yours being changed stays the one saved over.
    private func restart(from id: String) {
        let editing = draft.editing
        let name = draft.name
        if editing == id, let t = looks.theme(id) {
            draft = ThemeDraft(editing: t)
        } else {
            draft = fresh(Palette.named(id), name: name)
            draft.editing = editing
        }
    }

    private func save(asNew: Bool) {
        let p = draft.palette
        do {
            let id = try looks.save(p, basedOn: draft.basedOn, as: asNew ? nil : draft.editing)
            if let t = looks.theme(id) { draft = ThemeDraft(editing: t) }
            problem = nil
            store.say("Saved the “\(p.name)” theme, and it is the theme in use.", error: false)
        } catch {
            problem = "Couldn’t save it: \(error.localizedDescription)"
        }
    }

    private func delete(_ id: String) {
        let name = looks.theme(id)?.name ?? draft.name
        do {
            try looks.delete(theme: id)
            take(.new)
            store.say("Moved the “\(name)” theme to the Trash.", error: false)
        } catch {
            problem = "Couldn’t move it to the Trash: \(error.localizedDescription)"
        }
    }

    // MARK: Layouts

    @AppStorage(DetailsPanel.minimizedKey) private var detailsMinimized = false
    @AppStorage(DetailsPanel.heightKey) private var detailsHeight = DetailsPanel.compact
    /// The sidebar as the window has it, read again every second while the
    /// page shows: SwiftUI does not say when its edge is dragged.
    @State private var panes = PaneFacts()

    struct PaneFacts: Equatable {
        var sidebarShown = true
        var sidebarWidth = 290.0
        var windowWidth = 1200.0
    }

    private var layouts: some View {
        HStack(spacing: 0) {
            ScrollView {
                layoutList
                    .padding(18)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(width: 370)
            Divider()
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    Text("The window now")
                        .font(Theme.font(13, .semibold))
                        .foregroundStyle(Color.primary)
                    PaneDiagram(facts: panes, terminal: store.showTerminal, detailsMinimized: detailsMinimized,
                                detailsHeight: detailsHeight, side: store.side)
                        .frame(maxWidth: 560)
                    paneControls
                        .frame(maxWidth: 560)
                }
                .padding(18)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .background(Color(nsColor: .underPageBackgroundColor))
        }
        .task {
            while !Task.isCancelled {
                readPanes()
                try? await Task.sleep(nanoseconds: 1_000_000_000)
            }
        }
    }

    private func readPanes() {
        var f = panes
        if let s = WindowPanes.sidebar {
            f.sidebarShown = s.shown
            if s.shown { f.sidebarWidth = s.width }
        }
        if let w = WindowPanes.width { f.windowWidth = w }
        if f != panes { panes = f }
    }

    private var layoutList: some View {
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 6) {
                Text("Layouts")
                    .font(Theme.font(19, .bold))
                    .foregroundStyle(Color.primary)
                Text("A layout keeps how Irie’s window is arranged: what the right side shows, the sidebar, the details panel, the right sidebar and the text size. Arrange the window, here or by hand, and save it; View › Layout puts it back, and ⌃⌘1 to ⌃⌘9 your first nine.")
                    .font(.system(size: 12))
                    .foregroundStyle(Color.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            VStack(alignment: .leading, spacing: 8) {
                TextField("Name, such as Review", text: $layoutName)
                    .textFieldStyle(.roundedBorder)
                    .focused($naming)
                    .onSubmit { saveLayout() }
                Toggle("Change to \(Palette.named(Palette.chosen).name) with it", isOn: $withTheme)
                    .font(.system(size: 12))
                Button("Save the Window’s Layout") { saveLayout() }
                    .disabled(layoutName.trimmingCharacters(in: .whitespaces).isEmpty)
                if let problem, page == .layouts {
                    Text(problem).font(.system(size: 11)).foregroundStyle(Color(nsColor: .systemRed))
                }
            }
            if !looks.layouts.isEmpty {
                listHeader("Yours")
                ForEach(Array(looks.layouts.enumerated()), id: \.element.id) { i, l in
                    layoutRow(l, key: i < 9 ? "⌃⌘\(i + 1)" : nil)
                }
            }
            listHeader("Built in")
            ForEach(PaneLayout.builtIns) { l in layoutRow(l, key: nil) }
            if !looks.problems.isEmpty {
                listHeader("Files Irie couldn’t read")
                ForEach(looks.problems, id: \.self) { p in
                    Text(p).font(.system(size: 11)).foregroundStyle(Color(nsColor: .systemRed)).textSelection(.enabled)
                }
            }
        }
    }

    private func listHeader(_ s: String) -> some View {
        Text(s.uppercased())
            .font(.system(size: 10, weight: .semibold))
            .foregroundStyle(Color.secondary)
            .padding(.top, 4)
    }

    private func layoutRow(_ l: PaneLayout, key: String?) -> some View {
        HStack(alignment: .center, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(l.name)
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(Color.primary)
                Text(l.summary)
                    .font(.system(size: 11))
                    .foregroundStyle(Color.secondary)
                    .lineLimit(2)
            }
            Spacer(minLength: 4)
            if let key {
                Text(key)
                    .font(.system(size: 11).monospacedDigit())
                    .foregroundStyle(Color.secondary)
            }
            Button("Apply") { looks.apply(l) }
                .controlSize(.small)
            if !l.builtIn {
                Menu {
                    Button("Update to the Window’s Layout") { update(l) }
                    Button("Show in Finder") { NSWorkspace.shared.activateFileViewerSelecting([looks.file(layout: l.id)]) }
                    Divider()
                    Button("Move to Trash") { deleteLayout(l) }
                } label: {
                    Image(systemName: "ellipsis.circle")
                }
                .menuStyle(.borderlessButton)
                .menuIndicator(.hidden)
                .fixedSize()
                .help("More for this layout")
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
        .background(RoundedRectangle(cornerRadius: Theme.radius - 4, style: .continuous).fill(Color(nsColor: .controlBackgroundColor)))
        .overlay(RoundedRectangle(cornerRadius: Theme.radius - 4, style: .continuous).strokeBorder(Color(nsColor: .separatorColor), lineWidth: 1))
    }

    private func saveLayout() {
        let name = layoutName.trimmingCharacters(in: .whitespaces)
        guard !name.isEmpty else { return }
        // A layout of yours with that name is saved over.
        let same = looks.layouts.first { $0.name.caseInsensitiveCompare(name) == .orderedSame }
        do {
            try looks.save(PaneLayout.current(store, name: name, theme: withTheme ? Palette.chosen : nil), as: same?.id)
            layoutName = ""
            problem = nil
            store.say("Saved the “\(name)” layout.", error: false)
        } catch {
            problem = "Couldn’t save it: \(error.localizedDescription)"
        }
    }

    private func update(_ l: PaneLayout) {
        do {
            try looks.save(PaneLayout.current(store, name: l.name, theme: l.theme == nil ? nil : Palette.chosen), as: l.id)
            store.say("The “\(l.name)” layout is the window as it is now.", error: false)
        } catch {
            problem = "Couldn’t save it: \(error.localizedDescription)"
        }
    }

    private func deleteLayout(_ l: PaneLayout) {
        do {
            try looks.delete(layout: l.id)
            store.say("Moved the “\(l.name)” layout to the Trash.", error: false)
        } catch {
            problem = "Couldn’t move it to the Trash: \(error.localizedDescription)"
        }
    }

    /// The window's panes to set here, each at once.
    private var paneControls: some View {
        VStack(alignment: .leading, spacing: 14) {
            Picker("The right side shows", selection: Bindable(store).showTerminal) {
                Text("Conversation").tag(false)
                Text("Terminal").tag(true)
            }
            .pickerStyle(.segmented)
            controlRow("Sidebar", shown: Binding(get: { panes.sidebarShown }, set: { on in
                WindowPanes.setSidebar(shown: on, width: nil)
                panes.sidebarShown = on
            })) {
                widthSlider(value: panes.sidebarWidth, range: 260...420) { w in
                    panes.sidebarWidth = w
                    WindowPanes.setSidebar(shown: true, width: w)
                }
                .disabled(!panes.sidebarShown)
            }
            HStack {
                Text("Details panel")
                Spacer()
                Picker("Details panel", selection: $detailsMinimized) {
                    Text("Open").tag(false)
                    Text("One line").tag(true)
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .frame(width: 180)
            }
            widthSlider(value: detailsHeight, range: 58...400, unit: "pt tall") { h in detailsHeight = h }
                .disabled(detailsMinimized)
            controlRow("Right sidebar", shown: Binding(get: { store.side.open }, set: { on in
                if on { store.openSide(store.side.tab) } else { store.closeSide() }
            })) {
                HStack {
                    Picker("Tab", selection: Binding(get: { store.side.tab }, set: { store.openSide($0) })) {
                        ForEach(SideTab.built) { t in Text(t.title).tag(t) }
                    }
                    .labelsHidden()
                    .frame(width: 130)
                    widthSlider(value: store.side.width, range: SideState.minWidth...SideState.maxWidth) { w in store.setSideWidth(w) }
                }
            }
            HStack {
                Text("Terminal text")
                Spacer()
                Stepper(value: Binding(get: { Int(store.fontSize) }, set: { store.setFontSize(CGFloat($0)) }), in: 9...28) {
                    Text("\(Int(store.fontSize)) pt").monospacedDigit()
                }
            }
        }
        .font(.system(size: 13))
        .foregroundStyle(Color.primary)
    }

    private func controlRow(_ title: String, shown: Binding<Bool>, @ViewBuilder _ more: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Toggle(title, isOn: shown)
                .toggleStyle(.switch)
            more()
        }
    }

    /// A slider that moves the window's pane once it is let go, since each
    /// width the terminal passes through would resize the agent in it.
    private func widthSlider(value: Double, range: ClosedRange<Double>, unit: String = "pt wide",
                             set: @escaping (Double) -> Void) -> some View {
        LetGoSlider(value: value, range: range, unit: unit, set: set)
    }
}

/// A slider that says its value as it moves and sets it once let go.
private struct LetGoSlider: View {
    let value: Double
    let range: ClosedRange<Double>
    let unit: String
    let set: (Double) -> Void
    @State private var moving: Double?

    var body: some View {
        HStack(spacing: 8) {
            Slider(value: Binding(get: { moving ?? value }, set: { moving = $0 }), in: range) { editing in
                if !editing, let m = moving {
                    set(m.rounded())
                    moving = nil
                }
            }
            Text("\(Int((moving ?? value).rounded())) \(unit)")
                .font(.system(size: 11).monospacedDigit())
                .foregroundStyle(.secondary)
                .frame(width: 76, alignment: .trailing)
        }
    }
}

/// A colour as "#RRGGBB" to read or type; a value it can't read is put
/// back.
private struct HexField: View {
    @Binding var value: UInt32
    @State private var text = ""
    @FocusState private var focused: Bool

    var body: some View {
        TextField("#RRGGBB", text: $text)
            .font(.system(size: 11, design: .monospaced))
            .textFieldStyle(.roundedBorder)
            .frame(width: 78)
            .focused($focused)
            .onAppear { text = Swatches.hex(value) }
            .onChange(of: value) { if !focused { text = Swatches.hex(value) } }
            .onChange(of: focused) { if !focused { commit() } }
            .onSubmit(commit)
    }

    private func commit() {
        if let v = Swatches.parse(text), v != value { value = v }
        text = Swatches.hex(value)
    }
}

/// A theme being made: the colours it starts from, the three picks, and
/// both sets as they stand.
struct ThemeDraft: Equatable {
    /// The theme of yours it saves over; nil for a new one.
    var editing: String?
    var name: String
    /// What it started from: one of Irie's palettes or one of yours.
    var start: Palette
    /// The palette of Irie's its file names, whose colours fill in any a
    /// hand edit takes out.
    var basedOn: String
    var picks: Picks
    var light: Swatches
    var dark: Swatches
    /// The colours set by hand, as "light.accent", which the picks leave be.
    var byHand: Set<String> = []

    struct Picks: Equatable {
        var accent, tint, sun: UInt32
    }

    /// A palette's own picks: its light accent, its light window ground
    /// for the tint, and its sun.
    static func picks(of p: Palette) -> Picks { Picks(accent: p.light.accent, tint: p.light.ground, sun: p.light.sun) }

    /// A new theme from a palette; one of yours passes on the palette of
    /// Irie's it started from.
    init(new start: Palette, name: String, basedOn: String? = nil) {
        self.name = name
        self.start = start
        self.basedOn = start.isCustom ? basedOn ?? Palette.standard.id : start.id
        picks = Self.picks(of: start)
        light = start.light
        dark = start.dark
    }

    init(editing t: CustomTheme) {
        editing = t.id
        name = t.name
        start = t.palette
        basedOn = t.basedOn
        picks = Self.picks(of: t.palette)
        light = t.palette.light
        dark = t.palette.dark
    }

    var trimmedName: String { name.trimmingCharacters(in: .whitespaces) }

    var palette: Palette { Palette(id: editing ?? "new-theme", name: trimmedName, light: light, dark: dark) }

    /// Whether the colours are still those it started from.
    var isUnchanged: Bool { light == start.light && dark == start.dark }

    subscript(dark: Bool) -> Swatches {
        get { dark ? self.dark : light }
        set { if dark { self.dark = newValue } else { light = newValue } }
    }

    /// Works both sets out again from the picks, but for colours set by hand.
    mutating func rederive() {
        let from = Self.picks(of: start)
        light = ThemeMath.derive(start.light, dark: false, picks: picks, from: from, keep: kept(dark: false), current: light)
        dark = ThemeMath.derive(start.dark, dark: true, picks: picks, from: from, keep: kept(dark: true), current: dark)
    }

    private func kept(dark: Bool) -> Set<String> {
        let prefix = dark ? "dark." : "light."
        return Set(byHand.filter { $0.hasPrefix(prefix) }.map { String($0.dropFirst(prefix.count)) })
    }
}

/// The colour sums of Make a Theme, in OKLCH (Theme.swift) and WCAG 2.2's
/// contrast ratios.
enum ThemeMath {
    /// The grounds text and marks are drawn on, and the colours drawn on
    /// them with what each must keep there: internal/ui's
    /// TestPalettesMeetWCAG checks Irie's own palettes for the same.
    static let grounds = ["sidebar", "card", "panel", "field", "hover", "selection", "needs_tint", "ground"]
    static let marks: [(key: String, want: Double)] = [
        ("ink", 4.5), ("muted", 4.5), ("needs", 4.5), ("done", 4.5), ("paused", 4.5), ("frozen", 4.5), ("accent_ink", 4.5), ("accent", 3),
    ]

    static func role(_ key: String) -> ColorRole { ColorRole.all.first { $0.key == key }! }

    /// A colour that does not read on a ground.
    struct Clash: Identifiable, Equatable {
        let dark: Bool
        let fg: String
        let bg: String
        let ratio: Double
        let want: Double
        var id: String { "\(dark) \(fg) \(bg)" }
        var title: String { "\(ThemeMath.role(fg).title) on \(ThemeMath.role(bg).title.lowercased()), \(dark ? "dark" : "light")" }
    }

    static func clashes(_ s: Swatches, dark: Bool) -> [Clash] {
        var out: [Clash] = []
        for (key, want) in marks {
            let fg = Theme.luminance(s[keyPath: role(key).path])
            for g in grounds {
                let r = Theme.ratio(fg, Theme.luminance(s[keyPath: role(g).path]))
                if r < want { out.append(Clash(dark: dark, fg: key, bg: g, ratio: r, want: want)) }
            }
        }
        return out
    }

    /// The set with each colour drawn on the grounds that misses its
    /// contrast on one moved deeper on light grounds or lighter on dark
    /// ones, as little as gives it a little over its contrast on every
    /// ground; its hue and chroma stay. With `only`, just those.
    static func fixed(_ s: Swatches, only keys: Set<String>? = nil) -> Swatches {
        var s = s
        let gs = grounds.map { Theme.luminance(s[keyPath: role($0).path]) }
        let lightGrounds = gs.reduce(0, +) / Double(gs.count) > 0.18
        for (key, want) in marks where keys?.contains(key) ?? true {
            let path = role(key).path
            func reads(_ v: UInt32, _ need: Double) -> Bool { gs.allSatisfy { Theme.ratio(Theme.luminance(v), $0) >= need } }
            guard !reads(s[keyPath: path], want) else { continue }
            let o = Theme.oklch(s[keyPath: path])
            let l = reach(o.l, lightGrounds ? 0 : 1) { reads(Theme.hex(l: $0, c: o.c, h: o.h), want + 0.1) }
            s[keyPath: path] = Theme.hex(l: l, c: o.c, h: o.h)
        }
        return s
    }

    /// `from`, or the nearest value toward `to` for which `ok` holds, in 16
    /// halvings, as Theme.swift's reach.
    static func reach(_ from: Double, _ to: Double, _ ok: (Double) -> Bool) -> Double {
        if ok(from) { return from }
        var lo = from, hi = to
        for _ in 0..<16 {
            let mid = (lo + hi) / 2
            if ok(mid) { hi = mid } else { lo = mid }
        }
        return hi
    }

    /// One set worked out from the three picks. The grounds and the text
    /// turn round the wheel as far as the tint turned from the start's own
    /// window ground, and grow as much more or less colourful, within calm
    /// bounds; the accent's colours, the selection and the waves follow the
    /// accent the same way; the sun is the pick; the state colours keep
    /// their meaning. Each keeps the lightness the start gave it, so text
    /// reads as well as it did, and light mode's accent is the pick itself.
    /// Colours in `keep` stay as `current` has them, and any other text or
    /// accent that would then miss its contrast is moved till it reads.
    static func derive(_ start: Swatches, dark: Bool, picks: ThemeDraft.Picks, from: ThemeDraft.Picks,
                       keep: Set<String>, current: Swatches) -> Swatches {
        var s = start
        let a = Theme.oklch(picks.accent), a0 = Theme.oklch(from.accent)
        let t = Theme.oklch(picks.tint), t0 = Theme.oklch(from.tint)
        for role in ColorRole.all {
            if keep.contains(role.key) {
                s[keyPath: role.path] = current[keyPath: role.path]
                continue
            }
            let b = start[keyPath: role.path]
            switch role.kind {
            case .neutral:
                let cap = role.key == "ink" || role.key == "muted" ? 0.09 : 0.05
                s[keyPath: role.path] = turned(b, pick: t, from: t0, cap: cap, unchanged: picks.tint == from.tint)
            case .accent:
                let cap = role.key == "selection" ? 0.08 : 0.17
                s[keyPath: role.path] = turned(b, pick: a, from: a0, cap: cap, unchanged: picks.accent == from.accent)
            case .sun:
                if picks.sun != from.sun { s[keyPath: role.path] = picks.sun }
            case .state:
                break
            }
        }
        if !dark, !keep.contains("accent"), picks.accent != from.accent { s.accent = picks.accent }
        let moved = Set(ColorRole.all.map(\.key)).subtracting(keep)
        return picks == from ? s : fixed(s, only: moved)
    }

    /// A colour turned round the wheel by as much as `pick` turned from
    /// `from`, its chroma scaled by as much, up to `cap`. One near grey, or
    /// a start near grey, takes the pick's hue itself.
    private static func turned(_ v: UInt32, pick: (l: Double, c: Double, h: Double), from: (l: Double, c: Double, h: Double),
                               cap: Double, unchanged: Bool) -> UInt32 {
        if unchanged { return v }
        let b = Theme.oklch(v)
        let grey = 0.006
        let scale = min(pick.c / max(from.c, grey), 4)
        let hue = b.c < grey || from.c < grey ? pick.h : b.h + (pick.h - from.h)
        let c = min(max(b.c, from.c < grey ? grey : 0) * scale, cap)
        return Theme.hex(l: b.l, c: c, h: (hue + 360).truncatingRemainder(dividingBy: 360))
    }
}

extension Color {
    /// The colour as 0xRRGGBB in sRGB.
    var srgbHex: UInt32 {
        let c = NSColor(self).usingColorSpace(.sRGB) ?? .black
        func ch(_ v: CGFloat) -> UInt32 { UInt32((min(max(v, 0), 1) * 255).rounded()) }
        return ch(c.redComponent) << 16 | ch(c.greenComponent) << 8 | ch(c.blueComponent)
    }
}

/// A small Irie window in a palette: the sidebar's cards in their states,
/// the details panel and the task bar, beside a terminal edged in the
/// selected agent's colour, in light or dark, whatever the window's own
/// appearance.
struct ThemeSketch: View {
    let palette: Palette
    let dark: Bool

    private var s: Swatches { dark ? palette.dark : palette.light }
    private func c(_ v: UInt32) -> Color { Color(hex: v) }
    private func mark(_ slot: Int) -> UInt32 { Theme.agentHex(slot: slot, palette: palette, dark: dark, high: false) }
    private func wash(_ slot: Int) -> UInt32 { Theme.agentWashHex(slot: slot, palette: palette, dark: dark, high: false) }
    private func deep(_ slot: Int) -> UInt32 { Theme.agentDeepHex(slot: slot, palette: palette, dark: dark, high: false) }
    private func ink(_ slot: Int) -> UInt32 { Theme.agentInkHex(slot: slot, palette: palette, dark: dark, high: false) }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 12, style: .continuous)
        HStack(spacing: 0) {
            sidebar.frame(width: 210)
            terminal
        }
        .frame(height: 268)
        .background(c(s.ground))
        .clipShape(shape)
        .overlay(shape.strokeBorder(c(s.edge), lineWidth: 1))
        .environment(\.colorScheme, dark ? .dark : .light)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Irie’s window in this theme, \(dark ? "dark" : "light")")
    }

    private var sidebar: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 5) {
                SunMark(sun: c(s.sun), waves: c(s.waves))
                    .frame(width: 15, height: 15)
                Text("Irie")
                    .font(Theme.font(12, .bold))
                    .foregroundStyle(c(s.ink))
                Spacer(minLength: 2)
                Text("◐ 2  ● 1  ✓ 1  ‖ 1")
                    .font(Theme.font(9.5))
                    .foregroundStyle(c(s.muted))
            }
            .padding(.bottom, 3)
            card(mark: "◐", color: c(mark(0)), title: "Docs guide", detail: "Editing README.md",
                 fill: AnyShapeStyle(LinearGradient(colors: [c(s.selection), c(wash(0)), c(deep(0))], startPoint: .leading, endPoint: .trailing)),
                 edge: c(mark(0)), edgeWidth: 1.5)
            card(mark: "◐", color: c(mark(3)), title: "Site copy", detail: "Reading index.html",
                 fill: AnyShapeStyle(c(wash(3))), edge: nil)
            card(mark: "●", color: c(s.needs), title: "API fix", detail: "Asks: may it run the tests?", detailColor: c(s.needs),
                 fill: AnyShapeStyle(c(s.needsTint)), edge: c(s.needs))
            card(mark: "✓", color: c(s.done), title: "Release notes", detail: "Done · 4 min",
                 fill: AnyShapeStyle(c(s.card)), edge: c(s.edge))
            Spacer(minLength: 0)
            VStack(alignment: .leading, spacing: 2) {
                Text("Working · claude · 12k tokens")
                    .font(Theme.font(9.5, .semibold))
                    .foregroundStyle(c(ink(0)))
                Text("Editing README.md")
                    .font(Theme.font(9.5))
                    .foregroundStyle(c(s.muted))
            }
            .padding(6)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(c(s.panel)))
            HStack(spacing: 4) {
                Text("Type a task…")
                    .font(Theme.font(10))
                    .foregroundStyle(c(s.muted))
                Spacer(minLength: 2)
                Circle()
                    .fill(c(s.accentFill))
                    .frame(width: 16, height: 16)
                    .overlay(Image(systemName: "arrow.up").font(.system(size: 8, weight: .bold)).foregroundStyle(c(s.onAccent)))
            }
            .padding(.leading, 8)
            .padding(.trailing, 4)
            .frame(height: 24)
            .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(c(s.field)))
            .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(c(s.accent), lineWidth: 1.2))
        }
        .padding(8)
        .frame(maxHeight: .infinity)
        .background(c(s.sidebar))
    }

    private func card(mark: String, color: Color, title: String, detail: String, detailColor: Color? = nil,
                      fill: AnyShapeStyle, edge: Color?, edgeWidth: CGFloat = 1) -> some View {
        let shape = RoundedRectangle(cornerRadius: 8, style: .continuous)
        return HStack(alignment: .firstTextBaseline, spacing: 5) {
            Text(mark)
                .font(Theme.font(10, .bold))
                .foregroundStyle(color)
                .frame(width: 10)
            VStack(alignment: .leading, spacing: 1) {
                Text(title)
                    .font(Theme.font(10.5, .semibold))
                    .foregroundStyle(c(s.ink))
                Text(detail)
                    .font(Theme.font(9.5))
                    .foregroundStyle(detailColor ?? c(s.muted))
            }
            .lineLimit(1)
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 7)
        .padding(.vertical, 4)
        .background(shape.fill(fill))
        .overlay { if let edge { shape.strokeBorder(edge, lineWidth: edgeWidth) } }
    }

    private var terminal: some View {
        let frame = RoundedRectangle(cornerRadius: 10, style: .continuous)
        let fg = Color(nsColor: Theme.terminalForeground)
        return VStack(alignment: .leading, spacing: 4) {
            VStack(alignment: .leading, spacing: 0) {
                Text("Docs guide")
                    .font(.system(size: 10.5, weight: .semibold))
                    .foregroundStyle(c(s.ink))
                Text("Working · claude · irie/docs-guide")
                    .font(.system(size: 9))
                    .foregroundStyle(c(ink(0)))
            }
            VStack(alignment: .leading, spacing: 3) {
                Text("> tidy the README’s install steps").foregroundStyle(fg.opacity(0.7))
                Text("⏺ Read(docs/DESIGN.md)").foregroundStyle(fg)
                Text("⏺ Update(README.md)").foregroundStyle(fg)
                Text("  ⎿  Added 12 lines, removed 4").foregroundStyle(fg.opacity(0.7))
                Text("✻ Editing README.md…").foregroundStyle(c(mark(0)))
                Spacer(minLength: 0)
            }
            .font(.system(size: 9, design: .monospaced))
            .lineLimit(1)
            .padding(9)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .background(frame.fill(Color(nsColor: Theme.terminalBackground)))
            .overlay(frame.strokeBorder(c(mark(0)), lineWidth: 2))
        }
        .padding(10)
    }
}

/// The window's panes as they stand, drawn small: the sidebar with the
/// details panel and the task bar, the conversation or the terminal, and
/// the right sidebar, each as wide as it is beside the window.
private struct PaneDiagram: View {
    let facts: ThemeMaker.PaneFacts
    let terminal: Bool
    let detailsMinimized: Bool
    let detailsHeight: Double
    let side: SideState

    var body: some View {
        GeometryReader { g in
            let scale = g.size.width / max(facts.windowWidth, 600)
            HStack(spacing: 4) {
                if facts.sidebarShown {
                    VStack(spacing: 4) {
                        pane("Sessions", fill: Theme.sidebar)
                        pane(detailsMinimized ? "Details, a line" : "Details", fill: Theme.panel)
                            .frame(height: detailsMinimized ? 16 : max(22, min(detailsHeight * scale, g.size.height * 0.45)))
                        pane("Task bar", fill: Theme.field)
                            .frame(height: 18)
                    }
                    .frame(width: max(60, facts.sidebarWidth * scale))
                }
                pane(terminal ? "Terminal" : "Conversation",
                     fill: terminal ? Color(nsColor: Theme.terminalBackground) : Theme.card,
                     ink: terminal ? Color(nsColor: Theme.terminalForeground) : Theme.ink)
                if side.open {
                    pane(side.tab.title, fill: Theme.panel)
                        .frame(width: max(54, side.width * scale))
                }
            }
            .padding(6)
            .frame(width: g.size.width, height: g.size.height)
            .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(Theme.ground))
            .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
        }
        .aspectRatio(1.7, contentMode: .fit)
        .animation(.easeOut(duration: 0.2), value: facts)
        .animation(.easeOut(duration: 0.2), value: side)
        .accessibilityElement(children: .combine)
    }

    private func pane(_ title: String, fill: Color, ink: Color = Theme.ink) -> some View {
        RoundedRectangle(cornerRadius: 6, style: .continuous)
            .fill(fill)
            .overlay(RoundedRectangle(cornerRadius: 6, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
            .overlay(Text(title).font(Theme.font(10, .medium)).foregroundStyle(ink).lineLimit(1).padding(.horizontal, 3))
    }
}
