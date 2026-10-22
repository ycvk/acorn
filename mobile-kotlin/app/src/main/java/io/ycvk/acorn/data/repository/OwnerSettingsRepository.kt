package io.ycvk.acorn.data.repository

import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.apis.SettingsApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.OwnerSettingName
import io.ycvk.acorn.api.models.OwnerSettings
import io.ycvk.acorn.api.models.OwnerSettingsChanges
import io.ycvk.acorn.api.models.UpdateOwnerSettingsRequest
import io.ycvk.acorn.core.auth.ConnectionProfile
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Reads and changes the owner's settings: time zone, routine times, quiet
 * hours, daily budgets and approval rules. Blocking; call from an IO
 * dispatcher.
 */
@Singleton
class OwnerSettingsRepository @Inject constructor() {
    fun current(profile: ConnectionProfile): OwnerSettings = api(profile).clientGetSettings()

    fun save(profile: ConnectionProfile, changes: OwnerSettingsChanges): OwnerSettings =
        api(profile).clientUpdateSettings(UpdateOwnerSettingsRequest(set = changes))

    fun reset(profile: ConnectionProfile, name: OwnerSettingName): OwnerSettings =
        api(profile).clientUpdateSettings(UpdateOwnerSettingsRequest(reset = listOf(name)))

    /** The names of the tools the agent has, for picking approval rules. */
    fun toolNames(profile: ConnectionProfile): List<String> {
        ApiClient.accessToken = profile.accessToken
        return ClientApi(basePath = profile.serverUrl).clientListTools().items.map { it.name }.distinct().sorted()
    }

    private fun api(profile: ConnectionProfile): SettingsApi {
        ApiClient.accessToken = profile.accessToken
        return SettingsApi(basePath = profile.serverUrl)
    }
}
