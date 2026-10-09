// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import android.content.Intent
import androidx.core.content.ContextCompat
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.rule.ServiceTestRule
import org.json.JSONObject
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

/**
 * The foreground service as the app uses it: started, bound, then a
 * message sent through it gets its echo as an event.
 */
@RunWith(AndroidJUnit4::class)
class CoreServiceTest {
    @get:Rule
    val serviceRule = ServiceTestRule()

    @Test
    fun serviceRunsTheCoreInTheForeground() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val intent = Intent(context, CoreService::class.java)
        ContextCompat.startForegroundService(context, intent)
        val service = (serviceRule.bindService(intent) as CoreService.LocalBinder).service
        val events = LinkedBlockingQueue<JSONObject>()
        val listener: (ByteArray) -> Unit = { events.add(JSONObject(String(it, Charsets.UTF_8))) }
        service.addListener(listener)
        try {
            val conversationId = loginThroughService(service, events)
            val send = JSONObject().put("id", 10).put("command", "messages.send")
                .put("params", JSONObject().put("conversation_id", conversationId).put("text", "through the service"))
            service.call(send.toString().toByteArray())
            val deadline = System.currentTimeMillis() + 15_000
            while (true) {
                val remaining = deadline - System.currentTimeMillis()
                assertTrue("no echo within 15 s", remaining > 0)
                val event = events.poll(remaining, TimeUnit.MILLISECONDS) ?: continue
                if (isEcho(event, "through the service")) break
            }
        } finally {
            service.removeListener(listener)
            context.stopService(intent)
        }
    }
}
