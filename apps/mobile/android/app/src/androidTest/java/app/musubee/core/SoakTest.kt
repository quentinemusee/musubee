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
import java.io.File
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

/**
 * Long run of the foreground service (docs/ADR/0011): one message and its
 * echo every 30 seconds of awake time, and about every minute one JSON
 * sample of memory and CPU time, logged under the tag "MusubeeSoak" and
 * appended to files/soak.jsonl in the app's data. Skipped unless the
 * instrumentation argument musubee.soak.minutes is set:
 *
 *     adb shell am instrument -e musubee.soak.minutes 60 \
 *       -e class app.musubee.core.SoakTest app.musubee.test/androidx.test.runner.AndroidJUnitRunner
 *     adb shell run-as app.musubee cat files/soak.jsonl
 *
 * Without -w, the run does not depend on the adb connection: the phone can
 * be unplugged to measure its battery. The test holds no wake lock, so a
 * phone with its screen off suspends between messages, as it would with the
 * real app; samples carry both the elapsed and the awake (uptime) time.
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

    /** Waits for the echo; the deadline counts awake time only (uptime). */
    private fun waitEcho(text: String) {
        val deadline = SystemClock.uptimeMillis() + ECHO_TIMEOUT_MILLIS
        while (true) {
            val remaining = deadline - SystemClock.uptimeMillis()
            assertTrue("no echo of $text within $ECHO_TIMEOUT_MILLIS ms", remaining > 0)
            val event = events.poll(remaining, TimeUnit.MILLISECONDS) ?: continue
            if (event.optString("type") == "message" && !event.optBoolean("from_me") &&
                event.optString("body").contains(text)
            ) {
                return
            }
        }
    }

    private fun sample(service: CoreService, start: Long, startUptime: Long, messages: Int): JSONObject {
        val stats = request(service, "stats")
        return JSONObject()
            .put("elapsed_s", (SystemClock.elapsedRealtime() - start) / 1000)
            .put("awake_s", (SystemClock.uptimeMillis() - startUptime) / 1000)
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
        val output = File(context.filesDir, "soak.jsonl").apply { delete() }
        val record = { sample: JSONObject ->
            Log.i(SOAK_TAG, sample.toString())
            output.appendText(sample.toString() + "\n")
        }
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
            val startUptime = SystemClock.uptimeMillis()
            val end = start + minutes * 60_000L
            var nextSample = start + SAMPLE_INTERVAL_MILLIS
            var messages = 0
            record(sample(service, start, startUptime, 0))
            while (SystemClock.elapsedRealtime() < end) {
                val text = "soak ${++messages}"
                request(service, "send", JSONObject().put("room_id", roomId).put("text", text))
                waitEcho(text)
                if (SystemClock.elapsedRealtime() >= nextSample) {
                    record(sample(service, start, startUptime, messages))
                    nextSample += SAMPLE_INTERVAL_MILLIS *
                        (1 + (SystemClock.elapsedRealtime() - nextSample) / SAMPLE_INTERVAL_MILLIS)
                }
                // Awake time: a suspended phone stretches the interval.
                SystemClock.sleep(MESSAGE_INTERVAL_MILLIS)
            }
            record(sample(service, start, startUptime, messages))
        } finally {
            service.removeListener(listener)
            context.stopService(intent)
        }
    }

    companion object {
        private const val SOAK_TAG = "MusubeeSoak"
        private const val MINUTES_ARGUMENT = "musubee.soak.minutes"
        private const val MESSAGE_INTERVAL_MILLIS = 30_000L
        private const val SAMPLE_INTERVAL_MILLIS = 60_000L
        private const val ECHO_TIMEOUT_MILLIS = 15_000L
    }
}
