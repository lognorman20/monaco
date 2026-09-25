import MonacoCore
import SwiftUI

/// What a pre-IPO token carries that a stock does not: its sector and the round-the-clock
/// note, the private-market reference and the premium the token trades at, the issuer's
/// disclosure with its terms, and the other issuers the same company is available from.
struct PreIpoDetailSection: View {
    let detail: AssetDetailDTO
    /// Opens the same company from another issuer.
    var openVariant: (String) -> Void = { _ in }

    @State private var aboutExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            meta
            referenceRow
            about
            variants
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("asset-pre-ipo")
    }

    private var meta: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
            if let sector = detail.sector, !sector.isEmpty {
                Text(sector)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
            }
            Text(PreIpoCopy.tradesAroundTheClock)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
        }
    }

    private var referenceRow: some View {
        MonacoGroupedList {
            MonacoRow(title: PreIpoCopy.privateMarketReference, isLast: true) {
                EmptyView()
            } trailing: {
                VStack(alignment: .trailing, spacing: 4) {
                    if let mark = detail.referenceMarkUsdcMicros, detail.premiumBps != nil {
                        MoneyText(micros: mark, style: .row)
                    } else {
                        Text(PreIpoCopy.referenceUnavailable)
                            .font(MonacoTheme.Typo.body)
                            .foregroundStyle(MonacoTheme.muted)
                    }
                    if let bps = detail.premiumBps {
                        Text(PreIpoCopy.premiumChip(bps: bps))
                            .font(MonacoTheme.Typo.caption.weight(.semibold))
                            .foregroundStyle(abs(bps) >= 1000 ? MonacoTheme.warning : MonacoTheme.muted)
                            .accessibilityIdentifier("asset-pre-ipo-premium")
                    }
                    if let caption = companyValueCaption {
                        Text(caption)
                            .font(MonacoTheme.Typo.micro)
                            .foregroundStyle(MonacoTheme.tertiaryText)
                            .multilineTextAlignment(.trailing)
                    }
                }
            }
        }
        .accessibilityIdentifier("asset-pre-ipo-reference")
    }

    private var about: some View {
        DisclosureGroup(isExpanded: $aboutExpanded) {
            Text(PreIpoCopy.disclosure)
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.muted)
                .padding(.top, MonacoTheme.Space.xs)
            if let terms = URL(string: PreIpoCopy.termsURL) {
                Link(destination: terms) {
                    MonacoRow(title: PreIpoCopy.termsLinkTitle, chevron: true, isLast: true) {
                        EmptyView()
                    } trailing: { EmptyView() }
                }
                .buttonStyle(.plain)
            }
        } label: {
            Text(PreIpoCopy.aboutCardTitle)
                .font(MonacoTheme.Typo.section)
                .foregroundStyle(MonacoTheme.ink)
        }
        .tint(MonacoTheme.ink)
        .accessibilityIdentifier("asset-pre-ipo-about")
    }

    @ViewBuilder
    private var variants: some View {
        let variants = detail.variants ?? []
        if variants.count > 1 {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                MonacoSectionHeader(PreIpoCopy.alsoAvailableFrom)
                MonacoGroupedList {
                    ForEach(Array(variants.enumerated()), id: \.element.id) { index, variant in
                        let isCurrent = variant.symbol == detail.symbol
                        Button {
                            guard !isCurrent else { return }
                            openVariant(variant.symbol)
                        } label: {
                            MonacoRow(
                                title: variantTitle(variant),
                                subtitle: variantSubtitle(variant),
                                chevron: !isCurrent,
                                isLast: index == variants.count - 1
                            ) {
                                EmptyView()
                            } trailing: {
                                if let micros = variant.priceUsdcMicros {
                                    MoneyText(micros: micros, style: .row)
                                }
                            }
                        }
                        .buttonStyle(.monacoRow)
                        .disabled(isCurrent)
                        .accessibilityIdentifier("asset-pre-ipo-variant-\(variant.symbol)")
                    }
                }
            }
        }
    }

    private var companyValueCaption: String? {
        guard let valuation = detail.referenceValuationUsd else { return nil }
        let value = UsdAmountFormatter.compact(decimalString: String(valuation))
        guard let updated = detail.referenceUpdatedAt else {
            return "\(PreIpoCopy.companyValueCaption) \(value)"
        }
        let age = RelativeTimeFormatter.label(iso: updated)
        guard !age.isEmpty else {
            return "\(PreIpoCopy.companyValueCaption) \(value)"
        }
        return "\(PreIpoCopy.companyValueCaption) \(value) · updated \(age) ago"
    }

    private func variantTitle(_ variant: AssetVariantDTO) -> String {
        if variant.paused == true { return "\(variant.resolvedIssuerName) · paused" }
        if variant.bestPrice == true { return "\(variant.resolvedIssuerName) · Best price" }
        return variant.resolvedIssuerName
    }

    private func variantSubtitle(_ variant: AssetVariantDTO) -> String {
        var parts = [variantLiquidity(variant)]
        if let fee = variant.transferFeeBps, fee > 0 {
            parts.append(String(format: "Fee %.1f%%", Double(fee) / 100))
        }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private func variantLiquidity(_ variant: AssetVariantDTO) -> String {
        if let liquidity = variant.liquidityUsd, !liquidity.isEmpty {
            return "Liquidity \(UsdAmountFormatter.compact(decimalString: liquidity))"
        }
        return variant.symbol
    }
}
