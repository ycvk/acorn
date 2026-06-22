package io.ycvk.acorn.api.models

import com.squareup.moshi.Moshi
import io.ycvk.acorn.api.infrastructure.Serializer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MessagePartAdapterTest {

    private val moshi: Moshi = Serializer.moshi
    private val adapter = MessagePartAdapter(moshi)

    @Test
    fun `deserialize text message part`() {
        val json = """{"kind":"text","text":"Hello, world!"}"""
        val part = adapter.fromJson(json)
        assertTrue("expected TextMessagePart, got ${part?.javaClass}", part is TextMessagePart)
        assertEquals("Hello, world!", (part as TextMessagePart).text)
    }

    @Test
    fun `deserialize reasoning message part`() {
        val json = """{"kind":"reasoning","reasoning":"I should help the user."}"""
        val part = adapter.fromJson(json)
        assertTrue("expected ReasoningMessagePart, got ${part?.javaClass}", part is ReasoningMessagePart)
        assertEquals("I should help the user.", (part as ReasoningMessagePart).reasoning)
    }

    @Test
    fun `deserialize result message part`() {
        val json = """{"kind":"result","status":"completed","title":"read_file","summary":"Read 100 lines","changed":[],"verified":[],"risks":[]}"""
        val part = adapter.fromJson(json)
        assertTrue("expected ResultMessagePart, got ${part?.javaClass}", part is ResultMessagePart)
    }

    @Test
    fun `serialize and deserialize text part round trip`() {
        val original = TextMessagePart(
            kind = TextMessagePart.Kind.text,
            text = "Round trip text",
        )
        val json = adapter.toJson(original)
        val deserialized = adapter.fromJson(json) as TextMessagePart
        assertEquals(original.text, deserialized.text)
        assertEquals(original.kind, deserialized.kind)
    }

    @Test(expected = IllegalArgumentException::class)
    fun `unknown kind throws IllegalArgumentException`() {
        val json = """{"kind":"nonexistent","text":"oops"}"""
        adapter.fromJson(json)
    }

    @Test
    fun `deserialize null returns null`() {
        val result = adapter.fromJson("null")
        assertNull(result)
    }
}
