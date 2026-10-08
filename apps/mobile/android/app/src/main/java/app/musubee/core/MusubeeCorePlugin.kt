// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.IBinder
import androidx.core.content.ContextCompat
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import java.util.concurrent.CompletableFuture
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/**
 * Gives the web UI access to the core running in [CoreService].
 *
 * JavaScript: `call({request})` resolves with the JSON response of the core
 * (`{id, result}` or `{id, error}`), and the "event" listener receives every
 * event of the core. The commands are those of core/embedded until the
 * UI contract of T1.4.
 */
@CapacitorPlugin(name = "MusubeeCore")
class MusubeeCorePlugin : Plugin() {
    private val service = CompletableFuture<CoreService>()
    private val calls = Executors.newCachedThreadPool()
    private val forward: (ByteArray) -> Unit = { event ->
        notifyListeners("event", JSObject(String(event, Charsets.UTF_8)))
    }

    private val connection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName?, binder: IBinder?) {
            val bound = (binder as CoreService.LocalBinder).service
            bound.addListener(forward)
            service.complete(bound)
        }

        override fun onServiceDisconnected(name: ComponentName?) {
            // Same process: only happens if the process is dying.
        }
    }

    override fun load() {
        val intent = Intent(context, CoreService::class.java)
        // Started (so it outlives the activity) and bound (to talk to it).
        // The activity is in the foreground here, which Android requires to
        // start a foreground service.
        ContextCompat.startForegroundService(context, intent)
        context.bindService(intent, connection, Context.BIND_AUTO_CREATE)
    }

    @PluginMethod
    fun call(call: PluginCall) {
        val request = call.getObject("request")
        if (request == null) {
            call.reject("request is required")
            return
        }
        calls.execute {
            try {
                val core = service.get(BIND_TIMEOUT_SECONDS, TimeUnit.SECONDS)
                val response = core.call(request.toString().toByteArray(Charsets.UTF_8))
                call.resolve(JSObject(String(response, Charsets.UTF_8)))
            } catch (e: Exception) {
                call.reject("The core is not available: ${e.message}", e)
            }
        }
    }

    override fun handleOnDestroy() {
        if (service.isDone) {
            service.get().removeListener(forward)
        }
        context.unbindService(connection)
        calls.shutdown()
        super.handleOnDestroy()
    }

    companion object {
        private const val BIND_TIMEOUT_SECONDS = 30L
    }
}
