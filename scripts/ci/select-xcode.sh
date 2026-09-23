#!/usr/bin/env bash
# Select the newest released Xcode on a GitHub macOS runner. The runner's default Xcode can
# lag behind the SDK the app is written against; betas and release candidates are skipped.
# MONACO_CI_XCODE pins one instead (for example 26.2) when the newest one breaks the build.
set -euo pipefail

if [[ -n "${MONACO_CI_XCODE:-}" ]]; then
  xcode="/Applications/Xcode_${MONACO_CI_XCODE}.app"
else
  xcode="$(find /Applications -maxdepth 1 -name 'Xcode_[0-9]*.app' \
    | grep -viE 'beta|release_candidate|_rc' \
    | sed -E 's|.*/Xcode_([0-9.]+)\.app$|\1 &|' | sort -V | tail -1 | cut -d' ' -f2-)"
fi
[[ -d "$xcode" ]] || { echo "no Xcode at '${xcode}'" >&2; ls -d /Applications/Xcode* >&2; exit 1; }

sudo xcode-select -s "$xcode/Contents/Developer"
xcodebuild -version
xcrun simctl list runtimes available | grep -i ios
