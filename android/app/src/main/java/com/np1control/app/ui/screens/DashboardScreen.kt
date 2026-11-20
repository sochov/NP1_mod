package com.np1control.app.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.OpenInNew
import androidx.compose.material.icons.filled.LocationOn
import androidx.compose.material.icons.filled.MyLocation
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Videocam
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.Formatters
import com.np1control.app.data.GpsPosition
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.data.SystemSummary
import com.np1control.app.data.TrackData
import com.np1control.app.data.TrafficUsageData
import com.np1control.app.data.TripStats
import com.np1control.app.data.WebSettings
import com.np1control.app.data.dataObj
import com.np1control.app.data.dbl
import com.np1control.app.data.parseInstantMs
import com.np1control.app.ui.ShellInsets
import com.np1control.app.ui.components.AdaptiveColumns
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.CameraFrame
import com.np1control.app.ui.components.JourneyPanel
import com.np1control.app.ui.components.LiveFrameState
import com.np1control.app.ui.components.MapSheet
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.ScreenColumn
import com.np1control.app.ui.components.rememberLiveFrames
import com.np1control.app.ui.map.GeoPoint
import com.np1control.app.ui.map.MapFocus
import com.np1control.app.ui.map.MapLocation
import com.np1control.app.ui.map.NccMap
import com.np1control.app.ui.components.PanelAction
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import java.math.BigInteger
import java.time.Instant

// ── Data ───────────────────────────────────────────────────────────────────

/** Position shown on the dashboard: live fix (≤ 11 min old) or the last known place. */
data class DashboardPosition(
    val gps: GpsPosition?,
    val current: MapLocation?,
    val lastKnown: GeoPoint?,
    val lastLabel: String,
    val status: String,
)

private const val MAX_FIX_AGE = 11 * 60 * 1000L

fun resolvePosition(obs: Observation?, json: kotlinx.serialization.json.Json, trail: List<List<GeoPoint>>, now: Long): DashboardPosition {
    val gps = obs?.data?.let { runCatching { json.decodeFromJsonElement(GpsPosition.serializer(), it) }.getOrNull() }
    val at = parseInstantMs(gps?.at)
    val live = obs?.status == "available" && gps?.fix == true && gps.lat != null && gps.lon != null &&
        at != null && now - at <= MAX_FIX_AGE && at - now <= 60_000
    val current = if (live) MapLocation(gps!!.lat!!, gps.lon!!, true, gps.accuracyM) else null
    val known = gps?.lastKnown?.takeIf { it.lat != null && it.lon != null }?.let { GeoPoint(it.lat!!, it.lon!!) }
    val trailEnd = trail.lastOrNull()?.lastOrNull()
    return DashboardPosition(
        gps = gps,
        current = current,
        lastKnown = known ?: trailEnd,
        lastLabel = if (known != null) "最後に分かっている位置" else "最後の記録位置",
        status = when {
            current != null -> "現在地"
            gps?.fix == false -> "測位待ち"
            gps?.fix == true -> "現在地を更新できません"
            else -> "位置情報を取得できません"
        },
    )
}

/** Polls the per-second position, the 24-hour trail and the traffic counters while visible. */
@Composable
private fun rememberDashboardData(apiClient: NccApiClient, summary: SystemSummary): DashboardDataState {
    val state = remember { DashboardDataState() }
    LaunchedEffect(Unit) {
        while (isActive) {
            runCatching { apiClient.get<Observation>("/device/gps/live") }
                .onSuccess { state.gpsLive = it }
                .onFailure { state.gpsLive = null }
            delay(2000L)
        }
    }
    LaunchedEffect(Unit) {
        while (isActive) {
            val ok = runCatching {
                val now = Instant.now()
                val from = now.minusSeconds(24 * 3600)
                val obs = apiClient.get<Observation>("/recordings/track?from=$from&to=$now")
                val track = obs.data?.let { apiClient.json.decodeFromJsonElement(TrackData.serializer(), it) }
                state.trail = splitTrail(track, from.toEpochMilli(), now.toEpochMilli())
            }.isSuccess
            delay(if (ok) 60_000L else 10_000L)
        }
    }
    LaunchedEffect(Unit) {
        while (isActive) {
            runCatching {
                val obs = apiClient.get<Observation>("/traffic/usage")
                obs.data?.let { apiClient.json.decodeFromJsonElement(TrafficUsageData.serializer(), it) }
            }.onSuccess {
                state.usage = it
                state.usageAt = System.currentTimeMillis()
                state.usageFailed = false
            }.onFailure { state.usageFailed = true }
            state.now = System.currentTimeMillis()
            recordSamples(state, summary)
            delay(2000L)
        }
    }
    LaunchedEffect(summary) { recordSamples(state, summary) }
    return state
}

