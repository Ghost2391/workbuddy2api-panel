package com.wb2api.checkin.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.wb2api.checkin.AccountRow
import com.wb2api.checkin.LoginState
import com.wb2api.checkin.UiState
import com.wb2api.checkin.data.ImportSummary

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppScreen(
    state: UiState,
    onImport: () -> Unit,
    onExportAll: () -> Unit,
    onStartLogin: (String) -> Unit,
    onOpenLoginUrl: (String) -> Unit,
    onCancelLogin: () -> Unit,
    onCheckin: (String) -> Unit,
    onCheckinAll: () -> Unit,
    onCheckinPending: () -> Unit,
    onRefreshCredits: (String) -> Unit,
    onExportAccount: (String) -> Unit,
    onDeleteAccount: (String) -> Unit,
    onDismissImport: () -> Unit,
    onDismissInfo: () -> Unit,
    onDismissError: () -> Unit,
) {
    val snackbar = remember { SnackbarHostState() }
    var showLoginPicker by remember { mutableStateOf(false) }
    var detailUid by remember { mutableStateOf<String?>(null) }
    var confirmDeleteUid by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(state.error) {
        state.error?.let {
            snackbar.showSnackbar(it)
            onDismissError()
        }
    }
    LaunchedEffect(state.infoMessage) {
        state.infoMessage?.let {
            snackbar.showSnackbar(it)
            onDismissInfo()
        }
    }

    val detailRow = detailUid?.let { uid -> state.rows.firstOrNull { it.account.uid == uid } }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("WB 签到") },
                actions = {
                    TextButton(onClick = { showLoginPicker = true }, enabled = !state.busy) {
                        Text("登录")
                    }
                    TextButton(onClick = onImport, enabled = !state.busy) {
                        Text("导入")
                    }
                    if (state.rows.isNotEmpty()) {
                        TextButton(onClick = onExportAll, enabled = !state.busy) {
                            Text("导出")
                        }
                    }
                },
            )
        },
        snackbarHost = { SnackbarHost(snackbar) },
    ) { padding ->
        Column(
            modifier = Modifier
                .padding(padding)
                .fillMaxSize()
                .padding(horizontal = 16.dp),
        ) {
            Spacer(Modifier.height(8.dp))
            if (state.rows.isNotEmpty()) {
                SummaryCard(state.rows)
                Spacer(Modifier.height(10.dp))
            }
            val pendingCount = state.rows.count { row ->
                val st = row.result?.status
                st == null || st == "failed" || st == "auth_invalid"
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Button(
                    onClick = onCheckinAll,
                    enabled = !state.busy && state.rows.isNotEmpty(),
                    modifier = Modifier.weight(1f),
                ) {
                    val label = if (state.busy && state.progressTotal > 0) {
                        "签到中 ${state.progressDone}/${state.progressTotal}"
                    } else {
                        "全部签到"
                    }
                    Text(label)
                }
                if (pendingCount > 0 && pendingCount < state.rows.size) {
                    OutlinedButton(
                        onClick = onCheckinPending,
                        enabled = !state.busy,
                    ) {
                        Text("补签 ($pendingCount)")
                    }
                }
            }
            Spacer(Modifier.height(12.dp))
            when {
                state.loading -> Box(
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxWidth(),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator()
                }

                state.rows.isEmpty() -> Box(
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxWidth(),
                ) {
                    EmptyState(
                        onLogin = { showLoginPicker = true },
                        onImport = onImport,
                        enabled = !state.busy,
                    )
                }

                else -> LazyColumn(
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxWidth(),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    items(state.rows, key = { it.account.uid }) { row ->
                        AccountCard(
                            row = row,
                            enabled = !state.busy,
                            onClick = { detailUid = row.account.uid },
                            onCheckin = { onCheckin(row.account.uid) },
                        )
                    }
                    item { Spacer(Modifier.height(12.dp)) }
                }
            }
        }
    }

    state.importSummary?.let { summary ->
        ImportDialog(summary = summary, onDismiss = onDismissImport)
    }

    if (showLoginPicker) {
        LoginRealmDialog(
            onSelect = { realm ->
                showLoginPicker = false
                onStartLogin(realm)
            },
            onDismiss = { showLoginPicker = false },
        )
    }

    state.loginState?.let { login ->
        LoginPollingDialog(
            login = login,
            onOpenUrl = { onOpenLoginUrl(login.url) },
            onCancel = onCancelLogin,
        )
    }

    detailRow?.let { row ->
        AccountDetailDialog(
            row = row,
            busy = state.busy,
            onRefreshCredits = { onRefreshCredits(row.account.uid) },
            onExport = { onExportAccount(row.account.uid) },
            onDelete = { confirmDeleteUid = row.account.uid },
            onDismiss = { detailUid = null },
        )
    }

    confirmDeleteUid?.let { uid ->
        val name = state.rows.firstOrNull { it.account.uid == uid }?.account?.nickname?.ifBlank { uid.take(8) } ?: uid
        AlertDialog(
            onDismissRequest = { confirmDeleteUid = null },
            title = { Text("确认删除") },
            text = { Text("确定要删除账号「$name」吗？删除后本地账号文件和签到记录将被移除。") },
            confirmButton = {
                TextButton(
                    onClick = {
                        confirmDeleteUid = null
                        detailUid = null
                        onDeleteAccount(uid)
                    },
                ) {
                    Text("删除", color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmDeleteUid = null }) { Text("取消") }
            },
        )
    }
}

