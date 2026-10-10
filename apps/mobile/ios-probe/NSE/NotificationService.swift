// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import Foundation
import UserNotifications

/// The Notification Service Extension of the memory probe app. For every
/// push with "mutable-content": 1, it runs the Go memory probe in the
/// extension's process, logs the report and writes a summary in the
/// notification. A push may carry its own probe configuration as a JSON
/// string under the key "probe" (memprobe.Config); data_dir is always the
/// extension's own.
final class NotificationService: UNNotificationServiceExtension {
    private var contentHandler: ((UNNotificationContent) -> Void)?
    private var content: UNMutableNotificationContent?

    override func didReceive(
        _ request: UNNotificationRequest,
        withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void
    ) {
        self.contentHandler = contentHandler
        let content = (request.content.mutableCopy() as? UNMutableNotificationContent) ?? UNMutableNotificationContent()
        self.content = content

        var config = Probe.defaultConfig(dataDir: Probe.dataDir())
        if let custom = request.content.userInfo["probe"] as? String,
           var object = (try? JSONSerialization.jsonObject(with: Data(custom.utf8))) as? [String: Any] {
            object["data_dir"] = Probe.dataDir()
            config = Probe.json(object) ?? config
        }
        ProbeLog.nse.log("probe started")
        let report = Probe.run(config: config)
        ProbeLog.report(report, from: "nse", logger: ProbeLog.nse)
        content.body = Self.summary(of: report)
        contentHandler(content)
    }

    override func serviceExtensionTimeWillExpire() {
        ProbeLog.nse.log("probe timed out")
        if let contentHandler, let content {
            content.body = "Memory probe: timed out"
            contentHandler(content)
        }
    }

    /// The peak footprint of the run in MiB, or the error.
    static func summary(of report: String) -> String {
        guard let object = (try? JSONSerialization.jsonObject(with: Data(report.utf8))) as? [String: Any] else {
            return "Memory probe: unreadable report"
        }
        if let error = object["error"] as? String {
            return "Memory probe failed: \(error)"
        }
        let steps = object["steps"] as? [[String: Any]] ?? []
        let peak = steps.compactMap { ($0["peak_footprint_bytes"] as? NSNumber)?.doubleValue }.max() ?? -1
        return String(format: "Memory probe: peak %.1f MiB", peak / 1_048_576)
    }
}
