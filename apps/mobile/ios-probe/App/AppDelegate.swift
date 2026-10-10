// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import UIKit
import UserNotifications
import os

/// The memory probe app (T1.7). On launch it asks for provisional
/// notification authorization, which shows no prompt, so that pushes reach
/// its Notification Service Extension. Launched with the argument --probe,
/// it also runs the probe in its own process, to compare an app's memory
/// with the extension's.
@main
final class AppDelegate: UIResponder, UIApplicationDelegate {
    var window: UIWindow?

    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
    ) -> Bool {
        let label = UILabel()
        label.text = "Musubee memory probe"
        label.textAlignment = .center
        label.backgroundColor = .systemBackground
        let controller = UIViewController()
        controller.view = label
        window = UIWindow(frame: UIScreen.main.bounds)
        window?.rootViewController = controller
        window?.makeKeyAndVisible()

        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .provisional]) { granted, error in
            ProbeLog.app.log("notification authorization: granted=\(granted, privacy: .public) error=\(String(describing: error), privacy: .public)")
        }
        application.registerForRemoteNotifications()
        if ProcessInfo.processInfo.arguments.contains("--probe") {
            DispatchQueue.global(qos: .userInitiated).async {
                let report = Probe.run(config: Probe.defaultConfig(dataDir: Probe.dataDir()))
                ProbeLog.report(report, from: "app", logger: ProbeLog.app)
            }
        }
        return true
    }

    /// Logs the APNs device token: on an iPhone, the tester sends the probe
    /// push to it (README).
    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        let token = deviceToken.map { String(format: "%02x", $0) }.joined()
        ProbeLog.app.log("APNs device token: \(token, privacy: .public)")
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        ProbeLog.app.log("APNs registration failed: \(String(describing: error), privacy: .public)")
    }
}
