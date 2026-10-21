package io.ycvk.acorn.feature.share

import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.feature.live.RunFollower
import android.content.Context
import android.net.Uri
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.data.repository.CaptureRepository
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File
import javax.inject.Inject

sealed interface ShareState {
    data object Loading : ShareState
    data object Unsupported : ShareState
    data object NotPaired : ShareState
    data class Editing(val content: SharedContent, val error: String? = null) : ShareState
    data class Sending(val content: SharedContent) : ShareState
    data class Sent(val threadId: String) : ShareState
}

/** Sends one share to the paired server as a capture. */
@HiltViewModel
class ShareViewModel @Inject constructor(
    @ApplicationContext private val context: Context,
    private val authController: AuthController,
    private val captures: CaptureRepository,
    private val follower: RunFollower,
) : ViewModel() {

    private val _state = MutableStateFlow<ShareState>(ShareState.Loading)
    val state: StateFlow<ShareState> = _state.asStateFlow()

    /** Called on activity creation; later calls (after rotation) are ignored. */
    fun start(content: SharedContent?) {
        if (_state.value != ShareState.Loading) return
        if (content == null) {
            _state.value = ShareState.Unsupported
            return
        }
        viewModelScope.launch {
            val auth = authController.authState.first { it !is AuthState.Loading }
            _state.value = if (auth is AuthState.Connected) ShareState.Editing(content) else ShareState.NotPaired
        }
    }

    fun send(note: String) {
        val editing = _state.value as? ShareState.Editing ?: return
        val profile = (authController.authState.value as? AuthState.Connected)?.profile ?: run {
            _state.value = ShareState.NotPaired
            return
        }
        val content = editing.content
        _state.value = ShareState.Sending(content)
        // Shown as sending while the sheet is still in front, so its Live Update can start.
        val sendKey = follower.beginSend(content.subject?.takeIf { it.isNotBlank() } ?: "分享的内容")
        viewModelScope.launch {
            val next = try {
                withContext(Dispatchers.IO) { sendCapture(profile, content, note, sendKey) }
            } catch (e: Exception) {
                ShareState.Editing(content, userMessage(e))
            }
            if (next !is ShareState.Sent) follower.endSend(sendKey)
            _state.value = next
        }
    }

    /** Sends the capture; the run it starts shows as a Live Update until it ends. */
    private fun sendCapture(profile: ConnectionProfile, content: SharedContent, note: String, sendKey: String): ShareState {
        val image = content.imageUri?.let { uri ->
            copyImage(Uri.parse(uri)) ?: return ShareState.Editing(content, "图片超过 10 MiB，没法发送。")
        }
        try {
            val accepted = captures.send(profile, captureText(content.text, note), content.subject, image)
            follower.follow(profile, accepted.runId, accepted.threadId, title = null, live = true, sendKey = sendKey)
            return ShareState.Sent(accepted.threadId)
        } finally {
            image?.delete()
        }
    }

    /** Copies the shared image to a temp file, or returns null when it is over the limit. */
    private fun copyImage(uri: Uri): File? {
        val file = File.createTempFile("share-", ".img", context.cacheDir)
        val within = context.contentResolver.openInputStream(uri).use { input ->
            requireNotNull(input) { "Cannot read the shared image" }
            file.outputStream().use { output -> copyWithin(input, output, MAX_SHARED_IMAGE_BYTES) }
        }
        if (!within) {
            file.delete()
            return null
        }
        return file
    }
}
