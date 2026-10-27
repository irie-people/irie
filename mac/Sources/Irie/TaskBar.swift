import SwiftUI

/// The task bar at the foot of the sidebar. Nothing appears or vanishes
/// around it, so the list above never shifts: the hint is the placeholder,
/// and notices float above the bar.
struct TaskBar: View {
    @EnvironmentObject private var store: Store
    @State private var text = ""
    @FocusState private var focused: Bool

    var body: some View {
        HStack(alignment: .center, spacing: 8) {
            Image(systemName: "plus")
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
            TextField(focused ? "@repo +profile, then the task" : "New task…", text: $text, axis: .vertical)
                .textFieldStyle(.plain)
                .font(.system(size: 13))
                .lineLimit(1...6)
                .focused($focused)
                .onSubmit(submit)
                .disabled(store.spawning)
                .accessibilityLabel("New task")
                .accessibilityHint("Start with @repo or +profile to pick where and who. Return starts it.")
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
            .accessibilityLabel("Start the task")
        }
        .padding(.leading, 10)
        .padding(.trailing, 5)
        .padding(.vertical, 5)
        .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(Theme.field))
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .strokeBorder(focused ? Theme.sea : Color.primary.opacity(0.12), lineWidth: focused ? 1.5 : 1)
        )
        .padding(10)
        .overlay(alignment: .top) {
            if let n = store.notice {
                Text(n.text)
                    .font(.system(size: 11.5, weight: .medium))
                    .foregroundStyle(n.isError ? Theme.needs : Color.primary)
                    .lineLimit(4)
                    .textSelection(.enabled)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 7)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Theme.field).shadow(radius: 3, y: 1))
                    .padding(.horizontal, 10)
                    // 4 points above the field, over the bar's padding and the list
                    .alignmentGuide(.top) { d in d[.bottom] - 6 }
                    .transition(.opacity)
                    .accessibilityAddTraits(.updatesFrequently)
            }
        }
        .animation(.easeOut(duration: 0.15), value: store.notice)
        .onReceive(NotificationCenter.default.publisher(for: .irieNewTask)) { _ in focused = true }
    }

    private func submit() {
        let input = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !input.isEmpty, !store.spawning else { return }
        Task {
            if await store.spawn(input) { text = "" }
        }
    }
}
