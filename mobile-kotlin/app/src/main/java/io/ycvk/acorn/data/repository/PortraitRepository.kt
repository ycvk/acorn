package io.ycvk.acorn.data.repository

import io.ycvk.acorn.api.apis.PortraitApi
import io.ycvk.acorn.api.models.Portrait
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.auth.authorized
import javax.inject.Inject
import javax.inject.Singleton

/** Reads what Acorn has gathered about the owner and removes lines from it. Blocking; call from an IO dispatcher. */
@Singleton
class PortraitRepository @Inject constructor() {
    fun get(profile: ConnectionProfile): Portrait = api(profile).clientGetPortrait()

    fun removeLine(profile: ConnectionProfile, id: Long) {
        api(profile).clientRemovePortraitLine(id)
    }

    private fun api(profile: ConnectionProfile): PortraitApi {
        return PortraitApi(basePath = profile.serverUrl).authorized(profile)
    }
}
