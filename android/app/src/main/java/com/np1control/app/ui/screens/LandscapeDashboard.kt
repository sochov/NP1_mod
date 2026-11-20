package com.np1control.app.ui.screens

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.displayCutout
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CloudOff
import androidx.compose.material.icons.filled.DocumentScanner
import androidx.compose.material.icons.filled.LocationOn
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.MyLocation
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.Formatters
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.PlateRecordEnvelope
import com.np1control.app.data.PlateRecordsResponse
import com.np1control.app.data.PlateStatus
import com.np1control.app.data.SystemSummary
import com.np1control.app.data.WebSettings
import com.np1control.app.data.parseInstantMs
import com.np1control.app.ui.components.CameraFrame
import com.np1control.app.ui.components.JapanesePlateBadge
import com.np1control.app.ui.components.NccTag
import com.np1control.app.ui.components.Spacing
import com.np1control.app.ui.components.StatusPill
import com.np1control.app.ui.components.Tone
import com.np1control.app.ui.components.rememberLiveFrames
import com.np1control.app.ui.map.MapFocus
import com.np1control.app.ui.map.MapLocation
import com.np1control.app.ui.map.NccMap
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import java.time.Instant
import java.time.ZoneId

/** Width of the right-hand column; the shell uses it to keep the plate popup over the map. */
private val JST: ZoneId = ZoneId.of("Asia/Tokyo")

fun landscapePaneWidth(screenWidth: Dp): Dp = (screenWidth * 0.36f).coerceIn(300.dp, 460.dp)

// ── Plate feed ─────────────────────────────────────────────────────────────

class PlateFeedState {
    var records by mutableStateOf<List<PlateRecordEnvelope>>(emptyList())
    var loaded by mutableStateOf(false)
    var error by mutableStateOf<String?>(null)
    var status by mutableStateOf<PlateStatus?>(null)

    /** Records that appeared (or gained a rule match) after the first load, with the time they did. */
    var arrivals by mutableStateOf<Map<String, Long>>(emptyMap())
}

/**
 * Marks records that are new since the previous poll, or that gained a rule match, with [now].
 * The first poll ([baseline]) only remembers what is already there, so opening the screen does
 * not flash the whole list. [seen] maps record id → matched rule count and is updated in place.
 */
internal fun plateArrivals(
    seen: MutableMap<String, Int>,
    records: List<PlateRecordEnvelope>,
    previous: Map<String, Long>,
    baseline: Boolean,
    now: Long,
): Map<String, Long> {
    val fresh = HashMap(previous.filterValues { now - it < 60_000 })
    records.forEach { env ->
        val id = env.record.id
        val before = seen[id]
        if (!baseline && (before == null || env.record.matches.size > before)) fresh[id] = now
        seen[id] = env.record.matches.size
    }
    if (seen.size > 400) seen.keys.retainAll(records.map { it.record.id }.toSet())
    return fresh
}

/**
 * Newest plate records of today and yesterday, refreshed every [intervalMs] independently of the
 * popup filter, so the list keeps updating even when notifications are off.
 */
@Composable
fun rememberPlateFeed(apiClient: NccApiClient, intervalMs: Long = 1500L): PlateFeedState {
    val state = remember { PlateFeedState() }
    LaunchedEffect(Unit) {
        val seen = HashMap<String, Int>()
        var baseline = true
        while (isActive) {
            runCatching {
                apiClient.get<PlateRecordsResponse>(
                    "/plate/records?from=${Formatters.jstDay(-1)}&to=${Formatters.jstDay(0)}&limit=40",
                )
            }.onSuccess { r ->
                state.arrivals = plateArrivals(seen, r.records, state.arrivals, baseline, System.currentTimeMillis())
                baseline = false
                state.records = r.records
                state.error = null
                state.loaded = true
            }.onFailure { state.error = it.message ?: "ナンバーの記録を取得できません" }
            delay(intervalMs)
        }
    }
    LaunchedEffect(Unit) {
        while (isActive) {
            runCatching { apiClient.get<PlateStatus>("/plate/status") }.onSuccess { state.status = it }
            delay(15_000L)
        }
    }
    return state
}

// ── Screen ─────────────────────────────────────────────────────────────────

/**
 * Landscape dashboard: the map fills the screen on the left; the right column shows the front
 * camera above the live list of plate detections. No title or menu bar, only a small menu
 * button at the bottom left.
 */
