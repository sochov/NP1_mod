package com.np1control.app.ui.screens

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.Movie
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.Formatters
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.data.RecordingCatalog
import com.np1control.app.data.RecordingClip
import com.np1control.app.data.TrackData
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.NccTag
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.components.TrackMapCanvas
import com.np1control.app.ui.components.VideoPlayerBox
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.launch
import java.net.URLEncoder
import java.time.Instant
import java.time.LocalDate

@Composable
fun RecordingsScreen(
    apiClient: NccApiClient,
    initialTimestampIso: String? = null,
    isWide: Boolean,
) {
    val p = LocalNccPalette.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    var selectedDate by remember {
        mutableStateOf(
            initialTimestampIso?.let { Formatters.isoDateJst(it) } ?: Formatters.jstDay(0),
        )
    }
    var catalog by remember { mutableStateOf<RecordingCatalog?>(null) }
    var track by remember { mutableStateOf<TrackData?>(null) }
    var viewMode by remember { mutableStateOf("front") } // front | in | both
    var speed by remember { mutableFloatStateOf(1f) }
    var selectedFront by remember { mutableStateOf<RecordingClip?>(null) }
    var selectedCabin by remember { mutableStateOf<RecordingClip?>(null) }
    var cursorEpochMs by remember { mutableLongStateOf(0L) }
    var seekOffsetMs by remember { mutableLongStateOf(0L) }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }

    fun dayStartMs(ymd: String): Long = runCatching {
        LocalDate.parse(ymd).atStartOfDay(Formatters.JST).toInstant().toEpochMilli()
    }.getOrDefault(0L)

    fun clipAt(clips: List<RecordingClip>, cam: String, epochMs: Long): RecordingClip? =
        clips.firstOrNull { clip ->
            clip.camera == cam &&
                runCatching { Instant.parse(clip.startAt).toEpochMilli() <= epochMs && Instant.parse(clip.endAt).toEpochMilli() > epochMs }
                    .getOrDefault(false)
        }

    fun selectTime(epochMs: Long, clips: List<RecordingClip> = catalog?.clips.orEmpty()) {
        cursorEpochMs = epochMs
        val f = clipAt(clips, "front", epochMs)
        val c = clipAt(clips, "in", epochMs)
        selectedFront = f
        selectedCabin = c
        val primary = if (viewMode == "in") c else f ?: c
        seekOffsetMs = if (primary != null) {
            (epochMs - runCatching { Instant.parse(primary.startAt).toEpochMilli() }.getOrDefault(epochMs)).coerceAtLeast(0L)
        } else 0L
    }

    fun loadDay(ymd: String, targetIso: String? = null) {
        scope.launch {
            loading = true
            error = ""
            runCatching {
                val obs = apiClient.get<Observation>("/recordings?date=$ymd")
                var cat = obs.data?.let { apiClient.json.decodeFromJsonElement(RecordingCatalog.serializer(), it) }
                if (cat != null && targetIso == null && cat.days.none { it.date == ymd } && cat.days.isNotEmpty()) {
                    val latestDay = cat.days.first().date
                    selectedDate = latestDay
                    val obs2 = apiClient.get<Observation>("/recordings?date=$latestDay")
                    cat = obs2.data?.let { apiClient.json.decodeFromJsonElement(RecordingCatalog.serializer(), it) }
                }
                catalog = cat
                val clips = cat?.clips.orEmpty()
                val targetMs = targetIso?.let { runCatching { Instant.parse(it).toEpochMilli() }.getOrNull() }
                if (targetMs != null) {
                    selectTime(targetMs, clips)
                } else {
                    val latestClip = clips.maxByOrNull { runCatching { Instant.parse(it.startAt).toEpochMilli() }.getOrDefault(0L) }
                    if (latestClip != null) {
                        selectTime(Instant.parse(latestClip.startAt).toEpochMilli(), clips)
                    } else {
                        selectedFront = null
                        selectedCabin = null
                    }
                }
            }.onFailure { error = it.message ?: "録画一覧を取得できません" }

            runCatching {
                val obs = apiClient.get<Observation>("/recordings/track?date=$selectedDate")
                track = obs.data?.let { apiClient.json.decodeFromJsonElement(TrackData.serializer(), it) }
            }
            loading = false
        }
    }

    LaunchedEffect(initialTimestampIso) {
        loadDay(selectedDate, initialTimestampIso)
    }

    val clips = catalog?.clips.orEmpty()
    val startOfDay = dayStartMs(selectedDate)
    val nearestTrackPoint = remember(track, cursorEpochMs) {
        val pts = track?.points.orEmpty()
        if (pts.isEmpty() || cursorEpochMs <= 0L) null
        else pts.minByOrNull { pt ->
            val t = runCatching { Instant.parse(pt.at).toEpochMilli() }.getOrDefault(0L)
            kotlin.math.abs(t - cursorEpochMs)
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (error.isNotBlank()) NccBanner(error, BannerTone.ERROR, onDismiss = { error = "" })

        // Date selector bar
        NccPanel(
            title = Formatters.formatJpDate(selectedDate),
            subtitle = catalog?.days?.firstOrNull { it.date == selectedDate }?.let {
                "${it.count} 本 · ${Formatters.durationWithSeconds(it.recordedSeconds)} の録画"
            } ?: if (loading) "録画を読み込んでいます…" else "この日の確定録画はありません",
            icon = Icons.Default.Movie,
            headerEnd = {
                IconButton(onClick = { loadDay(selectedDate) }) {
                    Icon(Icons.Default.Refresh, contentDescription = "更新", tint = p.muted)
                }
            },
        ) {
            val days = catalog?.days.orEmpty()
            if (days.isNotEmpty()) {
                Row(
                    modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    days.take(30).forEach { d ->
                        val sel = d.date == selectedDate
                        NccTag(
                            text = "${d.date.takeLast(5)} (${d.count})",
                            color = if (sel) p.accent else p.text,
                            background = if (sel) p.soft else p.bg,
                            modifier = Modifier.clickable {
                                selectedDate = d.date
                                loadDay(d.date)
                            },
                        )
                    }
                }
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                SectionTabs(
                    tabs = listOf("front" to "前方", "in" to "車内", "both" to "両方"),
                    selectedId = viewMode,
                    onSelect = { viewMode = it },
                    modifier = Modifier.weight(1f),
                )
            }
            SectionTabs(
                tabs = listOf("0.5" to "0.5×", "1.0" to "1×", "1.5" to "1.5×", "2.0" to "2×"),
                selectedId = speed.toString(),
                onSelect = { speed = it.toFloatOrNull() ?: 1f },
            )
        }

        // Video player panel(s)
        val renderPlayer: @Composable (String, RecordingClip?, Boolean) -> Unit = { label, clip, isPrimary ->
            NccPanel(
                title = label,
                subtitle = clip?.let { "${Formatters.formatTime(it.startAt)} — ${Formatters.formatTime(it.endAt)} · ${Formatters.bytes(it.sizeBytes)}" },
                headerEnd = {
                    if (clip != null) {
                        val encId = URLEncoder.encode(clip.id, "UTF-8")
                        val dlUrl = apiClient.fullUrl("/recordings/$encId/download")
                        IconButton(onClick = {
                            runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(dlUrl))) }
                        }) {
                            Icon(Icons.Default.Download, contentDescription = "原本ダウンロード", tint = p.accent)
                        }
                    }
                },
            ) {
                if (clip != null) {
                    val encId = remember(clip.id) { URLEncoder.encode(clip.id, "UTF-8") }
                    val streamUrl = apiClient.fullUrl("/recordings/$encId/stream")
                    VideoPlayerBox(
                        streamUrl = streamUrl,
                        apiClient = apiClient,
                        playbackSpeed = speed,
                        muted = !isPrimary,
                        seekOffsetMs = seekOffsetMs,
                        onPositionChanged = if (isPrimary) { posMs ->
                            val clipStart = runCatching { Instant.parse(clip.startAt).toEpochMilli() }.getOrDefault(0L)
                            if (clipStart > 0L) cursorEpochMs = clipStart + posMs
                        } else null,
                        onEnded = if (isPrimary) {
                            {
                                val endMs = runCatching { Instant.parse(clip.endAt).toEpochMilli() }.getOrDefault(0L)
                                val next = clips
                                    .filter { it.camera == clip.camera }
                                    .sortedBy { it.startAt }
                                    .firstOrNull {
                                        val s = runCatching { Instant.parse(it.startAt).toEpochMilli() }.getOrDefault(0L)
                                        s >= endMs - 500 && s <= endMs + 5000
                                    }
                                if (next != null) {
                                    selectTime(Instant.parse(next.startAt).toEpochMilli(), clips)
                                }
                            }
                        } else null,
                    )
                } else {
                    Box(
                        modifier = Modifier
                            .fillMaxWidth()
                            .height(140.dp)
                            .clip(RoundedCornerShape(10.dp))
                            .background(Color(0xFF0E1218)),
                        contentAlignment = Alignment.Center,
                    ) {
                        Text("この時刻の録画がありません", color = Color(0xFFA2ACBB), fontSize = 12.sp)
                    }
                }
            }
        }

        if (viewMode == "both") {
            if (isWide) {
                Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    Box(modifier = Modifier.weight(1f)) { renderPlayer("前方カメラ", selectedFront, true) }
                    Box(modifier = Modifier.weight(1f)) { renderPlayer("車内カメラ", selectedCabin, false) }
                }
            } else {
                renderPlayer("前方カメラ", selectedFront, true)
                renderPlayer("車内カメラ", selectedCabin, false)
            }
        } else if (viewMode == "in") {
            renderPlayer("車内カメラ", selectedCabin, true)
        } else {
            renderPlayer("前方カメラ", selectedFront, true)
        }

        // 24h timeline bar + clip list
        NccPanel(
            title = "タイムライン",
            headerEnd = {
                if (cursorEpochMs > 0L) {
                    NccTag(text = Formatters.formatClock(cursorEpochMs), color = p.accent, background = p.soft)
                }
            },
        ) {
            val dayDurationMs = 86_400_000.0
            Canvas(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(54.dp)
                    .clip(RoundedCornerShape(8.dp))
                    .background(p.bg)
                    .pointerInput(clips, startOfDay) {
                        detectTapGestures { offset ->
                            val frac = (offset.x / size.width).coerceIn(0f, 1f)
                            val target = startOfDay + (frac * dayDurationMs).toLong()
                            selectTime(target, clips)
                        }
                    },
            ) {
                val laneH = size.height / 2f
                clips.forEach { clip ->
                    val s = runCatching { Instant.parse(clip.startAt).toEpochMilli() }.getOrDefault(startOfDay)
                    val e = runCatching { Instant.parse(clip.endAt).toEpochMilli() }.getOrDefault(s)
                    val x1 = (((s - startOfDay).coerceAtLeast(0L)) / dayDurationMs * size.width).toFloat()
                    val x2 = (((e - startOfDay).coerceAtMost(86_400_000L)) / dayDurationMs * size.width).toFloat()
                    val top = if (clip.camera == "front") 4f else laneH + 4f
                    val color = when {
                        clip.kind == "event" -> p.danger
                        clip.camera == "front" -> p.accent
                        else -> p.uploadOrange
                    }
                    drawRect(color = color, topLeft = Offset(x1, top), size = Size((x2 - x1).coerceAtLeast(3f), laneH - 8f))
                }
                if (cursorEpochMs >= startOfDay) {
                    val cx = (((cursorEpochMs - startOfDay) / dayDurationMs) * size.width).toFloat().coerceIn(0f, size.width)
                    drawLine(color = p.text, start = Offset(cx, 0f), end = Offset(cx, size.height), strokeWidth = 3f)
                }
            }
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                listOf("00:00", "06:00", "12:00", "18:00", "24:00").forEach {
                    Text(it, color = p.muted, fontSize = 10.sp)
                }
            }

            // Quick selectable clips
            val displayedClips = clips.filter { viewMode == "both" || it.camera == viewMode }.sortedByDescending { it.startAt }.take(20)
            if (displayedClips.isNotEmpty()) {
                Text("録画ファイル（タップして再生）", color = p.muted, fontSize = 11.sp, fontWeight = FontWeight.SemiBold)
                displayedClips.forEach { clip ->
                    val isSel = clip.id == selectedFront?.id || clip.id == selectedCabin?.id
                    Surface(
                        shape = RoundedCornerShape(8.dp),
                        color = if (isSel) p.soft else p.bg,
                        border = BorderStroke(1.dp, if (isSel) p.accent else p.line),
                        modifier = Modifier.fillMaxWidth().clickable {
                            selectTime(Instant.parse(clip.startAt).toEpochMilli(), clips)
                        },
                    ) {
                        Row(
                            modifier = Modifier.padding(horizontal = 10.dp, vertical = 8.dp),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(
                                "${if (clip.camera == "front") "前方" else "車内"} · ${Formatters.formatTime(clip.startAt)} — ${Formatters.formatTime(clip.endAt)}",
                                color = if (isSel) p.accent else p.text,
                                fontSize = 12.sp,
                                fontWeight = if (isSel) FontWeight.Bold else FontWeight.Normal,
                            )
                            Text(Formatters.bytes(clip.sizeBytes), color = p.muted, fontSize = 11.sp)
                        }
                    }
                }
            }
        }

        // GPS Track synced with playback cursor
        NccPanel(title = "走行経路（再生時刻と連動）") {
            TrackMapCanvas(
                points = track?.points.orEmpty(),
                currentPoint = nearestTrackPoint,
                height = 180.dp,
            )
        }
        Spacer(modifier = Modifier.height(12.dp))
    }
}
