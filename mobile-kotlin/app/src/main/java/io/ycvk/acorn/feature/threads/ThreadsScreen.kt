package io.ycvk.acorn.feature.threads

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle

/**
 * Threads tab: lists existing threads and lets the user create a new one or open
 * a thread's chat. Delegates all data work to [ThreadsViewModel].
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ThreadsScreen(
    onThreadClick: (String) -> Unit,
    modifier: Modifier = Modifier,
    viewModel: ThreadsViewModel = hiltViewModel(),
) {
    val threads by viewModel.threads.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()

    LaunchedEffect(Unit) { viewModel.loadThreads() }

    Scaffold(
        modifier = modifier,
        floatingActionButton = {
            FloatingActionButton(onClick = { viewModel.createNewThread(onThreadClick) }) {
                Icon(Icons.Filled.Add, contentDescription = "New Thread")
            }
        },
    ) { padding ->
        LazyColumn(modifier = Modifier.fillMaxSize().padding(padding)) {
            items(threads, key = { it.id }) { thread ->
                ListItem(
                    headlineContent = { Text(thread.title.ifBlank { "Untitled" }) },
                    supportingContent = { Text(thread.id) },
                    modifier = Modifier.clickable { onThreadClick(thread.id) },
                )
                HorizontalDivider()
            }

            if (threads.isEmpty() && error == null) {
                item {
                    Text(
                        "No threads yet. Tap + to start a new chat.",
                        modifier = Modifier.padding(16.dp),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }

            error?.let {
                item {
                    Text(
                        it,
                        modifier = Modifier.padding(16.dp),
                        color = MaterialTheme.colorScheme.error,
                    )
                }
            }
        }
    }
}
