import AppKit
import SwiftUI

// The right sidebar's Jammin tab (docs/proposals/jammin.md §6): a strip of
// the sources, one on show at a time; what plays on it, with its artwork,
// its progress, which a drag moves, and its controls; then its playlists
// and a search of it alone. A source not set up says what it needs, and
// offers the fix: Link Spotify… or Open Music. It is a pane of the app's
// own view, which shows while Jammin's Spotify part is on
// (PluginPanes.swift), drawn from Roots' parts: chips, cards, a field,
// soft and icon buttons (components.md).
//
// Spotify's content keeps Spotify's rules (§3): its full logo beside it,
// black on a light ground and white on a dark one, never under 70 points
// wide; a link back to each item; its artwork as Spotify gives it, never
// cropped or blurred, with nothing drawn over it, rounded 8 points large
// and 4 in a list; and names cut only where room ends, whole in a tooltip.

@MainActor
struct JamminTab: View {
    @Environment(Store.self) private var store

    var body: some View {
        let j = store.jammin
        VStack(spacing: 0) {
            if !j.sources.isEmpty {
                JamminStrip()
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 10) {
                    if let s = j.source {
                        JamminPage(source: s)
                    } else if let o = j.offline {
                        JamminNote(text: o)
                    } else if j.loaded {
                        SideNote(title: "No music yet", text: "Jammin has no source on. Settings › Plugins turns Spotify or Apple Music on.")
                    } else {
                        ProgressView().controlSize(.small).frame(maxWidth: .infinity).padding(.top, 24)
                    }
                }
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        // Every 2 seconds while the tab shows, and only while its window
        // is on screen: a hidden tab asks nothing.
        .task {
            while !Task.isCancelled {
                if !j.loaded || NSApp.occlusionState.contains(.visible) { await j.refresh() }
                try? await Task.sleep(nanoseconds: 2_000_000_000)
            }
        }
        .sheet(isPresented: Bindable(j).linking) {
            JamminSignInSheet(savedID: j.sources.first { $0.isSpotify }?.clientID).environment(store)
        }
    }
}

// MARK: Sources

/// The sources as chips across the tab: a click shows that one alone. A
/// source that plays wears a still speaker, whichever is on show.
@MainActor
private struct JamminStrip: View {
    @Environment(Store.self) private var store

    var body: some View {
        let j = store.jammin
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 5) {
                ForEach(j.sources) { s in
                    chip(s, on: s.name == j.source?.name)
                }
            }
            .padding(.horizontal, 10)
            .padding(.top, 10)
            .padding(.bottom, 2)
        }
    }

    private func chip(_ s: JamSource, on: Bool) -> some View {
        let j = store.jammin
        let playing = j.now(s)?.playing == true
        return Button { j.show(s.name) } label: {
            HStack(spacing: 4) {
                Image(systemName: s.symbol)
                    .font(.system(size: 9.5, weight: .semibold))
                    .foregroundStyle(Theme.muted)
                Text(s.title)
                    .font(Theme.font(11.5, on ? .bold : .medium))
                    .foregroundStyle(Theme.ink)
                if playing {
                    Image(systemName: "speaker.wave.2.fill")
                        .font(.system(size: 8.5, weight: .semibold))
                        .foregroundStyle(Theme.accentInk)
                } else if s.state == "needs" || s.state == "error" || s.state == "linking" {
                    StateMark(look: s.state == "linking" ? .starting : .needs, size: 8)
                }
            }
            .padding(.horizontal, 9)
            .padding(.vertical, 4)
            .background(Capsule().fill(on ? Theme.selection : Theme.field))
            .overlay(Capsule().strokeBorder(on ? Theme.accent : Theme.edge, lineWidth: on ? 1.5 : 1))
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .help(playing ? "\(s.title): playing" : s.ready ? s.title : "\(s.title): \(s.problem ?? s.state)")
        .accessibilityLabel(s.title + (playing ? ", playing" : s.ready ? "" : ", not ready"))
        .accessibilityAddTraits(on ? .isSelected : [])
    }
}

/// One source's music: its attribution, what plays, then its playlists or
/// what a search of it found.
@MainActor
private struct JamminPage: View {
    @Environment(Store.self) private var store
    let source: JamSource

