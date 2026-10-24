import AppKit
import SwiftUI
import WidgetKit

@main
enum WidgetMain {
  static func main() {
    let arguments = Array(CommandLine.arguments.dropFirst())
    guard arguments.first?.hasPrefix("--pk-") == true else {
      MyApp.main()
      return
    }
    do {
      try WidgetCommand.run(arguments)
    } catch {
      let message = Data("\(error.localizedDescription)\n".utf8)
      FileHandle.standardError.write(message)
      exit(EXIT_FAILURE)
    }
  }
}

struct MyApp: App {
  @State private var snapshot: PKSnapshot?

  var body: some Scene {
    WindowGroup {
      TimelineView(.explicit(expirationDates)) { context in
        ContentView(snapshot: snapshot, at: context.date)
          .padding()
          .frame(width: 164, height: 164)
      }
      .id(snapshot?.generatedAt)
      .task { refreshSnapshot() }
      .onReceive(
        NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)
      ) {
        _ in refreshSnapshot()
      }
    }
  }

  private var expirationDates: [Date] {
    let now = Date()
    guard let snapshot, snapshot.isCurrent(at: now) else { return [now] }
    let expiry = snapshot.generatedAt.addingTimeInterval(PKSnapshot.freshnessInterval)
    return [now, expiry]
  }

  private func refreshSnapshot() {
    let latest = try? SnapshotStore().read()
    let changed = snapshot?.generatedAt != latest?.generatedAt
    snapshot = latest
    if changed { WidgetCenter.shared.reloadTimelines(ofKind: "PKStats") }
  }
}