@Composable
fun LandscapeDashboard(
    summary: SystemSummary,
    webSettings: WebSettings?,
    apiClient: NccApiClient,
    connectionError: String?,
    onRetryConnection: () -> Unit,
    onOpenMenu: () -> Unit,
    onOpenPlate: (String) -> Unit,
) {
    val p = LocalNccPalette.current
    val data = rememberDashboardData(apiClient, summary, traffic = false)
    val frames = rememberLiveFrames(apiClient, listOf("front"), intervalMs = 1000L)
    val feed = rememberPlateFeed(apiClient)
    val position = resolvePosition(
        data.gpsLive?.takeIf { it.status.isNotBlank() } ?: summary["gps"],
        apiClient.json,
        data.trail,
        data.now,
    )
    var following by remember { mutableStateOf(true) }
    var recenterKey by remember { mutableIntStateOf(0) }

    BoxWithConstraints(Modifier.fillMaxSize().background(p.bg)) {
        val paneWidth = landscapePaneWidth(maxWidth)
        Row(Modifier.fillMaxSize()) {
            // Map
            Box(Modifier.weight(1f).fillMaxHeight()) {
                NccMap(
                    modifier = Modifier.fillMaxSize(),
                    view = "dashboard",
                    mapThemes = webSettings?.mapThemes.orEmpty(),
                    routes = data.trail,
                    location = position.current ?: position.lastKnown?.let { MapLocation(it.lat, it.lon, live = false) },
                    focus = if (following) MapFocus.LOCATION else MapFocus.NONE,
                    focusKey = recenterKey,
                    focusZoom = 15.5,
                    onUserMove = { following = false },
                    attribution = false,
                )
                val overlay = Modifier
                    .windowInsetsPadding(WindowInsets.displayCutout.only(WindowInsetsSides.Start))
                    .padding(Spacing.item)
                // Top left: only problems (no connection, no live position)
                Column(overlay.align(Alignment.TopStart), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (connectionError != null) {
                        MapNotice(
                            icon = Icons.Default.CloudOff,
                            text = "NP1に接続できません",
                            action = "再接続",
                            tint = p.warn,
                            onClick = onRetryConnection,
                        )
                    }
                    if (position.current == null) {
                        MapNotice(
                            icon = Icons.Default.LocationOn,
                            text = if (position.lastKnown != null) "${position.status} · 最後の位置を表示中" else position.status,
                            tint = p.warn,
                        )
                    }
                }
                // Bottom left: the only way into the rest of the app
                Surface(
                    onClick = onOpenMenu,
                    shape = CircleShape,
                    color = p.glass,
                    shadowElevation = 6.dp,
                    modifier = overlay.align(Alignment.BottomStart).size(44.dp),
                ) {
                    Box(contentAlignment = Alignment.Center) {
                        Icon(Icons.Default.Menu, contentDescription = "メニュー", tint = p.text, modifier = Modifier.size(22.dp))
                    }
                }
                // Bottom right: recenter + map credit
                Column(
                    Modifier.align(Alignment.BottomEnd).padding(Spacing.item),
                    horizontalAlignment = Alignment.End,
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    if (!following) {
                        Surface(
                            onClick = { following = true; recenterKey++ },
                            shape = CircleShape,
                            color = p.glass,
                            shadowElevation = 6.dp,
                            modifier = Modifier.size(44.dp),
                        ) {
                            Box(contentAlignment = Alignment.Center) {
                                Icon(Icons.Default.MyLocation, contentDescription = "現在地に戻る", tint = p.accent)
                            }
                        }
                    }
                    Text(
                        "© OpenStreetMap · OpenFreeMap",
                        color = p.muted,
                        fontSize = 9.sp,
                        modifier = Modifier
                            .clip(RoundedCornerShape(6.dp))
                            .background(p.glass.copy(alpha = 0.7f))
                            .padding(horizontal = 6.dp, vertical = 2.dp),
                    )
                }
            }
            // Right column: front camera above the detection list
            Column(
                Modifier
                    .width(paneWidth)
                    .fillMaxHeight()
                    .background(p.paper)
                    .windowInsetsPadding(WindowInsets.displayCutout.only(WindowInsetsSides.End))
                    .padding(Spacing.item),
                verticalArrangement = Arrangement.spacedBy(Spacing.item),
            ) {
                Box(Modifier.fillMaxWidth()) {
                    CameraFrame(frames, "front", Modifier.fillMaxWidth(), corner = 12)
                    // Recording state only when it is known (no clip count: not useful while driving).
                    val rec = recordingState(summary, data.now)
                    if (rec.tone != Tone.NEUTRAL) {
                        StatusPill(rec.label, rec.tone, Modifier.align(Alignment.TopStart).padding(8.dp))
                    }
                }
                PlateFeed(feed, data.now, onOpenPlate, Modifier.weight(1f).fillMaxWidth())
            }
        }
    }
}

