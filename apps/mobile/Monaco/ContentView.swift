import SwiftUI

struct ContentView: View {
    private let apiClient = MonacoAPIClient()

    @State private var healthStatus: String?
    @State private var errorMessage: String?
    @State private var isLoading = true

    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "chart.line.uptrend.xyaxis")
                .imageScale(.large)
                .foregroundStyle(.tint)

            Text("Monaco")
                .font(.title.bold())

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
        .padding()
        .task {
            await loadHealth()
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
}
