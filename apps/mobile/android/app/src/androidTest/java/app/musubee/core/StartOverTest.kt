// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import android.content.Intent
import androidx.core.content.ContextCompat
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.rule.ServiceTestRule
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File
import java.security.KeyStore
import java.time.Instant
import java.util.concurrent.ExecutionException

/**
 * Starting over once the core's key is lost (docs/ADR/0031): the unreadable
 * data and key are renamed aside, never deleted, and a new core starts.
 *
 * serviceStartsOverOnceTheKeyIsLost uses the app's own core, as
 * CoreServiceTest does: it sets the app's data aside, then removes what it
 * set aside. Run it on a test device or emulator only.
 */
@RunWith(AndroidJUnit4::class)
class StartOverTest {
    @get:Rule
    val serviceRule = ServiceTestRule()

    private val context = InstrumentationRegistry.getInstrumentation().targetContext

    @Test
    fun setAsideRenamesAndDeletesNothing() {
        val root = File(context.cacheDir, "start-over-test-${System.nanoTime()}").apply { mkdirs() }
        try {
            val now = Instant.parse("2026-10-13T21:05:09.123Z")
            for (expected in listOf("-unreadable-20261013T210509Z", "-unreadable-20261013T210509Z-2")) {
                val data = File(root, "core").apply { mkdirs() }
                File(data, "core.db").writeText(expected)
                val key = File(root, "core-key").apply { writeText(expected) }
                assertEquals(File(root, "core$expected"), StartOver.setAside(data, key, now))
                assertFalse(data.exists())
                assertFalse(key.exists())
                assertEquals(expected, File(root, "core$expected/core.db").readText())
                assertEquals(expected, File(root, "core-key$expected").readText())
            }
            assertNull("nothing to set aside", StartOver.setAside(File(root, "core"), File(root, "core-key"), now))
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun serviceStartsOverOnceTheKeyIsLost() {
        val intent = Intent(context, CoreService::class.java)
        val before = asideFiles()
        // The app's key exists, but its Keystore key is gone.
        CoreKey.forApp(context).load().fill(0)
        KeyStore.getInstance("AndroidKeyStore").apply { load(null) }.deleteEntry("app.musubee.core.key-wrap")
        ContextCompat.startForegroundService(context, intent)
        try {
            val service = (serviceRule.bindService(intent) as CoreService.LocalBinder).service
            try {
                hello(service)
                fail("the core started without its key")
            } catch (e: ExecutionException) {
                assertTrue("not a lost key: ${e.cause}", e.cause is KeyLostException)
            }

            service.startOver()
            assertTrue("the new core did not answer", hello(service).has("result"))
            val aside = asideFiles() - before
            assertEquals("set aside: $aside", setOf("core", "core-key"), aside.map { it.name.substringBefore("-unreadable-") }.toSet())

            // Once the core works, starting over is refused.
            try {
                service.startOver()
                fail("started over with a working core")
            } catch (e: IllegalStateException) {
                assertTrue(e.message.orEmpty(), e.message.orEmpty().contains("not lost"))
            }
        } finally {
            context.stopService(intent)
            (asideFiles() - before).forEach { it.deleteRecursively() }
        }
    }

    private fun asideFiles(): Set<File> =
        listOf(context.filesDir, context.noBackupFilesDir)
            .flatMap { dir -> dir.listFiles()?.toList().orEmpty() }
            .filter { it.name.contains("-unreadable-") }
            .toSet()

    private fun hello(service: CoreService): JSONObject {
        val request = JSONObject().put("id", 1).put("command", "core.hello")
        return JSONObject(String(service.call(request.toString().toByteArray()), Charsets.UTF_8))
    }
}
