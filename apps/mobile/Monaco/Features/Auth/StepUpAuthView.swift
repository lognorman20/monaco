import SwiftUI

struct StepUpAuthView: View {
    @ObservedObject var auth: DynamicAuthService
    @State private var otpCode = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Confirm it’s you")
                .font(.title2.bold())
                .foregroundStyle(MonacoTheme.primaryText)
            Text("Enter the code we sent so we can keep this session open.")
                .authSecondaryCaption()

            TextField(
                "",
                text: $otpCode,
                prompt: Text("6-digit code").foregroundStyle(MonacoTheme.tertiaryText)
            )
            .keyboardType(.numberPad)
            .textContentType(.oneTimeCode)
            .focused($focused)
            .authTextFieldStyle()
            .accessibilityIdentifier("stepUpCodeField")

            Button("Send code") {
                Task { await auth.sendStepUpCode() }
            }
            .buttonStyle(.monacoSecondary)
            .accessibilityIdentifier("stepUpSendButton")

            Button("Continue") {
                Task { await auth.completeStepUp(otpCode) }
            }
            .buttonStyle(.monacoPrimary)
            .disabled(otpCode.trimmingCharacters(in: .whitespacesAndNewlines).count < 4)
            .accessibilityIdentifier("stepUpVerifyButton")
        }
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .task {
            await auth.sendStepUpCode()
            focused = true
        }
    }
}
