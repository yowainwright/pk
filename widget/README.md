# pk widget

Native macOS WidgetKit stats for pk: service state, last check, tracked processes,
and killed/failed totals over 24 hours.

**Development only:** publication is manual. Automatic expiry in the open app
still fails the live check; headless widget refresh remains unverified.

<!-- Native setup from Widget/Widget.xcodeproj; versions from Widget/Version.xcconfig; CLI from Widget/Widget/WidgetCommand.swift -->

Use Xcode 27.1. App and extension need the same development team and authorized
[App Group](https://developer.apple.com/documentation/xcode/configuring-app-groups):
`group.com.yowainwright.pk.widget`. Local build number stays `1` in
[`Version.xcconfig`](Widget/Version.xcconfig); release packaging supplies its own.

## Build and install

Run commands from the pk repository root:

```sh
xcodebuild -project widget/Widget/Widget.xcodeproj -scheme Widget \
  -configuration Debug -destination 'platform=macOS,arch=arm64' \
  -derivedDataPath tmp/widget-build \
  -allowProvisioningUpdates build test
```

Quit any running copy of Widget before installing:

```sh
mkdir -p "$HOME/Applications"
ditto tmp/widget-build/Build/Products/Debug/Widget.app \
  "$HOME/Applications/PK Widget.app"
/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister \
  -f "$HOME/Applications/PK Widget.app"
```

## Publish manually

```sh
go run ./cmd/pk history --summary --json > tmp/pk-widget-report.json &&
  "$HOME/Applications/PK Widget.app/Contents/MacOS/Widget" \
  --pk-publish < tmp/pk-widget-report.json
```

Open `~/Applications/PK Widget.app` to read the report. Use its executable with
`--pk-status` to inspect the stored generation. The pk service does not publish yet.

## Lint

<!-- Swift checks configured in .swiftlint.yml -->

Requires [SwiftLint](https://github.com/realm/SwiftLint); function and closure limits are 20 lines.

```sh
swiftlint lint --strict --config widget/.swiftlint.yml
swift format lint --strict --recursive widget/Widget/Widget widget/Widget/PKStats widget/Widget/PKStatsTests
```
