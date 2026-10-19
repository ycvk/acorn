package io.ycvk.acorn.feature.browser

import android.graphics.BitmapFactory
import android.util.Base64
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.models.BrowserDragPoint
import io.ycvk.acorn.api.models.BrowserFrame
import io.ycvk.acorn.api.models.BrowserInputRequest
import io.ycvk.acorn.api.models.BrowserTab
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.data.repository.BrowserRepository
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.sse.EventSource
import javax.inject.Inject

private const val TAB_REFRESH_MS = 4_000L

// A drag's samples go to the server in batches up to 20 times a second. The
// page plays them 150 ms after the down arrives, so a batch is in time when it
// takes less than that to arrive.
private const val DRAG_BATCH_MS = 50L

/** The latest picture of the page and the viewport it spans. */
data class ShownFrame(val image: ImageBitmap, val width: Int, val height: Int)

sealed interface BrowserStatus {
    data object Loading : BrowserStatus
    data object Live : BrowserStatus
    data object NoTabs : BrowserStatus
    data class Failed(val message: String) : BrowserStatus
}

data class BrowserUiState(
    val tabs: List<BrowserTab> = emptyList(),
    val tab: String? = null,
    val frame: ShownFrame? = null,
    val status: BrowserStatus = BrowserStatus.Loading,
    val inputError: String? = null,
) {
    val current: BrowserTab? get() = tabs.firstOrNull { it.id == tab }
}

/**
 * The owner's view of the agent's browser. It streams one page while the
 * screen is shown ([start] and [stop]) and sends taps, scrolls, drags, text
 * and keys.
 */
