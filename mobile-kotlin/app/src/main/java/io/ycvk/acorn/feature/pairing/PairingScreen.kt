package io.ycvk.acorn.feature.pairing

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.PairingState
import io.ycvk.acorn.core.theme.Accent
import io.ycvk.acorn.core.theme.AccentDim
import io.ycvk.acorn.core.theme.Bg
import io.ycvk.acorn.core.theme.Border
import io.ycvk.acorn.core.theme.Danger
import io.ycvk.acorn.core.theme.OnAccent
import io.ycvk.acorn.core.theme.SurfaceVariant
import io.ycvk.acorn.core.theme.TextPrimary
import io.ycvk.acorn.core.theme.TextSecondary

@Composable
fun PairingScreen(
    onPaired: () -> Unit,
    authController: AuthController,
) {
    val pairingState by authController.pairingState.collectAsStateWithLifecycle()

    LaunchedEffect(pairingState) {
        if (pairingState is PairingState.Success) {
            onPaired()
            authController.resetPairingState()
        }
    }

    var serverUrl by remember { mutableStateOf("") }
    var pairingCode by remember { mutableStateOf("") }
    var deviceName by remember { mutableStateOf("Android") }

    val isPairing = pairingState is PairingState.Pairing
    val errorMessage = (pairingState as? PairingState.Error)?.message

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(Bg)
            .padding(24.dp),
        horizontalAlignment = Alignment.Start,
        verticalArrangement = Arrangement.Center,
    ) {
        // Terminal prompt header
        Text(
            text = "> acorn pair",
            style = MaterialTheme.typography.headlineMedium.copy(
                fontFamily = FontFamily.Monospace,
                color = Accent,
            ),
        )
        Spacer(Modifier.height(8.dp))
        Text(
            text = "connect to your self-hosted agent",
            style = MaterialTheme.typography.bodySmall,
            color = TextSecondary,
        )
        Spacer(Modifier.height(32.dp))

        OutlinedTextField(
            value = serverUrl,
            onValueChange = { serverUrl = it },
            label = { Text("server url", style = MaterialTheme.typography.labelSmall) },
            modifier = Modifier.fillMaxWidth(),
            singleLine = true,
            enabled = !isPairing,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            colors = outlinedFieldColors(),
        )
        Spacer(Modifier.height(12.dp))
        OutlinedTextField(
            value = pairingCode,
            onValueChange = { pairingCode = it },
            label = { Text("pairing code", style = MaterialTheme.typography.labelSmall) },
            modifier = Modifier.fillMaxWidth(),
            singleLine = true,
            enabled = !isPairing,
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            colors = outlinedFieldColors(),
        )
        Spacer(Modifier.height(12.dp))
        OutlinedTextField(
            value = deviceName,
            onValueChange = { deviceName = it },
            label = { Text("device name", style = MaterialTheme.typography.labelSmall) },
            modifier = Modifier.fillMaxWidth(),
            singleLine = true,
            enabled = !isPairing,
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            colors = outlinedFieldColors(),
        )
        Spacer(Modifier.height(24.dp))

        Button(
            onClick = { authController.pair(serverUrl, pairingCode, deviceName) },
            modifier = Modifier.fillMaxWidth(),
            enabled = !isPairing && serverUrl.isNotBlank() && pairingCode.isNotBlank(),
            colors = ButtonDefaults.buttonColors(
                containerColor = AccentDim,
                contentColor = OnAccent,
                disabledContainerColor = SurfaceVariant,
                disabledContentColor = TextSecondary,
            ),
        ) {
            if (isPairing) {
                CircularProgressIndicator(
                    modifier = Modifier.height(20.dp),
                    strokeWidth = 2.dp,
                    color = OnAccent,
                )
            } else {
                Text("> pair", style = MaterialTheme.typography.labelLarge.copy(
                    fontFamily = FontFamily.Monospace,
                    fontWeight = FontWeight.Medium,
                ))
            }
        }

        errorMessage?.let {
            Spacer(Modifier.height(12.dp))
            Text(
                "! $it",
                style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
                color = Danger,
            )
        }
    }
}

@Composable
private fun outlinedFieldColors() = OutlinedTextFieldDefaults.colors(
    focusedBorderColor = Accent,
    unfocusedBorderColor = Border,
    cursorColor = Accent,
    focusedLabelColor = Accent,
    unfocusedLabelColor = TextSecondary,
    focusedTextColor = TextPrimary,
    unfocusedTextColor = TextPrimary,
)
