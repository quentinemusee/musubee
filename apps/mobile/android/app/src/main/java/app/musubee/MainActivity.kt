// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package app.musubee

import android.os.Bundle
import app.musubee.core.MusubeeCorePlugin
import com.getcapacitor.BridgeActivity

class MainActivity : BridgeActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        // Local plugins must be registered before the bridge starts.
        registerPlugin(MusubeeCorePlugin::class.java)
        super.onCreate(savedInstanceState)
    }
}
