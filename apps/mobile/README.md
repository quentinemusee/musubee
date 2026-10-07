# apps/mobile — Capacitor shell (Android and iOS)

Mobile application: the UI from [`ui/`](../../ui/) inside Capacitor, plus a native plugin embedding the Go core. The native shells stay thin: Kotlin for Android, Swift for iOS.

**Status: empty.** Work planned in T1.3 (Android), T1.7 (iOS extension memory budget) then E3.3 (iOS). See [`docs/TASKS.md`](../../docs/TASKS.md).

## Planned layout

```
apps/mobile/
  android/   Gradle project: Capacitor plugin, foreground service, notifications
  ios/       Xcode project: Capacitor plugin + Notification Service Extension (NSE) in Swift
```

## Points of attention

- **iOS starts in "hosted bridge" mode.** The Notification Service Extension (NSE) is a separate process with very limited memory; a full Go runtime there is uncertain (measured in T1.7).
- **AGPL / App Store compatibility is unresolved**: to be validated by a lawyer before any iOS release.
- iOS builds require a Mac; push and the NSE on a real device require a paid Apple Developer account.

## Skills

`capacitor-*`, `debugging-capacitor`, `ios-android-logs`, `safe-area-handling`; iOS: `swift-concurrency`, `swift-testing`, `background-processing`, `push-notifications`, `debugging-instruments`, `ios-memgraph-analysis`, `app-store-review`; Android: `claude-android-ninja` (services, notifications, Gradle only).

## Headers

<!-- REUSE-IgnoreStart -->

Kotlin and Swift: `// SPDX-FileCopyrightText: …` and `// SPDX-License-Identifier: AGPL-3.0-or-later` at the top of every file; XML: in a `<!-- … -->` comment after the XML declaration.

<!-- REUSE-IgnoreEnd -->
