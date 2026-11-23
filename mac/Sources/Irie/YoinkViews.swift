#if !NOYOINK
import AppKit
import SwiftUI

// The right sidebar's Yoink tab (DESIGN §4.17): a box for a link, the
// sizes it comes in to pick from, and the downloads as they go, each with
// its mark, its bar and Show in Finder once it is there. It is a pane of
// the app's own view, which shows while the Yoink plugin's part is on
// (PluginPanes.swift), drawn from Roots' parts: a field with the start
// button, cards, state marks, soft and icon buttons (components.md).

@MainActor
struct YoinkTab: View {
    @Environment(Store.self) private var store

    var body: some View {
        let y = store.yoink
        VStack(spacing: 0) {
            YoinkLinkBox()
            if let part = store.yoinkPart, part.state == "needs" {
                YoinkBanner(text: part.detail)
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 8) {
                    if let p = y.problem {
                        Text(p)
                            .font(Theme.font(12))
                            .foregroundStyle(Theme.needs)
                            .fixedSize(horizontal: false, vertical: true)
                            .padding(.horizontal, 4)
                            .accessibilityLabel("Problem: \(p)")
                    }
                    if y.looking {
                        HStack(spacing: 8) {
                            ProgressView().controlSize(.small)
                            Text("Looking at the link…")
                                .font(Theme.font(12))
                                .foregroundStyle(Theme.muted)
                        }
                        .padding(.horizontal, 4)
                        .padding(.vertical, 6)
                    }
                    if let info = y.info {
                        YoinkChoices(info: info)
                    }
                    if !y.downloads.isEmpty {
                        YoinkHead()
                        ForEach(y.downloads) { YoinkRow(d: $0) }
                    } else if y.info == nil, !y.looking, y.loaded {
                        Text("Paste a link to a video from YouTube, Vimeo, Wikimedia Commons or another site yt-dlp knows, pick a size or the sound alone, and it lands in \(Self.tilde(y.folder)).")
                            .font(Theme.font(12))
                            .foregroundStyle(Theme.muted)
                            .fixedSize(horizontal: false, vertical: true)
                            .padding(.horizontal, 4)
                            .padding(.top, 4)
                    }
                }
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        // Each second while a download runs, else every few, and only
        // while the tab shows.
        .task {
            while !Task.isCancelled {
                await y.refresh()
                try? await Task.sleep(nanoseconds: y.running ? 1_000_000_000 : 4_000_000_000)
            }
        }
    }

    /// A folder under the home folder, as people write it: ~/Downloads.
    static func tilde(_ path: String) -> String {
        guard !path.isEmpty else { return "your Downloads folder" }
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        if path == home { return "~" }
        if path.hasPrefix(home + "/") { return "~" + path.dropFirst(home.count) }
        // The snapshots' home, which isn't this Mac's.
        if path.hasPrefix("/Users/alice/") { return "~" + path.dropFirst("/Users/alice".count) }
        return path
    }
}

/// The link's box: Roots' field, with the start button at its end, which
/// looks at the link; and where downloads land, which a click opens.
@MainActor
private struct YoinkLinkBox: View {
    @Environment(Store.self) private var store
    @FocusState private var focused: Bool

    var body: some View {
        let y = store.yoink
        let empty = y.link.trimmingCharacters(in: .whitespaces).isEmpty
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: "link")
                    .font(.system(size: 11))
                    .foregroundStyle(Theme.muted)
                    .accessibilityHidden(true)
                TextField("Paste a link to a video", text: Bindable(y).link)
                    .textFieldStyle(.plain)
                    .font(Theme.font(12.5))
                    .focused($focused)
                    .onSubmit { y.look() }
                    .accessibilityLabel("A link to a video")
                if empty {
                    Button("Paste") { y.paste() }
                        .buttonStyle(.plain)
                        .font(Theme.font(11.5, .semibold))
                        .foregroundStyle(Theme.accentInk)
                        .help("Look at the link on the pasteboard")
                } else {
                    Button { y.look() } label: {
                        Image(systemName: "arrow.right")
                            .font(.system(size: 10, weight: .bold))
                            .foregroundStyle(Theme.onAccent)
                            .frame(width: 20, height: 20)
                            .background(Circle().fill(Theme.accentFill))
                    }
                    .buttonStyle(.plain)
                    .disabled(y.looking)
                    .help("Look at the link: its sizes and the sound alone")
                    .accessibilityLabel("Look at the link")
                }
            }
            .padding(.leading, Theme.inset)
            .padding(.trailing, 6)
            .padding(.vertical, 5)
            .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.field))
            .overlay(
                RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
                    .strokeBorder(focused ? Theme.accent : Theme.edge, lineWidth: focused ? 1.5 : 1)
            )
            Button { y.showFolder() } label: {
                HStack(spacing: 4) {
                    Image(systemName: "folder")
                        .font(.system(size: 10))
                    Text("Downloads land in \(YoinkTab.tilde(y.folder))")
                        .font(Theme.font(11))
                }
                .foregroundStyle(Theme.muted)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(y.folder.isEmpty)
            .help("Open the folder in the Finder")
            .padding(.leading, 6)
        }
        .padding(.horizontal, 10)
        .padding(.top, 10)
        .padding(.bottom, 4)
    }
}

