package com.wb2api.checkin.panel

import java.security.SecureRandom
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

/** 手机内嵌面板的固定参数与 config.json 生成（纯逻辑，可 JVM 单测）。 */
object PanelConfig {
    const val HOST = "127.0.0.1"
    const val PORT = 7863
    const val BASE_URL = "http://$HOST:$PORT"
    const val PANEL_URL = "$BASE_URL/panel"
    const val API_BASE = "$BASE_URL/v1"

    /** 24 字节 = 48 位十六进制。 */
    const val KEY_BYTES = 24

    /** Android 没有 /etc/resolv.conf，面板必须显式配 DNS。 */
    val DEFAULT_DNS = listOf("223.5.5.5", "119.29.29.29")

    fun newKey(random: SecureRandom = SecureRandom()): String {
        val bytes = ByteArray(KEY_BYTES).also(random::nextBytes)
        return bytes.joinToString("") { (it.toInt() and 0xff).toString(16).padStart(2, '0') }
    }

    fun validKey(key: String): Boolean =
        key.length == KEY_BYTES * 2 && key.all { it in '0'..'9' || it in 'a'..'f' }

    /** 只写覆盖项；其余字段由面板 Load 的默认值补齐。 */
    fun buildJson(
        authDir: String,
        stateFile: String,
        apiKey: String,
        dns: List<String> = DEFAULT_DNS,
        listen: String = "$HOST:$PORT",
    ): String = buildJsonObject {
        put("listen", JsonPrimitive(listen))
        put("api_key", JsonPrimitive(apiKey))
        put("auth_dir", JsonPrimitive(authDir))
        put("state_file", JsonPrimitive(stateFile))
        put("dns_servers", JsonArray(dns.map { JsonPrimitive(it) }))
    }.toString()

    fun parseKey(json: String): String? = runCatching {
        Json.parseToJsonElement(json).jsonObject["api_key"]?.jsonPrimitive?.content
    }.getOrNull()
}
