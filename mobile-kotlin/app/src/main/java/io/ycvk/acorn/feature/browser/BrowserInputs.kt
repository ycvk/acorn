package io.ycvk.acorn.feature.browser

import java.net.URI

/** The phone's room for the page, in CSS pixels, within what the workstation accepts. */
data class PhoneScreen(val width: Int, val height: Int)

/** The page area of [widthDp] by [heightDp], where a CSS pixel is a dp as in a phone browser. */
fun phoneScreen(widthDp: Float, heightDp: Float): PhoneScreen =
    PhoneScreen(widthDp.toInt().coerceIn(240, 1280), heightDp.toInt().coerceIn(240, 2400))

/** A point on the shown frame, in the page's CSS pixels. */
data class ViewportPoint(val x: Double, val y: Double)

/**
 * Maps a touch on the frame, shown [shownWidth] by [shownHeight] pixels, to the
 * page's viewport of [frameWidth] by [frameHeight] CSS pixels.
 */
fun viewportPoint(touchX: Float, touchY: Float, shownWidth: Float, shownHeight: Float, frameWidth: Int, frameHeight: Int): ViewportPoint =
    ViewportPoint(
        x = (touchX / shownWidth * frameWidth).toDouble().coerceIn(0.0, frameWidth.toDouble()),
        y = (touchY / shownHeight * frameHeight).toDouble().coerceIn(0.0, frameHeight.toDouble()),
    )

/** Dragging the frame up by [dragPx] scrolls the page down by the matching CSS distance. */
fun scrollDistance(dragPx: Float, shownHeight: Float, frameHeight: Int): Double =
    (-dragPx / shownHeight * frameHeight).toDouble()

/** What the owner typed in the address field as an http(s) URL; a bare host gets https. Null when it is not one. */
fun navigationUrl(typed: String): String? {
    val text = typed.trim()
    if (text.isEmpty() || text.any { it.isWhitespace() }) return null
    val withScheme = if (Regex("^[a-zA-Z][a-zA-Z0-9+.-]*://").containsMatchIn(text)) text else "https://$text"
    val uri = runCatching { URI(withScheme) }.getOrNull() ?: return null
    if (uri.scheme !in setOf("http", "https") || uri.host.isNullOrEmpty() || !uri.host.contains('.') && uri.host != "localhost") return null
    return withScheme
}

/** The host of a page URL for the bar's subtitle, or the URL itself. */
fun pageHost(url: String): String = runCatching { URI(url).host }.getOrNull()?.removePrefix("www.") ?: url
