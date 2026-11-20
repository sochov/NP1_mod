package com.np1control.app.ui.screens

import android.graphics.BitmapFactory
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material.icons.filled.Videocam
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.CameraSessionResponse
import com.np1control.app.data.NccApiClient
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.NccTag
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun LiveCameraScreen(
    apiClient: NccApiClient,
    isWide: Boolean,
) {
    val p = LocalNccPalette.current
    val scope = rememberCoroutineScope()

    var viewMode by remember { mutableStateOf("front") } // front | in | both
    var resolution by remember { mutableStateOf("360p") } // 360p | 1080p
    var colorMode by remember { mutableStateOf("auto") } // auto | source
    var playing by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }

    var frontBmp by remember { mutableStateOf<ImageBitmap?>(null) }
    var cabinBmp by remember { mutableStateOf<ImageBitmap?>(null) }
    var frontStatus by remember { mutableStateOf("停止中") }
    var cabinStatus by remember { mutableStateOf("停止中") }

    val cameras = remember(viewMode) {
        when (viewMode) {
            "both" -> listOf("front", "in")
            "in" -> listOf("in")
            else -> listOf("front")
        }
    }

    // Stream loop for each active camera using Web UI /api/v1/camera/sessions API
    LaunchedEffect(playing, viewMode, resolution, colorMode) {
        if (!playing) {
            frontBmp = null
            cabinBmp = null
            frontStatus = "停止中"
            cabinStatus = "停止中"
            return@LaunchedEffect
        }
        error = ""
        val sessionIds = mutableListOf<String>()
        try {
            val jobs = cameras.map { cam ->
                launch {
                    var sessionId = ""
                    try {
                        if (cam == "front") frontStatus = "接続中…" else cabinStatus = "接続中…"
                        val created = apiClient.post<CameraSessionResponse>(
                            "/camera/sessions",
                            """{"source":"$cam","color":"$colorMode","resolution":"$resolution"}""",
                        )
                        sessionId = created.id
                        synchronized(sessionIds) { sessionIds.add(sessionId) }
                        while (isActive && playing) {
                            try {
                                val bytes = apiClient.getBytes("/camera/sessions/$sessionId/stream")
                                val bmp = withContext(Dispatchers.Default) {
                                    BitmapFactory.decodeByteArray(bytes, 0, bytes.size)?.asImageBitmap()
                                }
                                if (bmp != null) {
                                    if (cam == "front") {
                                        frontBmp = bmp
                                        frontStatus = "LIVE · $resolution"
                                    } else {
                                        cabinBmp = bmp
                                        cabinStatus = "LIVE · $resolution"
                                    }
                                }
                                delay(900L)
                            } catch (e: Exception) {
                                if (cam == "front") frontStatus = "再開待ち…" else cabinStatus = "再開待ち…"
                                delay(1800L)
                            }
                        }
                    } catch (e: Exception) {
                        error = e.message ?: "カメラセッションを開始できません"
                        if (cam == "front") frontStatus = "エラー" else cabinStatus = "エラー"
                    } finally {
                        if (sessionId.isNotEmpty()) {
                            runCatching { apiClient.requestRaw("/camera/sessions/$sessionId", "DELETE") }
                        }
                    }
                }
            }
            jobs.forEach { it.join() }
        } finally {
            sessionIds.forEach { id ->
                scope.launch { runCatching { apiClient.requestRaw("/camera/sessions/$id", "DELETE") } }
            }
        }
    }

    DisposableEffect(Unit) {
        onDispose { playing = false }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (error.isNotBlank()) NccBanner(error, BannerTone.ERROR, onDismiss = { error = "" })

        NccPanel(
            title = "ライブカメラ",
            icon = Icons.Default.Videocam,
            headerEnd = {
                if (!playing) {
                    Button(onClick = { playing = true }) {
                        Icon(Icons.Default.PlayArrow, contentDescription = null)
                        Text(" 映像を表示")
                    }
                } else {
                    OutlinedButton(onClick = { playing = false }) {
                        Icon(Icons.Default.Stop, contentDescription = null)
                        Text(" 停止")
                    }
                }
            },
        ) {
            FlowRow(
                horizontalArrangement = Arrangement.spacedBy(10.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                SectionTabs(
                    tabs = listOf("front" to "前方", "in" to "車内", "both" to "両方"),
                    selectedId = viewMode,
                    onSelect = { viewMode = it },
                    modifier = Modifier.fillMaxWidth(if (isWide) 0.45f else 1f),
                )
                SectionTabs(
                    tabs = listOf("360p" to "軽量 360p", "1080p" to "高画質 1080p"),
                    selectedId = resolution,
                    onSelect = { resolution = it },
                    modifier = Modifier.fillMaxWidth(if (isWide) 0.35f else 1f),
                )
                SectionTabs(
                    tabs = listOf("auto" to "色調: 自動", "source" to "元映像"),
                    selectedId = colorMode,
                    onSelect = { colorMode = it },
                    modifier = Modifier.fillMaxWidth(if (isWide) 0.35f else 1f),
                )
            }
        }

        val renderCam: @Composable (String, String, ImageBitmap?, String) -> Unit = { camTitle, statusLabel, bmp, _ ->
            NccPanel(
                title = camTitle,
                headerEnd = { NccTag(text = statusLabel, color = if (statusLabel.startsWith("LIVE")) p.ok else p.muted) },
            ) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .aspectRatio(16f / 9f)
                        .clip(RoundedCornerShape(10.dp))
                        .background(Color(0xFF0A0D12)),
                    contentAlignment = Alignment.Center,
                ) {
                    if (bmp != null) {
                        Image(
                            bitmap = bmp,
                            contentDescription = camTitle,
                            contentScale = ContentScale.Fit,
                            modifier = Modifier.fillMaxSize(),
                        )
                    } else {
                        Text(
                            text = if (playing) "フレームを取得しています…" else "「映像を表示」でライブ表示を開始します",
                            color = Color(0xFFA2ACBB),
                            fontSize = 13.sp,
                        )
                    }
                }
            }
        }

        if (isWide && cameras.size == 2) {
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Box(modifier = Modifier.weight(1f)) { renderCam("前方カメラ", frontStatus, frontBmp, "front") }
                Box(modifier = Modifier.weight(1f)) { renderCam("車内カメラ", cabinStatus, cabinBmp, "in") }
            }
        } else {
            cameras.forEach { cam ->
                if (cam == "front") renderCam("前方カメラ", frontStatus, frontBmp, "front")
                else renderCam("車内カメラ", cabinStatus, cabinBmp, "in")
            }
        }
        Spacer(modifier = Modifier.height(12.dp))
    }
}
