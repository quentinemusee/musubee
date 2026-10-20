// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.Person
import app.musubee.MainActivity
import app.musubee.R
import org.json.JSONException
import org.json.JSONObject

/**
 * Shows the notifications of new messages that [MessageNotifications]
 * decides, one per conversation (docs/ADR/0033). The service feeds it every
 * event of the core, on the event thread.
 *
 * On the lock screen, a notification hides its content: it says "New
 * message" only. It never logs a message's content.
 */
class MessageNotifier(private val context: Context) {
    private val rules = MessageNotifications(
        watermarks = PreferenceWatermarks(context),
        now = System::currentTimeMillis,
        visible = { AppVisibility.visible },
    )
    private val manager = NotificationManagerCompat.from(context)

    init {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            context.getSystemService(NotificationManager::class.java).createNotificationChannel(
                NotificationChannel(CHANNEL_ID, "Messages", NotificationManager.IMPORTANCE_HIGH).apply {
                    description = "New messages of your conversations"
                },
            )
        }
    }

    /** Handles one event of the core. */
    fun onEvent(event: ByteArray) {
        val actions = try {
            rules.onEvent(JSONObject(String(event, Charsets.UTF_8)))
        } catch (e: JSONException) {
            // org.json's messages quote the JSON, which can hold a message.
            Log.w(TAG, "An event of the core could not be read")
            return
        }
        for (action in actions) {
            when (action) {
                is MessageNotifications.Action.Post -> post(action)
                is MessageNotifications.Action.Cancel -> manager.cancel(action.conversationId, NOTIFICATION_ID)
            }
        }
    }

    private fun post(post: MessageNotifications.Action.Post) {
        if (!manager.areNotificationsEnabled()) return
        // A change of a notification that the user dismissed stays dismissed.
        if (post.silent && manager.activeNotifications.none { it.tag == post.conversationId && it.id == NOTIFICATION_ID }) return
        val me = Person.Builder().setName("You").build()
        val style = NotificationCompat.MessagingStyle(me)
            .setConversationTitle(post.title)
            .setGroupConversation(post.group)
        for (line in post.lines) {
            val sender = Person.Builder().setName(line.sender.ifEmpty { post.title }).build()
            style.addMessage(line.text, line.timestampMs, sender)
        }
        val open = Intent(context, MainActivity::class.java)
            .setAction(ACTION_OPEN_CONVERSATION)
            .putExtra(EXTRA_CONVERSATION_ID, post.conversationId)
            .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        val pending = PendingIntent.getActivity(
            context,
            post.conversationId.hashCode(),
            open,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        // What the lock screen shows instead: no name, no content.
        val public = NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_launcher_foreground)
            .setContentTitle("Musubee")
            .setContentText("New message")
            .build()
        val notification = NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_launcher_foreground)
            .setStyle(style)
            .setCategory(NotificationCompat.CATEGORY_MESSAGE)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setVisibility(NotificationCompat.VISIBILITY_PRIVATE)
            .setPublicVersion(public)
            .setContentIntent(pending)
            .setAutoCancel(true)
            .setOnlyAlertOnce(post.silent)
            .setWhen(post.lines.last().timestampMs)
            .build()
        try {
            manager.notify(post.conversationId, NOTIFICATION_ID, notification)
        } catch (e: SecurityException) {
            // The permission was withdrawn between the check and the post.
            Log.w(TAG, "Could not show a notification: ${e.message}")
        }
    }

    /** Keeps [MessageNotifications.Watermarks] in the app's preferences. */
    private class PreferenceWatermarks(context: Context) : MessageNotifications.Watermarks {
        private val preferences = context.getSharedPreferences("notifications", Context.MODE_PRIVATE)

        override fun since(accountId: String, now: Long): Long {
            val key = "since.$accountId"
            if (preferences.contains(key)) return preferences.getLong(key, now)
            preferences.edit().putLong(key, now).apply()
            return now
        }

        override fun forget(accountId: String) {
            preferences.edit().remove("since.$accountId").apply()
        }
    }

    companion object {
        private const val TAG = "MusubeeNotifications"
        const val CHANNEL_ID = "messages"
        private const val NOTIFICATION_ID = 2

        /** The intent that a notification sends to open its conversation. */
        const val ACTION_OPEN_CONVERSATION = "app.musubee.OPEN_CONVERSATION"
        const val EXTRA_CONVERSATION_ID = "app.musubee.CONVERSATION_ID"
    }
}

/** Whether the app's activity is on screen: then the inbox shows new messages. */
object AppVisibility {
    @Volatile
    var visible = false
}
