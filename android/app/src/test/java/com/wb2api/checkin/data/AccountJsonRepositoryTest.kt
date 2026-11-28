package com.wb2api.checkin.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AccountJsonRepositoryTest {
    @Test
    fun `file name is workbuddy uid json`() {
        assertEquals("workbuddy-abc123.json", AccountJsonOps.fileName("abc123"))
        assertEquals("workbuddy-u-1.json", AccountJsonOps.fileName("u-1"))
    }

    @Test
    fun `summary counts successes and failures`() {
        val files = listOf(
            ImportFile("a.json", "{}"),
            ImportFile("b.json", "{}"),
            ImportFile("c.json", null),
        )
        val results = listOf(
            Result.success(testAccount("a")),
            Result.failure(IllegalStateException("解析失败")),
            Result.failure(IllegalArgumentException("文件为空或无法读取")),
        )
        val summary = AccountJsonOps.summary(files, results)
        assertEquals(3, summary.total)
        assertEquals(1, summary.ok)
        assertEquals(2, summary.failures.size)
        assertTrue(summary.failures[0].contains("b.json"))
        assertTrue(summary.failures[0].contains("解析失败"))
        assertTrue(summary.failures[1].contains("c.json"))
    }

    @Test
    fun `summary with all successes has no failures`() {
        val files = listOf(ImportFile("a.json", "{}"), ImportFile("b.json", "{}"))
        val results = listOf(
            Result.success(testAccount("a")),
            Result.success(testAccount("b")),
        )
        val summary = AccountJsonOps.summary(files, results)
        assertEquals(2, summary.total)
        assertEquals(2, summary.ok)
        assertTrue(summary.failures.isEmpty())
    }

    @Test
    fun `summary with empty results`() {
        val summary = AccountJsonOps.summary(emptyList(), emptyList())
        assertEquals(0, summary.total)
        assertEquals(0, summary.ok)
        assertTrue(summary.failures.isEmpty())
    }

    private fun testAccount(uid: String) = Account(uid = uid)
}
