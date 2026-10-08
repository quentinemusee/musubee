// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import android.content.Intent
import android.os.Debug
import android.os.Process
import android.os.SystemClock
import android.util.Log
import androidx.core.content.ContextCompat
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.rule.ServiceTestRule
import org.json.JSONObject
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

/**
 * Long run of the foreground service (docs/ADR/0011): one message and its
 * echo every 30 seconds, and every minute one JSON sample of memory and CPU
 * time under the log tag "MusubeeSoak". Skipped unless the instrumentation
 * argument musubee.soak.minutes is set:
 *
 *     adb shell am instrument -w -e musubee.soak.minutes 60 \
 *       -e class app.musubee.core.SoakTest app.musubee.test/androidx.test.runner.AndroidJUnitRunner
 */
@RunWith(AndroidJUnit4::class)
class SoakTest {
    @get:Rule
    val serviceRule = ServiceTestRule.withTimeout(60, TimeUnit.SECONDS)

    private val events = LinkedBlockingQueue<JSONObject>()
    private var nextId = 0

    private fun request(service: CoreService, command: String, params: JSONObject? = null): JSONObject {
        val req = JSONObject().put("id", ++nextId).put("command", command)
        if (params != null) req.put("params", params)
        val resp = JSONObject(String(service.call(req.toString().toByteArray()), Charsets.UTF_8))
        assertTrue("$command: ${resp.optString("error")}", !resp.has("error"))
        return resp.getJSONObject("result")
    }

    private fun waitEcho(text: String) {
        val deadline = SystemClock.elapsedRealtime() + ECHO_TIMEOUT_MILLIS
        while (true) {
            val remaining = deadline - SystemClock.elapsedRealtime()
            assertTrue("no echo of $text within $ECHO_TIMEOUT_MILLIS ms", remaining > 0)
            val event = events.poll(remaining, TimeUnit.MILLISECONDS) ?: continue
            if (event.optString("type") == "message" && !event.optBoolean("from_me") &&
                event.optString("body").contains(text)
            ) {
                return
            }
        }
    }

    private fun sample(service: CoreService, minute: Int, messages: Int): JSONObject {
        val stats = request(service, "stats")
        return JSONObject()
            .put("minute", minute)
            .put("messages", messages)
            .put("pss_kib", Debug.getPss())
            .put("native_heap_kib", Debug.getNativeHeapAllocatedSize() / 1024)
            .put("go_heap_kib", stats.getLong("heap_alloc_bytes") / 1024)
            .put("go_heap_sys_kib", stats.getLong("heap_sys_bytes") / 1024)
            .put("goroutines", stats.getInt("goroutines"))
            .put("cpu_ms", Process.getElapsedCpuTime())
    }

    @Test
    fun serviceRunsForAnHour() {
        val minutes = InstrumentationRegistry.getArguments().getString(MINUTES_ARGUMENT)?.toIntOrNull() ?: 0
        assumeTrue("set -e $MINUTES_ARGUMENT N to run the soak test", minutes > 0)

        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val intent = Intent(context, CoreService::class.java)
        ContextCompat.startForegroundService(context, intent)
        val service = (serviceRule.bindService(intent) as CoreService.LocalBinder).service
        val listener: (ByteArray) -> Unit = { events.add(JSONObject(String(it, Charsets.UTF_8))) }
        service.addListener(listener)
        try {
            val rooms = request(service, "login", JSONObject().put("username", "alice")).getJSONArray("rooms")
            val roomId = (0 until rooms.length()).map { rooms.getJSONObject(it) }
                .first { it.getString("name") == "Instant Echo" }.getString("room_id")
            val start = SystemClock.elapsedRealtime()
            var messages = 0
            Log.i(SOAK_TAG, sample(service, 0, 0).toString())
            for (minute in 1..minutes) {
                repeat(MESSAGES_PER_MINUTE) {
                    val text = "soak ${++messages}"
                    request(service, "send", JSONObject().put("room_id", roomId).put("text", text))
                    waitEcho(text)
                    // Next message on the 30-second grid, whatever the echo took.
                    val next = start + messages * MESSAGE_INTERVAL_MILLIS
                    SystemClock.sleep((next - SystemClock.elapsedRealtime()).coerceAtLeast(0))
                }
                Log.i(SOAK_TAG, sample(service, minute, messages).toString())
            }
        } finally {
            service.removeListener(listener)
            context.stopService(intent)
        }
    }

    companion object {
        private const val SOAK_TAG = "MusubeeSoak"
        private const val MINUTES_ARGUMENT = "musubee.soak.minutes"
        private const val MESSAGES_PER_MINUTE = 2
        private const val MESSAGE_INTERVAL_MILLIS = 30_000L
        private const val ECHO_TIMEOUT_MILLIS = 15_000L
    }
}
