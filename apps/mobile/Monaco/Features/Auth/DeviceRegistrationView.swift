import SwiftUI

struct DeviceRegistrationView: View {
    @ObservedObject var auth: DynamicAuthService
    @State private var otpCode = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Confirm this device")
                .displayFont(.title)
                .foregroundStyle(MonacoTheme.Ink.fgPrimary)
            Text(auth.loginChannel.deviceRegistrationCopy)
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
            .accessibilityIdentifier("deviceRegistrationCodeField")

            if let message = auth.securityOTPMessage {
                Text(message)
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.lossOnHero)
            }

            Button("Send code") {
                Task { await auth.sendDeviceRegistrationCode() }
            }
            .buttonStyle(.monacoSecondary)
            .accessibilityIdentifier("deviceRegistrationSendButton")

            Button("Continue") {
                Task { await auth.completeDeviceRegistration(otpCode) }
            }
            .buttonStyle(.monacoPrimary)
            .disabled(otpCode.trimmingCharacters(in: .whitespacesAndNewlines).count < 4)
            .accessibilityIdentifier("deviceRegistrationVerifyButton")
        }
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .onAppear { focused = true }
    }
}
