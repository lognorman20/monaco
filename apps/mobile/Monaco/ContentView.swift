import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var auth: PrivyAuthService
    private let apiClient = MonacoAPIClient()

    @State private var healthStatus: String?
    @State private var errorMessage: String?
    @State private var isLoading = true

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 24) {
                    header

                    AuthGateView(auth: auth)

                    Divider()

                    apiHealthSection
                }
                .padding()
            }
            .task {
                await loadHealth()
            }
        }
    }

    private var header: some View {
        VStack(spacing: 8) {
            Image(systemName: "chart.line.uptrend.xyaxis")
                .imageScale(.large)
                .foregroundStyle(.tint)

            Text("Monaco")
                .font(.title.bold())
        }
    }

    private var apiHealthSection: some View {
        VStack(spacing: 12) {
            Text("API health")
                .font(.headline)

            if isLoading {
                ProgressView("Checking API…")
            } else if let healthStatus {
                Label("API \(healthStatus)", systemImage: "checkmark.circle.fill")
                    .foregroundStyle(.green)
            } else if let errorMessage {
                Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(.orange)
                    .multilineTextAlignment(.center)
            }
        }
    }

    private func loadHealth() async {
        isLoading = true
        errorMessage = nil
        healthStatus = nil

        do {
            let response = try await apiClient.health()
            healthStatus = response.status
        } catch {
            errorMessage = "API unreachable"
        }

        isLoading = false
    }
}

#Preview {
    ContentView()
        .environmentObject(PrivyAuthService())
}
