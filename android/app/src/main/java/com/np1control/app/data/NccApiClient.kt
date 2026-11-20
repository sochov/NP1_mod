package com.np1control.app.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.put
import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.IOException
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit

/** API failure. [message] is the server's own (Japanese) reason, or a plain explanation. */
class NccApiException(
    val statusCode: Int,
    val code: String,
    override val message: String,
) : IOException(message) {
    val isConflict: Boolean get() = statusCode == 409 || code == "REVISION_CONFLICT"
}

class InMemoryCookieJar : CookieJar {
    private val store = ConcurrentHashMap<String, MutableList<Cookie>>()

    override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
        val list = store.getOrPut(url.host) { mutableListOf() }
        synchronized(list) {
            for (cookie in cookies) {
                list.removeAll { it.name == cookie.name && it.path == cookie.path }
                if (cookie.expiresAt >= System.currentTimeMillis()) {
                    list.add(cookie)
                }
            }
        }
    }

    override fun loadForRequest(url: HttpUrl): List<Cookie> {
        val list = store[url.host] ?: return emptyList()
        val now = System.currentTimeMillis()
        return synchronized(list) {
            list.removeAll { it.expiresAt < now }
            list.filter { it.matches(url) }.toList()
        }
    }

    fun clear() {
        store.clear()
    }
}

data class ConnectionState(
    val connected: Boolean = false,
    val activeBaseUrl: String = AppConfig.DEFAULT_HOTSPOT_URL,
    val activeLabel: String = "未接続",
    val lastError: String? = null,
)

