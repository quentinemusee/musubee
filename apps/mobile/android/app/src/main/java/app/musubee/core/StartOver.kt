// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import java.io.File
import java.io.IOException
import java.time.Instant
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter

/**
 * Starting over once the core's key is lost (docs/ADR/0031). Nothing is
 * deleted: the core's data directory and its key file are renamed next to
 * where they were, with the suffix `-unreadable-<UTC time>`, and the next
 * core starts empty with a new key.
 */
object StartOver {
    private val STAMP = DateTimeFormatter.ofPattern("yyyyMMdd'T'HHmmss'Z'").withZone(ZoneOffset.UTC)

    /**
     * Renames [dataDir] and [keyFile] aside; either may be missing. Returns
     * the new name of the data directory, or null if there was none.
     */
    fun setAside(dataDir: File, keyFile: File, now: Instant = Instant.now()): File? {
        val stamp = STAMP.format(now)
        val data = if (dataDir.exists()) moveAside(dataDir, stamp) else null
        if (keyFile.exists()) {
            moveAside(keyFile, stamp)
        }
        return data
    }

    private fun moveAside(file: File, stamp: String): File {
        var target = File(file.parentFile, "${file.name}-unreadable-$stamp")
        var n = 2
        while (target.exists()) {
            target = File(file.parentFile, "${file.name}-unreadable-$stamp-$n")
            n++
        }
        if (!file.renameTo(target)) {
            throw IOException("could not move ${file.name} aside")
        }
        return target
    }
}
