package io.ycvk.acorn.core.ui

import java.net.URI

/** A link's host without a leading "www.", or the link itself when it has none. */
fun linkHost(url: String): String =
    runCatching { URI(url).host }.getOrNull()?.removePrefix("www.")?.takeIf { it.isNotEmpty() } ?: url
