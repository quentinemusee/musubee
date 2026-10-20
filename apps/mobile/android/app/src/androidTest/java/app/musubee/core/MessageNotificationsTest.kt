// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import androidx.test.ext.junit.runners.AndroidJUnit4
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The rules of [MessageNotifications] (docs/ADR/0033), with a fake clock and
 * marks in memory. They run on a device because org.json is Android's.
 */
@RunWith(AndroidJUnit4::class)
class MessageNotificationsTest {
    private var now = 1_000_000_000_000L
    private var visible = false
    private val marks = HashMap<String, Long>()
    private val rules = MessageNotifications(
        watermarks = object : MessageNotifications.Watermarks {
            override fun since(accountId: String, now: Long) = marks.getOrPut(accountId) { now }

            override fun forget(accountId: String) {
                marks.remove(accountId)
            }
        },
        now = { now },
        visible = { visible },
    )

    private fun event(type: String, key: String, data: JSONObject) =
        JSONObject().put("type", type).put("data", JSONObject().put(key, data))

    private fun account(state: String = "connected") =
        rules.onEvent(event("account.updated", "account", JSONObject().put("account_id", "a1").put("state", state)))

    private fun message(text: String, timestamp: Long, fromMe: Boolean = false) = rules.onEvent(
        event(
            "message.added",
            "message",
            JSONObject().put("conversation_id", "c1").put("from_me", fromMe).put("sender_name", "Bob")
                .put("timestamp_ms", timestamp).put("kind", "text").put("text", text),
        ),
    )

    private fun conversation(unread: Int, markedUnread: Boolean = false) = rules.onEvent(
        event(
            "conversation.updated",
            "conversation",
            JSONObject().put("conversation_id", "c1").put("account_id", "a1").put("name", "Bob").put("kind", "direct")
                .put("unread_count", unread).put("marked_unread", markedUnread),
        ),
    )

    private fun texts(actions: List<MessageNotifications.Action>) =
        actions.map { action ->
            when (action) {
                is MessageNotifications.Action.Post -> "post:" + action.lines.joinToString("|") { it.text }
                is MessageNotifications.Action.Cancel -> "cancel"
            }
        }

    @Test
    fun announcesANewMessageOnceItsConversationIsUnread() {
        account()
        now += 1_000
        assertEquals(emptyList<String>(), texts(message("hello", now)))
        val actions = conversation(1)
        assertEquals(listOf("post:hello"), texts(actions))
        val post = actions[0] as MessageNotifications.Action.Post
        assertEquals("Bob", post.title)
        assertEquals(false, post.group)
        assertEquals("Bob", post.lines[0].sender)
    }

    @Test
    fun skipsWhatCameBeforeTheAccountWasSeen() {
        account()
        // History that the network sends after a login.
        message("old", now - 60_000)
        assertEquals(emptyList<String>(), texts(conversation(1)))
        message("new", now + 1)
        assertEquals(listOf("post:new"), texts(conversation(2)))
    }

    @Test
    fun skipsMessagesOlderThanADay() {
        marks["a1"] = 0L // Seen long ago.
        message("from last week", now - 7 * 24 * 3_600_000L)
        assertEquals(emptyList<String>(), texts(conversation(1)))
        message("from this morning", now - 3_600_000L)
        assertEquals(listOf("post:from this morning"), texts(conversation(2)))
    }

    @Test
    fun skipsOwnMessagesAndWhatTheScreenShows() {
        account()
        message("mine", now + 1, fromMe = true)
        assertEquals(emptyList<String>(), texts(conversation(0)))
        visible = true
        message("seen", now + 2)
        assertEquals(emptyList<String>(), texts(conversation(1)))
    }

    @Test
    fun keepsTheLastLinesAndGoesAwayOnceRead() {
        account()
        for (i in 1..MessageNotifications.MAX_LINES + 2) {
            message("m$i", now + i)
            conversation(i)
        }
        message("last", now + 100)
        assertEquals(listOf("post:m4|m5|m6|m7|last"), texts(conversation(8)))
        assertEquals(listOf("cancel"), texts(conversation(0)))
        // Nothing left to cancel.
        assertEquals(emptyList<String>(), texts(conversation(0)))
    }

    @Test
    fun aConversationMarkedUnreadKeepsItsNotification() {
        account()
        message("hi", now + 1)
        conversation(1)
        assertEquals(emptyList<String>(), texts(conversation(0, markedUnread = true)))
    }

    @Test
    fun aLoggedOutAccountLosesItsNotificationsAndItsMark() {
        account()
        message("hi", now + 1)
        conversation(1)
        assertEquals(listOf("cancel"), texts(account("logged_out")))
        assertEquals(null, marks["a1"])
        // Logged in again later: the history in between is not news.
        now += 3_600_000L
        account()
        message("sent while logged out", now - 60_000)
        assertEquals(emptyList<String>(), texts(conversation(1)))
    }

    @Test
    fun saysNewMessageWhenThereIsNoText() {
        account()
        message("", now + 1)
        assertEquals(listOf("post:New message"), texts(conversation(1)))
    }
}
