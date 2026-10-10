// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee.core

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Binder
import android.os.Build
import android.os.IBinder
import android.util.Base64
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import app.musubee.BuildConfig
import app.musubee.R
import org.json.JSONObject
import java.io.File
import java.util.concurrent.CompletableFuture
import java.util.concurrent.CopyOnWriteArraySet
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/**
 * The foreground service that owns the core, so that the core keeps its
 * network connections while no activity is visible (docs/ADR/0011).
 *
 * The core is opened on a background thread when the service is created and
 * closed when it is destroyed. Clients bind to the service (see
 * [MusubeeCorePlugin]); [call] waits for the core to be open.
 */
class CoreService : Service() {
    inner class LocalBinder : Binder() {
        val service: CoreService get() = this@CoreService
    }

    private val binder = LocalBinder()
    private val opener = Executors.newSingleThreadExecutor()
    private val core = CompletableFuture<CoreLibrary>()
    private val listeners = CopyOnWriteArraySet<(ByteArray) -> Unit>()
    private var reader: Thread? = null

    override fun onCreate() {
        super.onCreate()
        startInForeground()
        opener.execute {
            try {
                val key = CoreKey.forApp(this).load()
                val config = try {
                    coreConfig(File(filesDir, "core"), key)
                } finally {
                    key.fill(0)
                }
                val opened = try {
                    CoreLibrary.open(config)
                } finally {
                    config.fill(0)
                }
                core.complete(opened)
                reader = Thread({ readEvents(opened) }, "musubee-core-events").also { it.start() }
            } catch (e: Throwable) {
                Log.e(TAG, "The core did not start", e)
                core.completeExceptionally(e)
            }
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int = START_STICKY

    override fun onBind(intent: Intent?): IBinder = binder

    override fun onDestroy() {
        opener.shutdown()
        opener.awaitTermination(OPEN_TIMEOUT_SECONDS, TimeUnit.SECONDS)
        if (core.isDone && !core.isCompletedExceptionally) {
            core.get().close()
        }
        reader?.join(READER_JOIN_MILLIS)
        super.onDestroy()
    }

    /** Runs one JSON request once the core is open; blocks the calling thread. */
    fun call(request: ByteArray): ByteArray = core.get(OPEN_TIMEOUT_SECONDS, TimeUnit.SECONDS).call(request)

    /** Receives every JSON event of the core, on the event thread. */
    fun addListener(listener: (ByteArray) -> Unit) = listeners.add(listener)

    fun removeListener(listener: (ByteArray) -> Unit) = listeners.remove(listener)

    private fun readEvents(library: CoreLibrary) {
        while (true) {
            val event = library.nextEvent(EVENT_WAIT_MILLIS) ?: continue
            for (listener in listeners) {
                try {
                    listener(event)
                } catch (e: Exception) {
                    Log.w(TAG, "An event listener failed", e)
                }
            }
            if (event.contentEquals(CLOSED_EVENT)) {
                return
            }
        }
    }

    private fun startInForeground() {
        val manager = getSystemService(NotificationManager::class.java)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            manager.createNotificationChannel(
                NotificationChannel(CHANNEL_ID, "Connection", NotificationManager.IMPORTANCE_LOW),
            )
        }
        val notification: Notification = NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_launcher_foreground)
            .setContentTitle("Musubee")
            .setContentText("Connected to your accounts")
            .setOngoing(true)
            .build()
        val type = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE
        } else {
            0
        }
        ServiceCompat.startForeground(this, NOTIFICATION_ID, notification, type)
    }

    companion object {
        /**
         * The core's configuration: its data directory, private to the app,
         * and its master key (docs/ADR/0019). Never log it: it holds the key.
         */
        fun coreConfig(dataDir: File, key: ByteArray): ByteArray {
            // JSONObject escapes the path.
            val config = JSONObject()
                .put("data_dir", dataDir.absolutePath)
                .put("log_level", "info")
                .put("database_key", Base64.encodeToString(key, Base64.NO_WRAP))
            if (BuildConfig.TELEGRAM_API_ID != 0) {
                config.put(
                    "telegram",
                    JSONObject()
                        .put("api_id", BuildConfig.TELEGRAM_API_ID)
                        .put("api_hash", BuildConfig.TELEGRAM_API_HASH),
                )
            }
            return config.toString().toByteArray(Charsets.UTF_8)
        }

        private const val TAG = "MusubeeCore"
        private const val CHANNEL_ID = "core"
        private const val NOTIFICATION_ID = 1
        private const val OPEN_TIMEOUT_SECONDS = 30L
        private const val EVENT_WAIT_MILLIS = 60_000
        private const val READER_JOIN_MILLIS = 2_000L
        private val CLOSED_EVENT = """{"type":"core.closed","data":{}}""".toByteArray(Charsets.UTF_8)
    }
}
