package io.ycvk.acorn.feature.shell

import androidx.lifecycle.ViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.core.auth.AuthController
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import javax.inject.Inject

@HiltViewModel
class ShellViewModel @Inject constructor(
    val authController: AuthController,
) : ViewModel() {

    private val _selectedTab = MutableStateFlow(0)
    val selectedTab: StateFlow<Int> = _selectedTab.asStateFlow()

    // Currently open chat thread, if any. Non-null means the shell shows the
    // ChatScreen instead of the tabbed content.
    private val _openThreadId = MutableStateFlow<String?>(null)
    val openThreadId: StateFlow<String?> = _openThreadId.asStateFlow()

    fun selectTab(index: Int) {
        _selectedTab.value = index
    }

    fun openThread(threadId: String) {
        _openThreadId.value = threadId
    }

    fun closeThread() {
        _openThreadId.value = null
    }

    companion object {
        const val TAB_THREADS = 0
        const val TAB_APPROVALS = 1
        const val TAB_SETTINGS = 2
    }
}
