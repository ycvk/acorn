package io.ycvk.acorn.feature.pairing

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.PairingState

@Composable
fun PairingScreen(
    onPaired: () -> Unit,
    authController: AuthController,
) {
    val pairingState by authController.pairingState.collectAsStateWithLifecycle()

    // Transition to the shell when pairing succeeds — authState will already be Connected.
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
            .padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text("Acorn", style = MaterialTheme.typography.headlineMedium)
        Spacer(Modifier.height(24.dp))
        OutlinedTextField(
            value = serverUrl,
            onValueChange = { serverUrl = it },
            label = { Text("Server URL") },
            modifier = Modifier.fillMaxWidth(),
            singleLine = true,
            enabled = !isPairing,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
        )
        Spacer(Modifier.height(12.dp))
        OutlinedTextField(
            value = pairingCode,
            onValueChange = { pairingCode = it },
            label = { Text("Pairing Code") },
            modifier = Modifier.fillMaxWidth(),
            singleLine = true,
            enabled = !isPairing,
        )
        Spacer(Modifier.height(12.dp))
        OutlinedTextField(
            value = deviceName,
            onValueChange = { deviceName = it },
            label = { Text("Device Name") },
            modifier = Modifier.fillMaxWidth(),
            singleLine = true,
            enabled = !isPairing,
        )
        Spacer(Modifier.height(24.dp))
        Button(
            onClick = { authController.pair(serverUrl, pairingCode, deviceName) },
            modifier = Modifier.fillMaxWidth(),
            enabled = !isPairing && serverUrl.isNotBlank() && pairingCode.isNotBlank(),
        ) {
            if (isPairing) {
                CircularProgressIndicator(
                    modifier = Modifier.height(20.dp),
                    strokeWidth = 2.dp,
                    color = MaterialTheme.colorScheme.onPrimary,
                )
            } else {
                Text("Pair Device")
            }
        }
        errorMessage?.let {
            Spacer(Modifier.height(12.dp))
            Text(it, color = MaterialTheme.colorScheme.error)
        }
    }
}
