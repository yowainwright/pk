import Foundation
import XCTest

final class PKSnapshotTests: XCTestCase {
  func testReadyContractFromGo() throws {
    let snapshot = try PKSnapshot.decode(fixture("ready"))
    XCTAssertEqual(snapshot.serviceState, "unknown")
    XCTAssertEqual(snapshot.outcomes?.processes.successes, 1)
    XCTAssertEqual(snapshot.outcomes?.processes.failures, 1)
    XCTAssertNotNil(snapshot.lastCheckAt)
    XCTAssertEqual(snapshot.trackedProcesses, 12)
  }

  func testZeroCountsAreDifferentFromMissingData() throws {
    let snapshot = try PKSnapshot.decode(fixture("zero"))
    XCTAssertEqual(snapshot.outcomes?.processes.successes, 0)
    XCTAssertEqual(snapshot.outcomes?.processes.failures, 0)
  }

  func testMissingCheckRemainsMissing() throws {
    let snapshot = try PKSnapshot.decode(fixture("no-check"))
    XCTAssertNil(snapshot.lastCheckAt)
  }

  func testZeroTrackedProcessesAreValid() throws {
    let data = try replacing("\"tracked_processes\": 12", with: "\"tracked_processes\": 0")
    XCTAssertEqual(try PKSnapshot.decode(data).trackedProcesses, 0)
  }

  func testOlderReportsHaveNoTrackedCount() throws {
    let data = try replacing("\"tracked_processes\": 12,", with: "")
    XCTAssertNil(try PKSnapshot.decode(data).trackedProcesses)
  }

  func testNegativeTrackedProcessesAreRejected() throws {
    let data = try replacing("\"tracked_processes\": 12", with: "\"tracked_processes\": -1")
    XCTAssertThrowsError(try PKSnapshot.decode(data))
  }

  func testUnavailableCountsRemainMissing() throws {
    let snapshot = try PKSnapshot.decode(fixture("unavailable"))
    XCTAssertNil(snapshot.outcomes)
    XCTAssertEqual(snapshot.status, "unavailable")
  }

  func testInvalidWindowIsRejected() throws {
    let data = try replacing("2026-10-17T19:30:00Z", with: "2026-10-18T19:30:00Z")
    XCTAssertThrowsError(try PKSnapshot.decode(data))
  }

  func testFutureCheckIsRejected() throws {
    let data = try replacing("19:29:58Z", with: "19:30:01Z")
    XCTAssertThrowsError(try PKSnapshot.decode(data))
  }

  func testFractionalTimestampsFromGo() throws {
    let data = try replacing("19:30:00Z", with: "19:30:00.123456789Z")
    let snapshot = try PKSnapshot.decode(data)
    let whole = try PKSnapshot.decode(fixture("ready"))
    let fraction = snapshot.generatedAt.timeIntervalSince(whole.generatedAt)
    XCTAssertEqual(fraction, 0.123456789, accuracy: 0.000001)
  }

  func testUnknownSchemaIsRejected() throws {
    let data = try replacing("\"schema_version\": 1", with: "\"schema_version\": 2")
    XCTAssertThrowsError(try PKSnapshot.decode(data))
  }

  func testNegativeCountsAreRejected() throws {
    let data = try replacing("\"successes\": 1", with: "\"successes\": -1")
    XCTAssertThrowsError(try PKSnapshot.decode(data))
  }

  func testIncompleteReadyReportIsRejected() throws {
    let data = try replacing("\"processes\"", with: "\"missing_processes\"")
    XCTAssertThrowsError(try PKSnapshot.decode(data))
  }

  func testUnsupportedServiceStateIsRejected() throws {
    let data = try replacing(
      "\"service_state\": \"unknown\"", with: "\"service_state\": \"running\"")
    XCTAssertThrowsError(try PKSnapshot.decode(data))
  }

  func testTrailingObjectsAndOversizedReportsAreRejected() throws {
    var data = try fixture("ready")
    data.append(Data("{}".utf8))
    XCTAssertThrowsError(try PKSnapshot.decode(data))
    XCTAssertThrowsError(try PKSnapshot.decode(Data(repeating: 32, count: 65_537)))
  }

  func testStaleAndFutureReportsAreNotCurrent() throws {
    let snapshot = try PKSnapshot.decode(fixture("ready"))
    XCTAssertTrue(snapshot.isCurrent(at: snapshot.generatedAt))
    XCTAssertFalse(snapshot.isCurrent(at: snapshot.generatedAt.addingTimeInterval(900)))
    XCTAssertFalse(snapshot.isCurrent(at: snapshot.generatedAt.addingTimeInterval(-1)))
  }

  func testOldReportExpiresAtItsOriginalDeadline() throws {
    let snapshot = try PKSnapshot.decode(fixture("ready"))
    let now = snapshot.generatedAt.addingTimeInterval(840)
    let refreshAt = snapshot.refreshDate(after: now)
    XCTAssertEqual(refreshAt.timeIntervalSince(now), 60)
    XCTAssertFalse(snapshot.isCurrent(at: refreshAt))
  }

  func testExpiredReportSchedulesALaterRetry() throws {
    let snapshot = try PKSnapshot.decode(fixture("ready"))
    let now = snapshot.generatedAt.addingTimeInterval(900)
    XCTAssertEqual(snapshot.refreshDate(after: now).timeIntervalSince(now), 900)
    XCTAssertFalse(snapshot.isCurrent(at: now))
  }

  func testInvalidWritePreservesValidReport() throws {
    let directory = try temporaryDirectory()
    defer { try? FileManager.default.removeItem(at: directory) }
    let store = SnapshotStore(directory: directory)
    try store.write(fixture("ready"))
    XCTAssertThrowsError(try store.write(Data("invalid".utf8)))
    XCTAssertEqual(try store.read().outcomes?.processes.successes, 1)
  }

  func testOlderPublisherCannotOverwriteNewerReport() throws {
    let directory = try temporaryDirectory()
    defer { try? FileManager.default.removeItem(at: directory) }
    let store = SnapshotStore(directory: directory)
    let current = try fixture("ready")
    try store.write(current)
    let older = try replacing("19:30:00Z", with: "19:29:59Z")
    XCTAssertThrowsError(try store.write(older)) { error in
      XCTAssertEqual(error as? SnapshotError, .staleWrite)
    }
    XCTAssertEqual(try store.read().generatedAt, try PKSnapshot.decode(current).generatedAt)
  }

  private func fixture(_ name: String) throws -> Data {
    let directory = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
    let root = directory.appendingPathComponent("../../..")
    let file = root.appendingPathComponent("tests/fixtures/widget/\(name).json")
    return try Data(contentsOf: file)
  }

  private func replacing(_ original: String, with replacement: String) throws -> Data {
    let text = String(decoding: try fixture("ready"), as: UTF8.self)
    return Data(text.replacingOccurrences(of: original, with: replacement).utf8)
  }

  private func temporaryDirectory() throws -> URL {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    return directory
  }
}
