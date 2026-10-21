package io.ycvk.acorn.feature.shell

import io.ycvk.acorn.data.repository.OWNER_NAME_MAX_LENGTH
import io.ycvk.acorn.data.repository.ownerNameDraft
import org.junit.Assert.assertEquals
import org.junit.Test

class OwnerGateTest {
    @Test
    fun `the app stays closed until the owner has a name`() {
        assertEquals(OwnerGate.Unnamed, ownerGateFor(""))
        assertEquals(OwnerGate.Unnamed, ownerGateFor("  "))
        assertEquals(OwnerGate.Named, ownerGateFor("ycvk"))
    }

    @Test
    fun `the name field drops line breaks and stops at the server's limit`() {
        assertEquals("温遥", ownerNameDraft("温\n遥\t"))
        assertEquals(OWNER_NAME_MAX_LENGTH, ownerNameDraft("a".repeat(OWNER_NAME_MAX_LENGTH + 5)).length)
    }
}