    var body: some View {
        let j = store.jammin
        let s = source
        VStack(alignment: .leading, spacing: 10) {
            if s.isSpotify {
                HStack(alignment: .center, spacing: 8) {
                    JamminSpotifyLogo()
                    Spacer(minLength: 6)
                    if let a = s.account, !a.isEmpty {
                        Text(a)
                            .font(Theme.font(11))
                            .foregroundStyle(Theme.muted)
                            .lineLimit(1)
                            .help("The Spotify account Jammin plays: \(a)")
                    }
                }
                .padding(.horizontal, 4)
                .padding(.top, 2)
                if s.throughApp {
                    Text("Through the Spotify app on this Mac")
                        .font(Theme.font(11))
                        .foregroundStyle(Theme.muted)
                        .padding(.horizontal, 4)
                        .padding(.top, -6)
                }
            }
            if s.isFiles, s.ready {
                JamminFolderLine(source: s)
            }
            if s.ready {
                JamminNowPlaying(source: s)
            } else {
                JamminSetUp(source: s)
            }
            if let p = j.problem {
                JamminNote(text: p) { j.problem = nil }
            }
            if let o = j.offline {
                JamminNote(text: o)
            }
            if s.ready, !s.isMusic || j.musicOpen {
                if s.does("search") {
                    JamminSearchBox(source: s)
                }
                if let f = j.found[s.name] {
                    JamminResults(source: s, found: f)
                } else if s.does("lists") {
                    JamminLists(source: s)
                }
            }
            if s.isSpotify, s.ready, s.throughApp {
                JamminLinkAccount()
            }
        }
    }
}

/// What a source lacks, and the fix: Link Spotify…, or Open Music. A
/// Roots banner, quiet: nothing here is an alarm.
@MainActor
private struct JamminSetUp: View {
    @Environment(Store.self) private var store
    let source: JamSource

    var body: some View {
        let j = store.jammin
        let s = source
        VStack(alignment: .leading, spacing: 9) {
            HStack(alignment: .top, spacing: 6) {
                StateMark(look: s.state == "linking" ? .starting : s.state == "off" ? .paused : .needs, size: 10)
                    .padding(.top, 3)
                JamminWords(text: s.problem ?? fallback)
                    .font(Theme.font(11.5))
                    .foregroundStyle(Theme.ink)
            }
            HStack(spacing: 6) {
                if s.isSpotify, s.state == "needs" || s.state == "linking" {
                    Button("Link Spotify…") { j.linking = true }
                        .buttonStyle(SoftButton(prominent: true))
                        .help("Make your Spotify app, or use the one you made, and sign in")
                }
                if s.isMusic, !j.musicOpen {
                    Button("Open Music") { j.openMusic() }
                        .buttonStyle(SoftButton(prominent: true))
                        .help("Opens the Music app hidden, behind what you work in")
                }
                if s.isFiles, s.state == "needs" {
                    Button("Choose Folder…") { j.chooseFolder() }
                        .buttonStyle(SoftButton(prominent: true))
                        .help("Pick the folder that holds your music files")
                }
            }
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Theme.needsTint))
        .accessibilityElement(children: .contain)
    }

    private var fallback: String {
        switch source.state {
        case "linking": return "Signing in to \(source.title)…"
        case "off": return "\(source.title) is off. Settings › Plugins turns it on."
        case "error": return "\(source.title) didn’t answer the last look."
        default: return "\(source.title) needs setting up."
        }
    }
}

/// A sentence from the core, its `commands` as code.
private struct JamminWords: View {
    let text: String

