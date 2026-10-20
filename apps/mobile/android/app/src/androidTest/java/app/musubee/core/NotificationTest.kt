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
 * (docs/ADR/0033), and follows the message's changes (docs/ADR/0034). It uses the service as the app does, without the
 * activity: the app counts as in the background.
 */
@RunWith(AndroidJUnit4::class)
class NotificationTest {
    @get:Rule
    val serviceRule = ServiceTestRule()

    /** Runs [test] with the service started, logged in, and the echo conversation read. */
    private fun withEcho(test: (manager: NotificationManager, conversationId: String, request: (String, JSONObject) -> Unit) -> Unit) {
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
            test(manager, conversationId, ::request)
        } finally {
            service.removeListener(listener)
            context.stopService(intent)
        }
    }

    @Test
    fun aNewMessageIsAnnouncedUntilItIsRead() {
        withEcho { manager, conversationId, request ->
            val text = "notify me ${System.nanoTime()}"
            request("messages.send", JSONObject().put("conversation_id", conversationId).put("text", text))

            // The one that shows this echo: an earlier test's echo in the same
            // conversation may still be shown until the read above cancels it.
            val shown = waitFor("the notification of the echo") {
                manager.activeNotifications.firstOrNull { it.tag == conversationId && lastLine(it.notification).contains(text) }
            }
            assertEquals(MessageNotifier.CHANNEL_ID, shown.notification.channelId)
            // The lock screen gets a version without the message.
            assertEquals(Notification.VISIBILITY_PRIVATE, shown.notification.visibility)
            val public = shown.notification.publicVersion
            assertNotNull(public)
            assertFalse(public.extras.getCharSequence(Notification.EXTRA_TEXT).toString().contains(text))

            request("conversations.markRead", JSONObject().put("conversation_id", conversationId))
            waitFor<Boolean>("the notification to go away") {
                true.takeIf { manager.activeNotifications.none { it.tag == conversationId } }
            }
        }
    }

    /**
     * A message deleted on the network leaves the notification, and an
     * edited one shows its new text (docs/ADR/0034): the echo network
     * deletes and edits its echoes on command.
     */
    @Test
    fun aDeletedMessageLeavesTheNotificationAndAnEditedOneChangesIt() {
        withEcho { manager, conversationId, request ->
            val nonce = System.nanoTime()
            val secret = "a secret $nonce"
            request("messages.send", JSONObject().put("conversation_id", conversationId).put("text", "/delete $secret"))
            request("messages.send", JSONObject().put("conversation_id", conversationId).put("text", "/edit typo $nonce"))
            val edited = "typo $nonce (edited)"
            waitFor("the notification with the edited echo alone") {
                manager.activeNotifications.firstOrNull { it.tag == conversationId && lines(it.notification) == listOf(edited) }
            }
            request("conversations.markRead", JSONObject().put("conversation_id", conversationId))
            waitFor<Boolean>("the notification to go away") {
                true.takeIf { manager.activeNotifications.none { it.tag == conversationId } }
            }
        }
    }

    /** The lines of a MessagingStyle notification. */
    private fun lines(notification: Notification): List<String> {
        val style = NotificationCompat.MessagingStyle.extractMessagingStyleFromNotification(notification)
        return style?.messages?.map { it.text.toString() }.orEmpty()
    }

    /** The last line of a MessagingStyle notification, or "" without one. */
    private fun lastLine(notification: Notification): String = lines(notification).lastOrNull() ?: ""

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
