package io.ycvk.acorn.feature.threads

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.core.sse.ChatState
import io.ycvk.acorn.core.sse.RunStatus

@Composable
fun ThreadsScreen(
    modifier: Modifier = Modifier,
    viewModel: ThreadsViewModel = hiltViewModel(),
) {
    val chatState by viewModel.chatState.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()

    var prompt by remember { mutableStateOf("Say hello in one sentence.") }

    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(16.dp),
    ) {
        Text(
            "Test Run",
            style = MaterialTheme.typography.titleLarge,
        )
        Spacer(Modifier.height(8.dp))

        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            OutlinedTextField(
                value = prompt,
                onValueChange = { prompt = it },
                label = { Text("Prompt") },
                modifier = Modifier.weight(1f),
                singleLine = true,
            )
            Button(
                onClick = { viewModel.startTestRun(prompt) },
                enabled = !chatState.isStreaming && prompt.isNotBlank(),
            ) {
                if (chatState.isStreaming) {
                    CircularProgressIndicator(
                        modifier = Modifier.height(20.dp),
                        strokeWidth = 2.dp,
                        color = MaterialTheme.colorScheme.onPrimary,
                    )
                } else {
                    Text("Start")
                }
            }
        }

        Spacer(Modifier.height(12.dp))

        ChatStatusBadge(chatState)

        error?.let {
            Spacer(Modifier.height(8.dp))
            Text(it, color = MaterialTheme.colorScheme.error)
        }

        Spacer(Modifier.height(12.dp))

        ChatOutput(
            state = chatState,
            modifier = Modifier.fillMaxWidth(),
        )
    }
}

@Composable
private fun ChatStatusBadge(state: ChatState) {
    val (label, color) = when (state.runStatus) {
        RunStatus.Idle -> "Idle" to MaterialTheme.colorScheme.outline
        RunStatus.Running -> "Running" to MaterialTheme.colorScheme.tertiary
        RunStatus.Completed -> "Completed" to MaterialTheme.colorScheme.primary
        RunStatus.Failed -> "Failed" to MaterialTheme.colorScheme.error
        RunStatus.Interrupted -> "Interrupted" to MaterialTheme.colorScheme.error
    }
    Surface(color = color.copy(alpha = 0.15f)) {
        Text(
            text = label,
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 4.dp),
            color = color,
            style = MaterialTheme.typography.labelSmall,
        )
    }
}

@Composable
private fun ChatOutput(state: ChatState, modifier: Modifier = Modifier) {
    LazyColumn(
        modifier = modifier,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (state.assistantText.isBlank() && state.assistantReasoning.isBlank() && state.activities.isEmpty()) {
            item {
                Text(
                    "No output yet. Start a run to stream SSE events.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        } else {
            if (state.assistantReasoning.isNotBlank()) {
                item {
                    ReasoningCard(state.assistantReasoning)
                }
            }
            if (state.assistantText.isNotBlank()) {
                item {
                    AssistantCard(state.assistantText, state.isStreaming)
                }
            }
            items(state.activities, key = { it.id }) { activity ->
                ActivityRow(activity.label)
            }
        }
    }
}

@Composable
private fun AssistantCard(text: String, streaming: Boolean) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.primaryContainer,
        ),
    ) {
        Column(Modifier.padding(12.dp)) {
            Text(
                text = if (streaming) "$text▌" else text,
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onPrimaryContainer,
            )
        }
    }
}

@Composable
private fun ReasoningCard(text: String) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.surfaceVariant,
        ),
    ) {
        Column(Modifier.padding(12.dp)) {
            Text(
                "Reasoning",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontFamily = FontFamily.Monospace,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                text = text,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontFamily = FontFamily.Monospace,
            )
        }
    }
}

@Composable
private fun ActivityRow(label: String) {
    Surface(
        color = MaterialTheme.colorScheme.secondaryContainer,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Text(
            text = label,
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp),
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSecondaryContainer,
        )
    }
}
