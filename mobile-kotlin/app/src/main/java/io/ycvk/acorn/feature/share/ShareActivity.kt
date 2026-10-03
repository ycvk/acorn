package io.ycvk.acorn.feature.share

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import androidx.core.content.IntentCompat
import dagger.hilt.android.AndroidEntryPoint
import io.ycvk.acorn.MainActivity
import io.ycvk.acorn.core.push.EXTRA_THREAD_ID
import io.ycvk.acorn.core.theme.AcornTheme

/** The system share target: a sheet over the sharing app that sends a capture. */
@AndroidEntryPoint
class ShareActivity : ComponentActivity() {
    private val viewModel: ShareViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val stream = IntentCompat.getParcelableExtra(intent, Intent.EXTRA_STREAM, Uri::class.java)
        viewModel.start(
            parseShare(
                action = intent.action,
                type = intent.type,
                text = intent.getStringExtra(Intent.EXTRA_TEXT),
                subject = intent.getStringExtra(Intent.EXTRA_SUBJECT),
                stream = stream?.toString(),
            ),
        )
        setContent {
            AcornTheme {
                ShareSheet(
                    viewModel = viewModel,
                    onClose = ::finish,
                    onOpenApp = { openApp(null) },
                    onOpenThread = { threadId -> openApp(threadId) },
                )
            }
        }
    }

    private fun openApp(threadId: String?) {
        startActivity(
            Intent(this, MainActivity::class.java).apply {
                flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP
                threadId?.let { putExtra(EXTRA_THREAD_ID, it) }
            },
        )
        finish()
    }
}