    var body: some View {
        Text((try? AttributedString(markdown: text, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(text))
            .textSelection(.enabled)
            .fixedSize(horizontal: false, vertical: true)
    }
}

/// Why something didn't work, in place and calm: muted words on a quiet
/// card, which a click puts away.
@MainActor
private struct JamminNote: View {
    let text: String
    var dismiss: (() -> Void)?

    var body: some View {
        HStack(alignment: .top, spacing: 6) {
            Image(systemName: "info.circle")
                .font(.system(size: 11))
                .foregroundStyle(Theme.muted)
                .padding(.top, 1)
                .accessibilityHidden(true)
            JamminWords(text: text)
                .font(Theme.font(11.5))
                .foregroundStyle(Theme.muted)
            Spacer(minLength: 4)
            if let dismiss {
                Button(action: dismiss) { Image(systemName: "xmark") }
                    .buttonStyle(IconButtonStyle())
                    .help("Put this away")
                    .accessibilityLabel("Put this away")
            }
        }
        .padding(8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Theme.card))
    }
}

// MARK: Now playing

/// What plays on the source: its artwork, names, progress and controls,
/// its volume and, for Spotify, the device it plays on and a link back.
@MainActor
private struct JamminNowPlaying: View {
    @Environment(Store.self) private var store
    let source: JamSource

    var body: some View {
        let j = store.jammin
        let s = source
        Group {
            if let n = j.now(s) {
                VStack(spacing: 10) {
                    JamminArtwork(address: n.art, side: 200, corner: 8)
                        .padding(.top, 2)
                    VStack(spacing: 2) {
                        Text(n.title)
                            .font(Theme.font(14, .semibold))
                            .foregroundStyle(Theme.ink)
                            .lineLimit(1)
                            .truncationMode(.tail)
                            .help(n.title)
                        if !n.by.isEmpty {
                            Text(n.by)
                                .font(Theme.font(12))
                                .foregroundStyle(Theme.ink.opacity(0.8))
                                .lineLimit(1)
                                .help(n.by)
                        }
                        if let album = n.album, !album.isEmpty {
                            Text(album)
                                .font(Theme.font(11.5))
                                .foregroundStyle(Theme.muted)
                                .lineLimit(1)
                                .help(album)
                        }
                    }
                    .frame(maxWidth: .infinity)
                    if let d = n.duration, d > 0 {
                        JamminProgress(now: n, canSeek: s.does("seek"))
                    }
                    JamminControls(source: s, now: n)
                    if s.does("volume"), let v = n.volume {
                        JamminVolume(volume: v)
                    }
                    if s.isSpotify {
                        JamminDeviceLine(device: n.device, canMove: s.does("devices"), link: n.link, viaApp: s.via == "app")
                    }
                }
            } else {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Nothing plays on \(s.title) now.")
                        .font(Theme.font(12.5, .semibold))
                        .foregroundStyle(Theme.ink)
                    Text(hint)
                        .font(Theme.font(11.5))
                        .foregroundStyle(Theme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                    if s.isMusic, !j.musicOpen {
                        Button("Open Music") { j.openMusic() }
                            .buttonStyle(SoftButton(prominent: true))
                            .help("Opens the Music app hidden, behind what you work in")
                    }
                    if s.isSpotify, s.does("devices") {
                        JamminDeviceLine(device: nil, canMove: true, link: nil)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(12)
        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Theme.card))
        .accessibilityElement(children: .contain)
    }

    private var hint: String {
        if source.isFiles { return "Start a folder below, or search your music." }
        if source.isMusic {
            return store.jammin.musicOpen ? "Start a playlist below, or search your library." : "The Music app isn’t open."
        }
        return "Start a playlist below, or open Spotify on a device of yours and pick it under Devices."
    }
}

/// The artwork as the source gives it: scaled to fit, never cropped or
/// blurred, its corners rounded, nothing drawn over it. Until it loads, or
/// with none, a quiet square holds its place.
@MainActor
struct JamminArtwork: View {
    @Environment(Store.self) private var store
    let address: String?
    let side: CGFloat
    let corner: CGFloat
    /// What the quiet square shows until there is artwork, or with none.
    var symbol = "music.note"

    var body: some View {
        let j = store.jammin
        Group {
            if let a = address, let image = j.art[a] {
                Image(nsImage: image)
                    .resizable()
                    .interpolation(.high)
                    .aspectRatio(contentMode: .fit)
                    .clipShape(RoundedRectangle(cornerRadius: corner, style: .continuous))
            } else {
                RoundedRectangle(cornerRadius: corner, style: .continuous)
                    .fill(Theme.field)
                    .overlay(
                        Image(systemName: symbol)
                            .font(.system(size: max(10, side * 0.28)))
                            .foregroundStyle(Theme.muted.opacity(0.7))
                    )
            }
        }
        .frame(width: side, height: side)
        .task(id: address) {
            if let a = address, !a.isEmpty { await j.loadArt(a) }
        }
        .accessibilityHidden(true)
    }
}

/// How far it is: a bar a drag moves, with the time gone and the length.
/// It runs on by itself between the core's answers while it plays.
@MainActor
private struct JamminProgress: View {
    @Environment(Store.self) private var store
    let now: JamNow
    let canSeek: Bool
    /// Where a drag holds it, from 0 to 1.
    @State private var drag: Double?
    @State private var hovering = false

    var body: some View {
        TimelineView(.periodic(from: .now, by: now.playing ? 1 : 60)) { t in
            bar(at: t.date)
        }
        .onHover { hovering = $0 }
    }

    private func bar(at date: Date) -> some View {
        let length = max(1, now.duration ?? 1)
        let at = drag.map { Int($0 * Double(length)) } ?? now.position(at: date)
        let f = min(1, max(0, Double(at) / Double(length)))
        return VStack(spacing: 3) {
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Capsule().fill(Theme.edge).frame(height: 4)
                    Capsule().fill(Theme.accent).frame(width: max(4, g.size.width * f), height: 4)
                    if canSeek, hovering || drag != nil {
                        Circle()
                            .fill(Theme.accent)
                            .frame(width: 10, height: 10)
                            .offset(x: g.size.width * f - 5)
                    }
                }
                .frame(width: g.size.width, height: 14)
                .contentShape(Rectangle())
                .gesture(
                    DragGesture(minimumDistance: 0)
                        .onChanged { v in drag = min(1, max(0, v.location.x / max(1, g.size.width))) }
                        .onEnded { v in
                            let to = min(1, max(0, v.location.x / max(1, g.size.width)))
                            store.jammin.seek(Int(to * Double(length)))
                            drag = nil
                        },
                    including: canSeek ? .all : .none
                )
            }
            .frame(height: 14)
            HStack {
                Text(JamNow.clock(at))
                Spacer()
                Text(JamNow.clock(length))
            }
            .font(Theme.font(10.5))
            .monospacedDigit()
            .foregroundStyle(Theme.muted)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Position")
        .accessibilityValue("\(JamNow.clock(at)) of \(JamNow.clock(length))")
        .accessibilityAdjustableAction { direction in
            guard canSeek else { return }
            let step = direction == .increment ? 10_000 : -10_000
            store.jammin.seek(min(length, max(0, now.position(at: Date()) + step)))
        }
    }
}

/// Shuffle, previous, play or pause, next and repeat: only those the
/// source can do. Shuffle and repeat mark their state on themselves.
@MainActor
private struct JamminControls: View {
    @Environment(Store.self) private var store
    let source: JamSource
    let now: JamNow

