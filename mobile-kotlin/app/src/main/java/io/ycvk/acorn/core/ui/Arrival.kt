package io.ycvk.acorn.core.ui

import androidx.compose.animation.core.Animatable
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.core.theme.AcornMotion
import kotlinx.coroutines.delay

/**
 * A page's content taking the place of its skeleton. What is composed in the
 * frames right after the wait ends is that content; each piece of it gets the
 * next place in line, so the pieces come one after another.
 */
class Arrival internal constructor() {
    internal var waited = false
    internal var landing = false

    /** With animations turned off in the system the content is simply there. */
    internal var still = false
    private var taken = 0

    internal fun begin() {
        landing = !still
        taken = 0
    }

    /** How long the next piece holds back, in milliseconds. */
    internal fun nextDelay(): Long = arrivalDelay(taken++)
}

/** The delay of the piece at [place] in line; from [ARRIVAL_LINE] on they come together. */
fun arrivalDelay(place: Int): Long = place.coerceIn(0, ARRIVAL_LINE) * ARRIVAL_STEP_MS

private const val ARRIVAL_LINE = 6
private const val ARRIVAL_STEP_MS = 40L
private const val ARRIVAL_MS = 320
private val ArrivalRise = 14.dp

/**
 * Follows a page's load: while it is [waiting] the page shows its skeleton,
 * and what replaces the skeleton arrives through [arrive]. A page that never
 * waited, such as one opened again with its content at hand, shows it at once.
 */
@Composable
fun rememberArrival(waiting: Boolean): Arrival {
    val arrival = remember { Arrival() }
    arrival.still = rememberReducedMotion()
    if (waiting) arrival.waited = true
    remember(waiting) { if (!waiting && arrival.waited) arrival.begin() }
    LaunchedEffect(waiting) {
        if (!arrival.landing) return@LaunchedEffect
        // A list's rows are composed as it is laid out, a frame after its data comes.
        repeat(2) { withFrameNanos { } }
        arrival.landing = false
        arrival.waited = false
    }
    return arrival
}

/** A piece of the content that replaces a skeleton: it rises a short way into place as it fades in, after the pieces before it. */
@Composable
fun Modifier.arrive(arrival: Arrival): Modifier {
    val wait = remember { if (arrival.landing) arrival.nextDelay() else null } ?: return this
    val shown = remember { Animatable(0f) }
    var arrived by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        delay(wait)
        shown.animateTo(1f, AcornMotion.travel(ARRIVAL_MS))
        arrived = true
    }
    if (arrived) return this
    return graphicsLayer {
        alpha = shown.value
        translationY = (1f - shown.value) * ArrivalRise.toPx()
    }
}
