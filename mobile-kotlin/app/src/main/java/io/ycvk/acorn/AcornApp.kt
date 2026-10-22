package io.ycvk.acorn

import io.ycvk.acorn.feature.approvals.ApprovalNotifications
import android.app.Application
import dagger.hilt.android.HiltAndroidApp
import io.ycvk.acorn.core.push.PushManager
import javax.inject.Inject
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.notifications.NotificationUploader
import io.ycvk.acorn.core.theme.ThemePreference
import io.ycvk.acorn.feature.live.AppVisibility
import io.ycvk.acorn.feature.live.LiveNotifications

@HiltAndroidApp
class AcornApp : Application() {
    @Inject lateinit var pushManager: PushManager
    @Inject lateinit var authController: AuthController
    @Inject lateinit var notificationUploader: NotificationUploader
    @Inject lateinit var approvalNotifications: ApprovalNotifications
    @Inject lateinit var themePreference: ThemePreference
    @Inject lateinit var appVisibility: AppVisibility
    @Inject lateinit var liveNotifications: LiveNotifications

    override fun onCreate() {
        super.onCreate()
        themePreference.tellSystem()
        registerActivityLifecycleCallbacks(appVisibility)
        pushManager.initialize()
        approvalNotifications.createChannel()
        liveNotifications.createChannel()
        notificationUploader.initialize()
        authController.loadStoredConnection()
    }
}
