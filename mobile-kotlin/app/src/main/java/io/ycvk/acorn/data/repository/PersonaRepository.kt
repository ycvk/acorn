package io.ycvk.acorn.data.repository

import io.ycvk.acorn.api.apis.PersonaApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.Persona
import io.ycvk.acorn.api.models.PersonaRevisionSummary
import io.ycvk.acorn.api.models.SavePersonaRequest
import io.ycvk.acorn.core.auth.ConnectionProfile
import javax.inject.Inject
import javax.inject.Singleton

/** Reads and saves the persona on the paired server. Blocking; call from an IO dispatcher. */
@Singleton
class PersonaRepository @Inject constructor() {
    fun current(profile: ConnectionProfile): Persona = api(profile).clientGetPersona()

    fun revisions(profile: ConnectionProfile): List<PersonaRevisionSummary> = api(profile).clientListPersonaRevisions().revisions

    fun revision(profile: ConnectionProfile, revision: Long): Persona = api(profile).clientGetPersonaRevision(revision)

    fun save(profile: ConnectionProfile, content: String, baseRevision: Long): Persona =
        api(profile).clientSavePersona(SavePersonaRequest(content = content, baseRevision = baseRevision))

    private fun api(profile: ConnectionProfile): PersonaApi {
        ApiClient.accessToken = profile.accessToken
        return PersonaApi(basePath = profile.serverUrl)
    }
}