/// What the part lacks, such as ffmpeg, while it lacks it: Roots' banner,
/// loud, since a download can't start until it is fixed.
private struct YoinkBanner: View {
    let text: String

    var body: some View {
        HStack(alignment: .top, spacing: 6) {
            StateMark(look: .needs, size: 10)
                .padding(.top, 3)
            // The core's sentence, its `commands` as code.
            Text((try? AttributedString(markdown: text, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(text))
                .font(Theme.font(11.5))
                .foregroundStyle(Theme.ink)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Theme.needsTint))
        .padding(.horizontal, 10)
        .padding(.top, 4)
        .accessibilityElement(children: .combine)
    }
}

/// What a link offers: its title and what it is, then each choice as a
/// row that starts its download with one click.
@MainActor
private struct YoinkChoices: View {
    @Environment(Store.self) private var store
    let info: YoinkInfo

    var body: some View {
        let y = store.yoink
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top, spacing: 6) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(info.title)
                        .font(Theme.font(13, .semibold))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(2)
                    if !info.about.isEmpty {
                        Text(info.about)
                            .font(Theme.font(11.5))
                            .foregroundStyle(Theme.muted)
                            .lineLimit(1)
                    }
                }
                Spacer(minLength: 4)
                Button { y.dismiss() } label: { Image(systemName: "xmark") }
                    .buttonStyle(IconButtonStyle())
                    .help("Put the link away")
                    .accessibilityLabel("Put the link away")
            }
            Text("Download")
                .font(Theme.font(Theme.headSize, .heavy))
                .tracking(Theme.headTracking)
                .foregroundStyle(Theme.muted)
                .padding(.top, 2)
            VStack(spacing: 1) {
                ForEach(info.choices) { c in
                    YoinkChoiceRow(choice: c, waiting: y.starting == c.get) { y.start(c) }
                        .disabled(y.starting != nil)
                }
            }
        }
        .padding(10)
        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Theme.card))
        .accessibilityElement(children: .contain)
    }
}

/// One choice: its name and about how big, and a click downloads it.
private struct YoinkChoiceRow: View {
    let choice: YoinkChoice
    let waiting: Bool
    let start: () -> Void
    @State private var hovering = false
    @Environment(\.isEnabled) private var enabled

    var body: some View {
        Button(action: start) {
            HStack(spacing: 8) {
                Image(systemName: choice.symbol)
                    .font(.system(size: 11))
                    .foregroundStyle(Theme.muted)
                    .frame(width: 16)
                Text(choice.label)
                    .font(Theme.font(12.5, choice.get == "best" ? .semibold : .regular))
                    .foregroundStyle(Theme.ink)
                Spacer(minLength: 6)
                if let size = choice.size, size > 0 {
                    Text("about " + YoinkDownload.bytes(size))
                        .font(Theme.font(11.5))
                        .foregroundStyle(Theme.muted)
                        .monospacedDigit()
                }
                Group {
                    if waiting {
                        ProgressView().controlSize(.mini)
                    } else {
                        Image(systemName: "arrow.down.circle")
                            .font(.system(size: 13))
                            .foregroundStyle(hovering && enabled ? Theme.accentInk : Theme.muted)
                    }
                }
                .frame(width: 18)
            }
            .padding(.horizontal, 6)
            .padding(.vertical, 5)
            .background(RoundedRectangle(cornerRadius: 6, style: .continuous).fill(hovering && enabled ? Theme.hover : .clear))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help("Download \(choice.label)")
        .accessibilityLabel("Download \(choice.label)\(choice.size.map { ", about " + YoinkDownload.bytes($0) } ?? "")")
    }
}

/// The downloads' head, quieter than its rows, with Clear while some have
/// finished.
@MainActor
private struct YoinkHead: View {
    @Environment(Store.self) private var store

