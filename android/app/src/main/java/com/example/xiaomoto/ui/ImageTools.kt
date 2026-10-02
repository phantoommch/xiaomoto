package com.example.xiaomoto.ui

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Canvas
import android.graphics.Matrix
import android.graphics.Rect
import android.media.ExifInterface
import android.net.Uri
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt

object ImageTools {
    fun decode(context: Context, uri: Uri, maxSide: Int = 1920): Bitmap {
        val bytes = context.contentResolver.openInputStream(uri)?.use { it.readBytes() } ?: error("无法读取图片")
        if (bytes.isEmpty()) error("无法读取图片")
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
        if (bounds.outWidth <= 0 || bounds.outHeight <= 0) error("无法读取图片")
        var sample = 1
        while (bounds.outWidth / sample > maxSide * 2 || bounds.outHeight / sample > maxSide * 2) sample *= 2
        val options = BitmapFactory.Options().apply {
            inSampleSize = sample
            inPreferredConfig = Bitmap.Config.ARGB_8888
        }
        val decoded = BitmapFactory.decodeByteArray(bytes, 0, bytes.size, options) ?: error("无法读取图片")
        return scale(applyExif(bytes, argb(decoded)), maxSide)
    }

    fun pixelate(source: Bitmap, cover: Rect): Bitmap {
        val left = cover.left.coerceIn(0, source.width - 1)
        val top = cover.top.coerceIn(0, source.height - 1)
        val right = cover.right.coerceIn(left + 1, source.width)
        val bottom = cover.bottom.coerceIn(top + 1, source.height)
        val width = right - left
        val height = bottom - top
        val out = source.copy(Bitmap.Config.ARGB_8888, true)
        val region = Bitmap.createBitmap(source, left, top, width, height)
        val smallW = max(1, width / 16)
        val smallH = max(1, height / 16)
        val small = Bitmap.createScaledBitmap(region, smallW, smallH, false)
        val big = Bitmap.createScaledBitmap(small, width, height, false)
        Canvas(out).drawBitmap(big, left.toFloat(), top.toFloat(), null)
        region.recycle()
        small.recycle()
        big.recycle()
        return out
    }

    fun jpeg(bitmap: Bitmap, quality: Int = 82): ByteArray {
        val stream = ByteArrayOutputStream()
        bitmap.compress(Bitmap.CompressFormat.JPEG, quality, stream)
        return stream.toByteArray()
    }

    fun mapRect(viewW: Float, viewH: Float, imageW: Int, imageH: Int, drag: android.graphics.RectF): Rect {
        val scale = min(viewW / imageW, viewH / imageH)
        val drawnW = imageW * scale
        val drawnH = imageH * scale
        val offsetX = (viewW - drawnW) / 2f
        val offsetY = (viewH - drawnH) / 2f
        fun px(value: Float, offset: Float, size: Int) = ((value - offset) / scale).roundToInt().coerceIn(0, size)
        return Rect(
            px(drag.left, offsetX, imageW),
            px(drag.top, offsetY, imageH),
            px(drag.right, offsetX, imageW),
            px(drag.bottom, offsetY, imageH),
        )
    }

    private fun argb(bitmap: Bitmap): Bitmap {
        if (bitmap.config == Bitmap.Config.ARGB_8888) return bitmap
        val copy = bitmap.copy(Bitmap.Config.ARGB_8888, false)
        bitmap.recycle()
        return copy
    }

    private fun applyExif(bytes: ByteArray, bitmap: Bitmap): Bitmap {
        val orientation = runCatching {
            ExifInterface(ByteArrayInputStream(bytes)).getAttributeInt(
                ExifInterface.TAG_ORIENTATION,
                ExifInterface.ORIENTATION_NORMAL,
            )
        }.getOrDefault(ExifInterface.ORIENTATION_NORMAL)
        val matrix = Matrix()
        when (orientation) {
            ExifInterface.ORIENTATION_ROTATE_90 -> matrix.postRotate(90f)
            ExifInterface.ORIENTATION_ROTATE_180 -> matrix.postRotate(180f)
            ExifInterface.ORIENTATION_ROTATE_270 -> matrix.postRotate(270f)
            ExifInterface.ORIENTATION_FLIP_HORIZONTAL -> matrix.preScale(-1f, 1f)
            ExifInterface.ORIENTATION_FLIP_VERTICAL -> matrix.preScale(1f, -1f)
            ExifInterface.ORIENTATION_TRANSPOSE -> {
                matrix.postRotate(90f)
                matrix.preScale(-1f, 1f)
            }
            ExifInterface.ORIENTATION_TRANSVERSE -> {
                matrix.postRotate(270f)
                matrix.preScale(-1f, 1f)
            }
            else -> return bitmap
        }
        val rotated = Bitmap.createBitmap(bitmap, 0, 0, bitmap.width, bitmap.height, matrix, true)
        if (rotated != bitmap) bitmap.recycle()
        return argb(rotated)
    }

    private fun scale(bitmap: Bitmap, maxSide: Int): Bitmap {
        val longest = max(bitmap.width, bitmap.height)
        if (longest <= maxSide) return bitmap
        val ratio = maxSide.toFloat() / longest
        val scaled = Bitmap.createScaledBitmap(bitmap, (bitmap.width * ratio).roundToInt(), (bitmap.height * ratio).roundToInt(), true)
        if (scaled != bitmap) bitmap.recycle()
        return scaled
    }
}
