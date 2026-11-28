package com.wb2api.checkin

import android.Manifest
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.wb2api.checkin.panel.PanelConfig
import com.wb2api.checkin.panel.PanelController
import com.wb2api.checkin.panel.PanelService
import com.wb2api.checkin.ui.PanelHostScreen
import com.wb2api.checkin.ui.WbCheckinTheme

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        if (!PanelController.state.value.running) PanelService.start(this)
        setContent {
            WbCheckinTheme {
                RequestNotificationPermission()
                val context = LocalContext.current
                val state by PanelController.state.collectAsStateWithLifecycle()
                PanelHostScreen(
                    state = state,
                    onStart = { PanelService.start(context) },
                    onStop = { PanelService.stop(context) },
                    onClearError = PanelController::clearError,
                    onOpenPanel = {
                        runCatching {
                            context.startActivity(
                                Intent(Intent.ACTION_VIEW, Uri.parse(PanelConfig.PANEL_URL)),
                            )
                        }
                    },
                    onCopyKey = {
                        copyToClipboard(context, state.apiKey)
                        Toast.makeText(context, "已复制 Key", Toast.LENGTH_SHORT).show()
                    },
                )
            }
        }
    }
}

@Composable
private fun RequestNotificationPermission() {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
    val context = LocalContext.current
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {}
    LaunchedEffect(Unit) {
        val granted = ContextCompat.checkSelfPermission(
            context,
            Manifest.permission.POST_NOTIFICATIONS,
        ) == PackageManager.PERMISSION_GRANTED
        if (!granted) launcher.launch(Manifest.permission.POST_NOTIFICATIONS)
    }
}

private fun copyToClipboard(context: Context, text: String) {
    if (text.isBlank()) return
    val manager = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
    manager.setPrimaryClip(ClipData.newPlainText("panel key", text))
}
