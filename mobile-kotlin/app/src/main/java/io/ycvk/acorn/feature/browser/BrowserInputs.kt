package io.ycvk.acorn.feature.browser

import io.ycvk.acorn.api.models.BrowserInputRequest
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.net.URI
import kotlin.math.abs
import kotlin.math.hypot

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

/** What one touch on the frame does to the page. */
enum class Touch { Tap, Scroll, Drag }

/**
 * A touch that has moved [dx] by [dy] pixels from where it went down: still
 * undecided (null) within [slop], a drag when it went mostly sideways, a
 * scroll otherwise.
 */
fun movedTouch(dx: Float, dy: Float, slop: Float): Touch? = when {
    hypot(dx, dy) <= slop -> null
    abs(dx) > abs(dy) -> Touch.Drag
    else -> Touch.Scroll
}

/**
 * Sends drags to the page in order, one request at a time. A move waiting to
 * be sent gives way to a newer one, so the page follows where the finger is
 * rather than replaying where it was; downs and ups are always sent. After a
 * move the next request waits [moveSpacingMs]. Call [add] on the main thread
 * with a [scope] that runs there.
 */
class DragSender(
    private val scope: CoroutineScope,
    private val moveSpacingMs: Long,
    private val send: suspend (BrowserInputRequest) -> Unit,
) {
    private val waiting = ArrayDeque<BrowserInputRequest>()
    private var sending: Job? = null

    fun add(request: BrowserInputRequest) {
        if (request.type == BrowserInputRequest.Type.move && waiting.lastOrNull()?.type == BrowserInputRequest.Type.move) {
            waiting.removeLast()
        }
        waiting.addLast(request)
        if (sending?.isActive != true) sending = scope.launch { drain() }
    }

    private suspend fun drain() {
        while (true) {
            val next = waiting.removeFirstOrNull() ?: return
            send(next)
            if (next.type == BrowserInputRequest.Type.move) delay(moveSpacingMs)
        }
    }
}

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
