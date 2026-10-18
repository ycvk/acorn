package io.ycvk.acorn.data.repository

import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.apis.SkillsApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.RollbackSkillRequest
import io.ycvk.acorn.api.models.SetSkillSecretRequest
import io.ycvk.acorn.api.models.SkillChangeRequest
import io.ycvk.acorn.api.models.SkillDetail
import io.ycvk.acorn.api.models.SkillDiff
import io.ycvk.acorn.api.models.SkillRevision
import io.ycvk.acorn.api.models.SkillRevisionSummary
import io.ycvk.acorn.api.models.SkillSecret
import io.ycvk.acorn.api.models.SkillSummary
import io.ycvk.acorn.core.auth.ConnectionProfile
import javax.inject.Inject
import javax.inject.Singleton

/** Reads the skills on the paired server and makes the owner's changes to them. Blocking; call from an IO dispatcher. */
@Singleton
class SkillRepository @Inject constructor() {
    fun list(profile: ConnectionProfile): List<SkillSummary> = api(profile).clientListSkills().items

    fun detail(profile: ConnectionProfile, name: String): SkillDetail = api(profile).clientGetSkill(name).item

    fun revisions(profile: ConnectionProfile, name: String): List<SkillRevisionSummary> = api(profile).clientListSkillRevisions(name).revisions

    fun revision(profile: ConnectionProfile, name: String, revision: Long): SkillRevision = api(profile).clientGetSkillRevision(name, revision)

    fun diff(profile: ConnectionProfile, name: String, from: Long, to: Long): SkillDiff = api(profile).clientDiffSkillRevisions(name, from, to)

    fun rollback(profile: ConnectionProfile, name: String, revision: Long, baseRevision: Long): SkillSummary =
        api(profile).clientRollbackSkill(name, RollbackSkillRequest(revision = revision, baseRevision = baseRevision))

    fun retire(profile: ConnectionProfile, name: String, baseRevision: Long): SkillSummary =
        api(profile).clientRetireSkill(name, SkillChangeRequest(baseRevision = baseRevision))

    fun restore(profile: ConnectionProfile, name: String, baseRevision: Long): SkillSummary =
        api(profile).clientRestoreSkill(name, SkillChangeRequest(baseRevision = baseRevision))

    fun setSecret(profile: ConnectionProfile, name: String, secret: String, value: String): List<SkillSecret> =
        api(profile).clientSetSkillSecret(name, secret, SetSkillSecretRequest(value = value)).secrets

    fun clearSecret(profile: ConnectionProfile, name: String, secret: String): List<SkillSecret> =
        api(profile).clientClearSkillSecret(name, secret).secrets

    /** The conversation a run happened in. */
    fun threadOfRun(profile: ConnectionProfile, runId: String): String {
        ApiClient.accessToken = profile.accessToken
        return ClientApi(basePath = profile.serverUrl).clientGetRun(runId).threadId
    }

    private fun api(profile: ConnectionProfile): SkillsApi {
        ApiClient.accessToken = profile.accessToken
        return SkillsApi(basePath = profile.serverUrl)
    }
}
