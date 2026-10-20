// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee

import android.content.Intent
import android.os.Bundle
import androidx.activity.OnBackPressedCallback
import app.musubee.core.AppVisibility
import app.musubee.core.MessageNotifier
import app.musubee.core.MusubeeCorePlugin
import com.getcapacitor.BridgeActivity

class MainActivity : BridgeActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        // Local plugins must be registered before the bridge starts.
        registerPlugin(MusubeeCorePlugin::class.java)
        // A recreated activity keeps the intent that started it: a
        // conversation opened from a notification is not opened again.
        if (savedInstanceState != null) intent.action = null
        // The bridge hands the starting intent to onNewIntent.
        super.onCreate(savedInstanceState)

        // Back goes back in the interface's history first: from a thread to
        // the inbox on a phone (ui/src/app/navigation.ts); then it leaves.
        onBackPressedDispatcher.addCallback(
            this,
            object : OnBackPressedCallback(true) {
                override fun handleOnBackPressed() {
                    val webView = bridge?.webView
                    if (webView != null && webView.canGoBack()) {
                        webView.goBack()
                        return
                    }
                    isEnabled = false
                    onBackPressedDispatcher.onBackPressed()
                    isEnabled = true
                }
            },
        )
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        openConversation(intent)
    }

    override fun onResume() {
        super.onResume()
        AppVisibility.visible = true
    }

    override fun onPause() {
        AppVisibility.visible = false
        super.onPause()
    }

    // A notification of new messages opens its conversation (docs/ADR/0033).
    private fun openConversation(intent: Intent) {
        if (intent.action != MessageNotifier.ACTION_OPEN_CONVERSATION) return
        if (intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY != 0) return
        val conversationId = intent.getStringExtra(MessageNotifier.EXTRA_CONVERSATION_ID) ?: return
        val plugin = bridge?.getPlugin("MusubeeCore")?.instance as MusubeeCorePlugin? ?: return
        plugin.openConversation(conversationId)
        intent.action = null
    }
}
