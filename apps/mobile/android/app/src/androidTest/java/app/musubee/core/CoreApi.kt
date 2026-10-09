// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import java.util.concurrent.BlockingQueue
import java.util.concurrent.TimeUnit

// Helpers of the instrumented tests for the core API (core/api/schema,
// docs/ADR/0012). The tests read the JSON by hand: the Kotlin shell only
// carries the API, and the typed client is the TypeScript one.

/** Whether an event is the echo of a message containing [text]. */
fun isEcho(event: JSONObject, text: String): Boolean {
    if (event.optString("type") != "message.added") return false
    val message = event.getJSONObject("data").getJSONObject("message")
    return !message.getBoolean("from_me") && message.getString("text").contains(text)
}

/**
 * Adds the echo account "alice" through the service, then finds the Instant
 * Echo conversation, waiting for it in [events] if needed; returns its ID.
 */
fun loginThroughService(service: CoreService, events: BlockingQueue<JSONObject>): String {
    fun request(id: Int, command: String, params: JSONObject): JSONObject {
        val req = JSONObject().put("id", id).put("command", command).put("params", params)
        val resp = JSONObject(String(service.call(req.toString().toByteArray()), Charsets.UTF_8))
        assertTrue("$command: ${resp.optJSONObject("error")}", !resp.has("error"))
        return resp.getJSONObject("result")
    }
    val step = request(1, "login.start", JSONObject().put("network_id", "echo").put("flow_id", "username"))
    val field = step.getJSONArray("fields").getJSONObject(0).getString("field_id")
    val done = request(
        2,
        "login.submit",
        JSONObject().put("process_id", step.getString("process_id")).put("values", JSONObject().put(field, "alice")),
    )
    assertEquals("complete", done.getString("type"))
    // The service keeps its data between runs: the conversation may exist
    // already, and then no event announces it.
    val conversations = request(3, "conversations.list", JSONObject().put("account_id", done.getString("account_id")))
        .getJSONArray("conversations")
    for (i in 0 until conversations.length()) {
        val conversation = conversations.getJSONObject(i)
        if (conversation.getString("name") == "Instant Echo") return conversation.getString("conversation_id")
    }
    val deadline = System.currentTimeMillis() + LOGIN_TIMEOUT_MILLIS
    while (true) {
        val remaining = deadline - System.currentTimeMillis()
        assertTrue("no Instant Echo conversation within $LOGIN_TIMEOUT_MILLIS ms", remaining > 0)
        val event = events.poll(remaining, TimeUnit.MILLISECONDS) ?: continue
        if (event.optString("type") != "conversation.updated") continue
        val conversation = event.getJSONObject("data").getJSONObject("conversation")
        if (conversation.getString("name") == "Instant Echo") return conversation.getString("conversation_id")
    }
}

private const val LOGIN_TIMEOUT_MILLIS = 15_000L
