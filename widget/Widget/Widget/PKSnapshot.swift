import Foundation

struct PKSnapshot: Decodable {
  struct Counts: Decodable {
    let successes: Int
    let failures: Int

    var isValid: Bool { successes >= 0 && failures >= 0 }
  }

  struct Outcomes: Decodable {
    let processes: Counts
    let containers: Counts
  }

  let schemaVersion: Int
  let generatedAt: Date
  let lastCheckAt: Date?
  let serviceState: String
  let trackedProcesses: Int?
  let windowStart: Date
  let windowEnd: Date
  let status: String
  let coverage: String
  let outcomes: Outcomes?

  static let maximumBytes = 65_536
  static let freshnessInterval: TimeInterval = 900

  func isCurrent(at date: Date) -> Bool {
    let age = date.timeIntervalSince(generatedAt)
    return age >= 0 && age < Self.freshnessInterval
  }

  func refreshDate(after date: Date) -> Date {
    guard isCurrent(at: date) else {
      return date.addingTimeInterval(Self.freshnessInterval)
    }
    return generatedAt.addingTimeInterval(Self.freshnessInterval)
  }

  static func decode(_ data: Data) throws -> PKSnapshot {
    guard data.count <= maximumBytes else { throw SnapshotError.oversized }
    let decoder = JSONDecoder()
    decoder.keyDecodingStrategy = .convertFromSnakeCase
    decoder.dateDecodingStrategy = .custom { value in
      let timestamp = try value.singleValueContainer().decode(String.self)
      return try Date.ISO8601FormatStyle().parse(timestamp)
    }
    let snapshot = try decoder.decode(PKSnapshot.self, from: data)
    try snapshot.validate()
    return snapshot
  }

  private func validate() throws {
    guard schemaVersion == 1, coverage == "retained" else { throw SnapshotError.unsupported }
    guard ["active", "inactive", "unknown"].contains(serviceState) else {
      throw SnapshotError.invalidState
    }
    if let trackedProcesses, trackedProcesses < 0 { throw SnapshotError.invalidCounts }
    try validateDates()
    try validateOutcomes()
  }

  private func validateDates() throws {
    let windowDuration = windowEnd.timeIntervalSince(windowStart)
    let isDay = abs(windowDuration - 86_400) < 0.001
    guard windowEnd == generatedAt, isDay else { throw SnapshotError.invalidWindow }
    if let lastCheckAt, lastCheckAt > generatedAt { throw SnapshotError.invalidDate }
  }

  private func validateOutcomes() throws {
    if status == "unavailable", outcomes == nil { return }
    guard status == "ready", let outcomes else { throw SnapshotError.invalidCounts }
    guard outcomes.processes.isValid, outcomes.containers.isValid else {
      throw SnapshotError.invalidCounts
    }
  }
}

enum SnapshotError: String, LocalizedError {
  case oversized = "The pk report exceeds 64 KiB."
  case unsupported = "The pk report uses an unsupported schema or coverage."
  case invalidState = "The pk service state is invalid."
  case invalidDate = "The pk report contains an invalid timestamp."
  case invalidWindow = "The pk report must cover the last 24 hours."
  case invalidCounts = "The pk report contains invalid process or cleanup counts."
  case unavailableGroup = "The App Group is unavailable. Check signing for the app and extension."
  case staleWrite = "A newer pk report is already stored."

  var errorDescription: String? { rawValue }
}
