package io.ycvk.acorn.core.theme

import android.app.UiModeManager
import android.content.Context
import android.os.Build
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.io.IOException
import javax.inject.Inject
import javax.inject.Singleton

/** How the app picks its colors: with the system, or always light or dark. */
enum class ThemeMode(val label: String) {
    System("跟随系统"),
    Light("浅色"),
    Dark("深色"),
}

/** Whether [ThemeMode] draws dark while the system is [systemDark]. */
fun ThemeMode.isDark(systemDark: Boolean): Boolean = when (this) {
    ThemeMode.System -> systemDark
    ThemeMode.Light -> false
    ThemeMode.Dark -> true
}

/**
 * The owner's theme choice, kept on this phone. From Android 12 the system
 * also learns it, so the launch screen and the system bars match before the
 * app draws.
 */
@Singleton
class ThemePreference @Inject constructor(@ApplicationContext private val context: Context) {
    private val prefs = context.getSharedPreferences("appearance", Context.MODE_PRIVATE)
    private val _mode = MutableStateFlow(prefs.getString(KEY, null)?.let(ThemeMode::valueOf) ?: ThemeMode.System)
    val mode: StateFlow<ThemeMode> = _mode.asStateFlow()

    fun set(mode: ThemeMode) {
        if (!prefs.edit().putString(KEY, mode.name).commit()) throw IOException("Could not save the theme")
        _mode.value = mode
        tellSystem()
    }

    /** Hands the stored choice to the system, which keeps its own copy per app. */
    fun tellSystem() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S) return
        val nightMode = when (_mode.value) {
            ThemeMode.System -> UiModeManager.MODE_NIGHT_AUTO
            ThemeMode.Light -> UiModeManager.MODE_NIGHT_NO
            ThemeMode.Dark -> UiModeManager.MODE_NIGHT_YES
        }
        context.getSystemService(UiModeManager::class.java).setApplicationNightMode(nightMode)
    }

    private companion object {
        const val KEY = "theme"
    }
}
