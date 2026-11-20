package com.np1control.app.ui.components

import android.graphics.BitmapFactory
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.CameraSessionResponse
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.NccApiException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.GlobalScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout

/** State of the low-load JPEG previews shown on the dashboard. */
class LiveFrameState {
    val frames = mutableStateMapOf<String, ImageBitmap>()
    val waiting = mutableStateMapOf<String, Boolean>()
    var paused by mutableStateOf(false)
}

fun cameraLabel(source: String) = if (source == "front") "前方" else "車内"

/**
 * Fetches still frames for [sources] one after another every two seconds, like the Web UI's
 * dashboard cameras (the NP1 broker serialises JPEG conversion across both cameras). Sessions are
 * released when the composable leaves the screen; the server also drops them after 30 s idle.
 */
@Composable
fun rememberLiveFrames(apiClient: NccApiClient, sources: List<String>, enabled: Boolean = true): LiveFrameState {
    val state = remember { LiveFrameState() }
    val sessions = remember { mutableMapOf<String, String>() }
    val running = enabled && !state.paused
    LaunchedEffect(sources, running) {
        if (!running) return@LaunchedEffect
        while (isActive) {
            for (source in sources) {
                runCatching {
                    val id = sessions[source] ?: apiClient.post<CameraSessionResponse>(
                        "/camera/sessions",
                        """{"source":"$source","color":"auto"}""",
                    ).id.also { sessions[source] = it }
                    val bytes = withTimeout(8000) { apiClient.getBytes("/camera/sessions/$id/stream") }
                    val bmp = withContext(Dispatchers.Default) {
                        BitmapFactory.decodeByteArray(bytes, 0, bytes.size)?.asImageBitmap()
                    }
                    if (bmp != null) {
                        state.frames[source] = bmp
                        state.waiting[source] = false
                    } else state.waiting[source] = true
                }.onFailure { e ->
                    if (e is NccApiException && (e.statusCode == 403 || e.statusCode == 404)) sessions.remove(source)
                    state.waiting[source] = true
                }
            }
            delay(2000L)
        }
    }
    DisposableEffect(Unit) {
        onDispose {
            val ids = sessions.values.toList()
            sessions.clear()
            if (ids.isNotEmpty()) releaseSessions(apiClient, ids)
        }
    }
    return state
}

@OptIn(kotlinx.coroutines.DelicateCoroutinesApi::class)
private fun releaseSessions(apiClient: NccApiClient, ids: List<String>) {
    val scope: CoroutineScope = GlobalScope
    scope.launch(Dispatchers.IO) {
        ids.forEach { id -> runCatching { apiClient.requestRaw("/camera/sessions/$id", "DELETE") } }
    }
}

/** One camera picture with its caption; shows a quiet placeholder while no frame has arrived. */
@Composable
fun CameraFrame(
    state: LiveFrameState,
    source: String,
    modifier: Modifier = Modifier,
    onClick: (() -> Unit)? = null,
    corner: Int = 14,
) {
    val bmp = state.frames[source]
    Box(
        modifier = modifier
            .aspectRatio(16f / 9f)
            .clip(RoundedCornerShape(corner.dp))
            .background(Color(0xFF0B0E13))
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier),
        contentAlignment = Alignment.Center,
    ) {
        if (bmp != null) {
            Image(bmp, contentDescription = "${cameraLabel(source)}のライブ映像", contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
        } else {
            Text(if (state.paused) "停止中" else "映像を待っています", color = Color(0xFF9AA4B4), fontSize = 11.sp)
        }
        Box(
            modifier = Modifier
                .align(Alignment.BottomStart)
                .padding(6.dp)
                .clip(RoundedCornerShape(6.dp))
                .background(Color(0x99000000))
                .padding(horizontal = 6.dp, vertical = 2.dp),
        ) {
            Text(cameraLabel(source), color = Color.White, fontSize = 10.sp, fontWeight = FontWeight.SemiBold)
        }
    }
}
