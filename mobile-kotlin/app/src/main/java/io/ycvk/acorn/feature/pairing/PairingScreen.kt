package io.ycvk.acorn.feature.pairing

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.PairingState
import io.ycvk.acorn.core.theme.AetherError
import io.ycvk.acorn.core.theme.AetherOnPrimary
import io.ycvk.acorn.core.theme.AetherOnSurface
import io.ycvk.acorn.core.theme.AetherOnSurfaceVariant
import io.ycvk.acorn.core.theme.AetherOutlineSoft
import io.ycvk.acorn.core.theme.AetherPrimary
import io.ycvk.acorn.core.theme.AetherSurfaceVariant
import io.ycvk.acorn.core.theme.gradientBackground

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
    val canPair = !isPairing && serverUrl.isNotBlank() && pairingCode.isNotBlank()

    Column(
        modifier = Modifier
            .fillMaxSize()
            .gradientBackground()
            .statusBarsPadding()
            .navigationBarsPadding()
            .imePadding()
            .padding(horizontal = 28.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Box(
            modifier = Modifier
                .size(44.dp)
                .background(AetherPrimary, MaterialTheme.shapes.large),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                text = "A",
                style = MaterialTheme.typography.headlineMedium,
                color = AetherOnPrimary,
                fontWeight = FontWeight.Bold,
            )
        }

        Spacer(Modifier.height(16.dp))

        Text(
            text = "acorn",
            style = MaterialTheme.typography.displaySmall,
            color = AetherOnSurface,
        )
        Spacer(Modifier.height(4.dp))
        Text(
            text = "your private ambient agent",
            style = MaterialTheme.typography.bodyMedium,
            color = AetherOnSurfaceVariant,
        )

        Spacer(Modifier.height(40.dp))

        PairingField(
            value = serverUrl,
            onValueChange = { serverUrl = it },
            label = "server url",
            enabled = !isPairing,
            keyboardType = KeyboardType.Uri,
        )
        Spacer(Modifier.height(16.dp))
        PairingField(
            value = pairingCode,
            onValueChange = { pairingCode = it },
            label = "pairing code",
            enabled = !isPairing,
        )
        Spacer(Modifier.height(16.dp))
        PairingField(
            value = deviceName,
            onValueChange = { deviceName = it },
            label = "device name",
            enabled = !isPairing,
        )

        Spacer(Modifier.height(28.dp))

        Button(
            onClick = { authController.pair(serverUrl, pairingCode, deviceName) },
            enabled = canPair,
            modifier = Modifier
                .fillMaxWidth()
                .height(50.dp),
            shape = RoundedCornerShape(26.dp),
            colors = ButtonDefaults.buttonColors(
                containerColor = AetherPrimary,
                contentColor = AetherOnPrimary,
                disabledContainerColor = AetherPrimary.copy(alpha = 0.4f),
                disabledContentColor = AetherOnPrimary.copy(alpha = 0.6f),
            ),
        ) {
            if (isPairing) {
                CircularProgressIndicator(
                    modifier = Modifier.size(20.dp),
                    strokeWidth = 2.dp,
                    color = AetherOnPrimary,
                )
            } else {
                Text(
                    "Connect",
                    style = MaterialTheme.typography.labelLarge,
                    color = AetherOnPrimary,
                    fontWeight = FontWeight.SemiBold,
                )
            }
        }

        errorMessage?.let {
            Spacer(Modifier.height(16.dp))
            Surface(
                color = AetherError.copy(alpha = 0.1f),
                shape = MaterialTheme.shapes.large,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(
                    "$it",
                    modifier = Modifier.padding(12.dp),
                    style = MaterialTheme.typography.bodySmall,
                    color = AetherError,
                )
            }
        }
    }
}

@Composable
private fun PairingField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    enabled: Boolean,
    keyboardType: KeyboardType = KeyboardType.Text,
) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        label = {
            Text(label, style = MaterialTheme.typography.labelSmall)
        },
        modifier = Modifier.fillMaxWidth(),
        singleLine = true,
        enabled = enabled,
        keyboardOptions = KeyboardOptions(keyboardType = keyboardType),
        textStyle = MaterialTheme.typography.bodyMedium.copy(color = AetherOnSurface),
        shape = RoundedCornerShape(16.dp),
        colors = OutlinedTextFieldDefaults.colors(
            focusedBorderColor = AetherPrimary,
            unfocusedBorderColor = AetherOutlineSoft,
            disabledBorderColor = AetherOutlineSoft,
            cursorColor = AetherPrimary,
            focusedLabelColor = AetherOnSurfaceVariant,
            unfocusedLabelColor = AetherOnSurfaceVariant,
            focusedTextColor = AetherOnSurface,
            unfocusedTextColor = AetherOnSurface,
            disabledTextColor = AetherOnSurface,
            unfocusedContainerColor = AetherSurfaceVariant,
            focusedContainerColor = AetherSurfaceVariant,
            disabledContainerColor = AetherSurfaceVariant,
        ),
    )
}
