import SwiftUI

/// The task bar at the foot of the sidebar: one line until you use it.
struct TaskBar: View {
    @EnvironmentObject private var store: Store
    @State private var text = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let n = store.notice {
                Text(n.text)
                    .font(.system(size: 11.5))
                    .foregroundStyle(n.isError ? Theme.needs : .secondary)
                    .lineLimit(4)
                    .textSelection(.enabled)
                    .transition(.opacity)
            }
            HStack(alignment: .center, spacing: 8) {
                Image(systemName: "plus").font(.system(size: 12, weight: .semibold)).foregroundStyle(.secondary)
                TextField("New task…", text: $text, axis: .vertical)
                    .textFieldStyle(.plain)
                    .font(.system(size: 13))
                    .lineLimit(1...6)
                    .focused($focused)
                    .onSubmit(submit)
                    .disabled(store.spawning)
                Button(action: submit) {
                    Group {
                        if store.spawning {
                            ProgressView().controlSize(.small)
                        } else {
                            Image(systemName: "arrow.up").font(.system(size: 12, weight: .bold))
                        }
                    }
                    .frame(width: 24, height: 24)
                    .background(Circle().fill(Theme.sun))
                    .foregroundStyle(Color(hex: 0x1B2B22))
                }
                .buttonStyle(.plain)
                .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || store.spawning)
                .help("Start (Return)")
            }
            .padding(.leading, 10)
            .padding(.trailing, 5)
            .padding(.vertical, 5)
            .background(RoundedRectangle(cornerRadius: 10).fill(.background.opacity(0.85)))
            .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(focused ? Theme.sea : Color.secondary.opacity(0.25)))
            if focused {
                Text("Start with @repo or +profile to pick where and who. Return starts it.")
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
            }
        }
        .padding(10)
        .onReceive(NotificationCenter.default.publisher(for: .irieNewTask)) { _ in focused = true }
        .animation(.easeOut(duration: 0.15), value: store.notice)
        .animation(.easeOut(duration: 0.15), value: focused)
    }

    private func submit() {
        let input = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !input.isEmpty, !store.spawning else { return }
        Task {
            if await store.spawn(input) { text = "" }
        }
    }
}
