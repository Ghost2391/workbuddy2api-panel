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
    private var stopJob: Job? = null

    /** 每次 start/stop 递增；启动协程与退出回调据此丢弃过期结果。 */
    private var generation = 0

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
            generation++
            startLocked(app, generation)
        }
    }

    private fun startLocked(app: Context, gen: Int) {
        _state.update { it.copy(starting = true, error = null) }
        val priorStop = stopJob
        val proc = process ?: PanelProcess(app, ::appendLog).also { process = it }
        val key = runCatching { proc.ensureApiKey() }.getOrElse { error ->
            _state.update { it.copy(starting = false, error = "生成面板配置失败：${error.message}") }
            return
        }
        _state.update { it.copy(apiKey = key) }
        scope.launch {
            // 等上一次停止真正退出，避免新旧进程争抢 7863 端口。
            priorStop?.join()
            // 生成代已变（用户再次 stop）则中止，绝不启动新进程。
            if (stale(gen)) {
                _state.update { it.copy(running = false, starting = false) }
                return@launch
            }
            val ok = proc.start { code -> onExit(gen, code) }
            var killStarted = false
            synchronized(lock) {
                if (stale(gen)) {
                    killStarted = ok
                    _state.update { it.copy(running = false, starting = false) }
                } else if (ok) {
                    attempt = 0
                    _state.update { it.copy(running = true, starting = false) }
                } else if (_state.value.error == null) {
                    _state.update { it.copy(running = false, starting = false, error = "面板启动失败") }
                }
            }
            // 启动过程中被停止：在锁外杀掉刚拉起的进程，不留孤儿也不长时间持锁。
            if (killStarted) proc.stop()
        }
    }

    /** 内部加锁，调用方可持有或不持有 lock（synchronized 可重入）。 */
    private fun stale(gen: Int): Boolean = synchronized(lock) { gen != generation }

    private fun onExit(gen: Int, code: Int) {
        synchronized(lock) {
            if (stale(gen)) return
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
                    if (!stale(gen) && !_state.value.running && appContext != null) {
                        startLocked(app, gen)
                    }
                }
            }
        }
    }

    fun stop() {
        synchronized(lock) {
            retryJob?.cancel()
            retryJob = null
            attempt = 0
            generation++
            val proc = process
            process = null
            _state.update { it.copy(running = false, starting = false, error = null) }
            if (proc != null) stopJob = scope.launch { proc.stop() }
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
