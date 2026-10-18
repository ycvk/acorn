package io.ycvk.acorn.data.repository

import io.ycvk.acorn.api.apis.OwnerApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.Owner
import io.ycvk.acorn.api.models.SetOwnerRequest
import io.ycvk.acorn.core.auth.ConnectionProfile
import javax.inject.Inject
import javax.inject.Singleton

/** Reads and sets what Acorn calls the owner. Blocking; call from an IO dispatcher. */
@Singleton
class OwnerRepository @Inject constructor() {
    fun current(profile: ConnectionProfile): Owner = api(profile).clientGetOwner()

    fun setName(profile: ConnectionProfile, name: String): Owner = api(profile).clientSetOwner(SetOwnerRequest(name = name))

    private fun api(profile: ConnectionProfile): OwnerApi {
        ApiClient.accessToken = profile.accessToken
        return OwnerApi(basePath = profile.serverUrl)
    }
}