@Composable
private fun SummaryCard(rows: List<AccountRow>) {
    val doneCount = rows.count { it.result?.status == "ok" || it.result?.status == "already" }
    val pendingCount = rows.count { it.result == null }
    val failedCount = rows.count { it.result?.status == "failed" || it.result?.status == "auth_invalid" }
    val withCredits = rows.filter { it.account.hasCredits }
    val totalRemain = withCredits.sumOf { it.account.creditsRemain }
    val totalExpiring = withCredits.sumOf { it.account.creditsExpiring }

    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f)),
    ) {
        Column(modifier = Modifier.padding(12.dp)) {
            Text(
                text = "共 ${rows.size} 个账号 · 已签 $doneCount · 待签 $pendingCount · 异常 $failedCount",
                style = MaterialTheme.typography.bodyMedium,
            )
            if (withCredits.isNotEmpty()) {
                Spacer(Modifier.height(4.dp))
                val expiringText = if (totalExpiring > 0) "（7天内过期 $totalExpiring）" else ""
                Text(
                    text = "已查积分账号 ${withCredits.size} 个 · 剩余积分合计 $totalRemain$expiringText",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun AccountCard(
    row: AccountRow,
    enabled: Boolean,
    onClick: () -> Unit,
    onCheckin: () -> Unit,
) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick),
    ) {
        Row(
            modifier = Modifier.padding(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = row.account.nickname.ifBlank { row.account.uid.take(8) },
                    style = MaterialTheme.typography.titleMedium,
                )
                Spacer(Modifier.height(2.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text(
                        text = row.account.uid.take(8),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Tag(if (row.account.realm == "global") "国际" else "国内")
                    if (row.account.enterprise) Tag("企业")
                }
                if (row.account.hasCredits) {
                    Spacer(Modifier.height(4.dp))
                    val exp = if (row.account.creditsExpiring > 0) " · 7天过期 ${row.account.creditsExpiring}" else ""
                    Text(
                        text = "积分 ${row.account.creditsRemain} / ${row.account.creditsTotal}$exp",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.primary,
                    )
                }
                row.result?.let { result ->
                    Spacer(Modifier.height(4.dp))
                    val timeHint = formatShortTime(result.checkedAt)
                    val msg = if (timeHint.isNotEmpty()) "${result.message} · $timeHint" else result.message
                    Text(
                        text = msg,
                        style = MaterialTheme.typography.bodySmall,
                        color = statusColor(result.status),
                    )
                }
            }
            Column(horizontalAlignment = Alignment.End) {
                StatusChip(row.result?.status)
                Spacer(Modifier.height(6.dp))
                OutlinedButton(onClick = onCheckin, enabled = enabled) { Text("签到") }
            }
        }
    }
}

@Composable
private fun AccountDetailDialog(
    row: AccountRow,
    busy: Boolean,
    onRefreshCredits: () -> Unit,
    onExport: () -> Unit,
    onDelete: () -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = {
            Text(row.account.nickname.ifBlank { row.account.uid.take(8) })
        },
        text = {
            SelectionContainer {
                Column(
                    modifier = Modifier.verticalScroll(rememberScrollState()),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Text("UID：${row.account.uid}", style = MaterialTheme.typography.bodySmall)
                    Text(
                        "域：${if (row.account.realm == "global") "国际版" else "国内版"}" +
                            if (row.account.enterprise) "（企业账号）" else "",
                        style = MaterialTheme.typography.bodySmall,
                    )
                    if (row.account.file.isNotBlank()) {
                        Text("文件：${row.account.file}", style = MaterialTheme.typography.bodySmall)
                    }
                    Spacer(Modifier.height(4.dp))
                    val res = row.result
                    if (res != null) {
                        Text("今日状态：${statusLabel(res.status)}", style = MaterialTheme.typography.bodySmall)
                        if (res.message.isNotBlank()) {
                            Text("详情：${res.message}", style = MaterialTheme.typography.bodySmall, color = statusColor(res.status))
                        }
                        if (res.checkedAt.isNotBlank()) {
                            Text("签到时间：${res.checkedAt}", style = MaterialTheme.typography.bodySmall)
                        }
                    } else {
                        Text("今日状态：未签到", style = MaterialTheme.typography.bodySmall)
                    }
                    Spacer(Modifier.height(4.dp))
                    if (row.account.hasCredits) {
                        Text(
                            "积分余额：${row.account.creditsRemain} / ${row.account.creditsTotal}",
                            style = MaterialTheme.typography.bodySmall,
                        )
                        Text(
                            "7 天内快过期：${row.account.creditsExpiring}",
                            style = MaterialTheme.typography.bodySmall,
                        )
                        if (row.account.creditsAt.isNotBlank()) {
                            Text("积分更新：${row.account.creditsAt}", style = MaterialTheme.typography.bodySmall)
                        }
                    } else {
                        Text("积分余额：暂无数据（点击下方刷新或签到获取）", style = MaterialTheme.typography.bodySmall)
                    }
                    Spacer(Modifier.height(8.dp))
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        OutlinedButton(
                            onClick = onRefreshCredits,
                            enabled = !busy,
                            modifier = Modifier.weight(1f),
                        ) {
                            Text("刷新积分")
                        }
                        OutlinedButton(
                            onClick = onExport,
                            enabled = !busy,
                            modifier = Modifier.weight(1f),
                        ) {
                            Text("导出 JSON")
                        }
                    }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onDismiss) { Text("关闭") }
        },
        dismissButton = {
            TextButton(onClick = onDelete, enabled = !busy) {
                Text("删除账号", color = MaterialTheme.colorScheme.error)
            }
        },
    )
}

@Composable
private fun LoginRealmDialog(
    onSelect: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("登录添加账号") },
        text = { Text("请选择账号所属版本，将打开浏览器完成 WorkBuddy 授权：") },
        confirmButton = {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                TextButton(onClick = { onSelect("global") }) { Text("国际版") }
                Button(onClick = { onSelect("cn") }) { Text("国内版") }
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text("取消") }
        },
    )
}

