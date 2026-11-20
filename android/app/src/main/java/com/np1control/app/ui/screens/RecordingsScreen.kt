package com.np1control.app.ui.screens

import android.app.DownloadManager
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Environment
import android.widget.Toast
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowLeft
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.Map
import androidx.compose.material.icons.filled.Movie
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Schedule
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.SkipPrevious
import androidx.compose.material.icons.filled.VolumeUp
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.Formatters
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.data.RecordingCatalog
import com.np1control.app.data.RecordingClip
import com.np1control.app.data.TrackData
import com.np1control.app.data.parseInstantMs
import com.np1control.app.ui.components.AdaptiveColumns
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.EmptyState
import com.np1control.app.ui.components.HintText
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.PanelAction
import com.np1control.app.ui.components.ScreenColumn
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.components.VideoPlayerBox
import com.np1control.app.ui.map.GeoPoint
import com.np1control.app.ui.map.MapFocus
import com.np1control.app.ui.map.MapLocation
import com.np1control.app.ui.map.NccMap
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.launch
import java.net.URLEncoder
import java.time.LocalDate
import kotlin.math.abs

private data class Span(val start: Long, val end: Long)

private fun clipStart(c: RecordingClip) = parseInstantMs(c.startAt) ?: 0L
private fun clipEnd(c: RecordingClip) = parseInstantMs(c.endAt) ?: 0L
private fun dayStart(ymd: String): Long = runCatching { LocalDate.parse(ymd).atStartOfDay(Formatters.JST).toInstant().toEpochMilli() }.getOrDefault(0L)
private fun kindLabel(kind: String) = when (kind) { "event" -> "イベント録画"; "parking" -> "駐車中の録画"; else -> "通常録画" }