    var body: some View {
        let j = store.jammin
        let s = source
        HStack(spacing: 12) {
            if s.does("shuffle") {
                let on = now.shuffle == true
                Button { j.setShuffle(!on) } label: { Image(systemName: "shuffle") }
                    .buttonStyle(JamminToggleStyle(on: on))
                    .help(on ? "Shuffle is on: play in order" : "Shuffle")
                    .accessibilityLabel("Shuffle")
                    .accessibilityValue(on ? "on" : "off")
            }
            if s.does("skip") {
                Button { j.previous() } label: { Image(systemName: "backward.fill") }
                    .buttonStyle(JamminControlStyle())
                    .help("Previous: the track before, or this one from its start")
                    .accessibilityLabel("Previous")
            }
            if s.does("play") {
                Button { j.toggle() } label: {
                    Image(systemName: now.playing ? "pause.fill" : "play.fill")
                        .font(.system(size: 15, weight: .bold))
                        .foregroundStyle(Theme.onAccent)
                        .frame(width: 38, height: 38)
                        .background(Circle().fill(Theme.accentFill))
                        .contentShape(Circle())
                }
                .buttonStyle(.plain)
                .help(now.playing ? "Pause" : "Play")
                .accessibilityLabel(now.playing ? "Pause" : "Play")
            }
            if s.does("skip") {
                Button { j.next() } label: { Image(systemName: "forward.fill") }
                    .buttonStyle(JamminControlStyle())
                    .help("Next")
                    .accessibilityLabel("Next")
            }
            if s.does("repeat") {
                let r = now.repeat ?? "off"
                Button { j.cycleRepeat() } label: { Image(systemName: r == "track" ? "repeat.1" : "repeat") }
                    .buttonStyle(JamminToggleStyle(on: r != "off"))
                    .help(r == "track" ? "Repeat this track: off next" : r == "all" ? "Repeat the list: this track next" : "Repeat")
                    .accessibilityLabel("Repeat")
                    .accessibilityValue(r == "track" ? "this track" : r == "all" ? "all" : "off")
            }
        }
        .frame(maxWidth: .infinity)
    }
}

/// A toggle that is its own mark: on, it wears the accent on a soft fill;
/// off, it is a quiet icon, as Roots' icon button.
private struct JamminToggleStyle: ButtonStyle {
    let on: Bool

