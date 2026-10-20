package io.ycvk.acorn.core.ui

import org.junit.Assert.assertTrue
import org.junit.Test

class OrbTest {
    @Test
    fun `every form draws finite dots inside its box, far to near`() {
        val engine = OrbEngine()
        for (state in OrbState.entries) {
            for (size in listOf(18f, 22f, 32f, 64f, 72f)) {
                val preset = resolvePreset(state.mode, size)
                for (t in listOf(0f, 0.6f, 3.7f, 41.2f)) {
                    val frame = engine.frame(preset, size, t)
                    assertTrue("$state at $size has dots", frame.count > 0)
                    var lastZ = Float.NEGATIVE_INFINITY
                    for (k in 0 until frame.count) {
                        val i = frame.order[k]
                        val where = "$state at $size, t=$t, dot $i"
                        assertTrue(where, frame.x[i].isFinite() && frame.y[i].isFinite() && frame.r[i].isFinite())
                        assertTrue(where, frame.x[i] in -1f..size + 1f && frame.y[i] in -1f..size + 1f)
                        assertTrue(where, frame.r[i] > 0f && frame.r[i] < size / 4)
                        assertTrue(where, frame.z[i] >= lastZ)
                        lastZ = frame.z[i]
                    }
                }
            }
        }
    }

    @Test
    fun `smaller orbs carry fewer dots`() {
        val engine = OrbEngine()
        for (state in OrbState.entries) {
            val small = engine.frame(resolvePreset(state.mode, 20f), 20f, 0.6f).count
            val large = engine.frame(resolvePreset(state.mode, 64f), 64f, 0.6f).count
            assertTrue("$state: $small at 20 vs $large at 64", small <= large)
        }
    }
}
