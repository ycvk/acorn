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

    fun selectTab(index: Int) {
        _selectedTab.value = index
    }

    companion object {
        const val TAB_THREADS = 0
        const val TAB_APPROVALS = 1
        const val TAB_SETTINGS = 2
    }
}
