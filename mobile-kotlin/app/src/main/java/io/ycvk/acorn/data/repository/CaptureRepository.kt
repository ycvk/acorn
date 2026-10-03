package io.ycvk.acorn.data.repository

import io.ycvk.acorn.api.apis.CapturesApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.CaptureAccepted
import io.ycvk.acorn.core.auth.ConnectionProfile
import java.io.File
import javax.inject.Inject
import javax.inject.Singleton

/** Sends shares to the paired server. Blocking; call from an IO dispatcher. */
@Singleton
class CaptureRepository @Inject constructor() {
    fun send(profile: ConnectionProfile, text: String?, subject: String?, image: File?): CaptureAccepted {
        ApiClient.accessToken = profile.accessToken
        return CapturesApi(basePath = profile.serverUrl).clientCreateCapture(text = text, subject = subject, image = image)
    }
}
