package io.ycvk.acorn

import android.content.Intent
import android.graphics.Color
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import dagger.hilt.android.AndroidEntryPoint
import io.ycvk.acorn.core.push.DeepLinks
import io.ycvk.acorn.core.push.EXTRA_ACTION_ID
import io.ycvk.acorn.core.push.EXTRA_BROWSER_URL
import io.ycvk.acorn.core.push.ThreadLink
import io.ycvk.acorn.core.push.threadIdFromExtras
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.feature.shell.AcornShell
import javax.inject.Inject

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    @Inject lateinit var deepLinks: DeepLinks

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        routeNotificationTap(intent)
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.auto(Color.TRANSPARENT, Color.TRANSPARENT),
            navigationBarStyle = SystemBarStyle.auto(Color.TRANSPARENT, Color.TRANSPARENT),
        )
        // Three-button navigation would otherwise get a light scrim over the tab bar.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) window.isNavigationBarContrastEnforced = false
        setContent {
            AcornTheme {
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
