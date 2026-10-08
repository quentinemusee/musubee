// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import app.musubee.gomobile.mobile.Core
import app.musubee.gomobile.mobile.Mobile

/**
 * One running core, through the classes gomobile bind generates from
 * core/mobile. Same API as the class of the "jni" flavor; see
 * docs/ADR/0011-core-on-android.md.
 */
class CoreLibrary private constructor(private val core: Core) : AutoCloseable {
    companion object {
        /** Starts a core; throws if it cannot start. */
        fun open(config: ByteArray): CoreLibrary = CoreLibrary(Mobile.open(config))

        /** The name of the binding, for measurements. */
        const val BINDING = "gomobile"
    }

    /** Runs one JSON request and returns the JSON response. */
    fun call(request: ByteArray): ByteArray = core.call(request)

    /** Waits up to [timeoutMs] for the next event; null on timeout. */
    fun nextEvent(timeoutMs: Int): ByteArray? = core.nextEvent(timeoutMs)?.takeIf { it.isNotEmpty() }

    /** Stops the core; [nextEvent] then returns the closed event. */
    override fun close() = core.close()
}
