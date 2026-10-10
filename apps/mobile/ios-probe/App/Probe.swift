// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import Foundation
import MusubeeMemProbe
import os

/// Calls the Go memory probe (core/cmd/memprobe, memprobe.h).
enum Probe {
    /// A data directory for the core, inside the process's own container.
    static func dataDir() -> String {
        FileManager.default.temporaryDirectory.appendingPathComponent("memprobe", isDirectory: true).path
    }

    /// The configuration of a full run: core and encryption steps.
    static func defaultConfig(dataDir: String) -> String {
        let config: [String: Any] = ["data_dir": dataDir, "core": true, "crypto": true, "messages": 20]
        return json(config) ?? "{}"
    }

    static func json(_ object: [String: Any]) -> String? {
        guard let data = try? JSONSerialization.data(withJSONObject: object) else { return nil }
        return String(decoding: data, as: UTF8.self)
    }

    /// Runs the probe; blocks for its duration. Returns the JSON report.
    static func run(config: String) -> String {
        guard let report = musubee_memprobe_run(config) else {
            return #"{"error":"the probe returned nothing"}"#
        }
        defer { free(report) }
        return String(cString: report)
    }
}

/// Unified logging of the probe. The CI reads the reports back with
/// "log show" (run-simulator.sh).
enum ProbeLog {
    static let subsystem = "app.musubee.memoryprobe"
    static let app = Logger(subsystem: subsystem, category: "app")
    static let nse = Logger(subsystem: subsystem, category: "nse")

    /// Logs a JSON report in chunks, since a log message is truncated after
    /// about 1 KiB. Each line reads "report SOURCE ID INDEX/COUNT CHUNK";
    /// run-simulator.sh puts the chunks back together.
    static func report(_ json: String, from source: String, logger: Logger) {
        let id = String(UUID().uuidString.prefix(8))
        let chars = Array(json)
        let chunkSize = 600
        let count = max(1, (chars.count + chunkSize - 1) / chunkSize)
        for index in 0..<count {
            let start = index * chunkSize
            let chunk = String(chars[start..<min(start + chunkSize, chars.count)])
            logger.log("report \(source, privacy: .public) \(id, privacy: .public) \(index + 1, privacy: .public)/\(count, privacy: .public) \(chunk, privacy: .public)")
        }
    }
}