class DashboardDataState {
    var gpsLive by mutableStateOf<Observation?>(null)
    var trail by mutableStateOf<List<List<GeoPoint>>>(emptyList())
    var usage by mutableStateOf<TrafficUsageData?>(null)
    var usageAt by mutableLongStateOf(0L)
    var usageFailed by mutableStateOf(false)
    var now by mutableLongStateOf(System.currentTimeMillis())
    var tick by mutableIntStateOf(0)

    val stale: Boolean
        get() {
            val sampled = parseInstantMs(usage?.sampledAt)
            return usageFailed || usage?.stale == true || (sampled != null && now - sampled > 10_000)
        }
}

private fun recordSamples(state: DashboardDataState, summary: SystemSummary) {
    val now = System.currentTimeMillis()
    val u = state.usage
    listOf("cellular" to u?.networks?.cellular, "wifi" to u?.networks?.wifi).forEach { (key, n) ->
        val ok = !state.stale && n != null && !n.stale && n.speedStatus == "available"
        val at = parseInstantMs(n?.sampledAt ?: u?.sampledAt) ?: now
        ResourceHistoryStore.remember("${key}rx", if (ok) at else now, if (ok) n!!.rxBytesPerSecond else null)
        ResourceHistoryStore.remember("${key}tx", if (ok) at else now, if (ok) n!!.txBytesPerSecond else null)
    }
    summary["cpu"]?.let { o -> parseInstantMs(o.observedAt)?.let { ResourceHistoryStore.remember("cpu", it, summary.dataObj("cpu", false).dbl("usedPercent")) } }
    summary["ram"]?.let { o -> parseInstantMs(o.observedAt)?.let { ResourceHistoryStore.remember("ram", it, summary.dataObj("ram", false).dbl("usedPercent")) } }
    summary["temperatures"]?.let { o ->
        parseInstantMs(o.observedAt)?.let { ResourceHistoryStore.remember("temperature", it, temperatures(summary).maxOfOrNull { t -> t.value }) }
    }
    state.tick++
}

fun splitTrail(track: TrackData?, fromMs: Long, toMs: Long): List<List<GeoPoint>> {
    val pts = track?.points.orEmpty()
        .mapNotNull { p -> parseInstantMs(p.at)?.let { it to p } }
        .filter { (at, p) -> at in fromMs..toMs && kotlin.math.abs(p.lat) <= 90 && kotlin.math.abs(p.lon) <= 180 && (p.lat != 0.0 || p.lon != 0.0) }
        .sortedBy { it.first }
    val segments = ArrayList<MutableList<GeoPoint>>()
    var lastAt = 0L
    var lastSeg = Int.MIN_VALUE
    for ((at, p) in pts) {
        if (segments.isEmpty() || p.segment != lastSeg || at - lastAt > 5000) segments.add(ArrayList())
        segments.last().add(GeoPoint(p.lat, p.lon))
        lastAt = at
        lastSeg = p.segment
    }
    return segments.filter { it.isNotEmpty() }
}

// ── Screen ─────────────────────────────────────────────────────────────────

@Composable
fun DashboardScreen(
    summary: SystemSummary,
    webSettings: WebSettings?,
    apiClient: NccApiClient,
    isWide: Boolean,
    isCompactLandscape: Boolean,
    overviewMode: Boolean,
    shellInsets: ShellInsets,
    connectionError: String?,
    onRetryConnection: () -> Unit,
    onOpenAppSettings: () -> Unit,
    onNavigate: (String) -> Unit,
    onWebSettingsChanged: () -> Unit,
) {
    val data = rememberDashboardData(apiClient, summary)
    val tripStats = remember(summary["tripStats"]) {
        summary["tripStats"]?.data?.let { runCatching { apiClient.json.decodeFromJsonElement(TripStats.serializer(), it) }.getOrNull() }
    }
    val position = resolvePosition(
        data.gpsLive?.takeIf { it.status.isNotBlank() } ?: summary["gps"],
        apiClient.json,
        data.trail,
        data.now,
    )
    val limit = Formatters.trafficBigInt(webSettings?.cellularLimitBytes)
    val mapThemes = webSettings?.mapThemes.orEmpty()

    val tiles = DashboardTiles(
        summary = summary,
        data = data,
        limit = limit,
        apiClient = apiClient,
        onNavigate = onNavigate,
        onWebSettingsChanged = onWebSettingsChanged,
    )

    if (!overviewMode) {
        DetailDashboard(summary, apiClient, tripStats, position, data, mapThemes, tiles, connectionError, onRetryConnection, onOpenAppSettings, onNavigate)
        return
    }
    if (isWide) {
        WideOverview(summary, apiClient, tripStats, position, data, mapThemes, tiles, isCompactLandscape, shellInsets, connectionError, onRetryConnection, onOpenAppSettings, onNavigate)
    } else {
        PhoneOverview(summary, apiClient, tripStats, position, data, mapThemes, tiles, shellInsets, connectionError, onRetryConnection, onOpenAppSettings, onNavigate)
    }
}

