package com.example.xiaomoto.data

import com.example.xiaomoto.MotoApplication
import com.example.xiaomoto.core.EnforcementPoint
import com.example.xiaomoto.core.PointCodec
import com.example.xiaomoto.net.CommunityClient
import java.io.File

class UploadQueue(
    dir: File,
    private val settings: SettingsStore,
    private val points: PointRepository,
) {
    private val file = File(dir, "upload-queue.json")
    private val lock = Any()

    fun saveAndUpload(point: EnforcementPoint): Boolean {
        points.upsert(point)
        return submitOrQueue(point)
    }

    fun submitOrQueue(point: EnforcementPoint, imageJpeg: ByteArray? = null): Boolean {
        synchronized(lock) {
            if (imageJpeg != null && imageJpeg.isNotEmpty()) storeImage(point.id, imageJpeg)
            if (!loggedIn()) {
                enqueueLocked(point)
                return false
            }
            val base = settings.communityBaseUrl.value.trim()
            if (base.isEmpty()) {
                enqueueLocked(point)
                return false
            }
            return try {
                CommunityClient.submit(base, settings.deviceId, point, imageJpeg ?: loadImage(point.id))
                removeLocked(point.id)
                true
            } catch (_: Exception) {
                enqueueLocked(point)
                false
            }
        }
    }

    fun flush(): Int {
        synchronized(lock) {
            if (!loggedIn()) return 0
            val pending = read()
            var uploaded = 0
            for (point in pending) {
                val base = settings.communityBaseUrl.value.trim()
                if (base.isEmpty()) break
                try {
                    CommunityClient.submit(base, settings.deviceId, point, loadImage(point.id))
                    removeLocked(point.id)
                    uploaded += 1
                } catch (_: Exception) {
                    enqueueLocked(point)
                    break
                }
            }
            return uploaded
        }
    }

    private fun loggedIn(): Boolean {
        return !MotoApplication.instance.session.token.value.isNullOrBlank()
    }

    private fun enqueueLocked(point: EnforcementPoint) {
        write(read().filterNot { it.id == point.id } + point)
    }

    private fun removeLocked(id: String) {
        write(read().filterNot { it.id == id })
        imageFile(id).delete()
    }

    private fun imageFile(id: String): File = File(File(file.parentFile, "upload-images"), "$id.jpg")

    private fun storeImage(id: String, bytes: ByteArray) {
        val dest = imageFile(id)
        dest.parentFile?.mkdirs()
        dest.writeBytes(bytes)
    }

    private fun loadImage(id: String): ByteArray? {
        val dest = imageFile(id)
        if (!dest.exists()) return null
        return dest.readBytes()
    }

    private fun read(): List<EnforcementPoint> {
        if (!file.exists()) return emptyList()
        return runCatching { PointCodec.decodeJSON(file.readText()) }.getOrDefault(emptyList())
    }

    private fun write(points: List<EnforcementPoint>) {
        file.parentFile?.mkdirs()
        file.writeText(PointCodec.encodeJSON(points))
    }
}
