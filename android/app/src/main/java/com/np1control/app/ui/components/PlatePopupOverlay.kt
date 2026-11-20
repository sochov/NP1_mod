package com.np1control.app.ui.components

import android.media.AudioManager
import android.media.ToneGenerator
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.NotificationsActive
import androidx.compose.material.icons.automirrored.filled.VolumeUp
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil.ImageLoader
import coil.compose.AsyncImage
import coil.request.ImageRequest
import com.np1control.app.data.AppConfig
import com.np1control.app.data.Formatters
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.PlatePopupEvent
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.delay
import java.net.URLEncoder

/**
 * Floating real-time Japanese license plate detection popup overlay.
 * Designed for instant glanceability on both Echo Show 5 (compact landscape) and smartphones (portrait).
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun PlatePopupOverlay(
    events: List<PlatePopupEvent>,
    config: AppConfig,
    apiClient: NccApiClient,
    compactLandscape: Boolean,
    onDismiss: (String) -> Unit,
    onDismissAll: () -> Unit,
    onOpenPlates: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    val latest = events.firstOrNull()
    val context = LocalContext.current
    val imageLoader = remember(apiClient) {
        ImageLoader.Builder(context)
            .okHttpClient(apiClient.okHttpClient)
            .build()
    }

    // Play a short alert chime when a new event arrives (if sound enabled)
    LaunchedEffect(latest?.uid) {
        if (latest != null && config.popupSoundEnabled) {
            runCatching {
                val tone = ToneGenerator(AudioManager.STREAM_NOTIFICATION, 75)
                val toneType = when {
                    latest.isDanger -> ToneGenerator.TONE_CDMA_ALERT_CALL_GUARD
                    latest.isMatched -> ToneGenerator.TONE_PROP_ACK
                    else -> ToneGenerator.TONE_PROP_BEEP
                }
                tone.startTone(toneType, if (latest.isMatched) 240 else 110)
                delay(320)
                tone.release()
            }
        }
    }

    AnimatedVisibility(
        visible = latest != null,
        enter = slideInVertically(initialOffsetY = { -it }) + fadeIn(),
        exit = slideOutVertically(targetOffsetY = { -it }) + fadeOut(),
        modifier = modifier,
    ) {
        if (latest == null) return@AnimatedVisibility
        val rec = latest.envelope.record
        val matched = latest.isMatched
        val danger = latest.isDanger

        // Countdown progress for auto-dismiss
        var remainingRatio by remember(latest.uid) { mutableFloatStateOf(1f) }
        LaunchedEffect(latest.uid, config.popupDisplaySeconds) {
            val ttl = config.popupDisplaySeconds
            if (ttl <= 0) {
                remainingRatio = 1f
                return@LaunchedEffect
            }
            val totalMs = ttl * 1000L
            while (true) {
                val elapsed = System.currentTimeMillis() - latest.detectedAtMs
                val rem = (totalMs - elapsed).coerceAtLeast(0L)
                remainingRatio = rem.toFloat() / totalMs.toFloat()
                if (rem <= 0L) {
                    onDismiss(latest.uid)
                    break
                }
                delay(100L)
            }
        }

        val cardBg = when {
            danger -> p.dangerSoft
            matched -> p.plateHitSoft
            else -> p.paper
        }
        val cardBorder = when {
            danger -> p.danger
            matched -> p.plateHit
            else -> p.accent
        }

        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = if (compactLandscape) 14.dp else 10.dp, vertical = 8.dp),
            horizontalAlignment = if (compactLandscape) Alignment.End else Alignment.CenterHorizontally,
        ) {
            Surface(
                modifier = Modifier
                    .widthIn(max = if (compactLandscape) 460.dp else 520.dp)
                    .fillMaxWidth()
                    .clickable { onOpenPlates(rec.id) },
                shape = RoundedCornerShape(16.dp),
                color = cardBg,
                border = BorderStroke(if (matched || danger) 2.dp else 1.5.dp, cardBorder),
                shadowElevation = 10.dp,
            ) {
                Column(modifier = Modifier.fillMaxWidth()) {
                    // Header strip
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .background(cardBorder.copy(alpha = if (p.isDark) 0.25f else 0.14f))
                            .padding(horizontal = 12.dp, vertical = 6.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        Icon(
                            imageVector = Icons.Default.NotificationsActive,
                            contentDescription = null,
                            tint = cardBorder,
                            modifier = Modifier.size(16.dp),
                        )
                        Text(
                            text = when {
                                danger -> "危険車両検知 (NumberShare)"
                                matched -> "条件一致ナンバーを検知"
                                else -> "ナンバープレートを検知"
                            } + if (latest.isTest) " [テスト]" else "",
                            color = if (danger) p.danger else if (matched) p.plateHit else p.accent,
                            fontSize = 12.sp,
                            fontWeight = FontWeight.Bold,
                            modifier = Modifier.weight(1f),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        Text(
                            text = Formatters.formatTime(rec.firstSeen),
                            color = p.muted,
                            fontSize = 11.sp,
                        )
                        IconButton(
                            onClick = { onDismiss(latest.uid) },
                            modifier = Modifier.size(26.dp),
                        ) {
                            Icon(Icons.Default.Close, contentDescription = "閉じる", tint = p.text, modifier = Modifier.size(16.dp))
                        }
                    }

                    // Main body: large Japanese plate badge + metadata + optional camera crop
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 12.dp, vertical = 10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        JapanesePlateBadge(
                            reading = rec.reading,
                            matched = matched,
                            danger = danger,
                            width = if (compactLandscape) 168.dp else 156.dp,
                        )

                        Column(
                            modifier = Modifier.weight(1f),
                            verticalArrangement = Arrangement.spacedBy(4.dp),
                        ) {
                            if (rec.matches.isNotEmpty()) {
                                FlowRow(
                                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                                    verticalArrangement = Arrangement.spacedBy(4.dp),
                                ) {
                                    rec.matches.forEach { m ->
                                        NccTag(
                                            text = m.ruleName.ifBlank { "条件一致" },
                                            color = Color.White,
                                            background = p.plateHit,
                                        )
                                    }
                                }
                            }
                            latest.envelope.numberShare?.takeIf { it.status == "exact" }?.let { ns ->
                                NccTag(
                                    text = "NumberShare: ${ns.category ?: "登録済み"}",
                                    color = if (danger) Color.White else p.accent,
                                    background = if (danger) p.danger else p.soft,
                                )
                                if (!ns.comment.isNullOrBlank()) {
                                    Text(
                                        text = ns.comment,
                                        color = p.text,
                                        fontSize = 11.sp,
                                        maxLines = 2,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                }
                            }
                            Text(
                                text = "信頼度 ${(rec.reading.confidence * 100).toInt()}% · ${rec.sightings}回 · ${Formatters.plateLocationLabel(rec)}",
                                color = p.muted,
                                fontSize = 11.sp,
                            )
                            if (!rec.speech.isNullOrBlank()) {
                                Row(
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                                ) {
                                    Icon(Icons.AutoMirrored.Filled.VolumeUp, contentDescription = null, tint = p.accent, modifier = Modifier.size(13.dp))
                                    Text(
                                        text = Formatters.speechLabel(rec.speech),
                                        color = p.accent,
                                        fontSize = 11.sp,
                                        fontWeight = FontWeight.Medium,
                                    )
                                }
                            }
                        }

                        // Optional detection frame thumbnail with bounding box
                        if (config.popupShowImage && latest.envelope.imageAvailable) {
                            val encodedId = remember(rec.id) { URLEncoder.encode(rec.id, "UTF-8") }
                            val imgUrl = apiClient.fullUrl("/plate/records/$encodedId/image")
                            Box(
                                modifier = Modifier
                                    .width(if (compactLandscape) 108.dp else 92.dp)
                                    .aspectRatio(16f / 9f)
                                    .clip(RoundedCornerShape(8.dp))
                                    .background(Color(0xFF10141A)),
                            ) {
                                AsyncImage(
                                    model = ImageRequest.Builder(context)
                                        .data(imgUrl)
                                        .crossfade(true)
                                        .build(),
                                    imageLoader = imageLoader,
                                    contentDescription = "検出画像",
                                    contentScale = ContentScale.Crop,
                                    modifier = Modifier.fillMaxSize(),
                                )
                                if (rec.box.size >= 4 && rec.box[2] > 0) {
                                    Canvas(modifier = Modifier.fillMaxSize()) {
                                        val left = (rec.box[0] / 1920.0 * size.width).toFloat()
                                        val top = (rec.box[1] / 1080.0 * size.height).toFloat()
                                        val bw = (rec.box[2] / 1920.0 * size.width).toFloat()
                                        val bh = (rec.box[3] / 1080.0 * size.height).toFloat()
                                        drawRect(
                                            color = p.plateHit,
                                            topLeft = Offset(left, top),
                                            size = Size(bw, bh),
                                            style = Stroke(width = 2.5f),
                                        )
                                    }
                                }
                            }
                        }
                    }

                    // Mini stack if multiple detections arrived close together
                    if (events.size > 1) {
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .background(p.bg.copy(alpha = 0.55f))
                                .padding(horizontal = 12.dp, vertical = 5.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            Text("直前:", color = p.muted, fontSize = 10.sp)
                            events.drop(1).forEach { prev ->
                                NccTag(
                                    text = Formatters.plateDisplay(prev.envelope.record),
                                    color = if (prev.isMatched) p.plateHit else p.text,
                                    background = p.paper,
                                )
                            }
                            Spacer(modifier = Modifier.weight(1f))
                            Text(
                                text = "すべて閉じる",
                                color = p.accent,
                                fontSize = 11.sp,
                                fontWeight = FontWeight.SemiBold,
                                modifier = Modifier.clickable { onDismissAll() },
                            )
                        }
                    }

                    // Auto-dismiss progress bar
                    if (config.popupDisplaySeconds > 0) {
                        LinearProgressIndicator(
                            progress = { remainingRatio },
                            modifier = Modifier
                                .fillMaxWidth()
                                .height(3.dp),
                            color = cardBorder,
                            trackColor = cardBorder.copy(alpha = 0.15f),
                        )
                    }
                }
            }
        }
    }
}
