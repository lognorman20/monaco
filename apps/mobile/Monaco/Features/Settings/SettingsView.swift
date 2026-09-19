import PhotosUI
import SwiftUI
import UIKit

/// App settings — profile photo, account wallet, and withdraw.
struct SettingsView: View {
    @ObservedObject var auth: PrivyAuthService

    private let apiClient = MonacoAPIClient()

    @State private var memberWalletAddress: String?
    @State private var profilePhotoURL: String?
    @State private var isLoadingAddress = true
    @State private var addressError: String?
    @State private var isUploadingPhoto = false
    @State private var selectedPhotoItem: PhotosPickerItem?
    @State private var toast: MonacoToast?

    var body: some View {
        Form {
            Section("Profile photo") {
                HStack(spacing: 16) {
                    profilePhotoPreview
                    VStack(alignment: .leading, spacing: 8) {
                        PhotosPicker(selection: $selectedPhotoItem, matching: .images) {
                            Label(isUploadingPhoto ? "Uploading…" : "Upload PFP", systemImage: "photo.on.rectangle.angled")
                        }
                        .disabled(isUploadingPhoto || auth.accessToken == nil)
                        .monacoFormSecondaryAction()
                        .accessibilityIdentifier("settings-upload-pfp")
                    }
                }
            }

            Section("Your deposit address") {
                if isLoadingAddress {
                    HStack(spacing: 12) {
                        ProgressView().tint(MonacoTheme.accent)
                        Text("Loading address…")
                            .monacoSecondaryCaption()
                    }
                    .accessibilityIdentifier("settings-deposit-address-loading")
                } else if let memberWalletAddress {
                    VStack(alignment: .leading, spacing: 12) {
                        Text("Send USDC on Solana here to add to your account balance.")
                            .monacoSecondaryCaption()
                        MonacoWalletAddressText(address: memberWalletAddress)
                            .accessibilityIdentifier("settings-deposit-address-value")
                            .onTapGesture { copyAddress(memberWalletAddress) }
                        Button {
                            copyAddress(memberWalletAddress)
                        } label: {
                            Label("Copy address", systemImage: "doc.on.doc")
                        }
                        .monacoFormSecondaryAction()
                        .accessibilityIdentifier("settings-deposit-address-copy")
                    }
                } else {
                    VStack(alignment: .leading, spacing: 12) {
                        Text(addressError ?? "Deposit address not ready yet.")
                            .foregroundStyle(MonacoTheme.warning)
                        Button("Try again") {
                            Task { await loadAccountDetails() }
                        }
                        .monacoFormSecondaryAction()
                        .accessibilityIdentifier("settings-deposit-address-retry")
                    }
                }
            }

            Section {
                NavigationLink {
                    WithdrawView(auth: auth)
                } label: {
                    Label("Withdraw", systemImage: "arrow.up.right")
                        .foregroundStyle(MonacoTheme.primaryText)
                }
                .accessibilityIdentifier("settings-withdraw-link")

                NavigationLink {
                    AdvancedSettingsView()
                } label: {
                    Label("Advanced", systemImage: "link")
                        .foregroundStyle(MonacoTheme.primaryText)
                }
                .accessibilityIdentifier("settings-advanced-link")
            }

            Section {
                Button("Sign out") {
                    Task { await auth.logout() }
                }
                .monacoFormDestructiveAction()
            }
        }
        .monacoFormScreen()
        .navigationTitle("Settings")
        .navigationBarTitleDisplayMode(.inline)
        .monacoToast($toast)
        .task(id: auth.accessToken) {
            await loadAccountDetails()
        }
        .onChange(of: selectedPhotoItem) { _, newItem in
            guard let newItem else { return }
            Task { await uploadSelectedPhoto(newItem) }
        }
    }

    @ViewBuilder
    private var profilePhotoPreview: some View {
        let trimmedURL = profilePhotoURL?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if let url = URL(string: trimmedURL), !trimmedURL.isEmpty {
            AsyncImage(url: url) { phase in
                switch phase {
                case .success(let image):
                    image
                        .resizable()
                        .scaledToFill()
                case .failure:
                    profilePhotoPlaceholder
                default:
                    ProgressView().tint(MonacoTheme.accent)
                }
            }
            .frame(width: 72, height: 72)
            .clipShape(Circle())
            .accessibilityIdentifier("settings-profile-photo-preview")
        } else {
            profilePhotoPlaceholder
                .accessibilityIdentifier("settings-profile-photo-placeholder")
        }
    }