@Composable
private fun LoginPollingDialog(
    login: LoginState,
    onOpenUrl: () -> Unit,
    onCancel: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onCancel,
        title = { Text(if (login.realm == "global") "国际版登录授权" else "国内版登录授权") },
        text = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                CircularProgressIndicator(modifier = Modifier.size(24.dp), strokeWidth = 2.dp)
                Spacer(Modifier.width(12.dp))
                Text(login.statusText, style = MaterialTheme.typography.bodyMedium)
            }
        },
        confirmButton = {
            TextButton(onClick = onOpenUrl) { Text("重新打开授权页") }
        },
        dismissButton = {
            TextButton(onClick = onCancel) { Text("取消") }
        },
    )
}

private fun statusLabel(status: String?): String = when (status) {
    null -> "未签到"
    "ok" -> "签到成功"
    "already" -> "今天已签到"
    "skipped" -> "已跳过"
    "auth_invalid" -> "凭证失效"
    "failed" -> "失败"
    else -> "未知"
}

private fun formatShortTime(iso: String): String {
    val tIdx = iso.indexOf('T')
    if (tIdx < 0 || iso.length < tIdx + 6) return ""
    return iso.substring(tIdx + 1, tIdx + 6)
}

@Composable
private fun StatusChip(status: String?) {
    val label = statusLabel(status)
    val color = when (status) {
        null -> MaterialTheme.colorScheme.onSurfaceVariant
        "ok" -> Color(0xFF2E7D32)
        "already" -> Color(0xFF1565C0)
        "skipped" -> Color(0xFFF9A825)
        else -> MaterialTheme.colorScheme.error
    }
    Box(
        modifier = Modifier
            .background(color.copy(alpha = 0.15f), RoundedCornerShape(12.dp))
            .padding(horizontal = 8.dp, vertical = 4.dp),
    ) {
        Text(label, style = MaterialTheme.typography.labelSmall, color = color)
    }
}

@Composable
private fun Tag(text: String) {
    Box(
        modifier = Modifier
            .background(MaterialTheme.colorScheme.secondaryContainer, RoundedCornerShape(4.dp))
            .padding(horizontal = 6.dp, vertical = 2.dp),
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSecondaryContainer,
        )
    }
}

@Composable
private fun statusColor(status: String): Color = when (status) {
    "ok" -> Color(0xFF2E7D32)
    "already" -> Color(0xFF1565C0)
    "skipped" -> Color(0xFFF9A825)
    "auth_invalid", "failed" -> MaterialTheme.colorScheme.error
    else -> MaterialTheme.colorScheme.onSurfaceVariant
}

@Composable
private fun EmptyState(onLogin: () -> Unit, onImport: () -> Unit, enabled: Boolean) {
    Column(
        modifier = Modifier.fillMaxSize(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text("还没有账号", style = MaterialTheme.typography.titleMedium)
        Spacer(Modifier.height(8.dp))
        Text(
            "可以直接浏览器授权登录，或把面板 auths/ 里的 workbuddy-*.json 导入手机",
            style = MaterialTheme.typography.bodyMedium,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(16.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Button(onClick = onLogin, enabled = enabled) { Text("登录账号") }
            OutlinedButton(onClick = onImport, enabled = enabled) { Text("导入 JSON") }
        }
    }
}

@Composable
private fun ImportDialog(summary: ImportSummary, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = { TextButton(onClick = onDismiss) { Text("知道了") } },
        title = { Text("导入结果") },
        text = {
            Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
                Text("成功 ${summary.ok} / 共 ${summary.total} 个文件")
                if (summary.failures.isNotEmpty()) {
                    Spacer(Modifier.height(8.dp))
                    summary.failures.forEach {
                        Text("• $it", style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        },
    )
}
