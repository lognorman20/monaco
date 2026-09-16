import SwiftUI

/// M5 product launch hero — social investing copy, no debug addresses.
struct LaunchScreenView: View {
    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "person.3.fill")
                .font(.system(size: 56))
                .foregroundStyle(.tint)
                .accessibilityLabel("Monaco")

            Text("Monaco")
                .font(.largeTitle.bold())

            Text("Invest with your people")
                .font(.title3)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)

            Text("Pool money with friends, vote on buys, and track who’s winning.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .padding(.horizontal)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 8)
    }
}

#Preview {
    LaunchScreenView()
}
