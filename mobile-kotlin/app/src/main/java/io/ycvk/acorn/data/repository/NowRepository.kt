package io.ycvk.acorn.data.repository

import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.NowResponse
import io.ycvk.acorn.core.auth.ConnectionProfile
import javax.inject.Inject
import javax.inject.Singleton

/** Reads and changes the now page on the paired server. Blocking; call from an IO dispatcher. */
@Singleton
class NowRepository @Inject constructor() {
    fun now(profile: ConnectionProfile): NowResponse = api(profile).clientGetNow()

    fun cancelCommitment(profile: ConnectionProfile, id: Long) = api(profile).clientCancelCommitment(id)

    fun pauseWatch(profile: ConnectionProfile, id: Long) = api(profile).clientPauseWatch(id)

    fun resumeWatch(profile: ConnectionProfile, id: Long) = api(profile).clientResumeWatch(id)

    private fun api(profile: ConnectionProfile): ClientApi {
        ApiClient.accessToken = profile.accessToken
        return ClientApi(basePath = profile.serverUrl)
    }
}
