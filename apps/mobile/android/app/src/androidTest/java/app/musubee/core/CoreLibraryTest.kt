// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import android.os.Debug
import android.os.SystemClock
import android.util.Log
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONArray
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File

/**
 * Drives the core through its JNI binding on a device or emulator: requests,
 * the echo round trip and close. It also logs measurements under the tag
 * "MusubeeBench" for docs/ADR/0011 (adb logcat -s MusubeeBench).
 */
@RunWith(AndroidJUnit4::class)
class CoreLibraryTest {
    private lateinit var dataDir: File
    private lateinit var core: CoreLibrary
    private var nextId = 0

    @Before
    fun open() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        dataDir = File(context.cacheDir, "core-test-${System.nanoTime()}")
        val config = JSONObject().put("data_dir", dataDir.absolutePath).put("echo_delay_ms", 200)
        val start = SystemClock.elapsedRealtimeNanos()
        core = CoreLibrary.open(config.toString().toByteArray())
        Log.i(BENCH_TAG, JSONObject().put("open_ms", (SystemClock.elapsedRealtimeNanos() - start) / 1e6).toString())
    }

    @After
    fun close() {
        core.close()
        dataDir.deleteRecursively()
    }

    private fun request(command: String, params: JSONObject? = null): JSONObject {
        val req = JSONObject().put("id", ++nextId).put("command", command)
        if (params != null) req.put("params", params)
        val resp = JSONObject(String(core.call(req.toString().toByteArray()), Charsets.UTF_8))
        assertEquals("response id", nextId, resp.getInt("id"))
        if (resp.has("error")) fail("$command: ${resp.getJSONObject("error")}")
        return resp
    }

    private fun waitEvent(what: String, match: (JSONObject) -> Boolean): JSONObject {
        val deadline = SystemClock.elapsedRealtime() + EVENT_TIMEOUT_MILLIS
        while (SystemClock.elapsedRealtime() < deadline) {
            val data = core.nextEvent((deadline - SystemClock.elapsedRealtime()).toInt().coerceAtLeast(1)) ?: continue
            val event = JSONObject(String(data, Charsets.UTF_8))
            if (match(event)) return event
        }
        fail("no $what within $EVENT_TIMEOUT_MILLIS ms")
        throw AssertionError()
    }

    private fun pssKiB(): Long = Debug.getPss()

    /** Adds the echo account "alice"; returns the ID of the Instant Echo conversation. */
    private fun login(): String {
        val step = request("login.start", JSONObject().put("network_id", "echo").put("flow_id", "username"))
            .getJSONObject("result")
        val field = step.getJSONArray("fields").getJSONObject(0).getString("field_id")
        val done = request(
            "login.submit",
            JSONObject().put("process_id", step.getString("process_id")).put("values", JSONObject().put(field, "alice")),
        ).getJSONObject("result")
        assertEquals("complete", done.getString("type"))
        waitEvent("connected account") {
            it.optString("type") == "account.updated" &&
                it.getJSONObject("data").getJSONObject("account").optString("state") == "connected"
        }
        val event = waitEvent("Instant Echo conversation") {
            it.optString("type") == "conversation.updated" &&
                it.getJSONObject("data").getJSONObject("conversation").optString("name") == "Instant Echo"
        }
        return event.getJSONObject("data").getJSONObject("conversation").getString("conversation_id")
    }

    private fun sendAndWaitEcho(conversationId: String, text: String) {
        request("messages.send", JSONObject().put("conversation_id", conversationId).put("text", text))
        waitEvent("echo of $text") { isEcho(it, text) }
    }

    @Test
    fun pingReturnsThePayload() {
        val payload = "hello 👋"
        val result = request("debug.ping", JSONObject().put("payload", payload)).getJSONObject("result")
        assertEquals(payload, result.getString("payload"))
    }

    @Test
    fun invalidRequestGetsAnError() {
        val resp = JSONObject(String(core.call("{not json".toByteArray()), Charsets.UTF_8))
        assertEquals(resp.toString(), "invalid_request", resp.getJSONObject("error").getString("code"))
    }

    @Test
    fun helloReportsTheApiVersion() {
        val result = request("core.hello").getJSONObject("result")
        assertTrue(result.toString(), result.getString("api_version").startsWith("1."))
    }

    @Test
    fun sendAndReceiveAnEcho() {
        sendAndWaitEcho(login(), "hello from Android")
    }

    @Test
    fun closeEndsTheEventStream() {
        login()
        core.close()
        waitEvent("closed event") { it.optString("type") == "core.closed" }
        assertNotNull(core.nextEvent(1))
        val resp = JSONObject(String(core.call("""{"id":9,"command":"core.hello"}""".toByteArray()), Charsets.UTF_8))
        assertEquals(resp.toString(), "closed", resp.getJSONObject("error").getString("code"))
    }

    /**
     * Call cost and memory: the numbers of docs/ADR/0011, logged as one JSON
     * line. Three ping loops: if the binding leaked the 1 KiB responses, the
     * memory would grow by about 20 MiB per loop; a steady state shows as
     * little growth between the second and the third loop.
     */
    @Test
    fun measurements() {
        val result = JSONObject().put("pss_open_kib", pssKiB())
        val ping = JSONObject().put("id", 1).put("command", "debug.ping")
            .put("params", JSONObject().put("payload", "x".repeat(1024))).toString().toByteArray()
        repeat(PING_WARMUP) { core.call(ping) }
        val loopUs = JSONArray()
        val loopPss = JSONArray().put(pssKiB())
        repeat(PING_LOOPS) {
            val start = SystemClock.elapsedRealtimeNanos()
            repeat(PING_ITERATIONS) { core.call(ping) }
            loopUs.put((SystemClock.elapsedRealtimeNanos() - start) / 1e3 / PING_ITERATIONS)
            loopPss.put(pssKiB())
        }
        result.put("ping_us", loopUs).put("ping_pss_kib", loopPss)

        val conversationId = login()
        repeat(ROUNDTRIP_WARMUP) { sendAndWaitEcho(conversationId, "warm-up $it") }
        var total = 0L
        var slowest = 0L
        repeat(ROUNDTRIP_ITERATIONS) {
            val start = SystemClock.elapsedRealtimeNanos()
            sendAndWaitEcho(conversationId, "message $it")
            val elapsed = SystemClock.elapsedRealtimeNanos() - start
            total += elapsed
            slowest = maxOf(slowest, elapsed)
        }
        result.put("roundtrip_mean_ms", total / 1e6 / ROUNDTRIP_ITERATIONS).put("roundtrip_max_ms", slowest / 1e6)
        val stats = request("debug.stats").getJSONObject("result")
        result.put("pss_end_kib", pssKiB())
            .put("native_heap_kib", Debug.getNativeHeapAllocatedSize() / 1024)
            .put("go_heap_kib", stats.getLong("heap_alloc_bytes") / 1024)
            .put("go_heap_sys_kib", stats.getLong("heap_sys_bytes") / 1024)
            .put("goroutines", stats.getInt("goroutines"))
        Log.i(BENCH_TAG, result.toString())
    }

    companion object {
        private const val BENCH_TAG = "MusubeeBench"
        private const val EVENT_TIMEOUT_MILLIS = 15_000L
        private const val PING_WARMUP = 1_000
        private const val PING_ITERATIONS = 20_000
        private const val PING_LOOPS = 3
        private const val ROUNDTRIP_WARMUP = 10
        private const val ROUNDTRIP_ITERATIONS = 100
    }
}
