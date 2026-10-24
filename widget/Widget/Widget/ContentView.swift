import SwiftUI

struct ContentView: View {
  let serviceStatus: String
  let lastCheck: Date?
  let tracked: String
  let killed: String
  let failed: String

  var body: some View {
    VStack(alignment: .leading, spacing: 6) {
      Text("PK").font(.title).bold()
      Text(serviceStatus).font(.caption2).bold()
      updateLabel
        .font(.caption2)
        .foregroundStyle(.secondary)
      Text("tracked \(tracked)").font(.caption2)
      Spacer(minLength: 0)
      cleanupMetrics
    }
  }

  private var cleanupMetrics: some View {
    HStack(alignment: .firstTextBaseline, spacing: 6) {
      metric(killed, label: "killed")
      Text("|").font(.caption2).foregroundStyle(.secondary)
      metric(failed, label: "failed")
    }
  }

  @ViewBuilder
  private var updateLabel: some View {
    if let lastCheck {
      Text("updated \(lastCheck, style: .relative) ago")
        .lineLimit(1)
        .minimumScaleFactor(0.8)
    } else {
      Text("updated —")
    }
  }

  private func metric(_ count: String, label: String) -> some View {
    HStack(alignment: .firstTextBaseline, spacing: 3) {
      Text(label).font(.caption2).fixedSize()
      Text(count)
        .font(.title2.weight(.medium))
        .lineLimit(1)
        .minimumScaleFactor(0.5)
    }
  }
}

extension ContentView {
  init(snapshot: PKSnapshot?, at date: Date = Date()) {
    lastCheck = snapshot?.lastCheckAt
    guard let snapshot, snapshot.isCurrent(at: date) else {
      serviceStatus = "Unknown"
      tracked = "—"
      killed = "—"
      failed = "—"
      return
    }
    serviceStatus = snapshot.serviceState.capitalized
    tracked = snapshot.trackedProcesses?.formatted() ?? "—"
    killed = snapshot.outcomes?.processes.successes.formatted() ?? "—"
    failed = snapshot.outcomes?.processes.failures.formatted() ?? "—"
  }
}

#Preview("Active · Sample") {
  ContentView(
    serviceStatus: "Active", lastCheck: .now.addingTimeInterval(-120), tracked: "12",
    killed: "1", failed: "1"
  )
  .padding()
  .frame(width: 164, height: 164)
}

#Preview("Inactive · Sample") {
  ContentView(
    serviceStatus: "Inactive", lastCheck: .now.addingTimeInterval(-86_400), tracked: "0",
    killed: "12", failed: "0"
  )
  .padding()
  .frame(width: 164, height: 164)
}

#Preview("Unknown · Sample") {
  ContentView(serviceStatus: "Unknown", lastCheck: nil, tracked: "—", killed: "—", failed: "—")
    .padding()
    .frame(width: 164, height: 164)
}