    private var profilePhotoPlaceholder: some View {
        ZStack {
            Circle()
                .fill(MonacoTheme.surface)
            Image(systemName: "person.crop.circle.fill")
                .font(.system(size: 44))
                .foregroundStyle(MonacoTheme.secondaryText)
        }
        .frame(width: 72, height: 72)
    }

    private func copyAddress(_ address: String) {
        UIPasteboard.general.string = address
        toast = MonacoToast(message: "Address copied.", isSuccess: true)
    }

    private func loadAccountDetails() async {
        guard let token = auth.accessToken else {
            memberWalletAddress = nil
            profilePhotoURL = nil
            addressError = "Sign in to view your deposit address."
            isLoadingAddress = false
            return
        }

        isLoadingAddress = true
        addressError = nil

        do {
            if let balance = try? await apiClient.getPlatformBalance(accessToken: token) {
                let fromBalance = balance.memberWalletAddress.trimmingCharacters(in: .whitespacesAndNewlines)
                if !fromBalance.isEmpty, !fromBalance.hasPrefix("FAKE") {
                    memberWalletAddress = fromBalance
                }
            }

            _ = try await apiClient.openSession(accessToken: token)
            let profile = try await apiClient.me(accessToken: token)
            profilePhotoURL = profile.profilePhotoUrl

            if memberWalletAddress == nil {
                let address = profile.memberWalletAddress.trimmingCharacters(in: .whitespacesAndNewlines)
                guard !address.isEmpty, !address.hasPrefix("FAKE") else {
                    memberWalletAddress = nil
                    addressError = "Deposit address not ready yet."
                    isLoadingAddress = false
                    return
                }
                memberWalletAddress = address
            }
        } catch MonacoAPIError.httpStatus(let status) {
            memberWalletAddress = nil
            addressError = "Could not load address (HTTP \(status))."
        } catch {
            memberWalletAddress = nil
            addressError = "Could not load deposit address."
        }

        isLoadingAddress = false
    }

    private func uploadSelectedPhoto(_ item: PhotosPickerItem) async {
        guard let token = auth.accessToken else { return }
        isUploadingPhoto = true
        defer {
            isUploadingPhoto = false
            selectedPhotoItem = nil
        }

        do {
            guard let data = try await item.loadTransferable(type: Data.self) else {
                toast = MonacoToast(message: "Could not read that photo.", isSuccess: false)
                return
            }
            guard let (preparedData, mimeType) = ProfilePhotoUploadPreparer.prepare(from: data) else {
                toast = MonacoToast(message: "Photo must be jpeg, png, or webp under 2MB.", isSuccess: false)
                return
            }
            let profile = try await apiClient.uploadProfilePhoto(
                accessToken: token,
                imageData: preparedData,
                mimeType: mimeType
            )
            profilePhotoURL = profile.profilePhotoUrl
            toast = MonacoToast(message: "Profile photo updated.", isSuccess: true)
        } catch MonacoAPIError.httpStatus(let status) {
            toast = MonacoToast(message: "Upload failed (HTTP \(status)).", isSuccess: false)
        } catch {
            toast = MonacoToast(message: "Could not upload profile photo.", isSuccess: false)
        }
    }
}

struct AdvancedSettingsView: View {
    var body: some View {
        Form {
            Section("Block explorers") {
                ForEach(SettingsAdvancedLinks.explorerLinks) { link in
                    Link(destination: link.url) {
                        Label(link.title, systemImage: "safari")
                            .foregroundStyle(MonacoTheme.accent)
                    }
                    .accessibilityIdentifier("settings-explorer-\(link.id)")
                }
            }
        }
        .monacoFormScreen()
        .navigationTitle("Advanced")
        .navigationBarTitleDisplayMode(.inline)
    }
}

#Preview {
    NavigationStack {
        SettingsView(auth: PrivyAuthService())
    }
}