    func makeBody(configuration: Configuration) -> some View {
        JamminButtonLook(configuration: configuration, on: on, size: 12)
    }
}

/// Previous and next: a little larger than Roots' icon button, as a
/// player's own.
private struct JamminControlStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        JamminButtonLook(configuration: configuration, on: nil, size: 14)
    }
}

private struct JamminButtonLook: View {
    let configuration: ButtonStyleConfiguration
    /// A toggle's state; nil for a plain control.
    let on: Bool?
    let size: CGFloat
    @Environment(\.isEnabled) private var enabled
    @State private var hovering = false

    var body: some View {
        let lit = enabled && (hovering || configuration.isPressed)
        let isOn = on == true
        configuration.label
            .font(.system(size: size, weight: .semibold))
            .foregroundStyle(isOn ? Theme.accentInk : on == nil ? Theme.ink : (lit ? Theme.ink : Theme.muted))
            .frame(width: 30, height: 28)
            .background(RoundedRectangle(cornerRadius: 7, style: .continuous).fill(isOn ? Theme.selection : lit ? Theme.hover : .clear))
            .opacity(enabled ? 1 : 0.45)
            .contentShape(Rectangle())
            .onHover { hovering = $0 }
    }
}

/// The volume: a slider that sets it once let go, or at once from the
/// keyboard.
@MainActor
private struct JamminVolume: View {
    @Environment(Store.self) private var store
    let volume: Int
    @State private var held: Double?
    @State private var dragging = false

    var body: some View {
        let j = store.jammin
        HStack(spacing: 6) {
            Image(systemName: "speaker.fill")
                .font(.system(size: 9))
                .foregroundStyle(Theme.muted)
                .accessibilityHidden(true)
            Slider(value: Binding(get: { held ?? Double(volume) }, set: { v in
                held = v
                if !dragging {
                    j.setVolume(Int(v.rounded()))
                    held = nil
                }
            }), in: 0 ... 100) { editing in
                dragging = editing
                if !editing, let v = held {
                    j.setVolume(Int(v.rounded()))
                    held = nil
                }
            }
            .controlSize(.small)
            .accessibilityLabel("Volume")
            Image(systemName: "speaker.wave.3.fill")
                .font(.system(size: 9))
                .foregroundStyle(Theme.muted)
                .accessibilityHidden(true)
        }
        .padding(.horizontal, 4)
        .help("Volume: \(Int((held ?? Double(volume)).rounded()))")
    }
}

/// Where Spotify plays, with Devices to move it, and the link back to
/// what plays: Play on Spotify.
@MainActor
private struct JamminDeviceLine: View {
    @Environment(Store.self) private var store
    let device: JamDevice?
    let canMove: Bool
    let link: String?
    /// Spotify plays through its app on this Mac, so the app is here.
    var viaApp = false
    @State private var picking = false

