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
 * field. Moshi cannot deserialize interfaces on its own, so this adapter peeks
 * the `kind` field, selects the matching data class, and delegates.
 */
class MessagePartAdapter(private val moshi: Moshi) : JsonAdapter<MessagePart>() {

    private val kindAdapters: Map<String, JsonAdapter<*>> = mapOf(
        "text" to moshi.adapter(TextMessagePart::class.java),
        "reasoning" to moshi.adapter(ReasoningMessagePart::class.java),
        "result" to moshi.adapter(ResultMessagePart::class.java),
        "decision" to moshi.adapter(DecisionMessagePart::class.java),
        "disclosure" to moshi.adapter(DisclosureMessagePart::class.java),
        "work_status" to moshi.adapter(WorkStatusMessagePart::class.java),
        "technical_detail_link" to moshi.adapter(TechnicalDetailLinkMessagePart::class.java),
    )

    @Suppress("UNCHECKED_CAST")
    override fun fromJson(reader: JsonReader): MessagePart? {
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

        return adapter.fromJson(reader) as? MessagePart
    }

    override fun toJson(writer: JsonWriter, value: MessagePart?) {
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
