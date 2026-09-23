import SwiftUI

struct StepUpAuthView: View {
    @ObservedObject var auth: DynamicAuthService
    @State private var otpCode = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Confirm it’s you")
                .displayFont(.title)
                .foregroundStyle(MonacoTheme.Ink.fgPrimary)
            Text("Enter the code we sent so we can keep this session open.")
                .authSecondaryCaption()

            TextField(
                "",
                text: $otpCode,
                prompt: Text("6-digit code").foregroundStyle(MonacoTheme.Ink.fgSubtle)
            )
            .keyboardType(.numberPad)
            .textContentType(.oneTimeCode)
            .focused($focused)
            .authTextFieldStyle()
            .accessibilityIdentifier("stepUpCodeField")

            if let message = auth.securityOTPMessage {
                Text(message)
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.lossOnHero)
            }

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
        .onAppear { focused = true }
    }
}
