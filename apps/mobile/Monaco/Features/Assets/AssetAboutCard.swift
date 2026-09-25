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
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        AssetDetailCard(title: about.title, identifier: "asset-detail-about") {
            VStack(alignment: .leading, spacing: 0) {
                body(about.body)
                    .padding(.bottom, MonacoTheme.Space.sm)
                facts
                disclosure
                    .padding(.top, MonacoTheme.Space.sm)
            }
        }
    }

    private func body(_ text: String) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text(text)
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(isExpanded ? nil : 3)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityIdentifier("asset-about-body")

            AssetSectionTextButton(title: isExpanded ? "Show less" : "Show more") {
                // A height change is a layout change, not a decoration: it animates
                // unless the reader has asked for less motion.
                if reduceMotion {
                    isExpanded.toggle()
                } else {
                    withAnimation(.easeInOut(duration: 0.2)) { isExpanded.toggle() }
                }
            }
            .accessibilityIdentifier("asset-about-toggle")
        }
    }

    /// Label and value on one ruled line each, the way the stats read above.
    private var facts: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(about.facts) { fact in
                AssetCardDivider()
                factRow(fact)
                    .padding(.vertical, MonacoTheme.Space.sm)
                    .accessibilityElement(children: .combine)
                    .accessibilityIdentifier("asset-about-fact-\(fact.id)")
            }
        }
    }

    @ViewBuilder
    private func factRow(_ fact: AssetAboutCopy.Fact) -> some View {
        // At the accessibility sizes every fact stacks, the way the other sections'
        // rows do, rather than squeezing a value against the right edge.
        if fact.isAddress || dynamicTypeSize.isAccessibilitySize {
            // The mint gets its own line under its label. Beside the label it had about
            // two thirds of the width, so its 44 characters broke onto a second line that
            // hung under the middle of the row; on a line of its own it fits whole.
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                label(fact.label)
                if fact.isAddress {
                    MonacoWalletAddressText(
                        address: fact.value,
                        textStyle: .footnote,
                        foreground: MonacoTheme.ink
                    )
                } else {
                    Text(fact.value)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.ink)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        } else {
            HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
                label(fact.label)
                Spacer(minLength: MonacoTheme.Space.s)
                Text(fact.value)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.ink)
                    .multilineTextAlignment(.trailing)
            }
        }
    }

    private func label(_ text: String) -> some View {
        Text(text)
            .font(MonacoTheme.Typo.caption)
            .foregroundStyle(MonacoTheme.muted)
    }

    private var disclosure: some View {
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
            Image(systemName: "info.circle")
                .font(MonacoTheme.Typo.captionStrong)
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

/// "Show more" and its kin: a text button in the brand's ink that belongs to the line
/// above it.
///
/// The target is still 44pt tall, but only 22 of it is layout. It used to be a 44pt
/// frame with the words centred in it, which put a 12pt gap on each side of the label
/// and left "Show more" floating halfway between the paragraph it opens and whatever
/// came after it.
struct AssetSectionTextButton: View {
    let title: String
    let action: () -> Void

    /// Hit area past the drawn label, above and below, taken straight back out of the
    /// layout — the same trick `DayChangePill` uses for its tap target.
    private static let targetOutset: CGFloat = 11

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(MonacoTheme.Typo.calloutStrong)
                .foregroundStyle(MonacoTheme.brand)
                .padding(.vertical, Self.targetOutset)
                .padding(.trailing, MonacoTheme.Space.m)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .padding(.vertical, -Self.targetOutset)
    }
}
