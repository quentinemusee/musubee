// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import android.content.Intent
import android.os.SystemClock
import android.util.Log
import androidx.core.content.ContextCompat
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.rule.ServiceTestRule
import app.musubee.BuildConfig
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import java.net.HttpURLConnection
import java.net.URL
import java.security.SecureRandom
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

/**
 * Telegram "on device" on Android (T1.6, docs/ADR/0014): the core in the
 * app's foreground service logs in as the bridge test bot, receives a post
 * of the peer bot in the private channel, answers it, and logs out. It uses
 * the test bots of ADR 0006, passed as instrumentation arguments, and an app
 * built with the Telegram application credentials (MUSUBEE_TG_API_ID and
 * MUSUBEE_TG_API_HASH in the build's environment):
 *
 *     ./gradlew :app:connectedDebugAndroidTest \
 *       -Pandroid.testInstrumentationRunnerArguments.class=app.musubee.core.TelegramTest \
 *       -Pandroid.testInstrumentationRunnerArguments.musubeeTgBridgeBotToken=... \
 *       -Pandroid.testInstrumentationRunnerArguments.musubeeTgPeerBotToken=... \
 *       -Pandroid.testInstrumentationRunnerArguments.musubeeTgChatId=...
 *
 * Without them it is skipped, unless musubeeTgRequire=1 (CI with secrets).
 * It never logs the tokens nor the messages.
 */
@RunWith(AndroidJUnit4::class)
class TelegramTest {
    @get:Rule
    val serviceRule = ServiceTestRule()

    private val args = InstrumentationRegistry.getArguments()
    private val bridgeToken = args.getString("musubeeTgBridgeBotToken").orEmpty()
    private val peerToken = args.getString("musubeeTgPeerBotToken").orEmpty()
    private val chatId = args.getString("musubeeTgChatId").orEmpty()

    @Test
    fun telegramOnDevice() {
        val configured = bridgeToken.isNotEmpty() && peerToken.isNotEmpty() && chatId.isNotEmpty() && BuildConfig.TELEGRAM_API_ID != 0
        val why = "needs the Telegram test bots and an app built with the Telegram credentials"
        if (args.getString("musubeeTgRequire") == "1") assertTrue(why, configured) else assumeTrue(why, configured)
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val intent = Intent(context, CoreService::class.java)
        ContextCompat.startForegroundService(context, intent)
        val service = (serviceRule.bindService(intent) as CoreService.LocalBinder).service
        val events = LinkedBlockingQueue<JSONObject>()
        val listener: (ByteArray) -> Unit = { events.add(JSONObject(String(it, Charsets.UTF_8))) }
        service.addListener(listener)
        val core = Core(service)
        var account: String? = null
        try {
            // 1. Log in as the bridge bot.
            var started = SystemClock.elapsedRealtime()
            val step = core.call("login.start", JSONObject().put("network_id", "telegram").put("flow_id", "bot"))
            val field = step.getJSONArray("fields").getJSONObject(0)
            assertEquals("token", field.getString("type"))
            val done = core.call(
                "login.submit",
                JSONObject().put("process_id", step.getString("process_id"))
                    .put("values", JSONObject().put(field.getString("field_id"), bridgeToken)),
            )
            assertEquals("complete", done.getString("type"))
            account = done.getString("account_id")
            Log.i(TAG, "login done in ${SystemClock.elapsedRealtime() - started} ms")
            assertNotNull(
                "the account did not connect within 2 minutes",
                events.find(120_000) { it.isAccount(account, "connected") },
            )
            Log.i(TAG, "connected ${SystemClock.elapsedRealtime() - started} ms after the login started")

            // 2. Telegram -> core, retried with a new post if one is missed.
            var conversation: String? = null
            for (attempt in 1..3) {
                val inbound = "musubee android e2e: from Telegram ${randomHex()} (attempt $attempt)"
                started = SystemClock.elapsedRealtime()
                botApi("sendMessage", JSONObject().put("chat_id", chatId.toLong()).put("text", inbound))
                val received = events.find(60_000) { it.isMessage("message.added", inbound) }
                if (received == null) {
                    Log.w(TAG, "attempt $attempt: the post did not reach the core within a minute")
                    continue
                }
                val message = received.getJSONObject("data").getJSONObject("message")
                assertFalse(message.getBoolean("from_me"))
                conversation = message.getString("conversation_id")
                Log.i(TAG, "Telegram -> core: ${SystemClock.elapsedRealtime() - started} ms (attempt $attempt)")
                break
            }
            assertNotNull("no post of the peer bot reached the core after 3 attempts", conversation)

            // 3. core -> Telegram.
            val outbound = "musubee android e2e: from the core ${randomHex()}"
            started = SystemClock.elapsedRealtime()
            val sent = core.call("messages.send", JSONObject().put("conversation_id", conversation).put("text", outbound))
                .getJSONObject("message").getString("message_id")
            assertNotNull(
                "the answer was not sent within a minute",
                events.find(60_000) { it.messageStatus(sent) == "sent" },
            )
            Log.i(TAG, "core -> Telegram, sent status: ${SystemClock.elapsedRealtime() - started} ms")
            assertTrue("the peer bot did not see the answer", peerSees(outbound, 120_000))
            Log.i(TAG, "core -> Telegram, seen by the peer bot: ${SystemClock.elapsedRealtime() - started} ms")

            val stats = core.call("debug.stats", JSONObject())
            Log.i(TAG, "core memory: heap ${stats.getLong("heap_alloc_bytes") / 1024} KiB in use, ${stats.getLong("heap_sys_bytes") / 1024} KiB from the system, ${stats.getLong("goroutines")} goroutines")

            // 4. Log out.
            core.call("accounts.logout", JSONObject().put("account_id", account))
            account = null
        } finally {
            // Never leave a session of the bot behind: the next run could not
            // receive its updates.
            account?.let { runCatching { core.call("accounts.logout", JSONObject().put("account_id", it)) } }
            service.removeListener(listener)
            context.stopService(intent)
        }
    }

