// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

/**
 * One running core, through the C shared library and its JNI entry points
 * (core/ffi/jni_android.go). The "gomobile" flavor has the same class on top
 * of gomobile bind; see docs/ADR/0011-core-on-android.md.
 *
 * Requests, responses and events are JSON documents in UTF-8 (core/embedded).
 * Every method may block: never call them on the main thread.
 */
class CoreLibrary private constructor(private val handle: Long) : AutoCloseable {
    companion object {
        /** Starts a core; throws IllegalStateException if it cannot start. */
        fun open(config: ByteArray): CoreLibrary = CoreLibrary(NativeCore.open(config))

        /** The name of the binding, for measurements. */
        const val BINDING = "jni"
    }

    /** Runs one JSON request and returns the JSON response. */
    fun call(request: ByteArray): ByteArray = NativeCore.call(handle, request)

    /** Waits up to [timeoutMs] for the next event; null on timeout. */
    fun nextEvent(timeoutMs: Int): ByteArray? = NativeCore.nextEvent(handle, timeoutMs)

    /** Stops the core; later calls fail and [nextEvent] returns the closed event. */
    override fun close() = NativeCore.close(handle)
}

/** The JNI entry points of libmusubee.so; the names must match core/ffi. */
internal object NativeCore {
    init {
        System.loadLibrary("musubee")
    }

    @JvmStatic external fun open(config: ByteArray): Long

    @JvmStatic external fun call(handle: Long, request: ByteArray): ByteArray

    @JvmStatic external fun nextEvent(handle: Long, timeoutMs: Int): ByteArray?

    @JvmStatic external fun close(handle: Long)
}