@HiltViewModel
class BrowserViewModel @Inject constructor(
    private val authController: AuthController,
    private val repository: BrowserRepository,
) : ViewModel() {
    private val _state = MutableStateFlow(BrowserUiState())
    val state: StateFlow<BrowserUiState> = _state.asStateFlow()

    private var stream: EventSource? = null
    private var tabsJob: Job? = null
    private var screen: PhoneScreen? = null
    private var pendingUrl: String? = null
    private val drags = DragSender(viewModelScope, DRAG_BATCH_MS) { request -> profile()?.let { input(it, request) } }

    /** The page streams laid out for [size]; a new size restarts the stream. */
    fun screen(size: PhoneScreen) {
        if (size == screen) return
        screen = size
        _state.value.tab?.let { if (tabsJob != null) connect(it) }
    }

    /** Starts streaming; [openUrl], when it is a web address, opens in the first page shown. */
    fun start(openUrl: String?) {
        stop()
        pendingUrl = openUrl?.let(::navigationUrl)
        _state.update { it.copy(status = BrowserStatus.Loading) }
        tabsJob = viewModelScope.launch {
            while (isActive) {
                refreshTabs()
                delay(TAB_REFRESH_MS)
            }
        }
    }

    fun stop() {
        tabsJob?.cancel()
        tabsJob = null
        stream?.cancel()
        stream = null
    }

    fun select(tab: String) {
        if (tab == _state.value.tab && stream != null) return
        _state.update { it.copy(tab = tab, frame = null, status = BrowserStatus.Loading) }
        connect(tab)
    }

    private suspend fun refreshTabs() {
        val profile = profile() ?: return
        val tabs = try {
            withContext(Dispatchers.IO) { repository.tabs(profile) }
        } catch (e: Exception) {
            _state.update { it.copy(status = BrowserStatus.Failed(userMessage(e))) }
            return
        }
        _state.update { it.copy(tabs = tabs) }
        val chosen = _state.value.tab?.takeIf { id -> tabs.any { it.id == id } }
        when {
            tabs.isEmpty() -> {
                stream?.cancel()
                stream = null
                _state.update { it.copy(tab = null, frame = null, status = BrowserStatus.NoTabs) }
            }
            chosen == null -> select(tabs.first().id)
            stream == null -> connect(chosen)
        }
        val url = pendingUrl
        if (url != null && _state.value.tab != null) {
            pendingUrl = null
            send(BrowserInputRequest.Type.navigate, url = url)
        }
    }

    private fun connect(tab: String) {
        val profile = profile() ?: return
        val size = screen ?: return
        stream?.cancel()
        stream = repository.frames(
            profile = profile,
            tab = tab,
            width = size.width,
            height = size.height,
            onFrame = { frame -> show(tab, frame) },
            onError = { e -> streamEnded(tab, e) },
            onClosed = { streamEnded(tab, null) },
        )
    }

    private fun show(tab: String, frame: BrowserFrame) {
        val bytes = Base64.decode(frame.jpeg, Base64.DEFAULT)
        val bitmap = BitmapFactory.decodeByteArray(bytes, 0, bytes.size) ?: return
        _state.update {
            if (it.tab != tab) it else it.copy(frame = ShownFrame(bitmap.asImageBitmap(), frame.width, frame.height), status = BrowserStatus.Live)
        }
    }

    /** A dropped stream reconnects on the next tab refresh; a failure is shown until then. */
    private fun streamEnded(tab: String, error: Throwable?) {
        if (_state.value.tab != tab) return
        stream = null
        if (error != null) {
            _state.update { it.copy(status = BrowserStatus.Failed(userMessage(error))) }
        }
    }

    fun tap(point: ViewportPoint) = send(BrowserInputRequest.Type.tap, x = point.x, y = point.y)

    fun scroll(at: ViewportPoint, dy: Double) = send(BrowserInputRequest.Type.scroll, x = at.x, y = at.y, dy = dy)

    fun dragDown(at: ViewportPoint) {
        val tab = _state.value.tab ?: return
        drags.add(BrowserInputRequest(tab = tab, type = BrowserInputRequest.Type.down, x = at.x, y = at.y))
    }

    fun dragMove(samples: List<DragSample>) {
        val tab = _state.value.tab ?: return
        if (samples.isEmpty()) return
        samples.chunked(MAX_DRAG_POINTS).forEach { chunk ->
            val points = chunk.map { BrowserDragPoint(x = it.at.x, y = it.at.y, t = it.t) }
            drags.add(BrowserInputRequest(tab = tab, type = BrowserInputRequest.Type.move, points = points))
        }
    }

    fun dragUp(at: ViewportPoint, t: Long) {
        val tab = _state.value.tab ?: return
        drags.add(BrowserInputRequest(tab = tab, type = BrowserInputRequest.Type.up, x = at.x, y = at.y, t = t))
    }

    fun type(text: String) {
        if (text.isNotEmpty()) send(BrowserInputRequest.Type.text, text = text)
    }

    fun press(key: BrowserInputRequest.Key) = send(BrowserInputRequest.Type.key, key = key)

    fun back() = send(BrowserInputRequest.Type.back)

    fun reload() = send(BrowserInputRequest.Type.reload)

    /** Returns false when [typed] is not an address, so the field can say so. */
    fun open(typed: String): Boolean {
        val url = navigationUrl(typed) ?: return false
        send(BrowserInputRequest.Type.navigate, url = url)
        return true
    }

    fun dismissInputError() = _state.update { it.copy(inputError = null) }

    private fun send(
        type: BrowserInputRequest.Type,
        x: Double? = null,
        y: Double? = null,
        dy: Double? = null,
        text: String? = null,
        key: BrowserInputRequest.Key? = null,
        url: String? = null,
    ) {
        val profile = profile() ?: return
        val tab = _state.value.tab ?: return
        val request = BrowserInputRequest(tab = tab, type = type, x = x, y = y, dy = dy, text = text, key = key, url = url)
        viewModelScope.launch { input(profile, request) }
    }

    private suspend fun input(profile: ConnectionProfile, request: BrowserInputRequest) {
        try {
            withContext(Dispatchers.IO) { repository.input(profile, request) }
        } catch (e: Exception) {
            _state.update { it.copy(inputError = userMessage(e)) }
        }
    }

    override fun onCleared() {
        stop()
    }

    private fun profile() = (authController.authState.value as? AuthState.Connected)?.profile
}