/** Bundles the four status tiles so every layout shows the same cards in the owner's order. */
class DashboardTiles(
    val summary: SystemSummary,
    val data: DashboardDataState,
    val limit: BigInteger?,
    val apiClient: NccApiClient,
    val onNavigate: (String) -> Unit,
    val onWebSettingsChanged: () -> Unit,
) {
    @Composable
    fun Cellular(modifier: Modifier = Modifier, glass: Boolean = false) = CellularTile(
        summary, data.usage?.networks?.cellular, data.stale, limit, apiClient,
        onOpen = { onNavigate("internet") }, onLimitSaved = onWebSettingsChanged, modifier = modifier, glass = glass,
    )

    @Composable
    fun Network(modifier: Modifier = Modifier, glass: Boolean = false) =
        NetworkTile(summary, data.usage?.networks?.wifi, onNavigate, modifier, glass)

    @Composable
    fun Resource(modifier: Modifier = Modifier, glass: Boolean = false) {
        @Suppress("UNUSED_VARIABLE") val tick = data.tick
        ResourceTile(summary, data.usage?.networks?.cellular, data.usage?.networks?.wifi, data.stale, data.now, onOpen = { onNavigate("system") }, modifier = modifier, glass = glass)
    }

    @Composable
    fun Device(modifier: Modifier = Modifier, glass: Boolean = false) =
        DeviceTile(summary, onOpen = { onNavigate("system") }, modifier = modifier, glass = glass)
}

// ── Map pieces ─────────────────────────────────────────────────────────────

