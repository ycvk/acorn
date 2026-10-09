package io.ycvk.acorn.core.push

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Hands a thread to open from a notification tap (MainActivity) to the shell,
 * which consumes it once the device is connected.
 */
@Singleton
class DeepLinks @Inject constructor() {
    private val _threadToOpen = MutableStateFlow<String?>(null)
    val threadToOpen: StateFlow<String?> = _threadToOpen.asStateFlow()

    fun openThread(threadId: String) {
        _threadToOpen.value = threadId
    }

    fun consumed() {
        _threadToOpen.value = null
    }
}
