import Darwin
import Foundation

struct SnapshotStore {
  let directory: URL

  init() throws {
    guard let group = Bundle.main.object(forInfoDictionaryKey: "PKAppGroupIdentifier") as? String,
      let directory = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group)
    else { throw SnapshotError.unavailableGroup }
    self.directory = directory
  }

  init(directory: URL) {
    self.directory = directory
  }

  func read() throws -> PKSnapshot {
    let file = try FileHandle(forReadingFrom: reportURL)
    defer { try? file.close() }
    let data = try file.read(upToCount: PKSnapshot.maximumBytes + 1) ?? Data()
    return try PKSnapshot.decode(data)
  }

  func write(_ data: Data) throws {
    let snapshot = try PKSnapshot.decode(data)
    try withLock {
      if let previous = try? read(), previous.generatedAt > snapshot.generatedAt {
        throw SnapshotError.staleWrite
      }
      try data.write(to: reportURL, options: .atomic)
      try FileManager.default.setAttributes(
        [.posixPermissions: 0o600], ofItemAtPath: reportURL.path)
    }
  }

  private var reportURL: URL { directory.appendingPathComponent("pk-summary.json") }

  private func withLock(_ operation: () throws -> Void) throws {
    let path = directory.appendingPathComponent("pk-summary.lock").path
    let descriptor = open(path, O_CREAT | O_RDWR | O_NOFOLLOW, S_IRUSR | S_IWUSR)
    guard descriptor >= 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
    defer { close(descriptor) }
    guard flock(descriptor, LOCK_EX | LOCK_NB) == 0 else {
      throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
    }
    defer { flock(descriptor, LOCK_UN) }
    try operation()
  }
}
