package com.np1control.app

import com.np1control.app.data.AppConfig
import com.np1control.app.data.ConnectionMode
import com.np1control.app.data.NccApiClient
import kotlinx.coroutines.test.runTest
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class NccApiClientTest {

    private lateinit var server: MockWebServer

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    @Test
    fun bootstrapsSessionSendsCsrfOriginRequestIdAndRetriesOn403CsrfRequired() = runTest {
        val baseUrl = server.url("").toString().trimEnd('/')
        val client = NccApiClient {
            AppConfig(
                connectionMode = ConnectionMode.CUSTOM,
                customUrl = baseUrl,
            )
        }

        // 1. Initial session bootstrap
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .addHeader("Set-Cookie", "ncc_session=sess1; Path=/")
                .setBody("""{"csrf":"tok-1","role":"admin","expires":"2026-11-21T00:00:00Z"}"""),
        )
        // 2. First PUT fails with CSRF_REQUIRED
        server.enqueue(
            MockResponse()
                .setResponseCode(403)
                .setBody("""{"status":"error","code":"CSRF_REQUIRED","reason":"セッションを再確認してください"}"""),
        )
        // 3. Automatic re-bootstrap
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .addHeader("Set-Cookie", "ncc_session=sess2; Path=/")
                .setBody("""{"csrf":"tok-2","role":"admin","expires":"2026-11-21T00:00:00Z"}"""),
        )
        // 4. Retried PUT succeeds
        server.enqueue(
            MockResponse()
                .setResponseCode(200)
                .setBody("""{"ok":true}"""),
        )

        val result = client.requestRaw("/device/led/test/stop", method = "POST", jsonBody = "{}")
        assertTrue(result.contains("true"))

        val req1 = server.takeRequest()
        assertEquals("/api/v1/auth/session", req1.path)

        val req2 = server.takeRequest()
        assertEquals("POST", req2.method)
        assertEquals("tok-1", req2.getHeader("X-CSRF-Token"))
        assertEquals(baseUrl, req2.getHeader("Origin"))
        assertNotNull(req2.getHeader("X-Request-ID"))
        assertTrue(req2.getHeader("Cookie").orEmpty().contains("ncc_session=sess1"))

        val req3 = server.takeRequest()
        assertEquals("/api/v1/auth/session", req3.path)

        val req4 = server.takeRequest()
        assertEquals("tok-2", req4.getHeader("X-CSRF-Token"))
        assertTrue(req4.getHeader("Cookie").orEmpty().contains("ncc_session=sess2"))
    }
}
