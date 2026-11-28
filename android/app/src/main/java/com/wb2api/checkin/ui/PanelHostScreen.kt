package com.wb2api.checkin.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.wb2api.checkin.panel.PanelConfig
import com.wb2api.checkin.panel.PanelState

/**
 * 「手机面板」主界面：启动/停止本机面板、展示地址与 Key、打开网页面板、查看日志。
 * 账号管理等复杂操作走面板自带的移动网页。
 */
@Composable
fun PanelHostScreen(
    state: PanelState,
    busy: Boolean = false,
    onStart: () -> Unit,
    onStop: () -> Unit,
    onClearError: () -> Unit,
    onOpenPanel: () -> Unit,
    onCopyKey: () -> Unit,
    onImport: () -> Unit,
    onExportAll: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("手机面板", style = MaterialTheme.typography.headlineSmall)
        Text(
            "在本机 127.0.0.1 托管 workbuddy2api 面板，账号、签到与积分都在面板网页里操作。dsh-Go 把它当作 OpenAI 兼容的模型服务使用。",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        StatusCard(state = state, onStart = onStart, onStop = onStop, onOpenPanel = onOpenPanel)

        if (state.error != null) {
            ErrorBanner(message = state.error, onClear = onClearError)
        }

        EndpointCard(state = state, onCopyKey = onCopyKey)
        AccountDataCard(
            busy = busy,
            onImport = onImport,
            onExportAll = onExportAll,
        )
        DshGoCard()
        LogCard(logs = state.logs)
    }
}

@Composable
private fun StatusCard(
    state: PanelState,
    onStart: () -> Unit,
    onStop: () -> Unit,
    onOpenPanel: () -> Unit,
) {
    val statusColor = when {
        state.error != null -> MaterialTheme.colorScheme.error
        state.running -> Color(0xFF2E7D32)
        state.starting -> MaterialTheme.colorScheme.tertiary
        else -> MaterialTheme.colorScheme.outline
    }
    val statusText = when {
        state.error != null -> "异常"
        state.starting -> "启动中…"
        state.running -> "运行中"
        else -> "已停止"
    }
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(
                    modifier = Modifier
                        .size(10.dp)
                        .background(statusColor, CircleShape),
                )
                Spacer(modifier = Modifier.size(8.dp))
                Text(statusText, style = MaterialTheme.typography.titleMedium)
                Spacer(modifier = Modifier.size(8.dp))
                Text(
                    state.url,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (state.running || state.starting) {
                    Button(onClick = onStop) { Text("停止面板") }
                } else {
                    Button(onClick = onStart) { Text("启动面板") }
                }
                OutlinedButton(onClick = onOpenPanel, enabled = state.running) {
                    Text("打开网页面板")
                }
            }
        }
    }
}

@Composable
private fun ErrorBanner(message: String, onClear: () -> Unit) {
    Surface(
        color = MaterialTheme.colorScheme.errorContainer,
        shape = MaterialTheme.shapes.medium,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Row(modifier = Modifier.padding(start = 16.dp, end = 8.dp, top = 4.dp, bottom = 4.dp)) {
            Text(
                message,
                modifier = Modifier
                    .weight(1f)
                    .padding(vertical = 12.dp),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onErrorContainer,
            )
            TextButton(onClick = onClear) { Text("知道了") }
        }
    }
}

@Composable
private fun EndpointCard(state: PanelState, onCopyKey: () -> Unit) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("连接信息", style = MaterialTheme.typography.titleMedium)
            InfoRow("网页面板", PanelConfig.PANEL_URL)
            InfoRow("OpenAI 兼容", PanelConfig.API_BASE)
            Text("API Key", style = MaterialTheme.typography.labelMedium)
            if (state.apiKey.isBlank()) {
                Text("启动面板后自动生成", style = MaterialTheme.typography.bodySmall)
            } else {
                SelectionContainer {
                    Text(
                        state.apiKey,
                        style = MaterialTheme.typography.bodySmall,
                        fontFamily = FontFamily.Monospace,
                        overflow = TextOverflow.Ellipsis,
                        maxLines = 2,
                    )
                }
                TextButton(onClick = onCopyKey) { Text("复制 Key") }
                Text(
                    "打开网页面板时粘贴同一个 Key 登录，浏览器会记住。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun AccountDataCard(
    busy: Boolean,
    onImport: () -> Unit,
    onExportAll: () -> Unit,
) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("账号 JSON", style = MaterialTheme.typography.titleMedium)
            Text(
                "导入单个账号 JSON（可多选），或导出全部账号为 ZIP。导出文件含明文凭证，请妥善保管。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = onImport, enabled = !busy) { Text("导入 JSON") }
                OutlinedButton(onClick = onExportAll, enabled = !busy) { Text("导出全部 ZIP") }
            }
            Text(
                "导入前会暂停面板，完成后自动恢复；导出不影响面板运行。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun DshGoCard() {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("在 dsh-Go 中使用", style = MaterialTheme.typography.titleMedium)
            Text(
                "在 dsh-Go 的 Provider 里选择 WorkBuddy 预设（或任意 OpenAI 兼容），填写：",
                style = MaterialTheme.typography.bodySmall,
            )
            Text(
                "Base URL：${PanelConfig.API_BASE}\nAPI Key：上面复制的 Key",
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
            )
            Text(
                "dsh-Go 的 PRoot 与应用共享 127.0.0.1，无需改动网络。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun LogCard(logs: List<String>) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("最近日志", style = MaterialTheme.typography.titleMedium)
            if (logs.isEmpty()) {
                Text(
                    "暂无日志",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            } else {
                SelectionContainer {
                    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                        logs.takeLast(MAX_VISIBLE_LOGS).forEach { line ->
                            Text(
                                line,
                                style = MaterialTheme.typography.bodySmall.copy(fontSize = 11.sp),
                                fontFamily = FontFamily.Monospace,
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun InfoRow(label: String, value: String) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(
            "$label：",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        SelectionContainer {
            Text(
                value,
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
            )
        }
    }
}

private const val MAX_VISIBLE_LOGS = 20
