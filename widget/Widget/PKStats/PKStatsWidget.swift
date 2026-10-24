import SwiftUI
import WidgetKit

struct PKStatsEntry: TimelineEntry {
  let date: Date
  let snapshot: PKSnapshot?
}

struct PKStatsProvider: TimelineProvider {
  func placeholder(in context: Context) -> PKStatsEntry {
    PKStatsEntry(date: Date(), snapshot: nil)
  }

  func getSnapshot(in context: Context, completion: @escaping (PKStatsEntry) -> Void) {
    let snapshot = try? SnapshotStore().read()
    completion(PKStatsEntry(date: Date(), snapshot: snapshot))
  }

  func getTimeline(in context: Context, completion: @escaping (Timeline<PKStatsEntry>) -> Void) {
    let snapshot = try? SnapshotStore().read()
    let now = Date()
    let refreshAt =
      snapshot?.refreshDate(after: now) ?? now.addingTimeInterval(PKSnapshot.freshnessInterval)
    let current = PKStatsEntry(date: now, snapshot: snapshot)
    let expired = PKStatsEntry(date: refreshAt, snapshot: snapshot)
    completion(Timeline(entries: [current, expired], policy: .after(refreshAt)))
  }
}

@main
struct PKStatsWidget: Widget {
  let kind = "PKStats"

  var body: some WidgetConfiguration {
    StaticConfiguration(kind: kind, provider: PKStatsProvider()) { entry in
      ContentView(snapshot: entry.snapshot, at: entry.date)
        .containerBackground(.fill.tertiary, for: .widget)
    }
    .configurationDisplayName("PK")
    .description("Service status and process cleanup over the last 24 hours.")
    .supportedFamilies([.systemSmall])
  }
}

#Preview(as: .systemSmall) {
  PKStatsWidget()
} timeline: {
  PKStatsEntry(date: Date(), snapshot: nil)
}
