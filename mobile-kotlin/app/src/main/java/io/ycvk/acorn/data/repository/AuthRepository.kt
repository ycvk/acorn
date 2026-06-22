package io.ycvk.acorn.data.repository

import io.ycvk.acorn.api.apis.DevicesApi
import io.ycvk.acorn.api.models.PairDeviceRequest
import io.ycvk.acorn.api.models.PairDeviceResponse
import io.ycvk.acorn.core.auth.ConnectionProfile

/**
 * Wraps the pairing flow against the generated [DevicesApi].
 *
 * Pairing is unauthenticated: the request exchanges a one-time operator code for a device
 * access token. The returned [ConnectionProfile] is then persisted by
 * [io.ycvk.acorn.core.auth.AuthController].
 */
class AuthRepository {

    /**
     * Pairs this device with the Acorn backend at [serverUrl].
     *
     * @param serverUrl Base URL of the backend, e.g. `http://127.0.0.1:8080`.
     * @param pairingCode One-time operator-issued pairing code.
     * @param deviceName Human-readable device name.
     * @param platform Platform identifier sent to the backend (defaults to "android").
     */
    fun pair(
        serverUrl: String,
        pairingCode: String,
        deviceName: String,
        platform: String = PLATFORM_ANDROID,
    ): ConnectionProfile {
        val devicesApi = DevicesApi(basePath = serverUrl)
        val response: PairDeviceResponse = devicesApi.clientPairDevice(
            PairDeviceRequest(
                pairingCode = pairingCode,
                deviceName = deviceName,
                platform = platform,
            ),
        )
        return ConnectionProfile(
            serverUrl = serverUrl,
            deviceId = response.device.deviceId,
            accessToken = response.accessToken,
        )
    }

    companion object {
        const val PLATFORM_ANDROID = "android"
    }
}
