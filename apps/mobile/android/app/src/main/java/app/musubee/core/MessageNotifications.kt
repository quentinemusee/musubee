// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import org.json.JSONObject

/**
 * Decides, from the core's events, which conversations get a notification
 * of new messages and which notifications go away (docs/ADR/0033). It holds
 * no Android type, so that its rules are tested alone; [MessageNotifier]
 * shows what it decides.
 *
 * A message from someone else is announced once the core says its
 * conversation is unread (`message.added`, then `conversation.updated`,
 * API 1.4). It is not announced when:
 * - it is older than the moment the service first saw its account
 *   ([Watermarks]): the history that a network sends after a login is not
 *   news;
 * - it is older than [MAX_AGE_MILLIS]: after days offline, the inbox says
 *   enough;
 * - the app is on screen ([visible]): the inbox shows it.
 *
 * A conversation that is read again (unread count 0 and not marked unread)
 * loses its notification, wherever it was read. An account that is logged
 * out loses its notifications and its mark: a new login starts afresh.
 *
 * A message that changes after it was announced (`message.updated`, API
 * 1.7, docs/ADR/0034) changes its line: an edit shows the new text, and a
 * deletion removes the line, so that a deleted text does not stay on the
 * screen; the notification goes once no line is left. These updates are
 * silent.
 */
class MessageNotifications(
    private val watermarks: Watermarks,
    private val now: () -> Long,
    private val visible: () -> Boolean,
) {
    /** The first time the service saw each account, kept between runs. */
    interface Watermarks {
        /** Returns the account's mark, setting it to [now] the first time. */
        fun since(accountId: String, now: Long): Long

        /** Forgets the account's mark. */
        fun forget(accountId: String)
    }

    /** One line of a notification: which message, who wrote it, and what. */
    data class Line(val messageId: String, val sender: String, val text: String, val timestampMs: Long)

    sealed interface Action {
        val conversationId: String

        /** Shows (or replaces) the conversation's notification. */
        data class Post(
            override val conversationId: String,
            val title: String,
            val group: Boolean,
            val lines: List<Line>,
            /** An update of a shown notification: no sound, and not shown again if dismissed. */
            val silent: Boolean = false,
        ) : Action

        /** Removes the conversation's notification. */
        data class Cancel(override val conversationId: String) : Action
    }

    private class Shown(val accountId: String, val title: String, val group: Boolean, val lines: List<Line>)

    // Messages waiting for their conversation's next update, and what is
    // shown, by conversation. Only the event thread touches them.
    private val waiting = HashMap<String, MutableList<Line>>()
    private val shown = HashMap<String, Shown>()

    /** Returns what to do after one event of the core. */
    fun onEvent(event: JSONObject): List<Action> {
        val data = event.optJSONObject("data") ?: return emptyList()
        return when (event.optString("type")) {
            "account.updated" -> data.optJSONObject("account")?.let(::onAccount).orEmpty()
            "message.added" -> {
                data.optJSONObject("message")?.let(::waitForConversation)
                emptyList()
            }
            "message.updated" -> data.optJSONObject("message")?.let(::onMessageChanged).orEmpty()
            "conversation.updated" -> data.optJSONObject("conversation")?.let(::onConversation).orEmpty()
            else -> emptyList()
        }
    }

    private fun onAccount(account: JSONObject): List<Action> {
        val accountId = account.optString("account_id")
        if (accountId.isEmpty()) return emptyList()
        if (account.optString("state") != "logged_out") {
            watermarks.since(accountId, now())
            return emptyList()
        }
        watermarks.forget(accountId)
        val gone = shown.filterValues { it.accountId == accountId }.keys
        shown.keys.removeAll(gone)
        return gone.map { Action.Cancel(it) }
    }

    private fun waitForConversation(message: JSONObject) {
        if (message.optBoolean("from_me")) return
        val conversationId = message.optString("conversation_id")
        if (conversationId.isEmpty()) return
        waiting.getOrPut(conversationId) { mutableListOf() }.add(
            Line(
                messageId = message.optString("message_id"),
                sender = message.optString("sender_name"),
                text = summary(message),
                timestampMs = message.optLong("timestamp_ms"),
            ),
        )
    }

    private fun onConversation(conversation: JSONObject): List<Action> {
        val conversationId = conversation.optString("conversation_id")
        if (conversationId.isEmpty()) return emptyList()
        val arrived = waiting.remove(conversationId).orEmpty()
        val unread = conversation.optInt("unread_count") > 0 || conversation.optBoolean("marked_unread")
        if (!unread) {
            return if (shown.remove(conversationId) != null) listOf(Action.Cancel(conversationId)) else emptyList()
        }
        val accountId = conversation.optString("account_id")
        val now = now()
        val since = maxOf(watermarks.since(accountId, now), now - MAX_AGE_MILLIS)
        val news = arrived.filter { it.timestampMs >= since }
        if (news.isEmpty() || visible()) return emptyList()
        val lines = (shown[conversationId]?.lines.orEmpty() + news).takeLast(MAX_LINES)
        val updated = Shown(accountId, conversation.optString("name"), conversation.optString("kind") == "group", lines)
        shown[conversationId] = updated
        return listOf(post(conversationId, updated, silent = false))
    }

    private fun post(conversationId: String, shown: Shown, silent: Boolean) =
        Action.Post(conversationId, shown.title, shown.group, shown.lines, silent)

    private fun onMessageChanged(message: JSONObject): List<Action> {
        val conversationId = message.optString("conversation_id")
        val messageId = message.optString("message_id")
        if (conversationId.isEmpty() || messageId.isEmpty()) return emptyList()
        val deleted = message.optBoolean("deleted")
        val change = { lines: List<Line> ->
            if (deleted) {
                lines.filter { it.messageId != messageId }
            } else {
                lines.map { if (it.messageId == messageId) it.copy(text = summary(message)) else it }
            }
        }
        waiting[conversationId]?.let { lines ->
            val changed = change(lines)
            lines.clear()
            lines.addAll(changed)
        }
        val before = shown[conversationId] ?: return emptyList()
        val lines = change(before.lines)
        if (lines == before.lines) return emptyList()
        if (lines.isEmpty()) {
            shown.remove(conversationId)
            return listOf(Action.Cancel(conversationId))
        }
        val updated = Shown(before.accountId, before.title, before.group, lines)
        shown[conversationId] = updated
        return listOf(post(conversationId, updated, silent = true))
    }

    companion object {
        /** Messages older than this are never announced. */
        const val MAX_AGE_MILLIS = 24 * 60 * 60 * 1000L

        /** The lines that a conversation's notification keeps. */
        const val MAX_LINES = 5

        /**
         * What a notification says of a message: its text (for a kind this
         * version cannot show, the core's fallback text), or "New message".
         */
        fun summary(message: JSONObject): String = message.optString("text").ifEmpty { "New message" }
    }
}
