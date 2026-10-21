package io.ycvk.acorn.feature.shell

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.net.LocalNetworkAccess
import io.ycvk.acorn.core.push.DeepLinks
import io.ycvk.acorn.core.push.PushManager
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.data.repository.OwnerRepository
import javax.inject.Inject
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

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

/** Whether the owner has told Acorn what to call them; the app opens only once they have. */
sealed interface OwnerGate {
    data object Checking : OwnerGate
    data object Unnamed : OwnerGate
    data object Named : OwnerGate

    /** The name could not be read, so it is not known whether one is set. */
    data class Failed(val message: String) : OwnerGate
}

fun ownerGateFor(name: String): OwnerGate = if (name.isBlank()) OwnerGate.Unnamed else OwnerGate.Named

@HiltViewModel
class ShellViewModel @Inject constructor(
    val authController: AuthController,
    val localNetwork: LocalNetworkAccess,
    private val pushManager: PushManager,
    private val deepLinks: DeepLinks,
    private val ownerRepository: OwnerRepository,
) : ViewModel() {

    private val _ownerGate = MutableStateFlow<OwnerGate>(OwnerGate.Checking)
    val ownerGate: StateFlow<OwnerGate> = _ownerGate.asStateFlow()
    private var ownerCheck: Job? = null

    private val _savingName = MutableStateFlow(false)
    val savingName: StateFlow<Boolean> = _savingName.asStateFlow()

    private val _nameError = MutableStateFlow<String?>(null)
    val nameError: StateFlow<String?> = _nameError.asStateFlow()

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
                    ownerCheck?.cancel()
                    _ownerGate.value = OwnerGate.Checking
                    _nameError.value = null
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

    /**
     * Reads what Acorn calls the owner, unless that is already known. Call it
     * once the server can be reached, after the nearby devices prompt.
     */
    fun checkOwner() {
        val profile = getConnectionProfile() ?: return
        val gate = _ownerGate.value
        if (gate == OwnerGate.Named || gate == OwnerGate.Unnamed || ownerCheck?.isActive == true) return
        _ownerGate.value = OwnerGate.Checking
        ownerCheck = viewModelScope.launch {
            _ownerGate.value = try {
                ownerGateFor(withContext(Dispatchers.IO) { ownerRepository.current(profile) }.name)
            } catch (e: CancellationException) {
                // Disconnecting cancels the check and has already reset the gate.
                throw e
            } catch (e: Exception) {
                OwnerGate.Failed(userMessage(e))
            }
        }
    }

    fun saveOwnerName(name: String) {
        val profile = getConnectionProfile() ?: return
        _savingName.value = true
        _nameError.value = null
        viewModelScope.launch {
            try {
                _ownerGate.value = ownerGateFor(withContext(Dispatchers.IO) { ownerRepository.setName(profile, name) }.name)
            } catch (e: Exception) {
                _nameError.value = userMessage(e)
            } finally {
                _savingName.value = false
            }
        }
    }

    fun disconnect() {
        authController.disconnect()
    }

    fun selectTab(tab: Tab) {
        _tab.value = tab
    }

    /** Whether the paired server is on the LAN and the owner has not allowed nearby devices. */
    suspend fun needsLocalNetwork(): Boolean = getConnectionProfile()?.let { localNetwork.needsPermission(it.serverUrl) } ?: false

    /** Registers this device for push; call after the notification permission prompt. */
    fun registerPush() {
        if (getConnectionProfile() != null) pushManager.register()
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
