package io.ycvk.acorn.core.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner

/**
 * Runs [block] each time the app comes back to the foreground, after the
 * first time; screens load on entry themselves. The system drops the app's
 * connections in the background, and the owner may have acted from a
 * notification meanwhile.
 */
@Composable
fun OnReturnToForeground(block: () -> Unit) {
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val current by rememberUpdatedState(block)
    DisposableEffect(lifecycle) {
        var started = false
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_START) {
                if (started) current() else started = true
            }
        }
        lifecycle.addObserver(observer)
        onDispose { lifecycle.removeObserver(observer) }
    }
}
