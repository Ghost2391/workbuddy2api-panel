package com.wb2api.checkin.panel

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PanelConfigTest {
    @Test
    fun keyIs48LowerHex() {
        val key = PanelConfig.newKey()
        assertEquals(48, key.length)
        assertTrue(PanelConfig.validKey(key))
        assertTrue(key.all { it in '0'..'9' || it in 'a'..'f' })
    }

    @Test
    fun jsonCarriesOnlyTheOverrides() {
        val json = PanelConfig.buildJson(
            authDir = "/data/auths",
            stateFile = "/data/state.json",
            apiKey = "a".repeat(48),
        )
        val obj = Json.parseToJsonElement(json).jsonObject
        assertEquals("127.0.0.1:7863", obj["listen"]!!.jsonPrimitive.content)
        assertEquals("/data/auths", obj["auth_dir"]!!.jsonPrimitive.content)
        assertEquals("/data/state.json", obj["state_file"]!!.jsonPrimitive.content)
        assertEquals(
            listOf("223.5.5.5", "119.29.29.29"),
            obj["dns_servers"]!!.jsonArray.map { it.jsonPrimitive.content },
        )
        assertEquals("a".repeat(48), PanelConfig.parseKey(json))
    }

    @Test
    fun badKeyOrJsonIsRejected() {
        assertFalse(PanelConfig.validKey("abc"))
        assertFalse(PanelConfig.validKey("z".repeat(48)))
        assertNull(PanelConfig.parseKey("not json"))
        assertNull(PanelConfig.parseKey("{}"))
    }
}
