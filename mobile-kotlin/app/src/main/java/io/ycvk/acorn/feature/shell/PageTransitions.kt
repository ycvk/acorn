package io.ycvk.acorn.feature.shell

import androidx.compose.animation.ContentTransform
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.LinearOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
import androidx.compose.runtime.Composable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.RoundRect
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Outline
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.layout.LayoutCoordinates
import androidx.compose.ui.layout.boundsInRoot
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.LayoutDirection
import io.ycvk.acorn.core.theme.AcornMotion

/** How the top of the page stack changes. */
enum class PageMove {
    /** Onto a deeper page: it slides in over the one before it, which steps back. */
    Push,

    /** Back to the page underneath: the top one shrinks away and the other comes forward. */
    Pop,

    /** A story's reading page growing out of its card on Today. */
    Grow,

    /** The reading page shrinking back into its card. */
    Shrink,
}

/** How deep [page] sits in [pages]; the tabs are 0, and a page that has left the stack was above all of it. */
fun depthOf(page: Page?, pages: List<Page>): Int =
    if (page == null) 0 else pages.indexOf(page).let { if (it < 0) Int.MAX_VALUE else it + 1 }

/** The move that takes the top of [pages] from [from] to [to]. */
fun pageMove(from: Page?, to: Page?, pages: List<Page>): PageMove = when {
    from == null && to is Page.Story -> PageMove.Grow
    from is Page.Story && to == null -> PageMove.Shrink
    depthOf(to, pages) >= depthOf(from, pages) -> PageMove.Push
    else -> PageMove.Pop
}

/**
 * How far into a pop a back gesture at [progress] holds the pages. The
 * gesture previews the first part, where the page has shrunk but is whole;
 * letting go plays the rest.
 */
fun backFraction(progress: Float): Float = progress.coerceIn(0f, 1f) * BACK_PREVIEW

private const val BACK_PREVIEW = 0.5f

/** The edge a back gesture started from; the page leaves the way the finger moves. */
enum class BackEdge(val direction: Int) { Left(1), Right(-1) }

/** Barely moves through the first half, the part a back gesture holds, then leaves. */
private val LeaveLate = CubicBezierEasing(0.7f, 0f, 1f, 1f)

internal const val PUSH_MS = 420
internal const val POP_MS = 360
internal const val GROW_MS = 460

/** The enter and exit of a [move]; a pop leaves toward the far side of [edge]. */
internal fun pageTransform(move: PageMove, edge: BackEdge, targetDepth: Float): ContentTransform {
    val transform = when (move) {
        // Pages are opaque and never fade over each other, so no two are read at once.
        PageMove.Push ->
            slideInHorizontally(AcornMotion.travel(PUSH_MS)) { it } togetherWith
                (scaleOut(AcornMotion.travel(PUSH_MS), targetScale = 0.95f) + fadeOut(tween(PUSH_MS, easing = LinearEasing), targetAlpha = 0.5f))
        // The page shrinks first and slides off late, so a held back gesture shows it whole over the page it returns to.
        PageMove.Pop ->
            (scaleIn(tween(POP_MS, easing = LinearOutSlowInEasing), initialScale = 0.95f) + fadeIn(tween(POP_MS, easing = LinearEasing), initialAlpha = 0.5f)) togetherWith
                (scaleOut(tween(POP_MS, easing = LinearOutSlowInEasing), targetScale = 0.88f) +
                    slideOutHorizontally(tween(POP_MS, easing = LeaveLate)) { edge.direction * it })
        // The reading page's own frame grows and shrinks, see [containerFrame]; Today holds still under it.
        PageMove.Grow ->
            fadeIn(tween(GROW_MS / 4, easing = LinearEasing)) togetherWith
                fadeOut(tween(GROW_MS, easing = LinearEasing), targetAlpha = 0.7f)
        PageMove.Shrink ->
            fadeIn(tween(GROW_MS, easing = LinearEasing), initialAlpha = 0.7f) togetherWith
                fadeOut(tween(GROW_MS / 4, delayMillis = GROW_MS * 3 / 4, easing = LinearEasing))
    }
    // A deeper page draws over a shallower one whichever way the stack moves.
    transform.targetContentZIndex = targetDepth
    return transform
}

/**
 * Where a page growing out of a card is drawn: scaled by [scale] from its top
 * left corner and moved to [x], [y], showing only the window [clipTop] ..
 * [clipTop] + [clipHeight] of its own height with corners of [corner].
 */
data class ContainerFrame(val scale: Float, val x: Float, val y: Float, val clipTop: Float, val clipHeight: Float, val corner: Float)

/**
 * The frame of a [width] by [height] page that has [grown] (0..1) out of the
 * card at [origin]. At 0 the window is the card and starts [bodyTop] down the
 * page, past its bar, so the page's heading sits where the card's did.
 */
fun containerFrame(origin: Rect, width: Float, height: Float, bodyTop: Float, cardCorner: Float, grown: Float): ContainerFrame {
    val at = grown.coerceIn(0f, 1f)
    fun between(from: Float, to: Float) = from + (to - from) * at
    val left = between(origin.left, 0f)
    val top = between(origin.top, 0f)
    val scale = (between(origin.right, width) - left) / width
    val skipped = between(bodyTop, 0f)
    return ContainerFrame(
        scale = scale,
        x = left,
        y = top - skipped * scale,
        clipTop = skipped,
        clipHeight = (between(origin.bottom, height) - top) / scale,
        corner = between(cardCorner, 0f) / scale,
    )
}

/** The window of a [ContainerFrame] as a layer's clip. */
internal class ContainerWindow(private val frame: ContainerFrame) : Shape {
    override fun createOutline(size: Size, layoutDirection: LayoutDirection, density: Density): Outline =
        Outline.Rounded(RoundRect(0f, frame.clipTop, size.width, frame.clipTop + frame.clipHeight, CornerRadius(frame.corner)))
}

/**
 * Where the story cards on Today sit on screen, by story id, kept by the
 * cards themselves so a reading page knows the card it grows out of and
 * shrinks back into. A card that has left the screen keeps its last place.
 */
class StoryOrigins {
    private val cards = mutableMapOf<Long, LayoutCoordinates>()
    private val last = object : LinkedHashMap<Long, Rect>() {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<Long, Rect>?): Boolean = size > REMEMBERED
    }

    fun placed(storyId: Long, coordinates: LayoutCoordinates) {
        if (storyId !in cards) cards.values.removeAll { !it.isAttached }
        cards[storyId] = coordinates
    }

    operator fun get(storyId: Long): Rect? {
        cards[storyId]?.takeIf { it.isAttached }?.let { last[storyId] = it.boundsInRoot() }
        return last[storyId]
    }

    private companion object {
        /** More than a day's briefing holds; older cards are gone from Today. */
        const val REMEMBERED = 32
    }
}

val LocalStoryOrigins = staticCompositionLocalOf<StoryOrigins?> { null }

/** Marks a story's card on Today as where its reading page grows from. */
@Composable
fun Modifier.storyOrigin(storyId: Long): Modifier {
    val origins = LocalStoryOrigins.current ?: return this
    return onGloballyPositioned { origins.placed(storyId, it) }
}
