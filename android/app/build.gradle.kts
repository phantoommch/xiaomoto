import java.util.Properties
import org.gradle.api.tasks.Copy

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

val gitVersionCode: Int = run {
    val fromProperty = (project.findProperty("VERSION_CODE") as? String)?.trim()?.toIntOrNull()
    if (fromProperty != null && fromProperty > 0) return@run fromProperty
    val repo = rootProject.projectDir.parentFile
    try {
        val process = ProcessBuilder("git", "-C", repo.absolutePath, "rev-list", "--count", "HEAD")
            .redirectErrorStream(true)
            .start()
        val text = process.inputStream.bufferedReader().readText().trim()
        val code = process.waitFor()
        text.toIntOrNull()?.takeIf { code == 0 && it > 0 } ?: 32
    } catch (_: Exception) {
        32
    }
}

android {
    namespace = "com.example.xiaomoto"
    compileSdk = 36

    defaultConfig {
        applicationId = "com.example.xiaomoto"
        minSdk = 29
        targetSdk = 36
        versionCode = gitVersionCode
        versionName = "1.0"

        val secrets = Properties()
        val secretsFile = rootProject.file("secrets.properties")
        if (secretsFile.exists()) {
            secretsFile.inputStream().use { secrets.load(it) }
        }
        fun secret(name: String, fallback: String = ""): String {
            val fromFile = (secrets.getProperty(name) ?: "").trim().replace("\"", "")
            if (fromFile.isNotEmpty()) return fromFile
            val fromProject = ((project.findProperty(name) as? String) ?: "").trim().replace("\"", "")
            if (fromProject.isNotEmpty()) return fromProject
            return fallback
        }
        buildConfigField("String", "AMAP_WEB_KEY", "\"${secret("AMAP_WEB_KEY")}\"")
        buildConfigField("String", "AMAP_ANDROID_KEY", "\"${secret("AMAP_ANDROID_KEY")}\"")
        buildConfigField(
            "String",
            "COMMUNITY_API_BASE_URL",
            "\"${secret("COMMUNITY_API_BASE_URL", "https://example.invalid")}\"",
        )
        manifestPlaceholders["AMAP_ANDROID_KEY"] = secret("AMAP_ANDROID_KEY")
        // The 3D map SDK ships arm ABIs only. x86 emulator builds would miss libAMapSDK.
        ndk {
            abiFilters += listOf("arm64-v8a", "armeabi-v7a")
        }
    }

    buildTypes {
        debug {
        }
        release {
            isMinifyEnabled = false
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
    buildFeatures {
        compose = true
        buildConfig = true
    }
    packaging {
        jniLibs {
            useLegacyPackaging = true
        }
        dex {
            useLegacyPackaging = true
        }
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }
}

dependencies {
    val composeBom = platform("androidx.compose:compose-bom:2025.01.01")
    implementation(composeBom)
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-core")
    implementation("androidx.compose.material:material-icons-extended")
    implementation("androidx.activity:activity-compose:1.10.0")
    implementation("androidx.navigation:navigation-compose:2.8.6")
    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.security:security-crypto:1.1.0-alpha06")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.7")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.8.7")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.8.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.9.0")
    implementation("com.amap.api:3dmap:10.0.600")
    // Bundled Chinese model. The public TextRecognition client still tries to
    // download an optional module; ChineseOcr calls the in-APK recognizer instead.
    implementation("com.google.mlkit:text-recognition-chinese:16.0.1")

    testImplementation("junit:junit:4.13.2")
    debugImplementation("androidx.compose.ui:ui-tooling")
}

val extractAmapJni = tasks.register<Copy>("extractAmapJni") {
    val amapJar = configurations.detachedConfiguration(
        dependencies.create("com.amap.api:3dmap:10.0.600"),
    ).apply { isTransitive = false }
    from(zipTree(amapJar.singleFile)) {
        include("lib/**/*.so")
        eachFile {
            path = path.removePrefix("lib/")
        }
        includeEmptyDirs = false
    }
    into(layout.buildDirectory.dir("generated/amapJni"))
}

android.sourceSets.getByName("main").jniLibs.srcDir(layout.buildDirectory.dir("generated/amapJni"))
tasks.matching { it.name == "preBuild" || it.name.contains("JniLib") || it.name.contains("NativeLib") }.configureEach {
    dependsOn(extractAmapJni)
}
