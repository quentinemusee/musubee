// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee

import android.os.Bundle
import androidx.activity.OnBackPressedCallback
import app.musubee.core.MusubeeCorePlugin
import com.getcapacitor.BridgeActivity

class MainActivity : BridgeActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        // Local plugins must be registered before the bridge starts.
        registerPlugin(MusubeeCorePlugin::class.java)
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
}
