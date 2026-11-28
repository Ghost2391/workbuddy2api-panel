package com.wb2api.checkin.panel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PanelBackoffTest {
    @Test
    fun delaysDoubleUpToSixteenSeconds() {
        assertEquals(1_000L, PanelBackoff.delayMs(1))
        assertEquals(2_000L, PanelBackoff.delayMs(2))
        assertEquals(4_000L, PanelBackoff.delayMs(3))
        assertEquals(8_000L, PanelBackoff.delayMs(4))
        assertEquals(16_000L, PanelBackoff.delayMs(5))
    }

    @Test
    fun stopsAfterFiveAttempts() {
        assertTrue(PanelBackoff.shouldRestart(0))
        assertTrue(PanelBackoff.shouldRestart(4))
        assertFalse(PanelBackoff.shouldRestart(5))
    }
}