@Composable
private fun MapNotice(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    text: String,
    tint: Color,
    action: String? = null,
    onClick: (() -> Unit)? = null,
) {
    val p = LocalNccPalette.current
    Row(
        modifier = Modifier
            .clip(RoundedCornerShape(12.dp))
            .background(p.glass)
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(horizontal = 10.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(16.dp))
        Text(text, color = p.text, fontSize = 12.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (action != null) Text(action, color = p.accent, fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
    }
}

@Composable
private fun PlateFeed(feed: PlateFeedState, now: Long, onOpenPlate: (String) -> Unit, modifier: Modifier = Modifier) {
    val p = LocalNccPalette.current
    val st = feed.status
    Column(modifier, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Icon(Icons.Default.DocumentScanner, contentDescription = null, tint = p.muted, modifier = Modifier.size(18.dp))
            Text("ナンバー検知", color = p.text, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
            when {
                feed.error != null && feed.loaded -> StatusPill("更新できません", Tone.WARN)
                st == null -> {}
                !st.enabled -> StatusPill("記録停止中", Tone.NEUTRAL)
                st.sensor.state == "running" -> StatusPill("認識中", Tone.OK)
                else -> StatusPill(Formatters.sensorLabel(st.sensor.state), Tone.WARN)
            }
        }
        when {
            !feed.loaded && feed.error != null -> FeedMessage("記録を取得できません", feed.error ?: "")
            !feed.loaded -> FeedMessage("読み込み中…", "")
            feed.records.isEmpty() -> FeedMessage(
                "今日はまだ検知していません",
                if (st != null && (!st.enabled || st.sensor.state != "running")) "ナンバー認識が動作していないため、新しい記録は増えません。"
                else "ナンバーを読み取ると、ここに新しい順で表示します。",
            )
            else -> LazyColumn(
                modifier = Modifier.fillMaxWidth().weight(1f),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                items(feed.records, key = { it.record.id }) { env ->
                    PlateFeedRow(
                        env = env,
                        now = now,
                        arrivedAt = feed.arrivals[env.record.id],
                        onClick = { onOpenPlate(env.record.id) },
                        modifier = Modifier.animateItem(),
                    )
                }
            }
        }
    }
}

@Composable
private fun FeedMessage(title: String, detail: String) {
    val p = LocalNccPalette.current
    Column(
        Modifier.fillMaxWidth().padding(vertical = 24.dp, horizontal = 8.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Text(title, color = p.muted, style = MaterialTheme.typography.bodyMedium)
        if (detail.isNotBlank()) Text(detail, color = p.faint, style = MaterialTheme.typography.bodySmall)
    }
}

@Composable
private fun PlateFeedRow(
    env: PlateRecordEnvelope,
    now: Long,
    arrivedAt: Long?,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    val rec = env.record
    val shared = env.numberShare?.takeIf { it.status == "exact" }
    val danger = shared?.category == "危険"
    val matched = rec.matches.isNotEmpty() || shared != null
    val accent = when {
        danger -> p.danger
        matched -> p.plateHit
        else -> p.line
    }
    val base = when {
        danger -> p.dangerSoft
        matched -> p.plateHitSoft
        else -> p.bg
    }
    // A new detection flashes briefly so the eye catches it while driving.
    var flashing by remember(rec.id, arrivedAt) { mutableStateOf(arrivedAt != null && now - arrivedAt < 4000) }
    LaunchedEffect(rec.id, arrivedAt) {
        if (arrivedAt != null) {
            delay((4000 - (System.currentTimeMillis() - arrivedAt)).coerceAtLeast(0))
            flashing = false
        }
    }
    val bg by animateColorAsState(if (flashing) p.soft else base, animationSpec = tween(700), label = "plateRow")
    val firstSeen = parseInstantMs(rec.firstSeen)
    val isToday = firstSeen != null &&
        Instant.ofEpochMilli(firstSeen).atZone(JST).toLocalDate().toString() == Formatters.jstDay(0)

    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(bg)
            .border(if (matched || flashing) 1.5.dp else 1.dp, if (flashing && !matched) p.accent else accent, RoundedCornerShape(12.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 10.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Column(Modifier.width(58.dp)) {
            Text(
                Formatters.formatTime(rec.firstSeen),
                color = p.text,
                fontSize = 13.sp,
                fontWeight = FontWeight.SemiBold,
                maxLines = 1,
            )
            Text(
                if (isToday) Formatters.ago(firstSeen, now) else "昨日",
                color = p.muted,
                fontSize = 10.sp,
                maxLines = 1,
            )
        }
        JapanesePlateBadge(reading = rec.reading, matched = matched, danger = danger, width = 88.dp)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            when {
                rec.matches.isNotEmpty() -> NccTag(
                    text = rec.matches.joinToString("・") { it.ruleName.ifBlank { "条件に一致" } },
                    color = Color.White,
                    background = if (danger) p.danger else p.plateHit,
                )
                shared != null -> NccTag(
                    text = "NumberShare: ${shared.category ?: "登録済み"}",
                    color = if (danger) Color.White else p.accent,
                    background = if (danger) p.danger else p.soft,
                )
            }
            Text(
                "${(rec.reading.confidence * 100).toInt()}% · ${rec.sightings}回",
                color = p.muted,
                fontSize = 11.sp,
                maxLines = 1,
            )
        }
    }
}
