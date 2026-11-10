package io.ycvk.acorn.core.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class ArrivalTest {
    @Test
    fun `pieces arrive one after another, and from the seventh on together`() {
        assertEquals(listOf(0L, 40L, 80L, 240L, 240L, 240L), listOf(0, 1, 2, 6, 7, 40).map(::arrivalDelay))
    }
}