    /** Calls the core; an error fails the test, with the core's message (which never quotes values). */
    private class Core(private val service: CoreService) {
        private var nextId = 0

        fun call(command: String, params: JSONObject): JSONObject {
            val request = JSONObject().put("id", ++nextId).put("command", command).put("params", params)
            val response = JSONObject(String(service.call(request.toString().toByteArray()), Charsets.UTF_8))
            assertTrue("$command: ${response.optJSONObject("error")}", !response.has("error"))
            return response.getJSONObject("result")
        }
    }

    private fun LinkedBlockingQueue<JSONObject>.find(withinMillis: Long, match: (JSONObject) -> Boolean): JSONObject? {
        val deadline = SystemClock.elapsedRealtime() + withinMillis
        while (true) {
            val remaining = deadline - SystemClock.elapsedRealtime()
            if (remaining <= 0) return null
            val event = poll(remaining, TimeUnit.MILLISECONDS) ?: return null
            if (match(event)) return event
        }
    }

    private fun JSONObject.isAccount(id: String?, state: String): Boolean {
        if (optString("type") != "account.updated") return false
        val account = getJSONObject("data").getJSONObject("account")
        return account.getString("account_id") == id && account.getString("state") == state
    }

    private fun JSONObject.isMessage(type: String, text: String): Boolean =
        optString("type") == type && getJSONObject("data").getJSONObject("message").getString("text") == text

    private fun JSONObject.messageStatus(id: String): String? {
        val type = optString("type")
        if (type != "message.added" && type != "message.updated") return null
        val message = getJSONObject("data").getJSONObject("message")
        return if (message.getString("message_id") == id) message.getString("status") else null
    }

    /** One Bot API call as the peer bot. Errors never quote the token. */
    private fun botApi(method: String, params: JSONObject): Any {
        val connection = URL("https://api.telegram.org/bot$peerToken/$method").openConnection() as HttpURLConnection
        try {
            connection.requestMethod = "POST"
            connection.connectTimeout = 30_000
            connection.readTimeout = 30_000
            connection.doOutput = true
            connection.setRequestProperty("Content-Type", "application/json")
            connection.outputStream.use { it.write(params.toString().toByteArray()) }
            val stream = if (connection.responseCode in 200..299) connection.inputStream else connection.errorStream
            val body = JSONObject(stream.bufferedReader().use { it.readText() })
            assertTrue("Bot API $method: ${body.optString("description")}", body.optBoolean("ok"))
            return body.get("result")
        } finally {
            connection.disconnect()
        }
    }

    /** Polls the peer bot's updates until a post with this text appears in the channel. */
    private fun peerSees(text: String, withinMillis: Long): Boolean {
        val deadline = SystemClock.elapsedRealtime() + withinMillis
        while (SystemClock.elapsedRealtime() < deadline) {
            val updates = botApi(
                "getUpdates",
                JSONObject().put("timeout", 10).put("allowed_updates", JSONArray(listOf("message", "channel_post"))),
            ) as JSONArray
            for (i in 0 until updates.length()) {
                val update = updates.getJSONObject(i)
                val post = update.optJSONObject("channel_post") ?: update.optJSONObject("message") ?: continue
                if (post.optString("text") == text && post.getJSONObject("chat").getLong("id").toString() == chatId) return true
            }
            SystemClock.sleep(2_000)
        }
        return false
    }

    private fun randomHex(): String {
        val bytes = ByteArray(4).also { SecureRandom().nextBytes(it) }
        return bytes.joinToString("") { "%02x".format(it) }
    }

    private companion object {
        const val TAG = "MusubeeTelegramTest"
    }
}
