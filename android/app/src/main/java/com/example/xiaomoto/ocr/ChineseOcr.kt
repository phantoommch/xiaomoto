package com.example.xiaomoto.ocr

import android.content.Context
import android.graphics.Bitmap
import android.os.SystemClock

/**
 * Runs the Chinese model that ships inside the APK.
 *
 * [com.google.mlkit.vision.text.TextRecognition] asks Dynamite / Play services
 * for an optional module. On Huawei and Honor that download never arrives, so
 * the public client returns no text. This talks to the bundled recognizer
 * directly and reads models from assets/mlkit-google-ocr-models.
 */
object ChineseOcr {
    private const val CONFIG = "taser_tflite_gocrchinese_and_latin_mbv2_aksara_layout_gcn_mobile"
    private const val MODULE = "optional-module-text-chinese"

    fun recognize(context: Context, bitmap: Bitmap): String {
        val booleanType = java.lang.Boolean.TYPE
        val intType = Integer.TYPE
        val longType = java.lang.Long.TYPE
        val optionsClass = Class.forName("com.google.android.gms.internal.mlkit_vision_text_bundled_common.zbom")
        val options = optionsClass.getConstructor(
            String::class.java,
            String::class.java,
            String::class.java,
            booleanType,
            intType,
            String::class.java,
            booleanType,
        ).newInstance(
            CONFIG,
            MODULE,
            null,
            false,
            2,
            "zh",
            false,
        )
        val creator = Class.forName("com.google.mlkit.vision.text.bundled.common.BundledTextRecognizerCreator")
            .getDeclaredConstructor()
            .newInstance()
        val wrapper = Class.forName("com.google.android.gms.dynamic.IObjectWrapper")
        val wrap = Class.forName("com.google.android.gms.dynamic.ObjectWrapper")
            .getMethod("wrap", Any::class.java)
        val recognizer = creator.javaClass.getMethod("newTextRecognizerWithOptions", wrapper, optionsClass)
            .invoke(creator, wrap.invoke(null, context.applicationContext), options)
            ?: error("中文识别器没有创建出来")
        try {
            recognizer.javaClass.getMethod("zbc").invoke(recognizer)
            val imageClass = Class.forName("com.google.android.gms.internal.mlkit_vision_text_bundled_common.zbnx")
            val meta = imageClass.getConstructor(intType, intType, intType, intType, longType).newInstance(
                -1,
                bitmap.width,
                bitmap.height,
                0,
                SystemClock.elapsedRealtime() * 1000,
            )
            val result = recognizer.javaClass.getMethod("zbb", wrapper, imageClass)
                .invoke(recognizer, wrap.invoke(null, bitmap), meta)
                ?: return ""
            val text = result.javaClass.getDeclaredField("zba")
            text.isAccessible = true
            return (text.get(result) as? String).orEmpty()
        } finally {
            runCatching { recognizer.javaClass.getMethod("zbd").invoke(recognizer) }
        }
    }
}
