package io.ycvk.acorn.api.models

import com.squareup.moshi.JsonAdapter
import com.squareup.moshi.JsonReader
import com.squareup.moshi.JsonWriter
import com.squareup.moshi.Moshi

/**
 * Moshi adapter for the generated [MessagePart] oneOf interface.
 *
 * The OpenAPI generator emits MessagePart as an interface with concrete subtypes
 * (TextMessagePart, ReasoningMessagePart, etc.) discriminated by the `kind` JSON
 * field. However, the generated subtypes do NOT implement the MessagePart interface,
 * so Moshi cannot deserialize to it directly.
 *
 * This adapter peeks the `kind` field, selects the matching data class, and delegates
 * to its own adapter. It returns `Any?` because the subtypes don't share a common
 * supertype. Callers use `filterIsInstance<TextMessagePart>()` etc. to access
 * typed fields.
 *
 * Registered as a Factory for `MessagePart::class.java` so Moshi uses it whenever
 * a `MessagePart` field is encountered during deserialization.
 */
class MessagePartAdapter(private val moshi: Moshi) : JsonAdapter<Any>() {

    @Suppress("UNCHECKED_CAST")
    private val kindAdapters: Map<String, JsonAdapter<*>> = mapOf(
        "text" to moshi.adapter(TextMessagePart::class.java),
        "reasoning" to moshi.adapter(ReasoningMessagePart::class.java),
        "result" to moshi.adapter(ResultMessagePart::class.java),
        "decision" to moshi.adapter(DecisionMessagePart::class.java),
        "disclosure" to moshi.adapter(DisclosureMessagePart::class.java),
        "work_status" to moshi.adapter(WorkStatusMessagePart::class.java),
        "technical_detail_link" to moshi.adapter(TechnicalDetailLinkMessagePart::class.java),
    )

    override fun fromJson(reader: JsonReader): Any? {
        if (reader.peek() == JsonReader.Token.NULL) {
            reader.nextNull<Any>()
            return null
        }

        val peeked = reader.peekJson()
        peeked.beginObject()
        var kind = ""
        while (peeked.hasNext()) {
            if (peeked.nextName() == "kind") {
                kind = peeked.nextString()
                break
            }
            peeked.skipValue()
        }
        peeked.close()

        val adapter = kindAdapters[kind]
            ?: throw IllegalArgumentException("Unknown MessagePart kind: $kind")

        return adapter.fromJson(reader)
    }

    override fun toJson(writer: JsonWriter, value: Any?) {
        if (value == null) {
            writer.nullValue()
            return
        }
        moshi.adapter(value.javaClass).toJson(writer, value)
    }

    companion object {
        val FACTORY: Factory = Factory { type, _, moshi ->
            if (type === MessagePart::class.java) {
                MessagePartAdapter(moshi)
            } else {
                null
            }
        }
    }
}
