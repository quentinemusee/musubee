// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

/**
 * The core's key is lost for good (docs/ADR/0031): its Keystore key is gone,
 * the Keystore cannot decrypt the key file, or the core's database was
 * written with another key. Waiting or restarting does not help; the app
 * offers to start over with a new key.
 *
 * The core's JNI layer throws it by name (core/ffi/jni_android.go): keep the
 * class name and its message constructor.
 */
class KeyLostException(message: String, cause: Throwable? = null) : IllegalStateException(message, cause)
