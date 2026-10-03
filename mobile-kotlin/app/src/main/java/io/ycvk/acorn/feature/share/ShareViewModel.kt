package io.ycvk.acorn.feature.share

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
        authController.loadStoredConnection()
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
        viewModelScope.launch {
            _state.value = try {
                withContext(Dispatchers.IO) { sendCapture(profile, content, note) }
            } catch (e: Exception) {
                ShareState.Editing(content, apiErrorMessage(e))
            }
        }
    }

    private fun sendCapture(profile: ConnectionProfile, content: SharedContent, note: String): ShareState {
        val image = content.imageUri?.let { uri ->
            copyImage(Uri.parse(uri)) ?: return ShareState.Editing(content, "This image is larger than 10 MiB.")
        }
        try {
            val accepted = captures.send(profile, captureText(content.text, note), content.subject, image)
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
