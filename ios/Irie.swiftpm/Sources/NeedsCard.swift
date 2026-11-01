import SwiftUI

// The needs card: while the agent waits on you in its terminal, what it
// asks, and its choices as buttons from the `screen` op, each pressing its
// key; a key pad for anything else, and the terminal's text.

@MainActor struct NeedsCard: View {
    let model: ConversationModel

    private static let keypad: [(String, String, String)] = [
        ("1", "1", "1"), ("2", "2", "2"), ("3", "3", "3"), ("Esc", "Escape", "Escape"),
        ("↑", "Up", "Up arrow"), ("↓", "Down", "Down arrow"), ("Tab", "Tab", "Tab"), ("Enter", "Enter", "Return"),
    ]

    var body: some View {
        let s = model.row
        let n = model.needs
        let title = firstNonEmpty(n?.phrase, n?.text, s?.detail) ?? "It needs you"
        let detail = firstNonEmpty(n?.detail, n == nil ? s?.command : nil) ?? ""
        let plan = !detail.isEmpty && (n?.tool == "ExitPlanMode" || (detail.contains("\n") && detail.range(of: #"(^|\n)(#|[-*] |\d+\. )"#, options: .regularExpression) != nil))
        let opts = (model.screen?.options ?? []).isEmpty ? nil : model.screen?.options
        ScrollViewReader { proxy in
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                HStack(alignment: .top, spacing: 9) {
                    StateMark(look: .needs, color: Theme.needs, size: 17).padding(.top, 2)
                    Text(title).font(Theme.rounded(16.5, .semibold)).foregroundStyle(Theme.needs)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .padding(.horizontal, 4)
                .accessibilityElement(children: .combine)
                if !detail.isEmpty {
                    Group {
                        if plan {
                            MarkdownView(detail, size: 15).padding(.horizontal, 12).padding(.vertical, 10)
                        } else {
                            Text(detail).font(Theme.mono(12.5)).foregroundStyle(Theme.ink).textSelection(.enabled)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .padding(.horizontal, 11).padding(.vertical, 9)
                        }
                    }
                    .background(Theme.card, in: RoundedRectangle(cornerRadius: 13, style: .continuous))
                }
                if opts == nil, let choices = n?.options, !choices.isEmpty {
                    VStack(alignment: .leading, spacing: 3) {
                        ForEach(Array(choices.enumerated()), id: \.offset) { _, o in
                            HStack(alignment: .firstTextBaseline, spacing: 7) {
                                Text("•").foregroundStyle(Theme.muted)
                                Text(o).foregroundStyle(Theme.ink)
                            }
                        }
                    }
                    .font(.system(size: 15))
                    .padding(.horizontal, 10)
                }
                if let opts {
                    VStack(spacing: 7) {
                        ForEach(opts, id: \.self) { o in
                            Button {
                                Task { await model.press([o.key]) }
                            } label: {
                                HStack(spacing: 11) {
                                    Text(o.key)
                                        .font(Theme.rounded(14, .bold))
                                        .foregroundStyle(o.selected == true ? Theme.onPrimary : Theme.muted)
                                        .frame(width: 28, height: 28)
                                        .background(o.selected == true ? Theme.accent : Theme.panel, in: Circle())
                                    Text(o.label).font(.system(size: 15.5)).foregroundStyle(Theme.ink)
                                        .frame(maxWidth: .infinity, alignment: .leading)
                                        .multilineTextAlignment(.leading)
                                }
                                .padding(.horizontal, 12).padding(.vertical, 9)
                                .frame(minHeight: 50)
                                .background(Theme.card, in: RoundedRectangle(cornerRadius: 15, style: .continuous))
                                .overlay(RoundedRectangle(cornerRadius: 15, style: .continuous)
                                    .strokeBorder(o.selected == true ? Theme.accent.opacity(0.45) : Theme.edge, lineWidth: 1))
                                .opacity(model.busyKey == o.key ? 0.6 : 1)
                                .contentShape(RoundedRectangle(cornerRadius: 15, style: .continuous))
                            }
                            .buttonStyle(PressStyle())
                            .disabled(model.busyKey != nil)
                            .accessibilityLabel("\(o.label), press \(o.key)")
                        }
                    }
                } else if model.screen == nil && model.screenProblem.isEmpty {
                    HStack(spacing: 8) {
                        ProgressView().controlSize(.small)
                        Text("Reading its screen…")
                    }
                    .font(.system(size: 13.5)).foregroundStyle(Theme.muted)
                }
                if opts == nil || model.showKeys {
                    LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 6), count: 4), spacing: 6) {
                        ForEach(Self.keypad, id: \.1) { label, key, spoken in
                            Button {
                                Task { await model.press([key]) }
                            } label: {
                                Text(label).font(Theme.rounded(15, .semibold)).foregroundStyle(Theme.ink)
                                    .frame(maxWidth: .infinity, minHeight: 44)
                                    .background(Theme.card, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                                    .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
                                    .opacity(model.busyKey == key ? 0.5 : 1)
                            }
                            .buttonStyle(PressStyle())
                            .disabled(model.busyKey != nil)
                            .accessibilityLabel(spoken)
                        }
                    }
                }
                if !model.screenProblem.isEmpty {
                    Text(model.screenProblem).font(.system(size: 13.5)).foregroundStyle(Theme.muted)
                }
                HStack {
                    Button {
                        model.toggleTerminal()
                    } label: {
                        Label(model.showTerm ? "Hide the terminal" : "Show the terminal", systemImage: "terminal")
                    }
                    Spacer()
                    if opts != nil {
                        Button(model.showKeys ? "Hide keys" : "Keys") { model.showKeys.toggle() }
                    }
                }
                .font(Theme.rounded(14.5, .semibold))
                .foregroundStyle(Theme.accentInk)
                .frame(minHeight: 36)
                if model.showTerm { TerminalText(model: model) }
                Color.clear.frame(height: 1).id("end")
            }
            .padding(.horizontal, 12).padding(.top, 14).padding(.bottom, 6)
        }
        .scrollBounceBehavior(.basedOnSize)
        // The terminal's last lines, where a prompt waits, in view.
        .onChange(of: model.showTerm) { if model.showTerm { withAnimation { proxy.scrollTo("end", anchor: .bottom) } } }
        .onChange(of: model.screen) { if model.showTerm { proxy.scrollTo("end", anchor: .bottom) } }
        }
        .frame(maxHeight: Screen.height * 0.55)
        .fixedSize(horizontal: false, vertical: true)
        .background(Theme.needsTint, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 20, style: .continuous).strokeBorder(Theme.needs.opacity(0.4), lineWidth: 1))
        .accessibilityElement(children: .contain)
        .accessibilityLabel("It needs you")
    }

    private func firstNonEmpty(_ values: String?...) -> String? {
        values.compactMap { $0 }.first { !$0.isEmpty }
    }
}