@Composable
fun RecordingsScreen(
    apiClient: NccApiClient,
    initialTimestampIso: String? = null,
    isWide: Boolean,
    mapThemes: Map<String, String> = emptyMap(),
) {
    val p = LocalNccPalette.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    var selectedDate by remember { mutableStateOf(initialTimestampIso?.let { Formatters.isoDateJst(it) } ?: Formatters.jstDay(0)) }
    var catalog by remember { mutableStateOf<RecordingCatalog?>(null) }
    var track by remember { mutableStateOf<TrackData?>(null) }
    var view by remember { mutableStateOf("front") }
    var speed by remember { mutableFloatStateOf(1f) }
    var cursor by remember { mutableLongStateOf(0L) }
    var front by remember { mutableStateOf<RecordingClip?>(null) }
    var cabin by remember { mutableStateOf<RecordingClip?>(null) }
    var seekSeq by remember { mutableIntStateOf(0) }
    var zoomHours by remember { mutableFloatStateOf(24f) }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var notice by remember { mutableStateOf("") }
    var reload by remember { mutableIntStateOf(0) }

    val dayStartMs = dayStart(selectedDate)
    val clips = catalog?.clips.orEmpty()
    val dayClips = remember(catalog, selectedDate) {
        clips.filter { clipEnd(it) > dayStartMs && clipStart(it) < dayStartMs + 86_400_000 }.sortedBy { clipStart(it) }
    }
    val spans = remember(dayClips) {
        val out = ArrayList<Span>()
        dayClips.map { Span(maxOf(dayStartMs, clipStart(it)), minOf(dayStartMs + 86_400_000, clipEnd(it))) }.sortedBy { it.start }.forEach { s ->
            val last = out.lastOrNull()
            if (last != null && s.start <= last.end + 2000) out[out.lastIndex] = Span(last.start, maxOf(last.end, s.end)) else out.add(s)
        }
        out
    }
    fun clipAt(cam: String, at: Long) = dayClips.firstOrNull { it.camera == cam && clipStart(it) <= at && clipEnd(it) > at }

    fun seek(at: Long) {
        cursor = at
        front = clipAt("front", at)
        cabin = clipAt("in", at)
        seekSeq++
        notice = ""
    }

    LaunchedEffect(selectedDate, reload) {
        loading = true
        error = ""
        if (track?.date != selectedDate) track = null
        runCatching {
            val obs = apiClient.get<Observation>("/recordings?date=$selectedDate")
            obs.data?.let { apiClient.json.decodeFromJsonElement(RecordingCatalog.serializer(), it) }
        }.onSuccess { cat ->
            catalog = cat
            val target = initialTimestampIso?.let { parseInstantMs(it) }?.takeIf { Formatters.isoDateJst(initialTimestampIso) == selectedDate }
            val list = cat?.clips.orEmpty().filter { clipEnd(it) > dayStartMs && clipStart(it) < dayStartMs + 86_400_000 }
            if (cursor !in dayStartMs until dayStartMs + 86_400_000 || reload == 0) {
                val start = target ?: list.maxByOrNull { clipStart(it) }?.let { clipStart(it) }
                if (start != null) {
                    cursor = start
                    front = list.firstOrNull { it.camera == "front" && clipStart(it) <= start && clipEnd(it) > start }
                    cabin = list.firstOrNull { it.camera == "in" && clipStart(it) <= start && clipEnd(it) > start }
                    seekSeq++
                } else {
                    cursor = dayStartMs
                    front = null
                    cabin = null
                }
            }
        }.onFailure { error = it.message ?: "録画一覧を取得できません" }
        runCatching {
            apiClient.get<Observation>("/recordings/track?date=$selectedDate").data?.let { apiClient.json.decodeFromJsonElement(TrackData.serializer(), it) }
        }.onSuccess { track = it }
        loading = false
    }

    val days = catalog?.days.orEmpty()
    val day = days.firstOrNull { it.date == selectedDate }
    val primaryCam = if (view == "in") "in" else "front"
    val primaryClip = if (primaryCam == "in") cabin else front

    ScreenColumn {
        // ── Day ──
        NccPanel(
            title = Formatters.formatJpDate(selectedDate),
            subtitle = day?.let { "${it.count} 本 · ${Formatters.duration(it.recordedSeconds)}の記録" } ?: if (loading) "録画を読み込んでいます…" else "この日の確定した録画はありません",
            icon = Icons.Default.Movie,
            headerEnd = {
                PanelAction(onClick = { reload++ }, enabled = !loading) { Icon(Icons.Default.Refresh, contentDescription = "録画一覧を更新") }
            },
        ) {
            if (days.isNotEmpty()) {
                Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    days.forEach { d ->
                        val sel = d.date == selectedDate
                        Column(
                            Modifier
                                .clip(RoundedCornerShape(14.dp))
                                .background(if (sel) p.accent else p.raised)
                                .clickable { selectedDate = d.date; cursor = 0L }
                                .padding(horizontal = 14.dp, vertical = 8.dp),
                        ) {
                            Text(
                                if (d.date == Formatters.jstDay(0)) "今日" else if (d.date == Formatters.jstDay(-1)) "昨日" else Formatters.shortDate(d.date),
                                color = if (sel) p.onAccent else p.text,
                                style = MaterialTheme.typography.labelLarge,
                            )
                            Text("${d.count / 2}区間", color = if (sel) p.onAccent.copy(alpha = 0.8f) else p.muted, fontSize = 11.sp)
                        }
                    }
                }
                HintText("SDカードに残っている日の録画を再生できます。古い録画は上書きされます。")
            }
            if (spans.isNotEmpty()) {
                Text("録画されている時間", color = p.muted, style = MaterialTheme.typography.labelMedium)
                Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    spans.forEach { s ->
                        val inside = cursor in s.start until s.end
                        Row(
                            Modifier
                                .clip(RoundedCornerShape(999.dp))
                                .background(if (inside) p.soft else p.raised)
                                .clickable { seek(s.start) }
                                .padding(horizontal = 12.dp, vertical = 6.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Icon(Icons.Default.Schedule, contentDescription = null, tint = if (inside) p.accent else p.muted, modifier = Modifier.size(14.dp))
                            Text(
                                " ${Formatters.formatShortTime(s.start)}–${Formatters.formatShortTime(s.end)}",
                                color = if (inside) p.accent else p.text,
                                fontSize = 12.sp,
                            )
                        }
                    }
                }
            }
        }
        if (error.isNotBlank()) NccBanner(error, BannerTone.ERROR, action = { TextButton(onClick = { reload++ }) { Text("再試行") } })

        // ── Player ──
        NccPanel(
            title = "再生",
            icon = Icons.Default.Movie,
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 16.dp),
        ) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                SectionTabs(tabs = listOf("front" to "前方", "in" to "車内", "both" to "両方"), selectedId = view, onSelect = { view = it; seekSeq++ }, fill = true, modifier = Modifier.weight(1f))
            }
            SectionTabs(
                tabs = listOf("0.5" to "0.5×", "1.0" to "1×", "1.5" to "1.5×", "2.0" to "2×"),
                selectedId = speed.toString(),
                onSelect = { speed = it.toFloat() },
                fill = true,
            )
            val cams = when (view) { "both" -> listOf("front", "in"); "in" -> listOf("in"); else -> listOf("front") }
            val slot: @Composable (String) -> Unit = { cam ->
                val clip = if (cam == "front") front else cabin
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text(if (cam == "front") "前方カメラ" else "車内カメラ", color = p.text, style = MaterialTheme.typography.labelLarge)
                    if (clip != null) {
                        val url = apiClient.fullUrl("/recordings/${URLEncoder.encode(clip.id, "UTF-8")}/stream")
                        val offset = (cursor - clipStart(clip)).coerceAtLeast(0)
                        VideoPlayerBox(
                            streamUrl = url,
                            apiClient = apiClient,
                            startOffsetMs = offset,
                            seekRequest = seekSeq to offset,
                            playbackSpeed = speed,
                            muted = cam != primaryCam,
                            onPositionChanged = if (cam == primaryCam) { pos -> cursor = clipStart(clip) + pos } else null,
                            onEnded = if (cam == primaryCam) {
                                {
                                    val end = clipEnd(clip)
                                    val next = dayClips.firstOrNull { it.camera == cam && clipStart(it) > clipStart(clip) && clipEnd(it) > end && clipStart(it) <= end + 2000 }
                                    if (next != null) seek(maxOf(end, clipStart(next)))
                                }
                            } else null,
                            onError = { notice = it },
                        )
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(
                                "${Formatters.formatTime(clip.startAt)}–${Formatters.formatTime(clip.endAt)} · ${kindLabel(clip.kind)}",
                                color = p.muted,
                                fontSize = 12.sp,
                                modifier = Modifier.weight(1f),
                            )
                            TextButton(onClick = { download(context, apiClient, clip) }) {
                                Icon(Icons.Default.Download, contentDescription = null, modifier = Modifier.size(16.dp))
                                Text(" ${Formatters.bytes(clip.sizeBytes)}", fontSize = 12.sp)
                            }
                        }
                    } else {
                        EmptyState(
                            Icons.Default.Movie,
                            if (loading) "録画を確認しています…" else "この時刻の録画はありません",
                            "下の時間バーで、録画のある区間を選んでください。",
                        )
                    }
                }
            }
            if (cams.size == 2 && isWide) AdaptiveColumns(minColumnWidth = 260.dp, left = { slot("front") }, right = { slot("in") })
            else cams.forEach { slot(it) }
            if (notice.isNotBlank()) NccBanner(notice, BannerTone.WARN, onDismiss = { notice = "" })
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Icon(Icons.Default.VolumeUp, contentDescription = null, tint = p.faint, modifier = Modifier.size(14.dp))
                HintText(if (view == "both") "音声は前方カメラの録画から再生します。" else "音声は録画の音をそのまま再生します。")
            }
        }

        // ── Timeline ──
        NccPanel(title = "時間バー", icon = Icons.Default.Schedule) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("再生している時刻", color = p.muted, style = MaterialTheme.typography.labelSmall)
                    Text(Formatters.formatClock(if (cursor > 0) cursor else dayStartMs), color = p.text, style = MaterialTheme.typography.headlineMedium)
                }
                IconButton(onClick = {
                    spans.lastOrNull { it.end <= cursor - 1000 || (it.start < cursor - 3000 && cursor >= it.end) }?.let { seek(it.start) }
                        ?: spans.lastOrNull { it.start < cursor - 3000 }?.let { seek(it.start) }
                }) { Icon(Icons.Default.SkipPrevious, contentDescription = "前の録画区間") }
                IconButton(onClick = { spans.firstOrNull { it.start > cursor + 1000 }?.let { seek(it.start) } }) {
                    Icon(Icons.Default.SkipNext, contentDescription = "次の録画区間")
                }
            }
            SectionTabs(
                tabs = listOf("24" to "24時間", "6" to "6時間", "1" to "1時間", "0.25" to "15分"),
                selectedId = if (zoomHours == 0.25f) "0.25" else zoomHours.toInt().toString(),
                onSelect = { zoomHours = it.toFloat() },
                fill = true,
            )
            Timeline(
                dayStartMs = dayStartMs,
                cursor = if (cursor > 0) cursor else dayStartMs,
                zoomHours = zoomHours,
                clips = dayClips,
                onSeek = { seek(it) },
            )
            Row(horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.CenterVertically) {
                LegendDot(Color(0xFF5E8FCE), "前方")
                LegendDot(Color(0xFFD4A065), "車内")
                LegendDot(p.line, "録画なし")
            }
            HintText("バーをタップ・なぞると、その時刻から再生します。細かく選ぶときは表示範囲を狭めてください。")
        }

        // ── Map ──
        val trackPts = track?.points.orEmpty()
        val here = remember(trackPts, cursor) {
            if (cursor <= 0) null else trackPts.minByOrNull { abs((parseInstantMs(it.at) ?: 0L) - cursor) }
                ?.takeIf { abs((parseInstantMs(it.at) ?: 0L) - cursor) <= 2500 }
        }
        NccPanel(
            title = "走行経路",
            subtitle = when {
                track == null && loading -> "位置記録を読み込んでいます…"
                trackPts.isEmpty() -> null
                here == null -> "再生している時刻の位置記録はありません"
                else -> "再生している位置を表示しています"
            },
            icon = Icons.Default.Map,
        ) {
            if (trackPts.isEmpty()) {
                EmptyState(Icons.Default.Map, if (track == null) "位置記録を読み込んでいます…" else "この日の位置記録はありません", track?.reason ?: "録画に保存された測位から経路を表示します。")
                return@NccPanel
            }
            Box(Modifier.fillMaxWidth().height(260.dp).clip(RoundedCornerShape(14.dp)).background(p.raised)) {
                NccMap(
                    modifier = Modifier.fillMaxSize(),
                    view = "playback",
                scrollFriendly = true,
                    mapThemes = mapThemes,
                    routes = remember(trackPts) { trackPts.groupBy { it.segment }.values.map { s -> s.map { GeoPoint(it.lat, it.lon) } } },
                    location = here?.let { MapLocation(it.lat, it.lon, true) },
                    focus = MapFocus.FIT,
                    focusKey = selectedDate + trackPts.size,
                )
            }
        }
        catalog?.let { c ->
            if (c.pendingFiles > 0) HintText("${c.pendingFiles} 本は確定待ちです。録画が確定すると時間バーに追加されます。")
            if (c.unreadableFiles > 0) HintText("${c.unreadableFiles} 本の録画情報を読み取れませんでした。必要に応じてシステムのSDカード画面から原本を確認してください。")
            if (c.truncated) HintText("録画数の上限に達したため、一部の録画は表示されていません。", color = p.danger)
        }
    }
}

