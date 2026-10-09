package io.ycvk.acorn

import android.content.Intent
import android.graphics.Color
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import dagger.hilt.android.AndroidEntryPoint
import io.ycvk.acorn.core.push.DeepLinks
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
            statusBarStyle = SystemBarStyle.light(Color.TRANSPARENT, Color.TRANSPARENT),
            navigationBarStyle = SystemBarStyle.light(Color.TRANSPARENT, Color.TRANSPARENT),
        )
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
        threadIdFromExtras { key -> intent?.getStringExtra(key) }?.let(deepLinks::openThread)
    }
}
