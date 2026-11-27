package com.wb2api.checkin

import com.wb2api.checkin.data.Account
import com.wb2api.checkin.data.CheckinResult
import com.wb2api.checkin.data.PollLoginResult
import com.wb2api.checkin.data.StartLoginResult
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class EngineJsonTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun `decodes account with persisted checkin and credits`() {
        val raw = """{"uid":"u1","nickname":"昵称","realm":"global","enterprise":true,"file":"workbuddy-u1.json","has_credits":true,"credits_remain":320,"credits_total":500,"credits_expiring":100,"last_checkin":{"uid":"u1","status":"ok","message":"签到成功","checked_at":"2026-11-27T09:00:00+08:00"},"extra":1}"""
        val a = json.decodeFromString<Account>(raw)
        assertEquals("u1", a.uid)
        assertEquals("昵称", a.nickname)
        assertEquals("global", a.realm)
        assertTrue(a.enterprise)
        assertTrue(a.hasCredits)
        assertEquals(320L, a.creditsRemain)
        assertEquals(500L, a.creditsTotal)
        assertEquals(100L, a.creditsExpiring)
        assertNotNull(a.lastCheckin)
        assertEquals("ok", a.lastCheckin!!.status)
    }

    @Test
    fun `decodes checkin result checked_at and credits`() {
        val raw = """{"uid":"u1","status":"ok","message":"签到成功","checked_at":"2026-11-21T09:00:00+08:00","has_credits":true,"credits_remain":150,"credits_total":200}"""
        val r = json.decodeFromString<CheckinResult>(raw)
        assertEquals("ok", r.status)
        assertEquals("2026-11-21T09:00:00+08:00", r.checkedAt)
        assertTrue(r.hasCredits)
        assertEquals(150L, r.creditsRemain)
    }

    @Test
    fun `decodes login start and poll results`() {
        val start = json.decodeFromString<StartLoginResult>("""{"state":"s1","url":"https://example.com","realm":"cn"}""")
        assertEquals("s1", start.state)
        assertEquals("https://example.com", start.url)

        val poll = json.decodeFromString<PollLoginResult>("""{"done":true,"account":{"uid":"u9","nickname":"新号"}}""")
        assertTrue(poll.done)
        assertEquals("u9", poll.account?.uid)
    }
}