@Composable
private fun LegendDot(color: Color, label: String) {
    val p = LocalNccPalette.current
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(width = 14.dp, height = 8.dp).clip(RoundedCornerShape(3.dp)).background(color))
        Text(" $label", color = p.muted, fontSize = 11.sp)
    }
}

@Composable
private fun Timeline(dayStartMs: Long, cursor: Long, zoomHours: Float, clips: List<RecordingClip>, onSeek: (Long) -> Unit) {
    val p = LocalNccPalette.current
    val windowMs = (zoomHours * 3_600_000L).toLong()
    val from = (cursor - windowMs / 2).coerceIn(dayStartMs, dayStartMs + 86_400_000 - windowMs)
    val to = from + windowMs
    val tickStep = when { zoomHours >= 24 -> 3 * 3_600_000L; zoomHours >= 6 -> 3_600_000L; zoomHours >= 1 -> 10 * 60_000L; else -> 3 * 60_000L }
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        val labels = buildList {
            var t = ((from + tickStep - 1) / tickStep) * tickStep
            while (t <= to) {
                val f = (t - from).toFloat() / windowMs
                if (f <= 0.96f) add(f to if (zoomHours >= 6) "${Formatters.formatShortTime(t).take(2).trimStart('0').ifEmpty { "0" }}時" else Formatters.formatShortTime(t))
                t += tickStep
            }
        }
        androidx.compose.foundation.layout.BoxWithConstraints(Modifier.fillMaxWidth().height(14.dp).padding(start = 34.dp)) {
            val w = maxWidth
            labels.forEach { (f, l) ->
                Text(l, color = p.faint, fontSize = 9.sp, modifier = Modifier.padding(start = (w * f - 14.dp).coerceIn(0.dp, w - 30.dp)))
            }
        }
        listOf("front" to Color(0xFF5E8FCE), "in" to Color(0xFFD4A065)).forEach { (cam, color) ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(if (cam == "front") "前方" else "車内", color = p.muted, fontSize = 11.sp, modifier = Modifier.width(34.dp))
                Canvas(
                    Modifier
                        .weight(1f)
                        .height(30.dp)
                        .clip(RoundedCornerShape(8.dp))
                        .background(if (p.isDark) p.line else Color(0xFFE9ECF1))
                        .pointerInput(from, windowMs) {
                            detectTapGestures { o -> onSeek(from + (o.x / size.width * windowMs).toLong()) }
                        }
                        .pointerInput(from, windowMs) {
                            detectHorizontalDragGestures(onDragEnd = {}) { change, _ ->
                                onSeek(from + (change.position.x.coerceIn(0f, size.width.toFloat()) / size.width * windowMs).toLong())
                            }
                        },
                ) {
                    clips.filter { it.camera == cam && clipEnd(it) > from && clipStart(it) < to }.forEach { c ->
                        val x0 = ((clipStart(c) - from).toFloat() / windowMs * size.width).coerceAtLeast(0f)
                        val x1 = ((clipEnd(c) - from).toFloat() / windowMs * size.width).coerceAtMost(size.width)
                        drawRoundRect(
                            color = if (c.kind == "event") Color(0xFFB26336) else color,
                            topLeft = Offset(x0, 4f),
                            size = Size((x1 - x0).coerceAtLeast(1.5f), size.height - 8f),
                            cornerRadius = CornerRadius(3f, 3f),
                        )
                    }
                    val cx = (cursor - from).toFloat() / windowMs * size.width
                    drawLine(p.danger, Offset(cx, 0f), Offset(cx, size.height), strokeWidth = 2.5f)
                    drawCircle(p.danger, radius = 4.5f, center = Offset(cx, 3f))
                }
            }
        }
    }
}

private fun download(context: Context, apiClient: NccApiClient, clip: RecordingClip) {
    val url = apiClient.fullUrl("/recordings/${URLEncoder.encode(clip.id, "UTF-8")}/download")
    runCatching {
        val dm = context.getSystemService(Context.DOWNLOAD_SERVICE) as DownloadManager
        val req = DownloadManager.Request(Uri.parse(url))
            .setTitle(clip.name)
            .setDescription("NP1の録画原本")
            .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
            .setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS, "NP1/${clip.name}")
        dm.enqueue(req)
        Toast.makeText(context, "ダウンロードを開始しました（ダウンロード/NP1）", Toast.LENGTH_SHORT).show()
    }.onFailure {
        runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url))) }
    }
}