    var body: some View {
        let j = store.jammin
        VStack(spacing: 8) {
            HStack(spacing: 6) {
                if let device {
                    Image(systemName: device.symbol)
                        .font(.system(size: 11))
                        .foregroundStyle(Theme.muted)
                        .accessibilityHidden(true)
                    Text("On \(device.shownName)")
                        .font(Theme.font(11.5))
                        .foregroundStyle(Theme.muted)
                        .lineLimit(1)
                        .help(device.shownName)
                }
                Spacer(minLength: 4)
                if canMove {
                    Button { picking = true } label: {
                        HStack(spacing: 3) {
                            Image(systemName: "hifispeaker.2")
                                .font(.system(size: 10, weight: .semibold))
                            Text("Devices")
                                .font(Theme.font(11.5, .semibold))
                        }
                        .foregroundStyle(Theme.accentInk)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .help("Where Spotify plays: your phone, a speaker, this Mac")
                    .popover(isPresented: $picking, arrowEdge: .bottom) {
                        JamminDevices { picking = false }
                            .environment(store)
                    }
                }
            }
            if let link, !link.isEmpty {
                Button(viaApp || JamminModel.spotifyInstalled ? "Play on Spotify" : "Get Spotify free") { j.open(link) }
                    .buttonStyle(SoftButton())
                    .help("Opens what plays in Spotify")
                    .frame(maxWidth: .infinity, alignment: .center)
            }
        }
    }
}

/// Spotify's devices, read when the list opens: a click moves the playing
/// there.
@MainActor
private struct JamminDevices: View {
    @Environment(Store.self) private var store
    let done: () -> Void

    var body: some View {
        let j = store.jammin
        VStack(alignment: .leading, spacing: 4) {
            Text("Play on")
                .font(Theme.font(Theme.headSize, .heavy))
                .tracking(Theme.headTracking)
                .foregroundStyle(Theme.muted)
                .padding(.horizontal, 6)
            if j.devices.isEmpty {
                if j.lookingForDevices {
                    HStack(spacing: 6) {
                        ProgressView().controlSize(.mini)
                        Text("Looking for your devices…")
                    }
                    .font(Theme.font(11.5))
                    .foregroundStyle(Theme.muted)
                    .padding(6)
                } else {
                    Text("No device of yours has Spotify open now. Open Spotify on one, and it shows here.")
                        .font(Theme.font(11.5))
                        .foregroundStyle(Theme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(6)
                }
            }
            ForEach(j.devices) { d in
                Button {
                    j.move(d)
                    done()
                } label: {
                    HStack(spacing: 8) {
                        Image(systemName: d.symbol)
                            .font(.system(size: 12))
                            .foregroundStyle(Theme.muted)
                            .frame(width: 18)
                        Text(d.shownName)
                            .font(Theme.font(12.5, d.active == true ? .semibold : .regular))
                            .foregroundStyle(Theme.ink)
                            .lineLimit(1)
                        Spacer(minLength: 6)
                        if d.active == true {
                            Image(systemName: "checkmark")
                                .font(.system(size: 10, weight: .bold))
                                .foregroundStyle(Theme.accentInk)
                        }
                    }
                    .padding(.horizontal, 6)
                    .padding(.vertical, 5)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .disabled(d.restricted == true)
                .help(d.restricted == true ? "\(d.name) takes no control from \(Brand.name)" : "Play on \(d.name)")
                .accessibilityAddTraits(d.active == true ? .isSelected : [])
            }
        }
        .padding(8)
        .frame(width: 250)
        .task { await j.loadDevices() }
    }
}

// MARK: Playlists and search

/// The search box: the source on show alone, never two at once.
@MainActor
private struct JamminSearchBox: View {
    @Environment(Store.self) private var store
    let source: JamSource
    @FocusState private var focused: Bool

    var body: some View {
        let j = store.jammin
        HStack(spacing: 6) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 11))
                .foregroundStyle(Theme.muted)
                .accessibilityHidden(true)
            TextField("Search \(source.title)", text: Bindable(j).query)
                .textFieldStyle(.plain)
                .font(Theme.font(12.5))
                .focused($focused)
                .onSubmit { j.search() }
                .accessibilityLabel("Search \(source.title)")
            if j.searching {
                ProgressView().controlSize(.mini)
            } else if j.found[source.name] != nil || !j.query.isEmpty {
                Button { j.clearSearch() } label: { Image(systemName: "xmark.circle.fill") }
                    .buttonStyle(.plain)
                    .foregroundStyle(Theme.muted)
                    .help("Put the search away: your playlists show again")
                    .accessibilityLabel("Clear the search")
            }
        }
        .padding(.leading, Theme.inset)
        .padding(.trailing, 8)
        .padding(.vertical, 6)
        .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.field))
        .overlay(
            RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
                .strokeBorder(focused ? Theme.accent : Theme.edge, lineWidth: focused ? 1.5 : 1)
        )
        .padding(.top, 2)
    }
}

