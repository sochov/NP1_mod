package com.np1control.app.ui.screens

import android.annotation.SuppressLint
import android.graphics.BitmapFactory
import android.os.Handler
import android.os.Looper
import android.webkit.JavascriptInterface
import android.webkit.WebChromeClient
import android.webkit.WebView
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Fullscreen
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material.icons.filled.Videocam
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.np1control.app.data.CameraSessionResponse
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.NccApiException
import com.np1control.app.ui.components.AdaptiveColumns
import com.np1control.app.ui.components.FieldLabel
import com.np1control.app.ui.components.HintText
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.ScreenColumn
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.components.StatusPill
import com.np1control.app.ui.components.Tone
import com.np1control.app.ui.components.PanelAction
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** What a live view reports back for its caption. */
data class LiveStatus(val state: String = "idle", val width: Int = 0, val height: Int = 0, val fps: Double = 0.0)

private fun camName(source: String) = if (source == "front") "前方カメラ" else "車内カメラ"

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun LiveCameraScreen(
    apiClient: NccApiClient,
    isWide: Boolean,
) {
    val p = LocalNccPalette.current
    var view by remember { mutableStateOf("front") }
    var format by remember { mutableStateOf("h264") }
    var resolution by remember { mutableStateOf("360p") }
    var color by remember { mutableStateOf("auto") }
    var playing by remember { mutableStateOf(false) }
    var fullscreen by remember { mutableStateOf<String?>(null) }
    val cameras = when (view) { "both" -> listOf("front", "in"); "in" -> listOf("in"); else -> listOf("front") }

    ScreenColumn {
        NccPanel(
            title = "ライブ映像",
            subtitle = "NP1のカメラ映像をリアルタイムで表示します。",
            icon = Icons.Default.Videocam,
        ) {
            SectionTabs(tabs = listOf("front" to "前方", "in" to "車内", "both" to "両方"), selectedId = view, onSelect = { view = it }, fill = true)
            FieldLabel("表示方法")
            SectionTabs(
                tabs = listOf("h264" to "動画（H.264）", "jpeg" to "静止画（JPEG）"),
                selectedId = format,
                onSelect = { format = it },
                fill = true,
            )
            if (format == "jpeg") {
                SectionTabs(tabs = listOf("360p" to "軽量 640×360", "1080p" to "高解像度 1920×1080"), selectedId = resolution, onSelect = { resolution = it }, fill = true)
                SectionTabs(tabs = listOf("auto" to "色を自動補正", "source" to "元の色"), selectedId = color, onSelect = { color = it }, fill = true)
                HintText("静止画は1秒ごとに更新します。通信量を抑えたいときや、動画が表示できないときに使います。")
            } else {
                HintText("純正カメラのH.264映像をそのまま表示します。表示されない場合は「静止画」に切り替えてください。")
            }
            if (!playing) {
                Button(onClick = { playing = true }, modifier = Modifier.fillMaxWidth()) {
                    Icon(Icons.Default.PlayArrow, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text(" 映像を表示")
                }
            } else {
                OutlinedButton(onClick = { playing = false }, modifier = Modifier.fillMaxWidth()) {
                    Icon(Icons.Default.Stop, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text(" 表示を止める")
                }
            }
        }
        val panel: @Composable (String) -> Unit = { cam ->
            CameraPanel(
                apiClient = apiClient,
                source = cam,
                format = format,
                resolution = resolution,
                color = color,
                playing = playing && fullscreen == null,
                onFullscreen = { fullscreen = cam },
                onFallback = { format = "jpeg" },
            )
        }
        if (cameras.size == 2 && isWide) {
            AdaptiveColumns(minColumnWidth = 280.dp, left = { panel("front") }, right = { panel("in") })
        } else cameras.forEach { panel(it) }
        Text(
            "映像はNP1から直接受け取ります。表示を止めるか画面を離れると、カメラの配信も終了します。",
            color = p.faint,
            style = MaterialTheme.typography.labelSmall,
        )
    }

    fullscreen?.let { cam ->
        Dialog(onDismissRequest = { fullscreen = null }, properties = DialogProperties(usePlatformDefaultWidth = false)) {
            Box(Modifier.fillMaxSize().background(Color.Black)) {
                LiveSurface(apiClient, cam, format, resolution, color, playing = true, onStatus = {}, onFallback = { format = "jpeg" }, modifier = Modifier.fillMaxSize())
                IconButton(onClick = { fullscreen = null }, modifier = Modifier.align(Alignment.TopEnd).padding(12.dp)) {
                    Icon(Icons.Default.Close, contentDescription = "全画面表示を閉じる", tint = Color.White)
                }
            }
        }
    }
}

@Composable
private fun CameraPanel(
    apiClient: NccApiClient,
    source: String,
    format: String,
    resolution: String,
    color: String,
    playing: Boolean,
    onFullscreen: () -> Unit,
    onFallback: () -> Unit,
) {
    val p = LocalNccPalette.current
    var status by remember(source, format) { mutableStateOf(LiveStatus()) }
    val caption = when {
        format == "h264" && status.state == "live" -> "${status.width} × ${status.height}${if (status.fps > 0) " · ${status.fps.toInt()} fps" else ""} · H.264"
        format == "h264" -> "H.264"
        else -> "${if (resolution == "1080p") "1920 × 1080" else "640 × 360"} · 最大 1 fps · JPEG"
    }
    NccPanel(
        title = camName(source),
        headerEnd = {
            StatusPill(
                when {
                    !playing -> "停止中"
                    status.state == "live" -> "ライブ"
                    status.state == "unsupported" -> "非対応"
                    status.state == "error" -> "エラー"
                    status.state == "waiting" || status.state == "retry" -> "再開待ち"
                    else -> "接続中"
                },
                when {
                    playing && status.state == "live" -> Tone.DANGER
                    status.state == "error" || status.state == "unsupported" -> Tone.WARN
                    else -> Tone.NEUTRAL
                },
            )
            PanelAction(onClick = onFullscreen, enabled = playing) { Icon(Icons.Default.Fullscreen, contentDescription = "全画面で表示", tint = p.muted) }
        },
    ) {
        Box(
            Modifier.fillMaxWidth().aspectRatio(16f / 9f).clip(RoundedCornerShape(14.dp)).background(Color(0xFF0B0E13)),
            contentAlignment = Alignment.Center,
        ) {
            if (playing) {
                LiveSurface(apiClient, source, format, resolution, color, playing = true, onStatus = { status = it }, onFallback = onFallback, modifier = Modifier.fillMaxSize())
            }
            if (!playing || status.state != "live") {
                Column(horizontalAlignment = Alignment.CenterHorizontally, modifier = Modifier.padding(16.dp)) {
                    Icon(Icons.Default.Videocam, contentDescription = null, tint = Color(0xFF6E7787), modifier = Modifier.size(34.dp))
                    Text(
                        when {
                            !playing -> "カメラは停止しています"
                            status.state == "waiting" || status.state == "retry" -> "映像の再開を待っています"
                            status.state == "unsupported" -> "この端末は動画表示に対応していません"
                            else -> "映像を開いています…"
                        },
                        color = Color(0xFFD5DAE3),
                        style = MaterialTheme.typography.titleSmall,
                    )
                    Text(
                        if (!playing) "「映像を表示」でライブ映像を開きます。" else "カメラの出力が再開すると自動で表示します。",
                        color = Color(0xFF9AA4B4),
                        fontSize = 11.sp,
                    )
                }
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(caption, color = p.muted, style = MaterialTheme.typography.labelSmall, modifier = Modifier.weight(1f))
            if (format == "h264" && playing && (status.state == "unsupported" || status.state == "retry")) {
                TextButton(onClick = onFallback) { Text("静止画に切り替える", fontSize = 12.sp) }
            }
        }
    }
}

/** H.264 (WebRTC in a WebView) or JPEG stills, depending on [format]. */
@Composable
fun LiveSurface(
    apiClient: NccApiClient,
    source: String,
    format: String,
    resolution: String,
    color: String,
    playing: Boolean,
    onStatus: (LiveStatus) -> Unit,
    onFallback: () -> Unit,
    modifier: Modifier = Modifier,
) {
    if (!playing) return
    if (format == "h264") H264View(apiClient, source, onStatus, modifier)
    else JpegView(apiClient, source, resolution, color, onStatus, modifier)
}

@SuppressLint("SetJavaScriptEnabled", "JavascriptInterface")
@Composable
private fun H264View(apiClient: NccApiClient, source: String, onStatus: (LiveStatus) -> Unit, modifier: Modifier) {
    val context = LocalContext.current
    val base = apiClient.currentBaseUrl.trimEnd('/')
    val html = remember { context.assets.open("live/h264.html").bufferedReader().use { it.readText() } }
    val web = remember(source, base) {
        WebView(context).apply {
            setBackgroundColor(android.graphics.Color.rgb(11, 14, 19))
            settings.javaScriptEnabled = true
            settings.mediaPlaybackRequiresUserGesture = false
            settings.domStorageEnabled = false
            webChromeClient = WebChromeClient()
            addJavascriptInterface(object {
                @JavascriptInterface
                fun status(state: String, w: Int, h: Int, fps: Double) {
                    Handler(Looper.getMainLooper()).post { onStatus(LiveStatus(state, w, h, fps)) }
                }
            }, "NccBridge")
            loadDataWithBaseURL("$base/app-live#source=$source", html, "text/html", "utf-8", null)
        }
    }
    DisposableEffect(web) {
        onDispose {
            web.evaluateJavascript("window.nccStop && window.nccStop()", null)
            Handler(Looper.getMainLooper()).postDelayed({ web.destroy() }, 1500)
        }
    }
    AndroidView(factory = { web }, modifier = modifier)
}

@Composable
private fun JpegView(apiClient: NccApiClient, source: String, resolution: String, color: String, onStatus: (LiveStatus) -> Unit, modifier: Modifier) {
    var frame by remember(source, resolution, color) { mutableStateOf<ImageBitmap?>(null) }
    var session by remember(source, resolution, color) { mutableStateOf("") }
    LaunchedEffect(source, resolution, color) {
        onStatus(LiveStatus("loading"))
        while (isActive) {
            runCatching {
                if (session.isEmpty()) {
                    session = apiClient.post<CameraSessionResponse>(
                        "/camera/sessions",
                        """{"source":"$source","color":"$color","resolution":"$resolution"}""",
                    ).id
                }
                val bytes = apiClient.getBytes("/camera/sessions/$session/stream")
                withContext(Dispatchers.Default) { BitmapFactory.decodeByteArray(bytes, 0, bytes.size)?.asImageBitmap() }
            }.onSuccess { bmp ->
                if (bmp != null) {
                    frame = bmp
                    onStatus(LiveStatus("live", bmp.width, bmp.height, 1.0))
                } else onStatus(LiveStatus("waiting"))
                delay(1000)
            }.onFailure { e ->
                if (e is NccApiException && (e.statusCode == 403 || e.statusCode == 404)) session = ""
                onStatus(LiveStatus("waiting"))
                delay(2000)
            }
        }
    }
    DisposableEffect(source, resolution, color) {
        onDispose {
            val id = session
            if (id.isNotEmpty()) {
                @OptIn(kotlinx.coroutines.DelicateCoroutinesApi::class)
                kotlinx.coroutines.GlobalScope.launch(Dispatchers.IO) { runCatching { apiClient.requestRaw("/camera/sessions/$id", "DELETE") } }
            }
        }
    }
    frame?.let { Image(it, contentDescription = "ライブ映像", contentScale = ContentScale.Fit, modifier = modifier) }
}
