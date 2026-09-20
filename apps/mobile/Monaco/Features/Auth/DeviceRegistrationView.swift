import SwiftUI

struct DeviceRegistrationView: View {
    @ObservedObject var auth: DynamicAuthService
    @State private var otpCode = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Confirm this device")
                .font(.title2.bold())
                .foregroundStyle(MonacoTheme.primaryText)
            Text("We’ll text or email a code so this phone can stay signed in.")
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
            .accessibilityIdentifier("deviceRegistrationCodeField")

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
        .task {
            await auth.sendDeviceRegistrationCode()
            focused = true
        }
    }
}
