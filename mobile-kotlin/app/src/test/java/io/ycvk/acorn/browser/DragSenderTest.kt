package io.ycvk.acorn.browser

import io.ycvk.acorn.api.models.BrowserInputRequest
import io.ycvk.acorn.api.models.BrowserInputRequest.Type.down
import io.ycvk.acorn.api.models.BrowserInputRequest.Type.move
import io.ycvk.acorn.api.models.BrowserInputRequest.Type.up
import io.ycvk.acorn.feature.browser.DragSender
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.currentTime
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class DragSenderTest {
    private fun at(type: BrowserInputRequest.Type, x: Double) = BrowserInputRequest(tab = "T1", type = type, x = x, y = 10.0)

    @Test
    fun movesWaitingBehindARequestGiveWayToTheLatest() = runTest {
        val sent = mutableListOf<BrowserInputRequest>()
        val drags = DragSender(this, 50) {
            sent += it
            delay(100)
        }
        drags.add(at(down, 1.0))
        runCurrent()
        drags.add(at(move, 2.0))
        drags.add(at(move, 3.0))
        drags.add(at(move, 4.0))
        drags.add(at(up, 5.0))
        advanceUntilIdle()
        assertEquals(listOf(down to 1.0, move to 4.0, up to 5.0), sent.map { it.type to it.x })
    }

    @Test
    fun movesAreSpacedApart() = runTest {
        val sent = mutableListOf<Triple<BrowserInputRequest.Type, Double?, Long>>()
        val drags = DragSender(this, 50) { sent += Triple(it.type, it.x, currentTime) }
        drags.add(at(down, 1.0))
        runCurrent()
        drags.add(at(move, 2.0))
        runCurrent()
        drags.add(at(move, 3.0))
        advanceTimeBy(20)
        drags.add(at(move, 4.0))
        drags.add(at(up, 5.0))
        advanceUntilIdle()
        assertEquals(
            listOf(Triple(down, 1.0, 0L), Triple(move, 2.0, 0L), Triple(move, 4.0, 50L), Triple(up, 5.0, 100L)),
            sent,
        )
    }
}
