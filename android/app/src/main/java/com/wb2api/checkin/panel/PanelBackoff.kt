package com.wb2api.checkin.panel

/** 面板异常退出的重启退避：1s、2s、4s、8s、16s，最多 5 次。 */
object PanelBackoff {
    const val MAX_ATTEMPTS = 5

    fun delayMs(attempt: Int): Long = when {
        attempt <= 1 -> 1_000L
        attempt >= MAX_ATTEMPTS -> 16_000L
        else -> 1_000L shl (attempt - 1)
    }

    fun shouldRestart(attempt: Int): Boolean = attempt < MAX_ATTEMPTS
}
