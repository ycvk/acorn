package io.ycvk.acorn.feature.shell

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.push.DeepLinks
import io.ycvk.acorn.core.push.PushManager
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.inject.Inject

enum class Tab { Today, Threads, Knowledge }

/** A page pushed over the tabs. A chat without a thread id is a new conversation. */
sealed interface Page {
    val key: String

    /** [actionId] is a pending action in the thread to bring up when the chat opens. */
    data class Chat(val threadId: String?, val actionId: String? = null, val nonce: Long = System.nanoTime()) : Page {
        override val key = "chat:${threadId ?: "new-$nonce"}"
    }

    data class Note(val path: String) : Page {
        override val key = "note:$path"
    }

    data class Memory(val id: String) : Page {
        override val key = "memory:$id"
    }

    data object Settings : Page {
        override val key = "settings"
    }

    data object Persona : Page {
        override val key = "persona"
    }

    data class PersonaRevision(val revision: Long) : Page {
        override val key = "persona:$revision"
    }

    data object Skills : Page {
        override val key = "skills"
    }

    data class Skill(val name: String) : Page {
        override val key = "skill:$name"
    }

    data class SkillRevision(val name: String, val revision: Long) : Page {
        override val key = "skill:$name:$revision"
    }

    /** The agent's browser; [openUrl] is a page to open in it when the screen shows. */
    data class Browser(val openUrl: String? = null) : Page {
        override val key = "browser"
    }
}

@HiltViewModel
class ShellViewModel @Inject constructor(
    val authController: AuthController,
    private val pushManager: PushManager,
    private val deepLinks: DeepLinks,
) : ViewModel() {

    private val _tab = MutableStateFlow(Tab.Today)
    val tab: StateFlow<Tab> = _tab.asStateFlow()

    private val _pages = MutableStateFlow<List<Page>>(emptyList())
    val pages: StateFlow<List<Page>> = _pages.asStateFlow()

    private val _pendingCount = MutableStateFlow(0)
    val pendingCount: StateFlow<Int> = _pendingCount.asStateFlow()

    init {
        viewModelScope.launch {
            // A notification tap opens its thread once the device is connected;
            // on a cold start the tap arrives before the stored connection loads.
            combine(deepLinks.threadToOpen, authController.authState) { link, auth ->
                link.takeIf { auth is AuthState.Connected }
            }.collect { link ->
                if (link != null) {
                    _pages.value = listOfNotNull(Page.Chat(link.threadId, link.actionId), link.browserUrl?.let { Page.Browser(it) })
                    deepLinks.consumed()
                }
            }
        }
        viewModelScope.launch {
            authController.authState.collect { state ->
                if (state is AuthState.Connected) {
                    pollPendingCount()
                } else {
                    _pendingCount.value = 0
                    _pages.value = emptyList()
                    _tab.value = Tab.Today
                }
            }
        }
    }

    private fun pollPendingCount() {
        viewModelScope.launch {
            while (true) {
                val profile = getConnectionProfile() ?: break
                try {
                    val resp = withContext(Dispatchers.IO) {
                        ApiClient.accessToken = profile.accessToken
                        ClientApi(profile.serverUrl).clientGetInbox()
                    }
                    _pendingCount.value = resp.pendingActions.size
                } catch (_: Exception) {
                    // The badge refreshes on the next poll; Today shows load failures.
                }
                kotlinx.coroutines.delay(POLL_INTERVAL_MS)
            }
        }
    }

    private fun getConnectionProfile(): ConnectionProfile? =
        (authController.authState.value as? AuthState.Connected)?.profile

    fun selectTab(tab: Tab) {
        _tab.value = tab
    }

    /** Registers this device for push; call after the notification permission prompt. */
    fun registerPush() {
        getConnectionProfile()?.let(pushManager::register)
    }

    fun open(page: Page) {
        _pages.value = _pages.value + page
    }

    /** Swaps the top page, e.g. a corrected memory for the one it replaced. */
    fun replace(page: Page) {
        _pages.value = _pages.value.dropLast(1) + page
    }

    fun back() {
        _pages.value = _pages.value.dropLast(1)
    }

    companion object {
        private const val POLL_INTERVAL_MS = 30_000L
    }
}
