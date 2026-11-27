package com.wb2api.checkin

import android.content.Context
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import com.wb2api.checkin.data.Account
import com.wb2api.checkin.data.CheckinResult
import com.wb2api.checkin.data.EngineClient
import com.wb2api.checkin.data.GoEngineClient
import com.wb2api.checkin.data.ImportFile
import com.wb2api.checkin.data.ImportSummary
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean

data class AccountRow(
    val account: Account,
    val result: CheckinResult? = null,
)

data class LoginState(
    val realm: String,
    val state: String,
    val url: String,
    val statusText: String = "请在浏览器中完成授权…",
)

data class UiState(
    val loading: Boolean = true,
    val rows: List<AccountRow> = emptyList(),
    val busy: Boolean = false,
    val progressDone: Int = 0,
    val progressTotal: Int = 0,
    val importSummary: ImportSummary? = null,
    val loginState: LoginState? = null,
    val infoMessage: String? = null,
    val error: String? = null,
)

class CheckinViewModel(
    private val engine: EngineClient,
    private val io: CoroutineDispatcher = Dispatchers.IO,
    private val loginPollDelayMs: Long = 2500L,
) : ViewModel() {

    private val _state = MutableStateFlow(UiState())
    val state: StateFlow<UiState> = _state.asStateFlow()

    private val opLock = AtomicBoolean(false)
    private var loginJob: Job? = null

    private fun claimOperation(): Boolean {
        if (!opLock.compareAndSet(false, true)) return false
        _state.update { it.copy(busy = true) }
        return true
    }

    private fun releaseOperation() {
        _state.update { it.copy(busy = false) }
        opLock.set(false)
    }

    init {
        refresh()
    }

    fun refresh() {
        viewModelScope.launch(io) {
            _state.update { it.copy(loading = true, error = null) }
            runCatching { engine.listAccounts() }
                .onSuccess { accounts ->
                    _state.update { s ->
                        s.copy(loading = false, rows = mergeRows(accounts, s.rows))
                    }
                }
                .onFailure { e ->
                    _state.update {
                        it.copy(loading = false, error = "读取账号失败：${e.message}")
                    }
                }
        }
    }

    fun importFiles(files: List<ImportFile>) {
        if (files.isEmpty()) return
        if (!claimOperation()) {
            _state.update { it.copy(error = "正在处理中，请稍后再导入") }
            return
        }
        viewModelScope.launch(io) {
            try {
                _state.update { it.copy(error = null) }
                var ok = 0
                val failures = mutableListOf<String>()
                for (file in files) {
                    val content = file.content
                    if (content == null) {
                        failures += "${file.name}：无法读取文件"
                        continue
                    }
                    runCatching { engine.importAccount(content) }
                        .onSuccess { ok++ }
                        .onFailure { failures += "${file.name}：${it.message ?: "导入失败"}" }
                }
                _state.update {
                    it.copy(importSummary = ImportSummary(files.size, ok, failures))
                }
                runCatching { engine.listAccounts() }
                    .onSuccess { accounts ->
                        _state.update { s -> s.copy(rows = mergeRows(accounts, s.rows)) }
                    }
                    .onFailure { e ->
                        _state.update { it.copy(error = "刷新账号失败：${e.message}") }
                    }
            } finally {
                releaseOperation()
            }
        }
    }

    fun checkin(uid: String) {
        if (!claimOperation()) return
        // 单号签到不产生进度，清空上一轮「全部签到」遗留的 N/N。
        _state.update { it.copy(progressDone = 0, progressTotal = 0) }
        viewModelScope.launch(io) {
            try {
                val result = runCheckin(uid)
                _state.update { s -> s.copy(rows = s.rows.merge(result)) }
            } finally {
                releaseOperation()
            }
        }
    }

    fun checkinAll() {
        checkinTargets(_state.value.rows.map { it.account.uid })
    }

    fun checkinPending() {
        val targets = _state.value.rows
            .filter { row ->
                val st = row.result?.status
                st == null || st == "failed" || st == "auth_invalid"
            }
            .map { it.account.uid }
        if (targets.isEmpty()) {
            _state.update { it.copy(infoMessage = "所有账号今日均已处理") }
            return
        }
        checkinTargets(targets)
    }

    private fun checkinTargets(targets: List<String>) {
        if (!claimOperation()) return
        if (targets.isEmpty()) {
            releaseOperation()
            return
        }
        viewModelScope.launch(io) {
            try {
                _state.update {
                    it.copy(progressDone = 0, progressTotal = targets.size)
                }
                targets.forEachIndexed { index, uid ->
                    val result = runCheckin(uid)
                    _state.update { s ->
                        s.copy(rows = s.rows.merge(result), progressDone = index + 1)
                    }
                }
            } finally {
                releaseOperation()
            }
        }
    }

    fun refreshCredits(uid: String) {
        if (!claimOperation()) return
        viewModelScope.launch(io) {
            try {
                runCatching { engine.refreshCredits(uid) }
                    .onSuccess { updated ->
                        _state.update { s ->
                            s.copy(
                                rows = s.rows.map { row ->
                                    if (row.account.uid == uid) {
                                        row.copy(
                                            account = updated,
                                            result = updated.lastCheckin ?: row.result,
                                        )
                                    } else {
                                        row
                                    }
                                },
                                infoMessage = "积分已刷新",
                            )
                        }
                    }
                    .onFailure { e ->
                        _state.update { it.copy(error = e.message ?: "刷新积分失败") }
                    }
            } finally {
                releaseOperation()
            }
        }
    }

    fun deleteAccount(uid: String) {
        if (!claimOperation()) return
        viewModelScope.launch(io) {
            try {
                runCatching { engine.deleteAccount(uid) }
                    .onSuccess {
                        _state.update { s ->
                            s.copy(
                                rows = s.rows.filterNot { it.account.uid == uid },
                                infoMessage = "已删除账号",
                            )
                        }
                    }
                    .onFailure { e ->
                        _state.update { it.copy(error = "删除失败：${e.message ?: "未知错误"}") }
                    }
            } finally {
                releaseOperation()
            }
        }
    }

    fun exportAccount(uid: String, onReady: (fileName: String, content: String) -> Unit) {
        viewModelScope.launch(io) {
            runCatching {
                val fileName = _state.value.rows
                    .firstOrNull { it.account.uid == uid }
                    ?.account?.file
                    ?.ifBlank { "workbuddy-$uid.json" }
                    ?: "workbuddy-$uid.json"
                fileName to engine.exportAccount(uid)
            }.onSuccess { (fileName, content) ->
                withContext(Dispatchers.Main) {
                    onReady(fileName, content)
                }
            }.onFailure { e ->
                _state.update { it.copy(error = "导出失败：${e.message ?: "未知错误"}") }
            }
        }
    }

    fun exportAllAccounts(onReady: (List<Pair<String, String>>) -> Unit) {
        val rows = _state.value.rows
        if (rows.isEmpty()) {
            _state.update { it.copy(infoMessage = "没有可导出的账号") }
            return
        }
        viewModelScope.launch(io) {
            runCatching {
                rows.map { row ->
                    val name = row.account.file.ifBlank { "workbuddy-${row.account.uid}.json" }
                    name to engine.exportAccount(row.account.uid)
                }
            }.onSuccess { files ->
                withContext(Dispatchers.Main) {
                    onReady(files)
                }
            }.onFailure { e ->
                _state.update { it.copy(error = "导出失败：${e.message ?: "未知错误"}") }
            }
        }
    }

    fun startLogin(realm: String, onOpenUrl: (String) -> Unit) {
        if (!claimOperation()) return
        loginJob?.cancel()
        loginJob = viewModelScope.launch(io) {
            try {
                val started = runCatching { engine.startLogin(realm) }
                    .getOrElse { e ->
                        _state.update { it.copy(error = e.message ?: "发起登录失败") }
                        return@launch
                    }
                _state.update {
                    it.copy(
                        loginState = LoginState(
                            realm = started.realm,
                            state = started.state,
                            url = started.url,
                        ),
                    )
                }
                withContext(Dispatchers.Main) {
                    onOpenUrl(started.url)
                }

                // 最多轮询约 5 分钟（120 次 * 2.5s）
                repeat(120) {
                    if (loginPollDelayMs > 0) delay(loginPollDelayMs)
                    val poll = runCatching { engine.pollLogin(started.state) }
                        .getOrElse { e ->
                            _state.update {
                                it.copy(
                                    loginState = null,
                                    error = e.message ?: "登录轮询失败",
                                )
                            }
                            return@launch
                        }
                    if (poll.done) {
                        runCatching { engine.listAccounts() }
                            .onSuccess { accounts ->
                                _state.update { s ->
                                    s.copy(
                                        rows = mergeRows(accounts, s.rows),
                                        loginState = null,
                                        infoMessage = "登录成功：${poll.account?.nickname?.ifBlank { poll.account.uid } ?: "已添加账号"}",
                                    )
                                }
                            }
                            .onFailure { e ->
                                _state.update {
                                    it.copy(loginState = null, error = "登录成功但刷新列表失败：${e.message}")
                                }
                            }
                        return@launch
                    }
                }
                _state.update {
                    it.copy(loginState = null, error = "登录超时，请重试")
                }
            } finally {
                releaseOperation()
            }
        }
    }

    fun cancelLogin() {
        loginJob?.cancel()
        loginJob = null
        _state.update { it.copy(loginState = null) }
        releaseOperation()
    }

    fun clearImportSummary() {
        _state.update { it.copy(importSummary = null) }
    }

    fun clearInfoMessage() {
        _state.update { it.copy(infoMessage = null) }
    }

    fun clearError() {
        _state.update { it.copy(error = null) }
    }

    private fun runCheckin(uid: String): CheckinResult =
        runCatching { engine.checkin(uid) }.getOrElse { e ->
            CheckinResult(
                uid = uid,
                status = "failed",
                message = "调用失败：${e.message ?: "未知错误"}",
            )
        }

    private fun mergeRows(accounts: List<Account>, old: List<AccountRow>): List<AccountRow> {
        val byUid = old.associateBy { it.account.uid }
        return accounts.map { acc ->
            AccountRow(acc, acc.lastCheckin ?: byUid[acc.uid]?.result)
        }
    }

    companion object {
        fun Factory(context: Context): ViewModelProvider.Factory = viewModelFactory {
            initializer {
                val app = context.applicationContext
                CheckinViewModel(GoEngineClient(File(app.filesDir, "auths")))
            }
        }
    }
}

private fun List<AccountRow>.merge(result: CheckinResult): List<AccountRow> =
    map { row ->
        if (row.account.uid == result.uid) {
            val updatedAccount = if (result.hasCredits) {
                row.account.copy(
                    lastCheckin = result,
                    hasCredits = true,
                    creditsRemain = result.creditsRemain,
                    creditsTotal = result.creditsTotal,
                    creditsExpiring = result.creditsExpiring,
                    creditsAt = result.creditsAt,
                )
            } else {
                row.account.copy(lastCheckin = result)
            }
            row.copy(account = updatedAccount, result = result)
        } else {
            row
        }
    }
