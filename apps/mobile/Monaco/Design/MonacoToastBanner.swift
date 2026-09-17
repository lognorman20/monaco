import SwiftUI

struct MonacoToast: Equatable, Identifiable {
    let id = UUID()
    let message: String
    var isSuccess = false
}

struct MonacoToastBanner: View {
    let message: String
    var isSuccess = false

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: isSuccess ? "checkmark.circle.fill" : "exclamationmark.triangle.fill")
                .foregroundStyle(isSuccess ? MonacoTheme.success : MonacoTheme.warning)
            Text(message)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(MonacoTheme.primaryText)
                .multilineTextAlignment(.leading)
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .strokeBorder(
                    isSuccess ? MonacoTheme.success.opacity(0.35) : MonacoTheme.warning.opacity(0.35),
                    lineWidth: 1
                )
        }
        .shadow(color: .black.opacity(0.12), radius: 8, y: 4)
        .padding(.horizontal, 16)
        .accessibilityIdentifier("monaco-toast-banner")
    }
}

private struct MonacoToastModifier: ViewModifier {
    @Binding var toast: MonacoToast?

    func body(content: Content) -> some View {
        content
            .overlay(alignment: .top) {
                if let toast {
                    MonacoToastBanner(message: toast.message, isSuccess: toast.isSuccess)
                        .transition(.move(edge: .top).combined(with: .opacity))
                        .padding(.top, 8)
                        .zIndex(1)
                }
            }
            .animation(.easeInOut(duration: 0.25), value: toast?.id)
            .onChange(of: toast?.id) { _, newID in
                guard let newID else { return }
                Task {
                    try? await Task.sleep(for: .seconds(4))
                    await MainActor.run {
                        if toast?.id == newID {
                            toast = nil
                        }
                    }
                }
            }
    }
}

extension View {
    func monacoToast(_ toast: Binding<MonacoToast?>) -> some View {
        modifier(MonacoToastModifier(toast: toast))
    }
}
