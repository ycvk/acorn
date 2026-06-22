package io.ycvk.acorn

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import dagger.hilt.android.AndroidEntryPoint
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.feature.shell.AcornShell

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            AcornTheme {
                AcornShell()
            }
        }
    }
}
