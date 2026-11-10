package com.example.xiaomoto.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class APIURLTest {
    private val origin = "https://api.xiaomoto.cloud"

    @Test
    fun v1PrefixIsNotDoubled() {
        val bases = listOf(
            origin,
            "$origin/",
            "$origin/v1",
            "$origin/v1/",
            "  $origin/v1/hajimi  ",
            "$origin/v1/hajimi/",
        )
        val paths = mapOf(
            APIPath.passwordLogin to "$origin/v1/hajimi/auth/login",
            APIPath.passwordRegister to "$origin/v1/hajimi/auth/register",
            APIPath.requestCode to "$origin/v1/auth/request-code",
            APIPath.verify to "$origin/v1/auth/verify",
            APIPath.emailCode to "$origin/v1/hajimi/auth/email/code",
            APIPath.emailVerify to "$origin/v1/hajimi/auth/email/verify",
            APIPath.passwordReset to "$origin/v1/hajimi/auth/password/reset",
            APIPath.terms to "$origin/v1/legal/terms",
            APIPath.points to "$origin/v1/points",
            APIPath.appLatestAndroid to "$origin/v1/app/latest?platform=android",
        )
        for (base in bases) {
            for ((path, want) in paths) {
                assertEquals("$base + $path", want, APIURL.resolve(base, path))
            }
        }
    }

    @Test
    fun otherMountPrefixIsKeptAndDoubledPathStaysDoubled() {
        assertEquals(
            "https://example.test/gateway/v1/points",
            APIURL.resolve("https://example.test/gateway/", APIPath.points),
        )
        assertEquals(
            "http://127.0.0.1:8787/v1/auth/verify",
            APIURL.resolve("http://127.0.0.1:8787", APIPath.verify),
        )
        assertEquals(
            "$origin/v1/v1/hajimi/auth/login",
            APIURL.resolve(origin, "v1/v1/hajimi/auth/login"),
        )
        assertEquals(
            "$origin/v1/v1/auth/login",
            APIURL.resolve("$origin/v1", "/v1/v1/auth/login"),
        )
    }

    @Test
    fun rejectsEmptyPlaceholderAndUnsafeBases() {
        assertNull(APIURL.resolve("", APIPath.passwordLogin))
        assertNull(APIURL.resolve("   ", APIPath.passwordLogin))
        assertNull(APIURL.resolve("$(COMMUNITY_API_BASE_URL)", APIPath.passwordLogin))
        assertNull(APIURL.resolve("ftp://api.xiaomoto.cloud", APIPath.passwordLogin))
        assertNull(APIURL.resolve(origin, ""))
        assertNull(APIURL.resolve(origin, "../v1/auth/login"))
        assertNull(APIURL.resolve("https://user:secret@api.xiaomoto.cloud", APIPath.passwordLogin))
    }
}
