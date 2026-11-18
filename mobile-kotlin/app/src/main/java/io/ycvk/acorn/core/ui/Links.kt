package io.ycvk.acorn.core.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import java.net.URI

/**
 * [url] as the phone opens it when it is an http or https address, or null.
 * Links come from the server, which takes some from feeds and web pages, so
 * a javascript:, intent:, file: or content: link must never reach the
 * system's opener.
 */
fun webLink(url: String): String? = url.toHttpUrlOrNull()?.toString()

/** A link's host without a leading "www.", or the link itself when it has none. */
fun linkHost(url: String): String =
    runCatching { URI(url).host }.getOrNull()?.removePrefix("www.")?.takeIf { it.isNotEmpty() } ?: url

/** The source a story or a note came from, named by its host; a tap opens it. A source that is not a web address shows nothing. */
@Composable
fun SourceLink(url: String, modifier: Modifier = Modifier) {
    val target = webLink(url) ?: return
    val colors = AcornTheme.colors
    val uriHandler = LocalUriHandler.current
    Row(
        modifier = modifier
            .clip(AcornShapes.small)
            .background(colors.surfaceMuted)
            .clickable(onClickLabel = "打开原文") { uriHandler.openUri(target) }
            .heightIn(min = 40.dp)
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Outlined.Link, contentDescription = null, tint = colors.inkSoft, modifier = Modifier.size(16.dp))
        Spacer(Modifier.width(8.dp))
        Text(
            "原文 · ${linkHost(url)}",
            style = AcornTheme.type.footnote,
            color = colors.inkSoft,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}
