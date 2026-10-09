import java.util.Properties
plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("com.google.devtools.ksp")
    id("com.google.dagger.hilt.android")
    id("org.jetbrains.kotlin.plugin.serialization")
}

android {
    namespace = "io.ycvk.acorn"
    compileSdk = 35

    val localProps = Properties().apply {
        rootProject.file("local.properties").takeIf { it.exists() }?.inputStream()?.use { load(it) }
    }

    defaultConfig {
        applicationId = "io.ycvk.acorn"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "0.3.1"
        vectorDrawables { useSupportLibrary = true }

        // Firebase is initialized from these values instead of google-services.json,
        // so builds without them still work and the app reports push as unconfigured.
        // Set acorn.firebase.* in local.properties or ACORN_FIREBASE_* in the environment.
        for ((field, key) in listOf(
            "FIREBASE_PROJECT_ID" to "projectId",
            "FIREBASE_APP_ID" to "appId",
            "FIREBASE_API_KEY" to "apiKey",
            "FIREBASE_SENDER_ID" to "senderId",
        )) {
            val value = localProps.getProperty("acorn.firebase.$key")
                ?: System.getenv("ACORN_FIREBASE_" + key.replace(Regex("([A-Z])"), "_$1").uppercase())
                ?: ""
            buildConfigField("String", field, "\"$value\"")
        }
    }

    val keyProps = file("key.properties").let { f ->
        if (f.exists()) {
            Properties().apply { f.inputStream().use { load(it) } }
        } else {
            null
        }
    }

    signingConfigs {
        create("release") {
            keyProps?.let { p ->
                storeFile = file(p.getProperty("storeFile"))
                storePassword = p.getProperty("storePassword")
                keyAlias = p.getProperty("keyAlias")
                keyPassword = p.getProperty("keyPassword")
                storeType = p.getProperty("storeType")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.getByName("release")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    buildFeatures {
        compose = true
        buildConfig = true
    }
    packaging {
        resources { excludes += "/META-INF/{AL2.0,LGPL2.1}" }
    }
}

dependencies {
    // Compose BOM
    val composeBom = platform("androidx.compose:compose-bom:2024.12.01")
    implementation(composeBom)
    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.7")
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")
    implementation("androidx.navigation:navigation-compose:2.8.5")

    // ViewModel
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.8.7")

    // Hilt
    implementation("com.google.dagger:hilt-android:2.53.1")
    ksp("com.google.dagger:hilt-android-compiler:2.53.1")
    implementation("androidx.hilt:hilt-navigation-compose:1.2.0")

    // OkHttp (for SSE + API calls)
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("com.squareup.okhttp3:okhttp-sse:4.12.0")

    // Moshi (JSON)
    implementation("com.squareup.moshi:moshi:1.15.1")
    implementation("com.squareup.moshi:moshi-kotlin:1.15.1")

    // EncryptedSharedPreferences
    implementation("androidx.security:security-crypto:1.1.0-alpha06")

    // Push notifications (FCM)
    implementation(platform("com.google.firebase:firebase-bom:33.7.0"))
    implementation("com.google.firebase:firebase-messaging")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-play-services:1.9.0")

    // Markdown rendering for assistant messages (JitPack).
    implementation("com.github.jeziellago:compose-markdown:0.5.7")

    // Testing
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.9.0")
    debugImplementation("androidx.compose.ui:ui-tooling")
}
