package com.wb2api.checkin.panel

import android.content.Context
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class PanelState(
    val running: Boolean = false,
    val starting: Boolean = false,
    val url: String = PanelConfig.BASE_URL,
    val panelUrl: String = PanelConfig.PANEL_URL,
    val apiBase: String = PanelConfig.API_BASE,
    val apiKey: String = "",
    val logs: List<String> = emptyList(),
    val error: String? = null,
)

/** 面板进程的全局状态机（前台服务与 UI 共用）。 */
object PanelController {
    private const val MAX_LOG_LINES = 200

    private val _state = MutableStateFlow(PanelState())
    val state: StateFlow<PanelState> = _state.asStateFlow()

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val lock = Any()
    private var process: PanelProcess? = null
    private var appContext: Context? = null
    private var attempt = 0
    private var retryJob: Job? = null

    fun start(context: Context) {
        val app = context.applicationContext
        synchronized(lock) {
            appContext = app
            // 服务被系统重建时进程可能仍在：只同步状态，不重复启动。
            if (process?.isRunning() == true) {
                _state.update { it.copy(running = true, starting = false) }
                return
            }
            if (_state.value.starting) return
            retryJob?.cancel()
            retryJob = null
            attempt = 0
            startLocked(app)
        }
    }

    private fun startLocked(app: Context) {
        _state.update { it.copy(starting = true, error = null) }
        val proc = process ?: PanelProcess(app, ::appendLog).also { process = it }
        val key = runCatching { proc.ensureApiKey() }.getOrElse { error ->
            _state.update { it.copy(starting = false, error = "生成面板配置失败：${error.message}") }
            return
        }
        _state.update { it.copy(apiKey = key) }
        scope.launch {
            val ok = proc.start { code -> onExit(code) }
            synchronized(lock) {
                if (ok) {
                    attempt = 0
                    _state.update { it.copy(running = true, starting = false) }
                } else if (_state.value.error == null) {
                    _state.update { it.copy(running = false, starting = false, error = "面板启动失败") }
                }
            }
        }
    }

    private fun onExit(code: Int) {
        synchronized(lock) {
            _state.update { it.copy(running = false, starting = false) }
            val app = appContext ?: return
            if (!PanelBackoff.shouldRestart(attempt)) {
                appendLog("面板已退出（code=$code），不再重试")
                _state.update { it.copy(error = "面板异常退出（code=$code）") }
                return
            }
            attempt++
            val waitMs = PanelBackoff.delayMs(attempt)
            appendLog("面板退出（code=$code），${waitMs}ms 后第 $attempt 次重启")
            retryJob?.cancel()
            retryJob = scope.launch {
                delay(waitMs)
                synchronized(lock) {
                    if (!_state.value.running && appContext != null) startLocked(app)
                }
            }
        }
    }

    fun stop() {
        synchronized(lock) {
            retryJob?.cancel()
            retryJob = null
            attempt = 0
            process?.stop()
            _state.update { it.copy(running = false, starting = false, error = null) }
        }
    }

    fun clearError() {
        _state.update { it.copy(error = null) }
    }

    private fun appendLog(line: String) {
        val clean = redact(line)
        _state.update { state ->
            state.copy(logs = (state.logs + clean).takeLast(MAX_LOG_LINES))
        }
    }

    private fun redact(line: String): String = line
        .replace(API_KEY_RE) { m -> m.groupValues[1] + "***" }
        .replace(BEARER_RE) { m -> m.groupValues[1] + "***" }
        .replace(SK_RE, "sk-***")

    private val API_KEY_RE = Regex("""(?i)(api[_-]?key["']?\s*[:=]\s*["']?)[^\s"',}]+""")
    private val BEARER_RE = Regex("""(?i)(bearer\s+)[^\s"',}]+""")
    private val SK_RE = Regex("""sk-[A-Za-z0-9_-]{8,}""")
}
