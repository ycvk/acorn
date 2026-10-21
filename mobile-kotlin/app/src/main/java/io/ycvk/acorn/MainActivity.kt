package io.ycvk.acorn

import android.content.Intent
import android.graphics.Color
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dagger.hilt.android.AndroidEntryPoint
import io.ycvk.acorn.core.push.DeepLinks
import io.ycvk.acorn.core.push.EXTRA_ACTION_ID
import io.ycvk.acorn.core.push.EXTRA_BROWSER_URL
import io.ycvk.acorn.core.push.ThreadLink
import io.ycvk.acorn.core.push.threadIdFromExtras
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.theme.ThemePreference
import io.ycvk.acorn.core.theme.isDark
import io.ycvk.acorn.feature.shell.AcornShell
import javax.inject.Inject

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    @Inject lateinit var deepLinks: DeepLinks
    @Inject lateinit var themePreference: ThemePreference

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        routeNotificationTap(intent)
        setContent {
            val mode by themePreference.mode.collectAsStateWithLifecycle()
            val dark = mode.isDark(isSystemInDarkTheme())
            // The bar icons follow the app's theme, which may differ from the system's.
            DisposableEffect(dark) {
                enableEdgeToEdge(
                    statusBarStyle = SystemBarStyle.auto(Color.TRANSPARENT, Color.TRANSPARENT) { dark },
                    navigationBarStyle = SystemBarStyle.auto(Color.TRANSPARENT, Color.TRANSPARENT) { dark },
                )
                // Three-button navigation would otherwise get a light scrim over the tab bar.
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) window.isNavigationBarContrastEnforced = false
                onDispose {}
            }
            AcornTheme(darkTheme = dark) {
                AcornShell()
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        routeNotificationTap(intent)
    }

    private fun routeNotificationTap(intent: Intent?) {
        val threadId = threadIdFromExtras { key -> intent?.getStringExtra(key) } ?: return
        deepLinks.openThread(
            ThreadLink(
                threadId = threadId,
                actionId = intent?.getStringExtra(EXTRA_ACTION_ID)?.takeIf { it.isNotBlank() },
                browserUrl = intent?.getStringExtra(EXTRA_BROWSER_URL)?.takeIf { it.isNotBlank() },
            ),
        )
    }
}
