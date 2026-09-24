import MonacoCore
import SwiftUI

/// "About AAPLx": what the token is, and the disclosure that has to sit under it.
///
/// The body clamps to three lines with a "Show more". The disclosure never does —
/// it is drawn separately, below the fold-out, so a collapsed paragraph cannot hide
/// the one sentence that matters: this is a tracker, not a share.
struct AssetAboutCard: View {
    let about: AssetAboutCopy

    @State private var isExpanded = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        AssetDetailCard(title: about.title, identifier: "asset-detail-about") {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                body(about.body)
                facts
                disclosure
            }
        }
    }

    private func body(_ text: String) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
            Text(text)
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(isExpanded ? nil : 3)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityIdentifier("asset-about-body")

            Button(isExpanded ? "Show less" : "Show more") {
                // A height change is a layout change, not a decoration: it animates
                // unless the reader has asked for less motion.
                if reduceMotion {
                    isExpanded.toggle()
                } else {
                    withAnimation(.easeInOut(duration: 0.2)) { isExpanded.toggle() }
                }
            }
            .font(MonacoTheme.Typo.callout.weight(.semibold))
            .foregroundStyle(MonacoTheme.brand)
            .frame(minHeight: 44, alignment: .leading)
            .accessibilityIdentifier("asset-about-toggle")
        }
    }

    private var facts: some View {
        VStack(spacing: MonacoTheme.Space.s) {
            ForEach(about.facts) { fact in
                HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
                    Text(fact.label)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                    Spacer(minLength: MonacoTheme.Space.s)
                    if fact.isAddress {
                        // A mint is 44 characters of base58. Truncating the middle
                        // keeps both ends, which is what anyone comparing one reads.
                        MonacoWalletAddressText(
                            address: fact.value,
                            textStyle: .footnote,
                            foreground: MonacoTheme.ink
                        )
                    } else {
                        Text(fact.value)
                            .font(MonacoTheme.Typo.caption.weight(.semibold))
                            .foregroundStyle(MonacoTheme.ink)
                            .multilineTextAlignment(.trailing)
                    }
                }
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("asset-about-fact-\(fact.id)")
            }
        }
    }

    private var disclosure: some View {
        HStack(alignment: .top, spacing: MonacoTheme.Space.s) {
            Image(systemName: "info.circle")
                .foregroundStyle(MonacoTheme.warning)
                .accessibilityHidden(true)
            Text(about.disclosure)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.ink)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(MonacoTheme.Space.sm)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            MonacoTheme.surfaceSunken,
            in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous)
        )
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("asset-about-disclosure")
    }
}
