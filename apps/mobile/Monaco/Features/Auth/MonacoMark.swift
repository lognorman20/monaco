import SwiftUI

/// The Monaco mark: the brand's four-person cluster, ink on paper in light mode and cream on the
/// canvas in dark.
///
/// It draws the `LaunchMark` image set rather than redrawing the geometry in a `Canvas`, so the
/// onboarding mark, the launch screen and the app icon are all the same artwork — rasterised from
/// `apps/web/assets/mark.svg` by `scripts/design/render-app-icon.swift`. The previous version drew
/// three circles with the top-right one in brand blue, which was neither the brand's mark nor the
/// brand's colour.
struct MonacoMark: View {
    var size: CGFloat = 88

    var body: some View {
        Image("LaunchMark")
            .resizable()
            .interpolation(.high)
            .aspectRatio(contentMode: .fit)
            .frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}

#Preview {
    VStack(spacing: 24) {
        MonacoMark(size: 88)
        MonacoMark(size: 24)
    }
    .padding()
}
