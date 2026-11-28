package com.wb2api.checkin

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.wb2api.checkin.data.readImportFiles
import com.wb2api.checkin.ui.AppScreen
import com.wb2api.checkin.ui.WbCheckinTheme
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.LocalDate
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            WbCheckinTheme {
                val context = LocalContext.current
                val factory = remember { CheckinViewModel.Factory(context) }
                val viewModel: CheckinViewModel = viewModel(factory = factory)
                val state by viewModel.state.collectAsStateWithLifecycle()
                val scope = rememberCoroutineScope()

                var pendingExportJson by remember { mutableStateOf<String?>(null) }
                var pendingExportZip by remember { mutableStateOf<List<Pair<String, String>>?>(null) }

                val importLauncher = rememberLauncherForActivityResult(
                    contract = ActivityResultContracts.OpenMultipleDocuments(),
                ) { uris ->
                    if (uris.isNotEmpty()) {
                        scope.launch {
                            val files = withContext(Dispatchers.IO) {
                                readImportFiles(context.contentResolver, uris)
                            }
                            viewModel.importFiles(files)
                        }
                    }
                }

                val exportJsonLauncher = rememberLauncherForActivityResult(
                    contract = ActivityResultContracts.CreateDocument("application/json"),
                ) { uri ->
                    val content = pendingExportJson
                    pendingExportJson = null
                    if (uri != null && content != null) {
                        scope.launch(Dispatchers.IO) {
                            runCatching {
                                context.contentResolver.openOutputStream(uri)?.use { out ->
                                    out.write(content.toByteArray(Charsets.UTF_8))
                                }
                            }
                        }
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

                val openUrl: (String) -> Unit = { url ->
                    runCatching {
                        context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
                    }
                }

                AppScreen(
                    state = state,
                    onImport = {
                        importLauncher.launch(
                            arrayOf(
                                "application/json",
                                "text/plain",
                                "application/octet-stream",
                            ),
                        )
                    },
                    onExportAll = {
                        viewModel.exportAllAccounts { files ->
                            pendingExportZip = files
                            exportZipLauncher.launch("wb-auths-${LocalDate.now()}.zip")
                        }
                    },
                    onStartLogin = { realm ->
                        viewModel.startLogin(realm, openUrl)
                    },
                    onOpenLoginUrl = openUrl,
                    onCancelLogin = viewModel::cancelLogin,
                    onCheckin = viewModel::checkin,
                    onCheckinAll = viewModel::checkinAll,
                    onCheckinModelChange = viewModel::setCheckinModel,
                    onCheckinPending = viewModel::checkinPending,
                    onRefreshCredits = viewModel::refreshCredits,
                    onExportAccount = { uid ->
                        viewModel.exportAccount(uid) { fileName, content ->
                            pendingExportJson = content
                            exportJsonLauncher.launch(fileName)
                        }
                    },
                    onDeleteAccount = viewModel::deleteAccount,
                    onDismissImport = viewModel::clearImportSummary,
                    onDismissInfo = viewModel::clearInfoMessage,
                    onDismissError = viewModel::clearError,
                )
            }
        }
    }
}