/// The source's playlists, read once the source shows ready.
@MainActor
private struct JamminLists: View {
    @Environment(Store.self) private var store
    let source: JamSource

    var body: some View {
        let j = store.jammin
        VStack(alignment: .leading, spacing: 4) {
            JamminHead(title: source.isFiles ? "Your folders" : "Your playlists")
            if let items = j.lists[source.name] {
                if items.isEmpty {
                    Text(source.isFiles ? "No folders of music in \(source.folder ?? "your folder") yet." : "No playlists in \(source.title) yet.")
                        .font(Theme.font(11.5))
                        .foregroundStyle(Theme.muted)
                        .padding(.horizontal, 4)
                } else {
                    VStack(spacing: 1) {
                        ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                            JamminItemRow(item: item, source: source)
                        }
                    }
                }
            } else if j.listing == source.name {
                HStack(spacing: 6) {
                    ProgressView().controlSize(.mini)
                    Text(source.isFiles ? "Reading your folders…" : "Reading your playlists…")
                }
                .font(Theme.font(11.5))
                .foregroundStyle(Theme.muted)
                .padding(.horizontal, 4)
            }
        }
        .task(id: source.name + source.state + (source.folder ?? "")) { await j.loadLists(source.name) }
    }
}

/// What a search of the source found: tracks, albums, playlists and
/// episodes, each under its head.
@MainActor
private struct JamminResults: View {
    let source: JamSource
    let found: JamFound

    private static let kinds = [("track", "Tracks"), ("album", "Albums"), ("playlist", "Playlists"), ("episode", "Episodes")]

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            if found.items.isEmpty {
                Text("Nothing in \(source.title) for “\(found.words)”.")
                    .font(Theme.font(11.5))
                    .foregroundStyle(Theme.muted)
                    .padding(.horizontal, 4)
            }
            ForEach(Self.kinds, id: \.0) { kind in
                let items = found.items.filter { $0.kind == kind.0 }
                if !items.isEmpty {
                    JamminHead(title: kind.1)
                    VStack(spacing: 1) {
                        ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                            JamminItemRow(item: item, source: source)
                        }
                    }
                }
            }
        }
    }
}

/// The files' folder, under the strip, with Change Folder….
@MainActor
private struct JamminFolderLine: View {
    @Environment(Store.self) private var store
    let source: JamSource

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "folder")
                .font(.system(size: 11))
                .foregroundStyle(Theme.muted)
                .accessibilityHidden(true)
            Text(source.folder ?? "")
                .font(Theme.font(11.5))
                .foregroundStyle(Theme.ink)
                .lineLimit(1)
                .truncationMode(.middle)
                .help(source.folder ?? "")
            Spacer(minLength: 6)
            Button("Change Folder…") { store.jammin.chooseFolder() }
                .buttonStyle(.plain)
                .font(Theme.font(11.5, .semibold))
                .foregroundStyle(Theme.accentInk)
                .disabled(store.jammin.busy == "folder")
                .help("Pick another folder of music files")
        }
        .padding(.horizontal, 4)
        .padding(.top, 2)
    }
}

/// Spotify through its app on this Mac: linking an account adds
/// playlists and search. Quiet, since it plays already.
@MainActor
private struct JamminLinkAccount: View {
    @Environment(Store.self) private var store

    var body: some View {
        HStack(spacing: 8) {
            Text("Link your account for playlists and search.")
                .font(Theme.font(11.5))
                .foregroundStyle(Theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 6)
            Button("Link Spotify…") { store.jammin.linking = true }
                .buttonStyle(SoftButton())
                .help("Make your Spotify app, or use the one you made, and sign in")
        }
        .padding(.horizontal, 4)
    }
}

/// A section's head, quieter than its rows.
private struct JamminHead: View {
    let title: String

