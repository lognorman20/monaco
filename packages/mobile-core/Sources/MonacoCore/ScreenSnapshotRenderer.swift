import Foundation

/// Deterministic text snapshots of group and home screens for fixture regression tests.
/// Mirrors visible copy from SwiftUI product views without UIKit/SwiftUI dependencies.
public enum ScreenSnapshotRenderer {
    public static func groupScreen(from view: GroupViewDTO) -> String {
        var lines: [String] = ["# \(view.name)", "", "## Pot"]
        if view.pot.isEmpty {
            lines.append("No holdings yet. Add money to get started.")
        } else {
            lines.append("Total | $\(view.resolvedPotTotalUsd)")
            for row in view.pot {
                var headline = "\(row.symbol) | $\(row.valueUsd)"
                if row.afterHours == true {
                    headline += " [After hours]"
                }
                lines.append(headline)
                lines.append("  \(row.dollarPnl)")
                lines.append("  \(row.units) units @ $\(row.markUsd)")
            }
        }

        lines.append("")
        lines.append("## You")
        lines.append("Your slice | $\(view.you.equityUsd)")
        lines.append("Slice % | \(formatSlicePercent(view.you.slicePercent))")
        lines.append("P&L | \(view.you.dollarPnl)")
        if let percentReturn = view.you.percentReturn {
            lines.append("Return | \(percentReturn)")
        } else {
            lines.append("Return | —")
        }

        lines.append("")
        lines.append("## Member board")
        if view.members.isEmpty {
            lines.append("No members ranked yet.")
        } else {
            for row in view.members {
                let pct = row.percentReturn ?? "—"
                lines.append("#\(row.rank) \(row.displayName) | \(pct) | \(row.dollarPnl)")
            }
        }

        return lines.joined(separator: "\n")
    }

    public static func appHome(from view: HomeViewDTO) -> String {
        var lines: [String] = ["# Home", "", "## Groups"]
        if view.groups.isEmpty {
            lines.append("No clubs yet. Create or join one to start investing together.")
        } else {
            for row in view.groups {
                lines.append("\(row.name) | $\(row.potValueUsd) | \(row.dollarPnl)")
            }
        }

        lines.append("")
        lines.append("## People")
        if view.people.isEmpty {
            lines.append("No leaderboard rows yet.")
        } else {
            for row in view.people {
                let pct = row.percentReturn ?? "—"
                lines.append("\(row.displayName) | \(pct) | \(row.dollarPnl)")
            }
        }

        return lines.joined(separator: "\n")
    }

    private static func formatSlicePercent(_ raw: String) -> String {
        SlicePercentFormatter.format(raw)
    }
}
