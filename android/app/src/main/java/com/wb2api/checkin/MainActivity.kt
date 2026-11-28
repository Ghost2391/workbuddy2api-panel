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
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.wb2api.checkin.data.AccountJsonRepository
import com.wb2api.checkin.data.ImportSummary
import com.wb2api.checkin.data.readImportFiles
import com.wb2api.checkin.panel.PanelConfig
import com.wb2api.checkin.panel.PanelController
import com.wb2api.checkin.panel.PanelService
import com.wb2api.checkin.ui.PanelHostScreen
import com.wb2api.checkin.ui.WbCheckinTheme
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File
import java.time.LocalDate
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            WbCheckinTheme {
                RequestNotificationPermission()
                val context = LocalContext.current
                val scope = rememberCoroutineScope()
                val state by PanelController.state.collectAsStateWithLifecycle()
                val repo = remember { AccountJsonRepository(File(context.filesDir, "auths")) }
                var busy by remember { mutableStateOf(false) }
                var pendingExportZip by remember { mutableStateOf<List<Pair<String, String>>?>(null) }

                val importLauncher = rememberLauncherForActivityResult(
                    contract = ActivityResultContracts.OpenMultipleDocuments(),
                ) { uris ->
                    if (uris.isEmpty()) return@rememberLauncherForActivityResult
                    scope.launch {
                        busy = true
                        val wasRunning = PanelController.state.value.running
                        val summary = runCatching {
                            withContext(Dispatchers.IO) {
                                val files = readImportFiles(context.contentResolver, uris)
                                // 导入前停面板，避免与面板并发读写 auths。
                                if (wasRunning) PanelController.stopAndWait()
                                repo.importAccounts(files)
                            }
                        }.getOrElse { error ->
                            ImportSummary(0, 0, listOf("导入失败：${error.message ?: "未知错误"}"))
                        }
                        // 只在导入前面板本就运行时才恢复（主线程启动前台服务）。
                        if (wasRunning) PanelService.start(context)
                        busy = false
                        showImportSummary(context, summary)
                    }
                }

                val exportZipLauncher = rememberLauncherForActivityResult(
                    contract = ActivityResultContracts.CreateDocument("application/zip"),
                ) { uri ->
                    val files = pendingExportZip
                    pendingExportZip = null
                    if (uri != null && files != null) {
                        scope.launch(Dispatchers.IO) {
                            runCatching {
                                context.contentResolver.openOutputStream(uri)?.use { out ->
                                    ZipOutputStream(out).use { zip ->
                                        for ((name, content) in files) {
                                            zip.putNextEntry(ZipEntry(name))
                                            zip.write(content.toByteArray(Charsets.UTF_8))
                                            zip.closeEntry()
                                        }
                                    }
                                }
                            }
                        }
                    }
                }

                PanelHostScreen(
                    state = state,
                    busy = busy,
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
                    onImport = {
                        importLauncher.launch(
                            arrayOf("application/json", "text/plain", "application/octet-stream"),
                        )
                    },
                    onExportAll = {
                        scope.launch {
                            busy = true
                            val files = withContext(Dispatchers.IO) {
                                runCatching { repo.exportAll() }.getOrDefault(emptyList())
                            }
                            busy = false
                            if (files.isEmpty()) {
                                Toast.makeText(context, "没有可导出的账号", Toast.LENGTH_SHORT).show()
                            } else {
                                // SAF launcher 必须在主线程调用。
                                pendingExportZip = files
                                exportZipLauncher.launch("wb-auths-${LocalDate.now()}.zip")
                            }
                        }
                    },
                )
            }
        }
    }
}

private fun showImportSummary(context: Context, summary: ImportSummary) {
    val message = if (summary.failures.isEmpty()) {
        "成功导入 ${summary.ok} 个账号"
    } else {
        "导入 ${summary.ok}/${summary.total} 个成功，${summary.failures.size} 个失败"
    }
    Toast.makeText(context, message, Toast.LENGTH_LONG).show()
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