class NccApiClient(
    private val configProvider: () -> AppConfig,
) {
    val json = Json {
        ignoreUnknownKeys = true
        coerceInputValues = true
        isLenient = true
        explicitNulls = false
        encodeDefaults = true
    }

    val cookieJar = InMemoryCookieJar()

    val okHttpClient: OkHttpClient = OkHttpClient.Builder()
        .cookieJar(cookieJar)
        .connectTimeout(5, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .writeTimeout(20, TimeUnit.SECONDS)
        .retryOnConnectionFailure(true)
        .build()

    private val sseClient: OkHttpClient = okHttpClient.newBuilder()
        .readTimeout(0, TimeUnit.MILLISECONDS)
        .build()

    private val probeClient: OkHttpClient = okHttpClient.newBuilder()
        .connectTimeout(2500, TimeUnit.MILLISECONDS)
        .readTimeout(8000, TimeUnit.MILLISECONDS)
        .build()

    private val sessionMutex = Mutex()
    @Volatile
    private var csrfToken: String = ""
    @Volatile
    private var resolvedBaseUrl: String = AppConfig.DEFAULT_HOTSPOT_URL

    private val _connectionState = MutableStateFlow(ConnectionState())
    val connectionState: StateFlow<ConnectionState> = _connectionState.asStateFlow()

    val currentBaseUrl: String
        get() = resolvedBaseUrl

    fun fullUrl(apiPath: String): String {
        val base = resolvedBaseUrl.trimEnd('/')
        val path = if (apiPath.startsWith("/api/")) apiPath else "/api/v1$apiPath"
        return "$base$path"
    }

    fun resetSession() {
        csrfToken = ""
        cookieJar.clear()
        val candidates = configProvider().candidateUrls()
        resolvedBaseUrl = candidates.firstOrNull() ?: AppConfig.DEFAULT_HOTSPOT_URL
        _connectionState.value = ConnectionState(
            connected = false,
            activeBaseUrl = resolvedBaseUrl,
            activeLabel = labelFor(resolvedBaseUrl),
        )
    }

    private fun labelFor(url: String): String {
        val cfg = configProvider()
        return when {
            url == cfg.normalizedHotspotUrl() -> "ホットスポット"
            url == cfg.normalizedTailscaleUrl() -> "Tailscale"
            url == cfg.normalizedCustomUrl() -> "URL指定"
            else -> url.removePrefix("http://").removePrefix("https://")
        }
    }

    /**
     * Resolves the best reachable NP1 URL according to ConnectionMode
     * (e.g., Hotspot 192.168.226.1 first, then Tailscale IP fallback) and bootstraps CSRF session.
     */
    suspend fun ensureSession(forceReprobe: Boolean = false): SessionResponse =
        withContext(Dispatchers.IO) {
            sessionMutex.withLock {
                if (!forceReprobe && csrfToken.isNotEmpty() && _connectionState.value.connected) {
                    return@withLock SessionResponse(csrf = csrfToken)
                }
                val candidates = configProvider().candidateUrls()
                var lastErr: Exception? = null
                // NP1 can stall for a few seconds under load; try every candidate twice.
                for (candidate in candidates + candidates) {
                    try {
                        val session = fetchSession(candidate, useFastTimeout = candidates.size > 1)
                        resolvedBaseUrl = candidate
                        csrfToken = session.csrf
                        _connectionState.value = ConnectionState(
                            connected = true,
                            activeBaseUrl = candidate,
                            activeLabel = labelFor(candidate),
                            lastError = null,
                        )
                        return@withLock session
                    } catch (e: Exception) {
                        lastErr = e
                    }
                }
                val fallback = candidates.firstOrNull() ?: AppConfig.DEFAULT_HOTSPOT_URL
                resolvedBaseUrl = fallback
                _connectionState.value = ConnectionState(
                    connected = false,
                    activeBaseUrl = fallback,
                    activeLabel = labelFor(fallback),
                    lastError = friendlyNetworkError(lastErr),
                )
                throw NccApiException(0, "UNREACHABLE", friendlyNetworkError(lastErr))
            }
        }

    suspend fun testConnection(targetUrl: String): Result<SessionResponse> = withContext(Dispatchers.IO) {
        runCatching {
            val normalized = AppConfig.normalizeBaseUrl(targetUrl)
            fetchSession(normalized, useFastTimeout = false)
        }
    }

    data class CandidateTestResult(
        val url: String,
        val ok: Boolean,
        val error: String? = null,
    )

    suspend fun testCandidates(candidates: List<String>): List<CandidateTestResult> = withContext(Dispatchers.IO) {
        val fast = candidates.size > 1
        candidates.distinct().map { raw ->
            val normalized = AppConfig.normalizeBaseUrl(raw)
            try {
                fetchSession(normalized, useFastTimeout = fast)
                CandidateTestResult(url = normalized, ok = true)
            } catch (e: Exception) {
                CandidateTestResult(url = normalized, ok = false, error = e.message ?: "接続失敗")
            }
        }
    }

    private fun fetchSession(baseUrl: String, useFastTimeout: Boolean): SessionResponse {
        val url = "${baseUrl.trimEnd('/')}/api/v1/auth/session"
        val req = Request.Builder().url(url).get().build()
        val client = if (useFastTimeout) probeClient else okHttpClient
        client.newCall(req).execute().use { resp ->
            val bodyStr = resp.body?.string().orEmpty()
            if (!resp.isSuccessful) {
                throw parseError(resp.code, bodyStr)
            }
            return json.decodeFromString(SessionResponse.serializer(), bodyStr)
        }
    }

    suspend fun requestRaw(
        path: String,
        method: String = "GET",
        jsonBody: String? = null,
        contentType: String = "application/json; charset=utf-8",
        retryOnCsrf: Boolean = true,
    ): String = withContext(Dispatchers.IO) {
        if (csrfToken.isEmpty()) {
            ensureSession(forceReprobe = !_connectionState.value.connected)
        }
        val base = resolvedBaseUrl.trimEnd('/')
        val full = if (path.startsWith("/api/")) "$base$path" else "$base/api/v1$path"
        val origin = originOf(base)

        val builder = Request.Builder().url(full)
        if (method != "GET" && method != "HEAD") {
            builder.header("Origin", origin)
            builder.header("X-CSRF-Token", csrfToken)
            builder.header("X-Request-ID", UUID.randomUUID().toString())
            val body = (jsonBody ?: "{}").toRequestBody(contentType.toMediaType())
            builder.method(method, if (method == "DELETE" && jsonBody == null) null else body)
        } else {
            builder.method(method, null)
        }

        try {
            okHttpClient.newCall(builder.build()).execute().use { resp ->
                val text = resp.body?.string().orEmpty()
                if (!resp.isSuccessful) {
                    val err = parseError(resp.code, text)
                    if (resp.code == 403 && err.code == "CSRF_REQUIRED" && retryOnCsrf && method != "GET") {
                        csrfToken = ""
                        ensureSession(forceReprobe = false)
                        return@withContext requestRaw(path, method, jsonBody, contentType, retryOnCsrf = false)
                    }
                    throw err
                }
                if (!_connectionState.value.connected) {
                    _connectionState.value = ConnectionState(
                        connected = true,
                        activeBaseUrl = resolvedBaseUrl,
                        activeLabel = labelFor(resolvedBaseUrl),
                        lastError = null,
                    )
                }
                return@withContext text
            }
        } catch (e: NccApiException) {
            throw e
        } catch (e: IOException) {
            // If AUTO mode has multiple candidates, try failing over once on connection failure
            val candidates = configProvider().candidateUrls()
            if (retryOnCsrf && candidates.size > 1) {
                try {
                    csrfToken = ""
                    ensureSession(forceReprobe = true)
                    return@withContext requestRaw(path, method, jsonBody, contentType, retryOnCsrf = false)
                } catch (_: Exception) {
                    // Fall through to mark disconnected
                }
            }
            // A single slow or failed request does not mean NP1 is gone: confirm with a quick probe.
            val stillReachable = runCatching { fetchSession(resolvedBaseUrl, useFastTimeout = true) }.isSuccess
            if (!stillReachable) {
                _connectionState.value = _connectionState.value.copy(
                    connected = false,
                    lastError = friendlyNetworkError(e),
                )
            }
            throw NccApiException(0, "NETWORK_ERROR", friendlyNetworkError(e))
        }
    }

    suspend fun getBytes(path: String): ByteArray = withContext(Dispatchers.IO) {
        if (csrfToken.isEmpty()) {
            ensureSession(forceReprobe = !_connectionState.value.connected)
        }
        val req = Request.Builder().url(fullUrl(path)).get().build()
        okHttpClient.newCall(req).execute().use { resp ->
            if (!resp.isSuccessful) {
                val errText = resp.body?.string().orEmpty()
                throw parseError(resp.code, errText)
            }
            resp.body?.bytes() ?: ByteArray(0)
        }
    }

    suspend inline fun <reified T> get(path: String): T {
        val raw = requestRaw(path, method = "GET")
        return json.decodeFromString(raw)
    }

    suspend inline fun <reified T> post(path: String, jsonBody: String? = null): T {
        val raw = requestRaw(path, method = "POST", jsonBody = jsonBody)
        return json.decodeFromString(raw)
    }

    suspend inline fun <reified T> put(path: String, jsonBody: String): T {
        val raw = requestRaw(path, method = "PUT", jsonBody = jsonBody)
        return json.decodeFromString(raw)
    }

    suspend inline fun <reified T> delete(path: String): T {
        val raw = requestRaw(path, method = "DELETE")
        return json.decodeFromString(raw)
    }

    suspend fun getJsonElement(path: String): JsonElement {
        val raw = requestRaw(path, method = "GET")
        return json.parseToJsonElement(raw)
    }

    /**
     * Subscribes to /api/v1/events (SSE) and emits on each "refresh" event with exponential backoff.
     */
    fun refreshEvents(): Flow<String> = flow {
        var backoffMs = 2000L
        while (true) {
            try {
                ensureSession(forceReprobe = !_connectionState.value.connected)
                val req = Request.Builder()
                    .url(fullUrl("/events"))
                    .header("Accept", "text/event-stream")
                    .get()
                    .build()
                sseClient.newCall(req).execute().use { resp ->
                    if (!resp.isSuccessful) {
                        throw NccApiException(resp.code, "SSE_FAILED", "イベントストリーム接続失敗 (${resp.code})")
                    }
                    backoffMs = 2000L
                    val source = resp.body?.source() ?: return@use
                    var eventName = ""
                    while (!source.exhausted()) {
                        val line = source.readUtf8Line() ?: break
                        when {
                            line.startsWith("event:") -> eventName = line.removePrefix("event:").trim()
                            line.startsWith("data:") -> {
                                val data = line.removePrefix("data:").trim()
                                if (eventName == "refresh" || eventName.isEmpty()) {
                                    emit(data)
                                }
                            }
                            line.isEmpty() -> eventName = ""
                        }
                    }
                }
            } catch (_: Exception) {
                delay(backoffMs)
                backoffMs = (backoffMs * 2).coerceAtMost(30_000L)
            }
        }
    }.flowOn(Dispatchers.IO)

    private fun originOf(baseUrl: String): String {
        val parsed = baseUrl.toHttpUrlOrNull() ?: return baseUrl
        val defaultPort = (parsed.scheme == "http" && parsed.port == 80) ||
            (parsed.scheme == "https" && parsed.port == 443)
        return if (defaultPort) {
            "${parsed.scheme}://${parsed.host}"
        } else {
            "${parsed.scheme}://${parsed.host}:${parsed.port}"
        }
    }

    private fun parseError(statusCode: Int, body: String): NccApiException {
        val parsed = runCatching { json.decodeFromString(ApiErrorBody.serializer(), body) }.getOrNull()
        val msg = parsed?.reason?.takeIf { it.isNotBlank() } ?: when (statusCode) {
            401, 403 -> "NP1が操作を受け付けませんでした。接続し直してください。"
            404 -> "NP1がこの機能に対応していません。NP1 Control Center を更新してください。"
            409 -> "別の操作で設定が変わりました。最新の状態を読み直してください。"
            in 500..599 -> "NP1で処理できませんでした（$statusCode）。しばらくしてからもう一度お試しください。"
            else -> "NP1が要求を受け付けませんでした（$statusCode）。"
        }
        return NccApiException(statusCode, parsed?.code.orEmpty(), msg)
    }

    /**
     * Reads `/settings` and writes it back with [changes] applied, keeping every other field
     * (map themes, favourites...) exactly as the server sent it.
     */
    suspend fun updateSettings(changes: Map<String, JsonElement>): JsonObject {
        val current = getJsonElement("/settings") as? JsonObject
            ?: throw NccApiException(0, "INVALID_SETTINGS", "表示設定を読み込めません")
        val revision = (current["revision"] as? JsonPrimitive)?.intOrNull ?: 1
        val body = buildJsonObject {
            put("expectedRevision", revision)
            put("settings", JsonObject(current + changes))
        }.toString()
        return json.parseToJsonElement(requestRaw("/settings", "PUT", body)) as JsonObject
    }

    companion object {
        fun friendlyNetworkError(e: Throwable?): String {
            val text = e?.message.orEmpty()
            return when {
                e is NccApiException && e.code != "NETWORK_ERROR" && e.code != "UNREACHABLE" -> e.message
                e is NccApiException -> e.message
                e is java.net.SocketTimeoutException || text.contains("timeout", true) ->
                    "NP1の応答がありません。電波状況や接続先を確認してください。"
                e is java.net.UnknownHostException -> "接続先が見つかりません。接続先の設定を確認してください。"
                else -> "NP1に接続できません。ホットスポットまたはTailscaleへの接続を確認してください。"
            }
        }
    }
}
