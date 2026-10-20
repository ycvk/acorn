import java.util.Properties
import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.plugin.compose")
    id("com.google.devtools.ksp")
    id("com.google.dagger.hilt.android")
}

android {
    namespace = "io.ycvk.acorn"
    compileSdk {
        version = release(37) {
            minorApiLevel = 2
        }
    }

    val localProps = Properties().apply {
        rootProject.file("local.properties").takeIf { it.exists() }?.inputStream()?.use { load(it) }
    }

    defaultConfig {
        applicationId = "io.ycvk.acorn"
        minSdk = 26
        targetSdk = 37
        versionCode = 1
        versionName = "0.4.0"
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
        sourceCompatibility = JavaVersion.VERSION_21
        targetCompatibility = JavaVersion.VERSION_21
    }
    buildFeatures {
        compose = true
        buildConfig = true
    }
    packaging {
        resources { excludes += "/META-INF/{AL2.0,LGPL2.1}" }
    }
}

kotlin {
    compilerOptions { jvmTarget = JvmTarget.JVM_21 }
}

dependencies {
    // Compose BOM
    val composeBom = platform("androidx.compose:compose-bom:2026.09.00")
    implementation(composeBom)
    implementation("androidx.core:core-ktx:1.19.1")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.11.0")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.11.0")
    implementation("androidx.activity:activity-compose:1.13.0")
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")

    // Liquid glass: blur, lens refraction and highlights drawn from a copy of what lies behind.
    implementation("io.github.kyant0:backdrop:2.0.1")

    // ViewModel
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.11.0")

    // Hilt
    implementation("com.google.dagger:hilt-android:2.60.1")
    ksp("com.google.dagger:hilt-android-compiler:2.60.1")
    implementation("androidx.hilt:hilt-lifecycle-viewmodel-compose:1.4.0")

    // OkHttp (for SSE + API calls)
    implementation("com.squareup.okhttp3:okhttp:5.5.0")
    implementation("com.squareup.okhttp3:okhttp-sse:5.5.0")

    // Moshi (JSON)
    implementation("com.squareup.moshi:moshi:1.15.2")
    implementation("com.squareup.moshi:moshi-kotlin:1.15.2")

    // EncryptedSharedPreferences
    implementation("androidx.security:security-crypto:1.1.0")

    // Push notifications (FCM)
    implementation(platform("com.google.firebase:firebase-bom:35.0.2"))
    implementation("com.google.firebase:firebase-messaging")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-play-services:1.11.0")

    // Markdown parsing; app/core/ui/Markdown.kt renders the tree with Acorn's typography.
    implementation("org.commonmark:commonmark:0.30.1")
    implementation("org.commonmark:commonmark-ext-gfm-tables:0.30.1")
    implementation("org.commonmark:commonmark-ext-gfm-strikethrough:0.30.1")
    implementation("org.commonmark:commonmark-ext-autolink:0.30.1")

    // Shared images in notes and capture cards.
    implementation("io.coil-kt.coil3:coil-compose:3.6.3")
    implementation("io.coil-kt.coil3:coil-network-okhttp:3.6.3")

    // Turns photos upright before they are resized for sending.
    implementation("androidx.exifinterface:exifinterface:1.4.2")

    // Testing
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.11.0")
    debugImplementation("androidx.compose.ui:ui-tooling")
}
