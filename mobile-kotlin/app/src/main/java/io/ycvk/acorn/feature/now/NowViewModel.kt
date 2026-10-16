package io.ycvk.acorn.feature.now

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.models.NowResponse
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.ui.plainSnippet
import io.ycvk.acorn.core.ui.userMessage
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
    data class Loaded(val now: NowResponse, val briefing: List<BriefingSection> = emptyList()) : NowLoad
    data class Failed(val message: String) : NowLoad
}

/** Backs the Today tab. Every change is followed by a fresh load; nothing is changed locally. */
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
            _load.value = NowLoad.Failed("还没有连接服务器")
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
            _actionError.value = "还没有连接服务器"
            return
        }
        _actionError.value = null
        loadJob?.cancel()
        loadJob = viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) { action(profile) }
            } catch (e: Exception) {
                _actionError.value = userMessage(e)
            }
            reload(profile)
        }
    }

    private suspend fun reload(profile: ConnectionProfile) {
        _refreshing.value = true
        if (_load.value !is NowLoad.Loaded) _load.value = NowLoad.Loading
        _load.value = try {
            withContext(Dispatchers.IO) {
                val now = repository.now(profile)
                val sections = now.briefing?.let { briefingSections(it.body) }.orEmpty()
                NowLoad.Loaded(now, sections)
            }
        } catch (e: Exception) {
            NowLoad.Failed(userMessage(e))
        } finally {
            _refreshing.value = false
        }
    }

    private fun profile() = (authController.authState.value as? AuthState.Connected)?.profile
}

private val timeOfDay = DateTimeFormatter.ofPattern("HH:mm")

/** When a commitment wakes, in the phone's time zone: a day ("今天", "明天", "10月14日") and a time. */
data class WakeTime(val day: String, val time: String)

fun wakeTime(at: OffsetDateTime, today: LocalDate, zone: ZoneId): WakeTime {
    val local = at.atZoneSameInstant(zone)
    val date = local.toLocalDate()
    val day = when (date) {
        today -> "今天"
        today.plusDays(1) -> "明天"
        else -> "${date.monthValue}月${date.dayOfMonth}日"
    }
    return WakeTime(day, local.format(timeOfDay))
}

/** How often a watch is checked: "每 30 分钟", "每 2 小时", "每天". */
fun intervalLabel(seconds: Long): String = when {
    seconds == 86_400L -> "每天"
    seconds % 86_400 == 0L -> "每 ${seconds / 86_400} 天"
    seconds % 3_600 == 0L -> "每 ${seconds / 3_600} 小时"
    else -> "每 ${seconds / 60} 分钟"
}

/** A part of the briefing: the lines under one heading, flattened for a preview. */
data class BriefingSection(val heading: String?, val text: String)

/** Splits a briefing at its headings; a heading with nothing under it is dropped. */
fun briefingSections(body: String): List<BriefingSection> {
    val sections = mutableListOf<BriefingSection>()
    var heading: String? = null
    val lines = mutableListOf<String>()
    fun flush() {
        val text = plainSnippet(lines.joinToString("\n"))
        if (text.isNotEmpty() || heading != null) sections += BriefingSection(heading, text)
        lines.clear()
    }
    for (line in body.lines()) {
        val trimmed = line.trim()
        when {
            trimmed.startsWith("#") -> {
                flush()
                heading = trimmed.trimStart('#').trim()
            }
            trimmed.isNotEmpty() -> lines += trimmed
        }
    }
    flush()
    return sections.filter { it.text.isNotEmpty() }
}
