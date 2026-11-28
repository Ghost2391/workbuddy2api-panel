package com.wb2api.checkin.data

import java.io.File

/**
 * 账号 JSON 文件级导入导出。复用 Go 引擎的 auth 解析（importAccount/exportAccount），
 * 这些方法只读写 auths 目录文件，不启动服务、不联网。调用方须在面板停止后使用（见 PanelController.runExclusive）。
 */
class AccountJsonRepository(authDir: File) {
    private val engine: EngineClient = GoEngineClient(authDir)

    /** 批量导入单个账号 JSON：逐个导入，汇总成功与失败。 */
    fun importAccounts(files: List<ImportFile>): ImportSummary {
        val results = files.map { file ->
            val content = file.content
            if (content.isNullOrBlank()) {
                Result.failure(IllegalArgumentException("文件为空或无法读取"))
            } else {
                runCatching { engine.importAccount(content) }
            }
        }
        return AccountJsonOps.summary(files, results)
    }

    /** 导出单个账号：文件名（ZIP 条目名）到原始 JSON 内容。 */
    fun exportAccount(uid: String): Pair<String, String> =
        AccountJsonOps.fileName(uid) to engine.exportAccount(uid)

    /** 导出全部账号：枚举 uid 后逐个导出。 */
    fun exportAll(): List<Pair<String, String>> =
        engine.listAccounts().map { exportAccount(it.uid) }
}

/** 纯逻辑：文件名生成与导入汇总，可 JVM 单测。 */
object AccountJsonOps {
    fun fileName(uid: String): String = "workbuddy-$uid.json"

    fun summary(files: List<ImportFile>, results: List<Result<Account>>): ImportSummary {
        val failures = files.indices.mapNotNull { i ->
            results.getOrNull(i)?.exceptionOrNull()?.let { error ->
                val reason = error.message?.takeIf { it.isNotBlank() } ?: "导入失败"
                "${files[i].name}：$reason"
            }
        }
        val ok = results.count { it.isSuccess }
        return ImportSummary(total = files.size, ok = ok, failures = failures)
    }
}
