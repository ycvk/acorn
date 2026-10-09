package io.ycvk.acorn.core.di

import java.io.File
import androidx.core.app.NotificationManagerCompat
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.notifications.NotificationQueue
import io.ycvk.acorn.core.notifications.NotificationPreferences
import io.ycvk.acorn.core.notifications.NotificationDelivery
import io.ycvk.acorn.data.repository.PhoneNotificationRepository

import android.content.Context
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.SecureStore
import io.ycvk.acorn.core.sse.RunEventProjection
import io.ycvk.acorn.data.repository.AuthRepository
import javax.inject.Singleton

/**
 * Application-scoped Hilt module for core infrastructure: encrypted credential storage,
 * the pairing repository, the auth controller, and the run-event projection.
 *
 * [io.ycvk.acorn.core.auth.AuthController] is a `@Singleton` (not a ViewModel) so it
 * can be shared across `@HiltViewModel`s and injected directly into composables via
 * `hiltViewModel()` on a host ViewModel, or collected as a shared StateFlow.
 */
@Module
@InstallIn(SingletonComponent::class)
object AppModule {

    @Provides
    @Singleton
    fun provideSecureStore(@ApplicationContext context: Context): SecureStore =
        SecureStore(context)

    @Provides
    @Singleton
    fun provideAuthRepository(): AuthRepository = AuthRepository()

    @Provides
    @Singleton
    fun provideAuthController(
        secureStore: SecureStore,
        authRepository: AuthRepository,
    ): AuthController = AuthController(secureStore, authRepository)

    @Provides
    @Singleton
    fun provideRunEventProjection(): RunEventProjection = RunEventProjection()

    @Provides
    @Singleton
    fun provideNotificationQueue(@ApplicationContext context: Context): NotificationQueue =
        NotificationQueue(File(context.filesDir, "phone-notifications.json"))

    @Provides
    @Singleton
    fun provideNotificationDelivery(
        @ApplicationContext context: Context,
        queue: NotificationQueue,
        auth: AuthController,
        preferences: NotificationPreferences,
        repository: PhoneNotificationRepository,
    ): NotificationDelivery = NotificationDelivery(
        queue = queue,
        profile = { (auth.authState.value as? AuthState.Connected)?.profile },
        allowed = { preferences.allowed.value },
        hasAccess = { NotificationManagerCompat.getEnabledListenerPackages(context).contains(context.packageName) },
        send = repository::send,
    )
}
