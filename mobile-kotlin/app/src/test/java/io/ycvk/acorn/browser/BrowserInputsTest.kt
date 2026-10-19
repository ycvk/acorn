package io.ycvk.acorn.browser

import io.ycvk.acorn.feature.browser.PhoneScreen
import io.ycvk.acorn.feature.browser.Touch
import io.ycvk.acorn.feature.browser.ViewportPoint
import io.ycvk.acorn.feature.browser.movedTouch
import io.ycvk.acorn.feature.browser.navigationUrl
import io.ycvk.acorn.feature.browser.pageHost
import io.ycvk.acorn.feature.browser.phoneScreen
import io.ycvk.acorn.feature.browser.scrollDistance
import io.ycvk.acorn.feature.browser.viewportPoint
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class BrowserInputsTest {
    @Test
    fun phoneScreensStayInRange() {
        assertEquals(PhoneScreen(411, 620), phoneScreen(411.4f, 620.9f))
        assertEquals(PhoneScreen(240, 2400), phoneScreen(120f, 3000f))
    }

    @Test
    fun touchesMapToTheViewport() {
        assertEquals(ViewportPoint(640.0, 450.0), viewportPoint(540f, 380f, 1080f, 760f, 1280, 900))
        assertEquals(ViewportPoint(0.0, 900.0), viewportPoint(-3f, 800f, 1080f, 760f, 1280, 900))
    }

    @Test
    fun draggingUpScrollsDown() {
        assertEquals(450.0, scrollDistance(-380f, 760f, 900), 0.001)
        assertEquals(-90.0, scrollDistance(76f, 760f, 900), 0.001)
    }

    @Test
    fun touchesAreDecidedByHowTheyFirstMove() {
        assertNull(movedTouch(3f, -4f, 8f))
        assertEquals(Touch.Drag, movedTouch(-12f, 5f, 8f))
        assertEquals(Touch.Scroll, movedTouch(5f, 12f, 8f))
        assertEquals(Touch.Scroll, movedTouch(9f, -9f, 8f))
    }

    @Test
    fun addressesBecomeHttpUrls() {
        assertEquals("https://github.com/login", navigationUrl("  github.com/login "))
        assertEquals("http://localhost:8080", navigationUrl("http://localhost:8080"))
        assertEquals("https://localhost", navigationUrl("localhost"))
        assertNull(navigationUrl(""))
        assertNull(navigationUrl("hello world"))
        assertNull(navigationUrl("hello"))
        assertNull(navigationUrl("file:///etc/passwd"))
        assertNull(navigationUrl("javascript://alert(1)"))
    }

    @Test
    fun hostsDropWww() {
        assertEquals("github.com", pageHost("https://www.github.com/login"))
        assertEquals("about:blank", pageHost("about:blank"))
    }
}