    var body: some View {
        HStack {
            Text("Downloads")
                .font(Theme.font(Theme.headSize, .heavy))
                .tracking(Theme.headTracking)
                .foregroundStyle(Theme.muted)
            Spacer()
            if store.yoink.finished {
                Button("Clear") { store.yoink.clear() }
                    .buttonStyle(.plain)
                    .font(Theme.font(11.5, .semibold))
                    .foregroundStyle(Theme.muted)
                    .help("Take the finished downloads off the list. Their files stay.")
            }
        }
        .padding(.horizontal, 4)
        .padding(.top, Theme.headGap)
    }
}

/// One download: its mark and name, its bar while it runs, how it stands,
/// and its buttons: Cancel while it runs, Show in Finder once it is there,
/// Try Again once it failed.
@MainActor
private struct YoinkRow: View {
    @Environment(Store.self) private var store
    let d: YoinkDownload

    var body: some View {
        let y = store.yoink
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top, spacing: 7) {
                StateMark(look: d.look, size: 10)
                    .padding(.top, 3)
                Text(d.name)
                    .font(Theme.font(12.5, .semibold))
                    .foregroundStyle(Theme.ink)
                    .lineLimit(2)
                Spacer(minLength: 4)
                if d.running {
                    Button { y.cancel(d) } label: { Image(systemName: "xmark") }
                        .buttonStyle(IconButtonStyle())
                        .help("Cancel the download, and remove what it downloaded")
                        .accessibilityLabel("Cancel \(d.title)")
                }
            }
            if d.running {
                YoinkBar(fraction: d.fraction)
                    .padding(.vertical, 1)
            }
            VStack(alignment: .leading, spacing: 1) {
                Text(d.standing)
                    .font(Theme.font(11.5))
                    .foregroundStyle(d.state == "failed" ? Theme.needs : Theme.muted)
                    .lineLimit(d.state == "failed" ? 3 : 2)
                    .fixedSize(horizontal: false, vertical: true)
                if let figures = d.figures {
                    Text(figures)
                        .font(Theme.font(11.5))
                        .foregroundStyle(Theme.muted)
                        .monospacedDigit()
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            if d.state == "done" || d.state == "failed" {
                HStack(spacing: 6) {
                    if d.state == "done" {
                        Button("Show in Finder") { y.reveal(d) }
                            .buttonStyle(SoftButton())
                        Button("Open") { y.open(d) }
                            .buttonStyle(SoftButton())
                    } else {
                        Button("Try Again") { y.start(url: d.url, get: d.get) }
                            .buttonStyle(SoftButton())
                    }
                    Spacer()
                }
                .padding(.top, 1)
            }
        }
        .padding(10)
        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Theme.card))
        .contextMenu {
            if d.state == "done" {
                Button("Open \(d.file.map { ($0 as NSString).lastPathComponent } ?? d.title)") { y.open(d) }
                Button("Show in Finder") { y.reveal(d) }
            }
            if d.running {
                Button("Cancel Download") { y.cancel(d) }
            }
            Button("Copy Link") { y.copyLink(d) }
            if !d.running {
                Divider()
                Button("Take Off the List") { y.clear(d) }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("\(d.name): \(d.standing)")
    }
}

/// A download's bar, drawn from Roots' roles: a track in `edge` and its
/// fill in `accent`. While how far isn't known, as while ffmpeg merges, a
/// short fill goes slowly to and fro; under Reduce Motion it stands still
/// in the middle.
private struct YoinkBar: View {
    let fraction: Double?
    @Environment(\.accessibilityReduceMotion) private var still
    @State private var there = false

    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(Theme.edge)
                if let f = fraction {
                    Capsule().fill(Theme.accent)
                        .frame(width: max(4, g.size.width * f))
                        .animation(.smooth(duration: 0.4), value: f)
                } else {
                    Capsule().fill(Theme.accent.opacity(0.75))
                        .frame(width: g.size.width * 0.3)
                        .offset(x: still ? g.size.width * 0.35 : (there ? g.size.width * 0.7 : 0))
                }
            }
        }
        .frame(height: 4)
        .clipShape(Capsule())
        .onAppear {
            guard fraction == nil, !still else { return }
            withAnimation(.easeInOut(duration: 1.4).repeatForever(autoreverses: true)) { there = true }
        }
        .accessibilityElement()
        .accessibilityLabel("How far it is")
        .accessibilityValue(fraction.map { "\(Int($0 * 100)) percent" } ?? "under way")
    }
}
#endif