    var body: some View {
        Text(title)
            .font(Theme.font(Theme.headSize, .heavy))
            .tracking(Theme.headTracking)
            .foregroundStyle(Theme.muted)
            .padding(.horizontal, 4)
            .padding(.top, Theme.headGap)
    }
}

/// A playlist, album, track or episode: its artwork, name and who made
/// it, and a click plays it. A Spotify item has its link back beside it.
@MainActor
private struct JamminItemRow: View {
    @Environment(Store.self) private var store
    let item: JamItem
    let source: JamSource
    @State private var hovering = false

    var body: some View {
        let j = store.jammin
        let about = item.about
        HStack(spacing: 2) {
            Button { j.play(item) } label: {
                HStack(spacing: 8) {
                    JamminArtwork(address: item.art, side: 36, corner: 4, symbol: item.kind == "folder" ? "folder" : "music.note")
                    VStack(alignment: .leading, spacing: 1) {
                        Text(item.title)
                            .font(Theme.font(12.5, .medium))
                            .foregroundStyle(Theme.ink)
                            .lineLimit(1)
                            .truncationMode(.tail)
                        if !about.isEmpty {
                            Text(about)
                                .font(Theme.font(11))
                                .foregroundStyle(Theme.muted)
                                .lineLimit(1)
                                .truncationMode(.tail)
                        }
                    }
                    Spacer(minLength: 4)
                    Group {
                        if j.busy == "play:" + item.uri {
                            ProgressView().controlSize(.mini)
                        } else {
                            Image(systemName: "play.fill")
                                .font(.system(size: 10))
                                .foregroundStyle(Theme.accentInk)
                                .opacity(hovering ? 1 : 0)
                        }
                    }
                    .frame(width: 16)
                }
                .padding(.leading, 4)
                .padding(.vertical, 4)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help(about.isEmpty ? item.title : "\(item.title)\n\(about)")
            .accessibilityLabel("Play \(item.title)" + (about.isEmpty ? "" : ", \(about)"))
            if source.isSpotify, let link = item.link, !link.isEmpty {
                Button { j.open(link) } label: { Image(systemName: "arrow.up.forward") }
                    .buttonStyle(IconButtonStyle())
                    .help(JamminModel.spotifyInstalled ? "Play on Spotify" : "Get Spotify free")
                    .accessibilityLabel("Play \(item.title) on Spotify")
                    .padding(.trailing, 2)
            }
        }
        .background(RoundedRectangle(cornerRadius: 6, style: .continuous).fill(hovering ? Theme.hover : .clear))
        .onHover { hovering = $0 }
        .contextMenu {
            Button("Play") { j.play(item) }
            if let link = item.link, !link.isEmpty {
                Button(source.isSpotify ? "Play on Spotify" : "Open in \(source.title)") { j.open(link) }
            }
        }
    }
}

// MARK: Spotify's logo

/// Spotify's full logo, icon and wordmark, as Spotify gives it
/// (mac/Resources, from its design guidelines): black on a light ground,
/// white on a dark one, never under 70 points wide.
@MainActor
struct JamminSpotifyLogo: View {
    @Environment(\.colorScheme) private var scheme
    var width: CGFloat = 80

    var body: some View {
        let w = max(70, width)
        if let image = scheme == .dark ? JamminLogo.white : JamminLogo.black {
            Image(nsImage: image)
                .resizable()
                .interpolation(.high)
                .aspectRatio(contentMode: .fit)
                .frame(width: w)
                .accessibilityLabel("Spotify")
        } else {
            // A build without the logo's files says the name instead.
            Text("Spotify")
                .font(.system(size: 15, weight: .bold))
                .foregroundStyle(scheme == .dark ? Color.white : Color.black)
        }
    }
}

/// The logo's two files, read once from the app's resources.
@MainActor
enum JamminLogo {
    static let black = load("Spotify_Full_Logo_RGB_Black")
    static let white = load("Spotify_Full_Logo_RGB_White")

    private static func load(_ name: String) -> NSImage? {
        Bundle.main.url(forResource: name, withExtension: "png").flatMap { NSImage(contentsOf: $0) }
    }
}
