// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import android.database.sqlite.SQLiteDatabase
import android.os.SystemClock
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File
import java.security.KeyStore
import javax.crypto.SecretKey

/**
 * The core's master key on Android (docs/ADR/0019): kept wrapped by the
 * Android Keystore, and given to the core, which seals the accounts'
 * sessions with it. Each test uses its own file and Keystore alias, never
 * the app's.
 */
@RunWith(AndroidJUnit4::class)
class CoreKeyTest {
    private val context = InstrumentationRegistry.getInstrumentation().targetContext
    private lateinit var dir: File
    private lateinit var alias: String

    @Before
    fun setUp() {
        dir = File(context.cacheDir, "core-key-test-${System.nanoTime()}").apply { mkdirs() }
        alias = "app.musubee.test.key-wrap-${System.nanoTime()}"
    }

    @After
    fun tearDown() {
        keystore().deleteEntry(alias)
        dir.deleteRecursively()
    }

    private fun keystore(): KeyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }

    @Test
    fun keyIsStableAndWrapped() {
        val file = File(dir, "core-key")
        val key = CoreKey(file, alias).load()
        assertEquals(32, key.size)
        assertArrayEquals("a new instance reads the same key", key, CoreKey(file, alias).load())
        val stored = file.readBytes()
        assertFalse("the file holds the key in clear", stored.asList().windowed(key.size).any { it == key.asList() })
        // The Keystore key cannot be read out of the Keystore.
        val wrapping = keystore().getKey(alias, null) as SecretKey
        assertNull("the Keystore key is exportable", wrapping.encoded)
        assertFalse("two keys are equal", key.contentEquals(CoreKey(File(dir, "other"), alias).load()))
    }

    @Test
    fun lostKeystoreKeyFailsClosed() {
        val file = File(dir, "core-key")
        CoreKey(file, alias).load()
        keystore().deleteEntry(alias)
        try {
            CoreKey(file, alias).load()
            fail("the key loaded without its Keystore key")
        } catch (e: IllegalStateException) {
            // Expected.
        }
        assertTrue("the key file was replaced", file.exists())
    }

    @Test
    fun coreSealsTheSessions() {
        val dataDir = File(dir, "core")
        val key = CoreKey(File(dir, "core-key"), alias).load()
        val core = CoreLibrary.open(CoreService.coreConfig(dataDir, key))
        val account = try {
            loginEcho(core)
        } finally {
            core.close()
        }

        // Read the file once the core is closed: two copies of SQLite must
        // not share an open database in one process.
        SQLiteDatabase.openDatabase(File(dataDir, "core.db").path, null, SQLiteDatabase.OPEN_READONLY).use { db ->
            db.rawQuery("SELECT metadata FROM user_login", null).use { rows ->
                assertEquals(1, rows.count)
                while (rows.moveToNext()) {
                    assertTrue("a session is stored in clear", rows.getString(0).startsWith("musubee-sealed:v1:"))
                }
            }
        }

        // The same key reads the session back; another one is refused.
        CoreLibrary.open(CoreService.coreConfig(dataDir, key)).use { reopened ->
            val accounts = call(reopened, 1, "accounts.list", JSONObject()).getJSONArray("accounts")
            assertEquals(account, accounts.getJSONObject(0).getString("account_id"))
        }
        try {
            CoreLibrary.open(CoreService.coreConfig(dataDir, ByteArray(32))).close()
            fail("the core opened with another key")
        } catch (e: IllegalStateException) {
            assertTrue(e.message.orEmpty(), e.message.orEmpty().contains("another key"))
        }
    }

    private fun call(core: CoreLibrary, id: Int, command: String, params: JSONObject): JSONObject {
        val req = JSONObject().put("id", id).put("command", command).put("params", params)
        val resp = JSONObject(String(core.call(req.toString().toByteArray()), Charsets.UTF_8))
        assertFalse("$command: ${resp.optJSONObject("error")}", resp.has("error"))
        return resp.getJSONObject("result")
    }

    /** Adds the echo account "alice" and waits for it to connect; returns its ID. */
    private fun loginEcho(core: CoreLibrary): String {
        val step = call(core, 1, "login.start", JSONObject().put("network_id", "echo").put("flow_id", "username"))
        val field = step.getJSONArray("fields").getJSONObject(0).getString("field_id")
        val done = call(
            core,
            2,
            "login.submit",
            JSONObject().put("process_id", step.getString("process_id")).put("values", JSONObject().put(field, "alice")),
        )
        assertEquals("complete", done.getString("type"))
        val account = done.getString("account_id")
        val deadline = SystemClock.elapsedRealtime() + 30_000
        while (SystemClock.elapsedRealtime() < deadline) {
            val data = core.nextEvent(1_000) ?: continue
            val event = JSONObject(String(data, Charsets.UTF_8))
            if (event.optString("type") != "account.updated") continue
            val a = event.getJSONObject("data").getJSONObject("account")
            if (a.getString("account_id") == account && a.getString("state") == "connected") return account
        }
        fail("the echo account did not connect within 30 s")
        throw AssertionError()
    }
}
