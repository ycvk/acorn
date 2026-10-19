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

/** Where the finger was during a drag, [t] milliseconds after it went down. */
data class DragSample(val at: ViewportPoint, val t: Long)

/** The most points one move carries, as the server accepts. */
const val MAX_DRAG_POINTS = 1000

/**
 * Sends drags to the page in order, one request at a time, without dropping a
 * point. Samples that come while a request is out join the waiting move, and
 * moves go at most every [batchMs] until the finger lifts; then the last move
 * and the up go at once. The page plays the samples at the times the finger
 * made them. Call [add] on the main thread with a [scope] that runs there.
 */
class DragSender(
    private val scope: CoroutineScope,
    private val batchMs: Long,
    private val send: suspend (BrowserInputRequest) -> Unit,
) {
    private val waiting = ArrayDeque<BrowserInputRequest>()
    private var sending: Job? = null
    private var gap: Job? = null

    fun add(request: BrowserInputRequest) {
        val last = waiting.lastOrNull()
        val joined = last?.points.orEmpty() + request.points.orEmpty()
        if (request.type == BrowserInputRequest.Type.move && last?.type == BrowserInputRequest.Type.move && joined.size <= MAX_DRAG_POINTS) {
            waiting[waiting.lastIndex] = last.copy(points = joined)
        } else {
            waiting.addLast(request)
        }
        if (request.type != BrowserInputRequest.Type.move) gap?.cancel()
        if (sending?.isActive != true) sending = scope.launch { drain() }
    }

    private suspend fun drain() {
        while (true) {
            val next = waiting.firstOrNull() ?: return
            if (next.type == BrowserInputRequest.Type.move) {
                // The move waits here for the batch's time and gathers what comes meanwhile.
                gap?.join()
                gap = scope.launch { delay(batchMs) }
            }
            send(waiting.removeFirst())
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
