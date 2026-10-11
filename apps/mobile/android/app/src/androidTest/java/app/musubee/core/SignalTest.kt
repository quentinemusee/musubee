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
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Signal in the Android app (T2.12, docs/ADR/0029): the core of an app built
 * with Signal (Gradle properties musubee.signal and musubee.libsignal) offers
 * it, and starting the QR code login reaches Signal's service: the core opens
 * a provisioning connection and generates its keys with libsignal, then
 * shows a linking code. The login is cancelled; no account is linked, so the
 * test needs no phone.
 *
 * Skipped when the app is built without Signal, unless
 * musubeeSignalRequire=1 (CI). It never logs the linking code, which carries
 * a public key and the provisioning address.
 */
@RunWith(AndroidJUnit4::class)
class SignalTest {
    @get:Rule
    val serviceRule = ServiceTestRule()

    private val args = InstrumentationRegistry.getArguments()

    @Test
    fun signalOffersALinkingCode() {
        val why = "needs an app built with Signal (-Pmusubee.signal=staging -Pmusubee.libsignal=DIR)"
        val built = BuildConfig.SIGNAL_ENVIRONMENT.isNotEmpty()
        if (args.getString("musubeeSignalRequire") == "1") assertTrue(why, built) else assumeTrue(why, built)
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val intent = Intent(context, CoreService::class.java)
        ContextCompat.startForegroundService(context, intent)
        val service = (serviceRule.bindService(intent) as CoreService.LocalBinder).service
        val core = Core(service)
        try {
            val networks = core.call("networks.list", JSONObject()).getJSONArray("networks")
            val signal = (0 until networks.length()).map { networks.getJSONObject(it) }
                .singleOrNull { it.getString("network_id") == "signal" }
            assertTrue("the core does not offer Signal", signal != null)
            val flows = signal!!.getJSONArray("login_flows")
            assertEquals(1, flows.length())
            assertEquals("qr", flows.getJSONObject(0).getString("flow_id"))

            val started = SystemClock.elapsedRealtime()
            val step = core.call("login.start", JSONObject().put("network_id", "signal").put("flow_id", "qr"))
            Log.i(TAG, "linking code in ${SystemClock.elapsedRealtime() - started} ms")
            assertEquals("display_and_wait", step.getString("type"))
            val display = step.getJSONObject("display")
            assertEquals("qr", display.getString("type"))
            assertTrue("not a Signal linking code", display.getString("data").startsWith("sgnl://linkdevice?"))
            core.call("login.cancel", JSONObject().put("process_id", step.getString("process_id")))

            // The service's data directory is shared with the other tests,
            // which leave echo accounts: count Signal's only.
            val accounts = core.call("accounts.list", JSONObject()).getJSONArray("accounts")
            val signalAccounts = (0 until accounts.length()).count { accounts.getJSONObject(it).getString("network_id") == "signal" }
            assertEquals("a cancelled login left a Signal account", 0, signalAccounts)
        } finally {
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

    private companion object {
        const val TAG = "MusubeeSignalTest"
    }
}
