package com.wb2api.checkin.panel

import android.content.Context
import android.util.Log
import java.io.File
import java.net.InetSocketAddress
import java.net.Socket
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.concurrent.thread

/**
 * 手机内嵌面板进程：从 nativeLibraryDir 执行 libwbpanel.so，工作目录 filesDir/panel。
 * 面板与签到 App 共用 filesDir/auths，state 落在 filesDir/data。
 */
class PanelProcess(
    private val context: Context,
    private val onLog: (String) -> Unit,
) {
    private var process: Process? = null
    private var reader: Thread? = null
    private val stopping = AtomicBoolean(false)

    fun workDir(): File = File(context.filesDir, "panel").apply { mkdirs() }

    fun configFile(): File = File(workDir(), "config.json")

    fun authDir(): File = File(context.filesDir, "auths").apply { mkdirs() }

    fun stateFile(): File = File(context.filesDir, "data/state.json").also { it.parentFile?.mkdirs() }

    fun binary(): File = File(context.applicationInfo.nativeLibraryDir, "libwbpanel.so")

    /** 复用已有配置里的 Key；缺失/损坏时重新生成完整配置。 */
    fun ensureApiKey(): String {
        val file = configFile()
        val text = if (file.isFile) runCatching { file.readText() }.getOrNull() else null
        val saved = text?.let(PanelConfig::parseKey)?.takeIf(PanelConfig::validKey)
        if (text != null && saved != null) return saved
        val key = PanelConfig.newKey()
        file.writeText(
            PanelConfig.buildJson(
                authDir = authDir().absolutePath,
                stateFile = stateFile().absolutePath,
                apiKey = key,
            ),
        )
        return key
    }

    fun isRunning(): Boolean = process?.isAlive == true

    /** 启动并等待端口就绪；失败返回 false（异常只记日志，不抛）。 */
    fun start(onExit: (Int) -> Unit): Boolean {
        if (isRunning()) return true
        val bin = binary()
        if (!bin.isFile) {
            onLog("面板二进制不存在：${bin.absolutePath}")
            return false
        }
        ensureApiKey()
        stopping.set(false)
        return runCatching {
            val p = ProcessBuilder(bin.absolutePath, "-config", configFile().absolutePath)
                .directory(workDir())
                .redirectErrorStream(true)
                .start()
            process = p
            reader = thread(name = "wbpanel-log", isDaemon = true) {
                p.inputStream.bufferedReader().useLines { lines ->
                    lines.forEach { line ->
                        onLog(line)
                        Log.i(TAG, line)
                    }
                }
                val code = runCatching { p.waitFor() }.getOrDefault(-1)
                if (!stopping.get()) onExit(code)
            }
            val ready = waitReady()
            if (!ready) {
                onLog("面板未就绪，结束进程")
                stop()
            }
            ready
        }.getOrElse { error ->
            onLog("启动面板失败：${error.message}")
            stop()
            false
        }
    }

    /** 阻塞到进程退出；只能从后台线程调用（PanelController 在 IO 上调度）。 */
    fun stop() {
        stopping.set(true)
        val p = process ?: return
        process = null
        reader?.interrupt()
        reader = null
        runCatching { p.destroy() }
        val exited = runCatching { p.waitFor(3, TimeUnit.SECONDS) }.getOrDefault(false)
        if (!exited) runCatching { p.destroyForcibly() }
    }

    private fun waitReady(timeoutMs: Long = 15_000L): Boolean {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            if (!isRunning()) return false
            val ready = runCatching {
                Socket().use { it.connect(InetSocketAddress(PanelConfig.HOST, PanelConfig.PORT), 300) }
            }.isSuccess
            if (ready) return true
            Thread.sleep(200)
        }
        onLog("面板启动超时：${PanelConfig.BASE_URL} 未就绪")
        return false
    }

    private companion object {
        const val TAG = "WbPanel"
    }
}
