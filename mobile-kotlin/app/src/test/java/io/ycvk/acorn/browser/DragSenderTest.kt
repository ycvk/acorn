package io.ycvk.acorn.browser

import io.ycvk.acorn.api.models.BrowserDragPoint
import io.ycvk.acorn.api.models.BrowserInputRequest
import io.ycvk.acorn.api.models.BrowserInputRequest.Type.down
import io.ycvk.acorn.api.models.BrowserInputRequest.Type.move
import io.ycvk.acorn.api.models.BrowserInputRequest.Type.up
import io.ycvk.acorn.feature.browser.DragSender
import io.ycvk.acorn.feature.browser.MAX_DRAG_POINTS
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.currentTime
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class DragSenderTest {
    private fun down() = BrowserInputRequest(tab = "T1", type = down, x = 0.0, y = 10.0)

    private fun move(vararg t: Long) = BrowserInputRequest(tab = "T1", type = move, points = t.map { BrowserDragPoint(x = it.toDouble(), y = 10.0, t = it) })

    private fun up(t: Long) = BrowserInputRequest(tab = "T1", type = up, x = t.toDouble(), y = 10.0, t = t)

    /** A finger that makes a sample every 8 ms for [ms], delivered a frame's worth at a time like MotionEvent history. */
    private fun TestScope.finger(drags: DragSender, ms: Long) {
        for (frame in 16L..ms step 16) {
            advanceTimeBy(16)
            runCurrent()
            drags.add(move(frame - 8, frame))
        }
    }

    @Test
    fun everySampleGoesInOrderWhileRequestsAreSlow() = runTest {
        val sent = mutableListOf<BrowserInputRequest>()
        val drags = DragSender(this, 50) {
            sent += it
            delay(120)
        }
        drags.add(down())
        finger(drags, 800)
        drags.add(up(800))
        advanceUntilIdle()
        assertEquals(down, sent.first().type)
        assertEquals(up(800), sent.last())
        val moves = sent.drop(1).dropLast(1)
        assertTrue(moves.all { it.type == move })
        assertEquals((8L..800L step 8).toList(), moves.flatMap { it.points.orEmpty() }.map { it.t })
        // Each request waits for the one before it, so the moves come in a few batches.
        assertTrue("${moves.size} moves", moves.size in 6..8)
    }

    @Test
    fun batchesGoAtMostEveryBatchMsAndTheUpDoesNotWait() = runTest {
        val sent = mutableListOf<Pair<BrowserInputRequest, Long>>()
        val drags = DragSender(this, 50) { sent += it to currentTime }
        drags.add(down())
        runCurrent()
        finger(drags, 304)
        drags.add(up(304))
        advanceUntilIdle()
        val times = sent.filter { it.first.type == move }.map { it.second }
        assertEquals(listOf(16L, 66L, 116L, 166L, 216L, 266L, 304L), times)
        // The up goes with the last batch, right after the finger lifts.
        assertEquals(up(304) to 304L, sent.last())
        assertEquals((8L..304L step 8).toList(), sent.flatMap { it.first.points.orEmpty() }.map { it.t })
    }

    @Test
    fun aMoveCarriesAtMostTheServersLimit() = runTest {
        val sent = mutableListOf<BrowserInputRequest>()
        val drags = DragSender(this, 50) {
            sent += it
            delay(10_000)
        }
        drags.add(down())
        runCurrent()
        for (t in 1L..(MAX_DRAG_POINTS + 5).toLong()) drags.add(move(t))
        advanceUntilIdle()
        assertEquals(listOf(MAX_DRAG_POINTS, 5), sent.drop(1).map { it.points.orEmpty().size })
    }
}
