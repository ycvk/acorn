package io.ycvk.acorn.feature.settings

import android.content.Context
import android.content.Intent
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.core.graphics.drawable.toBitmap
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import io.ycvk.acorn.core.notifications.NotificationPreferences
import io.ycvk.acorn.core.notifications.NotificationUploader
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.inject.Inject

data class NotificationApp(val packageName: String, val name: String, val icon: ImageBitmap)

@HiltViewModel
class NotificationAccessViewModel @Inject constructor(
    @ApplicationContext private val context: Context,
    preferences: NotificationPreferences,
    private val uploader: NotificationUploader,
) : ViewModel() {
    val allowed = preferences.allowed
    val upload = uploader.status
    private val _access = MutableStateFlow(false)
    val access = _access.asStateFlow()
    private val _apps = MutableStateFlow<List<NotificationApp>>(emptyList())
    val apps = _apps.asStateFlow()
    private val _error = MutableStateFlow<String?>(null)
    val error = _error.asStateFlow()
    private val _loading = MutableStateFlow(false)
    val loading = _loading.asStateFlow()

    fun refresh() {
        _access.value = uploader.hasAccess()
        if (_access.value) uploader.retry()
        viewModelScope.launch {
            _loading.value = true
            try {
                _apps.value = withContext(Dispatchers.IO) {
                    val pm = context.packageManager
                    pm.queryIntentActivities(Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER), 0)
                        .map { it.activityInfo.applicationInfo }
                        .distinctBy { it.packageName }
                        .filter { it.packageName != context.packageName }
                        .map { NotificationApp(it.packageName, pm.getApplicationLabel(it).toString(), pm.getApplicationIcon(it).toBitmap(96, 96).asImageBitmap()) }
                        .sortedBy { it.name.lowercase() }
                }
                _error.value = null
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) { _error.value = e.message ?: "Could not load apps" }
            finally { _loading.value = false }
        }
    }

    fun select(packageName: String, enabled: Boolean) = uploader.setAllowed(packageName, enabled)
    fun retry() = uploader.retry()
}
