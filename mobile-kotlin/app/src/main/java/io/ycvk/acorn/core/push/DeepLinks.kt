package io.ycvk.acorn.core.push

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import javax.inject.Inject
import javax.inject.Singleton

/** A thread to open, and the pending action in it to bring up, if any. */
data class ThreadLink(val threadId: String, val actionId: String? = null)

/**
 * Hands a thread to open from a notification tap (MainActivity) to the shell,
 * which consumes it once the device is connected.
 */
@Singleton
class DeepLinks @Inject constructor() {
    private val _threadToOpen = MutableStateFlow<ThreadLink?>(null)
    val threadToOpen: StateFlow<ThreadLink?> = _threadToOpen.asStateFlow()

    fun openThread(link: ThreadLink) {
        _threadToOpen.value = link
    }

    fun consumed() {
        _threadToOpen.value = null
    }
}
