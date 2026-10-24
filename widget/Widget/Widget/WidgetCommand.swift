import Foundation
import WidgetKit

enum WidgetCommand {
  static func run(_ arguments: [String]) throws {
    guard arguments.count == 1 else { throw CocoaError(.coderInvalidValue) }
    let store = try SnapshotStore()
    switch arguments[0] {
    case "--pk-publish":
      try store.write(readInput())
      WidgetCenter.shared.reloadTimelines(ofKind: "PKStats")
    case "--pk-status":
      let snapshot = try store.read()
      print("service: \(snapshot.serviceState)")
      print("generated_at: \(snapshot.generatedAt.formatted(.iso8601))")
    default:
      throw CocoaError(.coderInvalidValue)
    }
  }

  private static func readInput() throws -> Data {
    var data = Data()
    while data.count <= PKSnapshot.maximumBytes {
      let remaining = PKSnapshot.maximumBytes + 1 - data.count
      guard let chunk = try FileHandle.standardInput.read(upToCount: remaining), !chunk.isEmpty
      else {
        return data
      }
      data.append(chunk)
    }
    throw SnapshotError.oversized
  }
}
