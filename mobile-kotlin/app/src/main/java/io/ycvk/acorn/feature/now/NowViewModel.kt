package io.ycvk.acorn.feature.now

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.models.NowResponse
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.data.repository.NowRepository
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import javax.inject.Inject

sealed interface NowLoad {
    data object Loading : NowLoad
    data class Loaded(val now: NowResponse) : NowLoad
    data class Failed(val message: String) : NowLoad
}

/** Backs the Now tab. Every change is followed by a fresh load; nothing is changed locally. */
@HiltViewModel
class NowViewModel @Inject constructor(
    private val authController: AuthController,
    private val repository: NowRepository,
) : ViewModel() {

    private val _load = MutableStateFlow<NowLoad>(NowLoad.Loading)
    val load: StateFlow<NowLoad> = _load.asStateFlow()

    private val _refreshing = MutableStateFlow(false)
    val refreshing: StateFlow<Boolean> = _refreshing.asStateFlow()

    /** The last failed change, shown until dismissed or the next change. */
    private val _actionError = MutableStateFlow<String?>(null)
    val actionError: StateFlow<String?> = _actionError.asStateFlow()

    private var loadJob: Job? = null

    fun refresh() {
        val profile = profile() ?: run {
            _load.value = NowLoad.Failed("Not connected to a server")
            return
        }
        loadJob?.cancel()
        loadJob = viewModelScope.launch { reload(profile) }
    }

    fun cancelCommitment(id: Long) = change { repository.cancelCommitment(it, id) }

    fun pauseWatch(id: Long) = change { repository.pauseWatch(it, id) }

    fun resumeWatch(id: Long) = change { repository.resumeWatch(it, id) }

    fun dismissActionError() {
        _actionError.value = null
    }

    private fun change(action: (ConnectionProfile) -> Unit) {
        val profile = profile() ?: run {
            _actionError.value = "Not connected to a server"
            return
        }
        _actionError.value = null
        loadJob?.cancel()
        loadJob = viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) { action(profile) }
            } catch (e: Exception) {
                _actionError.value = e.message ?: "The change failed"
            }
            reload(profile)
        }
    }

    private suspend fun reload(profile: ConnectionProfile) {
        _refreshing.value = true
        if (_load.value !is NowLoad.Loaded) _load.value = NowLoad.Loading
        _load.value = try {
            NowLoad.Loaded(withContext(Dispatchers.IO) { repository.now(profile) })
        } catch (e: Exception) {
            NowLoad.Failed(e.message ?: "Failed to load")
        } finally {
            _refreshing.value = false
        }
    }

    private fun profile() = (authController.authState.value as? AuthState.Connected)?.profile
}

private val timeOfDay = DateTimeFormatter.ofPattern("HH:mm")
private val dayAndTime = DateTimeFormatter.ofPattern("M/d HH:mm")

/** When a commitment wakes, in the phone's time zone: "Today 09:00", "Tomorrow 09:00" or "10/14 09:00". */
fun wakeLabel(at: OffsetDateTime, today: LocalDate, zone: ZoneId): String {
    val local = at.atZoneSameInstant(zone)
    return when (local.toLocalDate()) {
        today -> "Today ${local.format(timeOfDay)}"
        today.plusDays(1) -> "Tomorrow ${local.format(timeOfDay)}"
        else -> local.format(dayAndTime)
    }
}

/** How often a watch is checked: "every 30m", "every 2h", "every 1d". */
fun intervalLabel(seconds: Long): String = when {
    seconds % 86_400 == 0L -> "every ${seconds / 86_400}d"
    seconds % 3_600 == 0L -> "every ${seconds / 3_600}h"
    else -> "every ${seconds / 60}m"
}
