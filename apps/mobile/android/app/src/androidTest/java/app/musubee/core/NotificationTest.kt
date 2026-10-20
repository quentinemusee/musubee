// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import android.Manifest
import android.app.Notification
import android.app.NotificationManager
import android.content.Intent
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.rule.ServiceTestRule
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

/**
 * A message that arrives while the app is not on screen shows a
 * notification, which goes away once the conversation is read
 * (docs/ADR/0033). It uses the service as the app does, without the
 * activity: the app counts as in the background.
 */
@RunWith(AndroidJUnit4::class)
class NotificationTest {
    @get:Rule
    val serviceRule = ServiceTestRule()

    @Test
    fun aNewMessageIsAnnouncedUntilItIsRead() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            instrumentation.uiAutomation.grantRuntimePermission(context.packageName, Manifest.permission.POST_NOTIFICATIONS)
        }
        val manager = context.getSystemService(NotificationManager::class.java)
        val intent = Intent(context, CoreService::class.java)
        ContextCompat.startForegroundService(context, intent)
        val service = (serviceRule.bindService(intent) as CoreService.LocalBinder).service
        val events = LinkedBlockingQueue<JSONObject>()
        val listener: (ByteArray) -> Unit = { events.add(JSONObject(String(it, Charsets.UTF_8))) }
        service.addListener(listener)
        var nextId = 10
        fun request(command: String, params: JSONObject) {
            val req = JSONObject().put("id", nextId++).put("command", command).put("params", params)
            val resp = JSONObject(String(service.call(req.toString().toByteArray()), Charsets.UTF_8))
            assertTrue("$command: ${resp.optJSONObject("error")}", !resp.has("error"))
        }
        try {
            val conversationId = loginThroughService(service, events)
            // Read first: an earlier run may have left the conversation unread.
            request("conversations.markRead", JSONObject().put("conversation_id", conversationId))
            val text = "notify me ${System.nanoTime()}"
            request("messages.send", JSONObject().put("conversation_id", conversationId).put("text", text))

            val shown = waitFor("the notification") {
                manager.activeNotifications.firstOrNull { it.tag == conversationId }
            }
            assertEquals(MessageNotifier.CHANNEL_ID, shown.notification.channelId)
            val style = NotificationCompat.MessagingStyle.extractMessagingStyleFromNotification(shown.notification)
            assertNotNull(style)
            assertTrue("the notification shows the echo", style!!.messages.last().text.toString().contains(text))
            // The lock screen gets a version without the message.
            assertEquals(Notification.VISIBILITY_PRIVATE, shown.notification.visibility)
            val public = shown.notification.publicVersion
            assertNotNull(public)
            assertFalse(public.extras.getCharSequence(Notification.EXTRA_TEXT).toString().contains(text))

            request("conversations.markRead", JSONObject().put("conversation_id", conversationId))
            waitFor("the notification to go away") {
                true.takeIf { manager.activeNotifications.none { it.tag == conversationId } }
            }
        } finally {
            service.removeListener(listener)
            context.stopService(intent)
        }
    }

    private fun <T : Any> waitFor(what: String, check: () -> T?): T {
        val deadline = System.currentTimeMillis() + TIMEOUT_MILLIS
        while (true) {
            check()?.let { return it }
            assertTrue("waited $TIMEOUT_MILLIS ms for $what", System.currentTimeMillis() < deadline)
            TimeUnit.MILLISECONDS.sleep(100)
        }
    }

    private companion object {
        const val TIMEOUT_MILLIS = 15_000L
    }
}