@Composable
private fun HeroMap(
    position: DashboardPosition,
    data: DashboardDataState,
    mapThemes: Map<String, String>,
    padding: PaddingValues,
    following: Boolean,
    recenterKey: Int,
    onUserMove: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    val hasAnything = position.current != null || position.lastKnown != null
    Box(modifier.background(p.bg)) {
        NccMap(
            modifier = Modifier.fillMaxSize(),
            view = "dashboard",
            mapThemes = mapThemes,
            routes = data.trail,
            location = position.current ?: position.lastKnown?.let { MapLocation(it.lat, it.lon, live = false) },
            focus = if (following) MapFocus.LOCATION else MapFocus.NONE,
            focusKey = recenterKey,
            cameraPadding = padding,
            onUserMove = onUserMove,
            attribution = false,
        )
        if (!hasAnything) {
            Column(
                Modifier.fillMaxSize().padding(padding).padding(24.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.Center,
            ) {
                Surface(shape = RoundedCornerShape(18.dp), color = p.glass, shadowElevation = 4.dp) {
                    Column(Modifier.padding(horizontal = 20.dp, vertical = 16.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                        Icon(Icons.Default.LocationOn, contentDescription = null, tint = p.faint, modifier = Modifier.size(28.dp))
                        Text(position.status, color = p.text, style = MaterialTheme.typography.titleSmall)
                        Text("測位すると、ここに現在地を表示します。", color = p.muted, style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        }
    }
}

/** Translucent coordinates, satellites and accuracy (no labels, like the Web UI) plus the map credit. */
@Composable
private fun LocationReadout(position: DashboardPosition, now: Long, modifier: Modifier = Modifier) {
    val p = LocalNccPalette.current
    val cur = position.current
    val gps = position.gps
    val first = when {
        cur != null -> String.format("%.6f, %.6f", cur.lat, cur.lon)
        position.lastKnown != null -> "${position.lastLabel}  " + String.format("%.5f, %.5f", position.lastKnown.lat, position.lastKnown.lon)
        else -> position.status
    }
    val second = buildList {
        if (cur != null) {
            gps?.satellites?.let { add("衛星 $it") }
            gps?.accuracyM?.takeIf { it > 0 }?.let { add("精度 ${it.toInt()} m") }
            val at = parseInstantMs(gps?.at)
            if (at != null && now - at > 20_000) add("${Formatters.ago(at, now)}の位置")
        } else if (gps?.fix == false && position.lastKnown != null) add("測位できていません")
        add("© OpenStreetMap · OpenFreeMap")
    }.joinToString("   ")
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(10.dp))
            .background(p.glass.copy(alpha = 0.78f))
            .padding(horizontal = 9.dp, vertical = 4.dp),
    ) {
        Text(first, color = if (cur == null) p.warn else p.text, fontSize = 11.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.labelMedium)
        Text(second, color = p.muted, fontSize = 9.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
private fun RecenterButton(visible: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val p = LocalNccPalette.current
    if (!visible) return
    Surface(shape = CircleShape, color = p.glass, shadowElevation = 6.dp, modifier = modifier.size(46.dp), onClick = onClick) {
        Box(contentAlignment = Alignment.Center) { Icon(Icons.Default.MyLocation, contentDescription = "現在地に戻る", tint = p.accent) }
    }
}

@Composable
private fun CameraCard(frames: LiveFrameState, sources: List<String>, summary: SystemSummary, onNavigate: (String) -> Unit, modifier: Modifier = Modifier) {
    val p = LocalNccPalette.current
    NccPanel(
        modifier = modifier,
        title = "ライブ",
        icon = Icons.Default.Videocam,
        glass = true,
        contentPadding = PaddingValues(start = 12.dp, end = 12.dp, top = 0.dp, bottom = 12.dp),
        headerEnd = {
            RecordingChip(summary, onClick = { onNavigate("recordings") })
            PanelAction(onClick = { frames.paused = !frames.paused }) {
                Icon(if (frames.paused) Icons.Default.PlayArrow else Icons.Default.Pause, contentDescription = if (frames.paused) "ライブ映像を再開" else "ライブ映像を止める", tint = p.muted)
            }
            PanelAction(onClick = { onNavigate("camera") }) { Icon(Icons.AutoMirrored.Filled.OpenInNew, contentDescription = "ライブを開く", tint = p.muted, modifier = Modifier.size(18.dp)) }
        },
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            sources.forEach { src -> CameraFrame(frames, src, Modifier.weight(1f), onClick = { onNavigate("camera") }) }
        }
    }
}

@Composable
private fun ConnectionNotice(error: String?, onRetry: () -> Unit, onSettings: () -> Unit, modifier: Modifier = Modifier) {
    if (error == null) return
    NccBanner(
        message = error,
        tone = BannerTone.WARN,
        modifier = modifier,
        action = {
            Row {
                TextButton(onClick = onRetry) { Text("再接続") }
                TextButton(onClick = onSettings) { Text("接続先") }
            }
        },
    )
}

// ── Phone: map background, front camera at the top right, sheet below ─────

@Composable
private fun PhoneOverview(
    summary: SystemSummary,
    apiClient: NccApiClient,
    tripStats: TripStats?,
    position: DashboardPosition,
    data: DashboardDataState,
    mapThemes: Map<String, String>,
    tiles: DashboardTiles,
    shellInsets: ShellInsets,
    connectionError: String?,
    onRetryConnection: () -> Unit,
    onOpenAppSettings: () -> Unit,
    onNavigate: (String) -> Unit,
) {
    val frames = rememberLiveFrames(apiClient, listOf("front"))
    var following by remember { mutableStateOf(true) }
    var recenterKey by remember { mutableIntStateOf(0) }
    var sheetRest by remember { mutableStateOf(300.dp) }
    val density = LocalDensity.current
    val statusTop = with(density) { WindowInsets.statusBars.getTop(this).toDp() }
    val topClear = statusTop + shellInsets.top

    BoxWithConstraints(Modifier.fillMaxSize()) {
        val stageHeight = maxHeight
        HeroMap(
            position = position,
            data = data,
            mapThemes = mapThemes,
            padding = PaddingValues(top = topClear + 40.dp, bottom = sheetRest + 30.dp),
            following = following,
            recenterKey = recenterKey,
            onUserMove = { following = false },
            modifier = Modifier.fillMaxSize(),
        )
        // Recording status and front camera under the floating bar
        Column(Modifier.padding(top = topClear + 4.dp, start = 12.dp).width(190.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            RecordingChip(summary, onClick = { onNavigate("recordings") })
        }
        CameraFrame(
            frames,
            "front",
            modifier = Modifier
                .align(Alignment.TopEnd)
                .padding(top = topClear + 4.dp, end = 12.dp)
                .width(150.dp),
            onClick = { onNavigate("camera") },
        )
        // Above the sheet: readout + recenter
        Row(
            modifier = Modifier
                .align(Alignment.BottomStart)
                .fillMaxWidth()
                .padding(bottom = sheetRest + 8.dp, start = 12.dp, end = 12.dp),
            verticalAlignment = Alignment.Bottom,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            LocationReadout(position, data.now, Modifier.weight(1f, fill = false))
            Box(Modifier.width(8.dp))
            RecenterButton(!following, { following = true; recenterKey++ })
        }
        MapSheet(
            maxHeight = stageHeight - topClear - 8.dp,
            onRestHeight = { sheetRest = it },
            modifier = Modifier.align(Alignment.BottomCenter),
        ) {
            ConnectionNotice(connectionError, onRetryConnection, onOpenAppSettings)
            JourneyPanel(apiClient = apiClient, tripStats = tripStats, chartHeight = 150.dp, compact = true, onOpenTrips = { onNavigate("trips") })
            tiles.Cellular()
            tiles.Network()
            tiles.Resource()
            tiles.Device()
        }
    }
}

// ── Wide / landscape (tablets, Echo Show 5) ────────────────────────────────

@Composable
private fun WideOverview(
    summary: SystemSummary,
    apiClient: NccApiClient,
    tripStats: TripStats?,
    position: DashboardPosition,
    data: DashboardDataState,
    mapThemes: Map<String, String>,
    tiles: DashboardTiles,
    compact: Boolean,
    shellInsets: ShellInsets,
    connectionError: String?,
    onRetryConnection: () -> Unit,
    onOpenAppSettings: () -> Unit,
    onNavigate: (String) -> Unit,
) {
    val frames = rememberLiveFrames(apiClient, listOf("front", "in"))
    var following by remember { mutableStateOf(true) }
    var recenterKey by remember { mutableIntStateOf(0) }
    val density = LocalDensity.current
    val statusTop = with(density) { WindowInsets.statusBars.getTop(this).toDp() }
    val navBottom = with(density) { WindowInsets.navigationBars.getBottom(this).toDp() }
    val topClear = statusTop + shellInsets.top

    BoxWithConstraints(Modifier.fillMaxSize()) {
        val fullW = maxWidth
        val fullH = maxHeight
        val frame = fullW >= 900.dp && fullH >= 640.dp
        if (frame) {
            // Web layout: trip record top left, live top right, four equal tiles along the bottom.
            val tileHeight = 300.dp
            HeroMap(
                position, data, mapThemes,
                PaddingValues(top = topClear + 12.dp, bottom = tileHeight + navBottom + 24.dp, start = fullW * 0.42f, end = fullW * 0.3f),
                following, recenterKey, { following = false }, Modifier.fillMaxSize(),
            )
            Row(
                Modifier.fillMaxWidth().padding(top = topClear + 4.dp, start = 12.dp, end = 12.dp),
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Column(Modifier.width(fullW * 0.4f), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    ConnectionNotice(connectionError, onRetryConnection, onOpenAppSettings)
                    JourneyPanel(apiClient = apiClient, tripStats = tripStats, chartHeight = 150.dp, compact = true, glass = true, onOpenTrips = { onNavigate("trips") })
                }
                Box(Modifier.weight(1f))
                CameraCard(frames, listOf("front", "in"), summary, onNavigate, Modifier.width(fullW * 0.34f))
            }
            Column(Modifier.align(Alignment.BottomStart).padding(start = 12.dp, end = 12.dp, bottom = navBottom + 12.dp)) {
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.Bottom) {
                    LocationReadout(position, data.now, Modifier.weight(1f, fill = false))
                    Box(Modifier.width(8.dp))
                    RecenterButton(!following, { following = true; recenterKey++ })
                }
                Row(Modifier.fillMaxWidth().height(tileHeight).padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    val m = Modifier.weight(1f).fillMaxHeight()
                    Box(m.verticalScroll(rememberScrollState())) { tiles.Cellular(glass = true) }
                    Box(m.verticalScroll(rememberScrollState())) { tiles.Network(glass = true) }
                    Box(m.verticalScroll(rememberScrollState())) { tiles.Resource(glass = true) }
                    Box(m.verticalScroll(rememberScrollState())) { tiles.Device(glass = true) }
                }
            }
        } else {
            // Compact landscape: cards in a scrolling column on the left, map on the right.
            val panelWidth = (fullW * 0.46f).coerceIn(300.dp, 440.dp)
            HeroMap(
                position, data, mapThemes,
                PaddingValues(top = topClear + 8.dp, start = panelWidth + 12.dp, bottom = navBottom + 36.dp),
                following, recenterKey, { following = false }, Modifier.fillMaxSize(),
            )
            Column(
                Modifier
                    .width(panelWidth + 12.dp)
                    .fillMaxHeight()
                    .verticalScroll(rememberScrollState())
                    .padding(start = 10.dp, end = 2.dp, top = topClear + 2.dp, bottom = navBottom + 10.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                ConnectionNotice(connectionError, onRetryConnection, onOpenAppSettings)
                JourneyPanel(apiClient = apiClient, tripStats = tripStats, chartHeight = if (compact) 120.dp else 150.dp, compact = true, glass = true, onOpenTrips = { onNavigate("trips") })
                tiles.Cellular(glass = true)
                tiles.Network(glass = true)
                tiles.Resource(glass = true)
                tiles.Device(glass = true)
            }
            Column(
                Modifier.align(Alignment.TopEnd).padding(top = topClear + 4.dp, end = 10.dp).width((fullW - panelWidth - 40.dp).coerceAtMost(220.dp)),
                horizontalAlignment = Alignment.End,
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                CameraFrame(frames, "front", Modifier.fillMaxWidth(), onClick = { onNavigate("camera") })
                RecordingChip(summary, onClick = { onNavigate("recordings") })
            }
            Row(
                Modifier.align(Alignment.BottomEnd).padding(start = panelWidth + 24.dp, end = 10.dp, bottom = navBottom + 8.dp),
                verticalAlignment = Alignment.Bottom,
            ) {
                LocationReadout(position, data.now, Modifier.weight(1f, fill = false))
                Box(Modifier.width(8.dp))
                RecenterButton(!following, { following = true; recenterKey++ })
            }
        }
    }
}

// ── Detail dashboard (all information as cards) ────────────────────────────

@Composable
private fun DetailDashboard(
    summary: SystemSummary,
    apiClient: NccApiClient,
    tripStats: TripStats?,
    position: DashboardPosition,
    data: DashboardDataState,
    mapThemes: Map<String, String>,
    tiles: DashboardTiles,
    connectionError: String?,
    onRetryConnection: () -> Unit,
    onOpenAppSettings: () -> Unit,
    onNavigate: (String) -> Unit,
) {
    val p = LocalNccPalette.current
    val frames = rememberLiveFrames(apiClient, listOf("front", "in"))
    var following by remember { mutableStateOf(true) }
    var recenterKey by remember { mutableIntStateOf(0) }
    ScreenColumn {
        AdaptiveColumns(
            left = {
                JourneyPanel(apiClient = apiClient, tripStats = tripStats, onOpenTrips = { onNavigate("trips") })
                NccPanel(
                    title = "現在地",
                    subtitle = position.status,
                    icon = Icons.Default.LocationOn,
                    contentPadding = PaddingValues(0.dp),
                ) {
                    Box(Modifier.fillMaxWidth().height(260.dp)) {
                        NccMap(
                            modifier = Modifier.fillMaxSize(),
                            view = "dashboard",
                            mapThemes = mapThemes,
                            routes = data.trail,
                            location = position.current ?: position.lastKnown?.let { MapLocation(it.lat, it.lon, false) },
                            focus = if (following) MapFocus.LOCATION else MapFocus.NONE,
                            focusKey = recenterKey,
                            onUserMove = { following = false },
                            scrollFriendly = true,
                        )
                        LocationReadout(position, data.now, Modifier.align(Alignment.BottomStart).padding(8.dp))
                        RecenterButton(!following, { following = true; recenterKey++ }, Modifier.align(Alignment.TopEnd).padding(8.dp))
                    }
                }
                CameraCard(frames, listOf("front", "in"), summary, onNavigate)
            },
            right = {
                ConnectionNotice(connectionError, onRetryConnection, onOpenAppSettings)
                tiles.Cellular()
                tiles.Network()
                tiles.Resource()
                tiles.Device()
            },
        )
        Text("NP1 Control Center", color = p.faint, fontSize = 10.sp, modifier = Modifier.align(Alignment.CenterHorizontally).offset(y = 0.dp))
    }
}
