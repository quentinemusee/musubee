// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import android.Manifest
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.Build
import android.os.IBinder
import androidx.core.content.ContextCompat
import com.getcapacitor.JSObject
import com.getcapacitor.PermissionState
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import com.getcapacitor.annotation.Permission
import com.getcapacitor.annotation.PermissionCallback
import java.util.concurrent.CompletableFuture
import java.util.concurrent.ExecutionException
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/** The plugin's alias of Android's permission to post notifications. */
private const val NOTIFICATIONS = "notifications"

/**
 * Gives the web UI access to the core running in [CoreService].
 *
 * JavaScript: `call({request})` resolves with the JSON response of the core
 * (`{id, result}` or `{id, error}`), and the "event" listener receives every
 * event of the core. The commands are those of core/embedded until the
 * UI contract of T1.4.
 *
 * When the core cannot start because its key is lost, `call` rejects with
 * the code [KEY_LOST], and `startOver()` sets the unreadable data aside and
 * starts a new, empty core (docs/ADR/0031).
 *
 * Notifications of new messages (docs/ADR/0033): `askToNotify()` asks the
 * user for Android's permission to post them, and the "openConversation"
 * listener receives `{conversation_id}` when the user taps one; the event is
 * kept until a listener takes it, since a tap can start the app.
 */
@CapacitorPlugin(
    name = "MusubeeCore",
    permissions = [Permission(alias = NOTIFICATIONS, strings = [Manifest.permission.POST_NOTIFICATIONS])],
)
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
                val cause = if (e is ExecutionException) e.cause ?: e else e
                if (cause is KeyLostException) {
                    call.reject("The core's key is lost: ${cause.message}", KEY_LOST, cause)
                } else {
                    call.reject("The core is not available: ${cause.message}", e)
                }
            }
        }
    }

    @PluginMethod
    fun startOver(call: PluginCall) {
        calls.execute {
            try {
                service.get(BIND_TIMEOUT_SECONDS, TimeUnit.SECONDS).startOver()
                call.resolve()
            } catch (e: Exception) {
                call.reject("Could not start over: ${e.message}", e)
            }
        }
    }

    /** Resolves with `{granted}` once the user answered, or at once if they already did. */
    @PluginMethod
    fun askToNotify(call: PluginCall) {
        // Below Android 13, posting needs no permission.
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU || getPermissionState(NOTIFICATIONS) == PermissionState.GRANTED) {
            call.resolve(JSObject().put("granted", true))
            return
        }
        requestPermissionForAlias(NOTIFICATIONS, call, "notificationsAnswered")
    }

    @PermissionCallback
    private fun notificationsAnswered(call: PluginCall) {
        call.resolve(JSObject().put("granted", getPermissionState(NOTIFICATIONS) == PermissionState.GRANTED))
    }

    /** Tells the interface to show a conversation: the user tapped its notification. */
    fun openConversation(conversationId: String) {
        notifyListeners("openConversation", JSObject().put("conversation_id", conversationId), true)
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

        /** The rejection code of `call` when the core's key is lost. */
        const val KEY_LOST = "KEY_LOST"
    }
}
