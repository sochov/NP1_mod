package com.np1control.app.ui.screens

import android.graphics.BitmapFactory
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CellTower
import androidx.compose.material.icons.filled.DirectionsCar
import androidx.compose.material.icons.filled.LocationOn
import androidx.compose.material.icons.filled.Memory
import androidx.compose.material.icons.automirrored.filled.OpenInNew
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Router
import androidx.compose.material.icons.filled.Speed
import androidx.compose.material.icons.filled.Videocam
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
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
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.Formatters
import com.np1control.app.data.JourneyHistory
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.data.SystemSummary
import com.np1control.app.data.TrackPoint
import com.np1control.app.data.TrafficUsageData
import com.np1control.app.data.WebSettings
import com.np1control.app.ui.components.JourneyBarItem
import com.np1control.app.ui.components.JourneyChartCanvas
import com.np1control.app.ui.components.KeyValueRow
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.NccTag
import com.np1control.app.ui.components.SparklineCanvas
import com.np1control.app.ui.components.TrackMapCanvas
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import java.math.BigInteger

private fun JsonObject?.str(key: String): String? = (this?.get(key) as? JsonPrimitive)?.contentOrNull
private fun JsonObject?.dbl(key: String): Double? = (this?.get(key) as? JsonPrimitive)?.doubleOrNull
private fun JsonObject?.bool(key: String): Boolean? = (this?.get(key) as? JsonPrimitive)?.booleanOrNull
private fun JsonObject?.int(key: String): Int? = (this?.get(key) as? JsonPrimitive)?.intOrNull

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun DashboardScreen(
    summary: SystemSummary,
    webSettings: WebSettings?,
    apiClient: NccApiClient,
    isWide: Boolean,
    overviewMode: Boolean,
    onNavigate: (String) -> Unit,
) {
    val p = LocalNccPalette.current
    var period by remember { mutableStateOf("7") }
    var journey by remember { mutableStateOf<JourneyHistory?>(null) }
    var selectedBar by remember { mutableStateOf<JourneyBarItem?>(null) }
    var liveUsage by remember { mutableStateOf<TrafficUsageData?>(null) }
    var livePreviewActive by remember { mutableStateOf(false) }
    var frontBitmap by remember { mutableStateOf<ImageBitmap?>(null) }
    var liveSessionId by remember { mutableStateOf<String?>(null) }

    // Rolling resource history for sparklines (up to 30 samples)
    val cpuHistory = remember { mutableStateListOf<Double?>() }
    val ramHistory = remember { mutableStateListOf<Double?>() }
    val tempHistory = remember { mutableStateListOf<Double?>() }
    val cellRxHistory = remember { mutableStateListOf<Double?>() }
    val cellTxHistory = remember { mutableStateListOf<Double?>() }
    val wifiRxHistory = remember { mutableStateListOf<Double?>() }
    val wifiTxHistory = remember { mutableStateListOf<Double?>() }

    // Fetch journey stats when period changes
    LaunchedEffect(period) {
        runCatching {
            val obs = apiClient.get<Observation>("/trips?period=$period")
            obs.data?.let { journey = apiClient.json.decodeFromJsonElement(JourneyHistory.serializer(), it) }
        }
    }

    // Poll /traffic/usage every 2.5s for live speeds
    LaunchedEffect(Unit) {
        while (isActive) {
            runCatching {
                val obs = apiClient.get<Observation>("/traffic/usage")
                obs.data?.let {
                    val parsed = apiClient.json.decodeFromJsonElement(TrafficUsageData.serializer(), it)
                    liveUsage = parsed
                    cellRxHistory.add(parsed.networks?.cellular?.rxBytesPerSecond)
                    cellTxHistory.add(parsed.networks?.cellular?.txBytesPerSecond)
                    wifiRxHistory.add(parsed.networks?.wifi?.rxBytesPerSecond)
                    wifiTxHistory.add(parsed.networks?.wifi?.txBytesPerSecond)
                    if (cellRxHistory.size > 30) cellRxHistory.removeAt(0)
                    if (cellTxHistory.size > 30) cellTxHistory.removeAt(0)
                    if (wifiRxHistory.size > 30) wifiRxHistory.removeAt(0)
                    if (wifiTxHistory.size > 30) wifiTxHistory.removeAt(0)
                }
            }
            delay(2500L)
        }
    }

    // Record CPU / RAM / temp samples when summary updates
    LaunchedEffect(summary) {
        val cpuPct = (summary["cpu"]?.data as? JsonObject).dbl("usedPercent")
        val ramPct = (summary["ram"]?.data as? JsonObject).dbl("usedPercent")
        val temps = (summary["temperatures"]?.data as? JsonArray)
            ?.mapNotNull { (it as? JsonObject).dbl("celsius") }
            ?.maxOrNull()
        cpuHistory.add(cpuPct)
        ramHistory.add(ramPct)
        tempHistory.add(temps)
        if (cpuHistory.size > 30) cpuHistory.removeAt(0)
        if (ramHistory.size > 30) ramHistory.removeAt(0)
        if (tempHistory.size > 30) tempHistory.removeAt(0)
    }

    // Lightweight JPEG preview on dashboard when toggled ON
    LaunchedEffect(livePreviewActive) {
        if (!livePreviewActive) {
            liveSessionId?.let { id -> runCatching { apiClient.requestRaw("/camera/sessions/$id", "DELETE") } }
            liveSessionId = null
            frontBitmap = null
            return@LaunchedEffect
        }
        try {
            val res = apiClient.post<com.np1control.app.data.CameraSessionResponse>(
                "/camera/sessions",
                """{"source":"front","color":"auto","resolution":"360p"}""",
            )
            liveSessionId = res.id
            while (isActive && livePreviewActive) {
                runCatching {
                    val bytes = apiClient.getBytes("/camera/sessions/${res.id}/stream")
                    val bmp = withContext(Dispatchers.Default) {
                        BitmapFactory.decodeByteArray(bytes, 0, bytes.size)?.asImageBitmap()
                    }
                    if (bmp != null) frontBitmap = bmp
                }
                delay(1200L)
            }
        } catch (_: Exception) {
            livePreviewActive = false
        }
    }
    DisposableEffect(Unit) {
        onDispose {
            liveSessionId?.let { id ->
                // Best-effort cleanup handled by server 30s idle timeout if coroutine is cancelled
            }
        }
    }

    // Extract summary data
    val modem = summary["modem"]?.data as? JsonObject
    val gps = summary["gps"]?.data as? JsonObject
    val recording = summary["recording"]?.data as? JsonObject
    val tailscale = summary["tailscale"]?.data as? JsonObject
    val wifi = summary["wifi"]?.data as? JsonObject
    val ap = summary["ap"]?.data as? JsonObject ?: summary["hotspot"]?.data as? JsonObject
    val sd = summary["sd"]?.data as? JsonObject
    val storage = summary["internalStorage"]?.data as? JsonObject
    val uptimeSec = (summary["uptime"]?.data as? JsonPrimitive)?.doubleOrNull
    val loadStr = (summary["load"]?.data as? JsonPrimitive)?.contentOrNull ?: "— — —"
    val loadParts = loadStr.trim().split(Regex("\\s+")).take(3)
    val cpuPct = (summary["cpu"]?.data as? JsonObject).dbl("usedPercent")
    val ramObj = summary["ram"]?.data as? JsonObject
    val ramPct = ramObj.dbl("usedPercent")
    val maxTemp = (summary["temperatures"]?.data as? JsonArray)
        ?.mapNotNull { (it as? JsonObject).dbl("celsius") }
        ?.maxOrNull()

    val cellularNet = liveUsage?.networks?.cellular
    val wifiNet = liveUsage?.networks?.wifi
    val monthCellBytes = run {
        val rx = Formatters.trafficBigInt(cellularNet?.monthRX)
        val tx = Formatters.trafficBigInt(cellularNet?.monthTX)
        if (rx != null && tx != null) rx.add(tx) else null
    }
    val todayCellBytes = run {
        val rx = Formatters.trafficBigInt(cellularNet?.todayRX)
        val tx = Formatters.trafficBigInt(cellularNet?.todayTX)
        if (rx != null && tx != null) rx.add(tx) else null
    }
    val cellLimit = Formatters.trafficBigInt(webSettings?.cellularLimitBytes)?.takeIf { it > BigInteger.ZERO }

    val gpsPoint = remember(gps) {
        val lat = gps.dbl("lat") ?: gps.dbl("latitude")
        val lon = gps.dbl("lon") ?: gps.dbl("longitude")
        if (lat != null && lon != null && (lat != 0.0 || lon != 0.0)) {
            TrackPoint(at = summary["gps"]?.observedAt.orEmpty(), lat = lat, lon = lon)
        } else null
    }

    val barItems = remember(journey) {
        val h = journey ?: return@remember emptyList()
        var cumDist = 0.0
        var cumHrs = 0.0
        if (h.range.period == "24h" && h.hours.isNotEmpty()) {
            h.hours.map { hr ->
                val d = hr.distanceKm
                val hrs = hr.drivingSeconds?.div(3600.0)
                if (d != null) cumDist += d
                if (hrs != null) cumHrs += hrs
                JourneyBarItem(
                    key = hr.start,
                    label = Formatters.formatTime(hr.start).take(5),
                    distanceKm = d,
                    hours = hrs,
                    cumulativeDistanceKm = if (d != null) cumDist else null,
                    cumulativeHours = if (hrs != null) cumHrs else null,
                )
            }
        } else {
            h.days.map { day ->
                val d = day.distanceKm
                val hrs = day.drivingSeconds?.div(3600.0)
                if (d != null) cumDist += d
                if (hrs != null) cumHrs += hrs
                JourneyBarItem(
                    key = day.date,
                    label = day.date.takeLast(5).replace('-', '/'),
                    distanceKm = d,
                    hours = hrs,
                    cumulativeDistanceKm = if (d != null) cumDist else null,
                    cumulativeHours = if (hrs != null) cumHrs else null,
                )
            }
        }
    }

    // Reusable cards
    val journeyCard: @Composable () -> Unit = {
        NccPanel(
            title = "走行の記録",
            icon = Icons.Default.DirectionsCar,
            headerEnd = {
                IconButton(onClick = { onNavigate("trips") }, modifier = Modifier.size(28.dp)) {
                    Icon(Icons.AutoMirrored.Filled.OpenInNew, contentDescription = "走行履歴", tint = p.muted, modifier = Modifier.size(16.dp))
                }
            },
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(20.dp),
            ) {
                Column {
                    Text("走行距離", color = p.muted, fontSize = 11.sp)
                    Row(verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(3.dp)) {
                        Text(
                            text = Formatters.distanceText(journey?.period?.distanceKm),
                            color = p.accent,
                            fontSize = 24.sp,
                            fontWeight = FontWeight.Bold,
                        )
                        Text("km", color = p.muted, fontSize = 12.sp, modifier = Modifier.padding(bottom = 3.dp))
                    }
                }
                Column {
                    Text("走行時間", color = p.muted, fontSize = 11.sp)
                    Text(
                        text = Formatters.duration(journey?.period?.drivingSeconds),
                        color = p.uploadOrange,
                        fontSize = 20.sp,
                        fontWeight = FontWeight.Bold,
                    )
                }
            }
            FlowRow(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                listOf("24h" to "24時間", "7" to "7日", "30" to "30日", "90" to "90日", "365" to "365日", "all" to "全期間").forEach { (id, lbl) ->
                    val sel = period == id
                    NccTag(
                        text = lbl,
                        color = if (sel) p.accent else p.muted,
                        background = if (sel) p.soft else p.bg,
                        modifier = Modifier.clickable { period = id; selectedBar = null },
                    )
                }
            }
            JourneyChartCanvas(
                items = barItems,
                selectedKey = selectedBar?.key,
                onSelect = { selectedBar = it },
                height = if (isWide && overviewMode) 120.dp else 145.dp,
            )
            selectedBar?.let { b ->
                Text(
                    text = "${b.label}: 距離 ${Formatters.distanceText(b.distanceKm)} km / 時間 ${Formatters.duration(b.hours?.times(3600.0))}",
                    color = p.text,
                    fontSize = 11.sp,
                    fontWeight = FontWeight.Medium,
                )
            }
        }
    }

    val gpsLiveCard: @Composable () -> Unit = {
        val isWriting = recording.bool("writing")
        val clipCount = recording.int("finalizedClips")
        NccPanel(
            title = "GPS・録画・ライブ",
            icon = Icons.Default.LocationOn,
            headerEnd = {
                NccTag(
                    text = when (isWriting) {
                        true -> "● 録画中${clipCount?.let { " $it 件" } ?: ""}"
                        false -> "録画停止"
                        null -> "録画確認中"
                    },
                    color = if (isWriting == true) p.danger else p.muted,
                    background = if (isWriting == true) p.dangerSoft else p.bg,
                    modifier = Modifier.clickable { onNavigate("recordings") },
                )
                IconButton(onClick = { livePreviewActive = !livePreviewActive }, modifier = Modifier.size(28.dp)) {
                    Icon(
                        imageVector = if (livePreviewActive) Icons.Default.Pause else Icons.Default.PlayArrow,
                        contentDescription = "軽量ライブ切替",
                        tint = p.accent,
                        modifier = Modifier.size(18.dp),
                    )
                }
                IconButton(onClick = { onNavigate("camera") }, modifier = Modifier.size(28.dp)) {
                    Icon(Icons.Default.Videocam, contentDescription = "ライブカメラ", tint = p.muted, modifier = Modifier.size(16.dp))
                }
            },
        ) {
            if (livePreviewActive) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .aspectRatio(16f / 9f)
                        .clip(RoundedCornerShape(10.dp))
                        .background(Color(0xFF0A0D12))
                        .clickable { onNavigate("camera") },
                    contentAlignment = Alignment.Center,
                ) {
                    val bmp = frontBitmap
                    if (bmp != null) {
                        Image(
                            bitmap = bmp,
                            contentDescription = "前方カメラ軽量ライブ",
                            contentScale = ContentScale.Crop,
                            modifier = Modifier.fillMaxSize(),
                        )
                    } else {
                        Text("前方カメラ取得中…", color = Color(0xFFA2ACBB), fontSize = 12.sp)
                    }
                    NccTag(
                        text = "前方 LIVE",
                        color = Color.White,
                        background = Color(0xAA000000),
                        modifier = Modifier.align(Alignment.TopStart).padding(8.dp),
                    )
                }
            }
            TrackMapCanvas(
                points = emptyList(),
                currentPoint = gpsPoint,
                height = if (isWide && overviewMode) 130.dp else 155.dp,
            )
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                val sats = gps.int("satellites")?.let { "衛星 $it" } ?: "衛星 —"
                val acc = gps.dbl("accuracyM")?.let { "精度 ${it.toInt()}m" } ?: "測位待ち"
                Text("$sats · $acc", color = p.muted, fontSize = 11.sp)
                val spd = gps.dbl("speedKmh")?.let { "${it.toInt()} km/h" }
                    ?: gps.dbl("speedMps")?.let { "${(it * 3.6).toInt()} km/h" }
                    ?: ""
                if (spd.isNotEmpty()) {
                    Text(spd, color = p.text, fontSize = 11.sp, fontWeight = FontWeight.Bold)
                }
            }
        }
    }

    val cellularCard: @Composable () -> Unit = {
        val carrier = Formatters.carrierName(modem.str("carrier"), modem.str("plmn"))
        val band = modem.str("band")?.let { "Band ${it.removePrefix("B")}" } ?: "—"
        val rsrp = modem.dbl("rsrp")?.let { "${it.toInt()} dBm" } ?: "—"
        NccPanel(
            title = "4G データ通信",
            icon = Icons.Default.CellTower,
            headerEnd = {
                IconButton(onClick = { onNavigate("internet") }, modifier = Modifier.size(28.dp)) {
                    Icon(Icons.AutoMirrored.Filled.OpenInNew, contentDescription = "4G設定", tint = p.muted, modifier = Modifier.size(16.dp))
                }
            },
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column {
                    Text(carrier, color = p.text, fontSize = 16.sp, fontWeight = FontWeight.Bold)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                        NccTag(text = "LTE $band", color = p.accent, background = p.soft)
                        Text(rsrp, color = p.muted, fontSize = 11.sp)
                    }
                }
                if (cellLimit != null && monthCellBytes != null) {
                    val rem = (cellLimit - monthCellBytes).coerceAtLeast(BigInteger.ZERO)
                    Column(horizontalAlignment = Alignment.End) {
                        Text("残り目安", color = p.muted, fontSize = 10.sp)
                        Text(Formatters.trafficVolume(rem), color = p.ok, fontSize = 16.sp, fontWeight = FontWeight.Bold)
                    }
                }
            }
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                Column {
                    Text("今月", color = p.muted, fontSize = 10.sp)
                    Text(Formatters.trafficVolume(monthCellBytes), color = p.text, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
                }
                Column {
                    Text("本日", color = p.muted, fontSize = 10.sp)
                    Text(Formatters.trafficVolume(todayCellBytes), color = p.text, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
                }
                Column(horizontalAlignment = Alignment.End) {
                    Text("上限", color = p.muted, fontSize = 10.sp)
                    Text(
                        text = cellLimit?.let { Formatters.trafficVolume(it) } ?: "未設定",
                        color = p.muted,
                        fontSize = 13.sp,
                    )
                }
            }
            if (cellLimit != null && monthCellBytes != null && cellLimit > BigInteger.ZERO) {
                val pct = (monthCellBytes.toDouble() / cellLimit.toDouble()).toFloat().coerceIn(0f, 1f)
                LinearProgressIndicator(
                    progress = { pct },
                    modifier = Modifier.fillMaxWidth().height(5.dp).clip(RoundedCornerShape(3.dp)),
                    color = if (pct > 0.9f) p.danger else p.accent,
                    trackColor = p.bg,
                )
            }
        }
    }

    val networkCard: @Composable () -> Unit = {
        val tsOnline = tailscale.bool("online") == true
        val tsName = tailscale.str("deviceName") ?: tailscale.str("hostName") ?: "Tailscale"
        val tsIp = (tailscale?.get("ips") as? JsonArray)?.firstOrNull()?.jsonPrimitive?.contentOrNull ?: "未割当"
        val wifiSsid = wifi.str("ssid")?.ifBlank { null } ?: "未接続"
        val wifiConn = wifi.str("state") == "COMPLETED"
        val apEnabled = ap.bool("enabled") == true
        val apSsid = ap.str("ssid")?.ifBlank { null } ?: (ap?.get("settings") as? JsonObject).str("ssid") ?: "NP1 Hotspot"
        val apClients = ap.int("clientCount")

        NccPanel(
            title = "ネットワーク",
            icon = Icons.Default.Router,
            headerEnd = {
                IconButton(onClick = { onNavigate("wifi") }, modifier = Modifier.size(28.dp)) {
                    Icon(Icons.AutoMirrored.Filled.OpenInNew, contentDescription = "Wi-Fi/AP", tint = p.muted, modifier = Modifier.size(16.dp))
                }
            },
        ) {
            Row(
                modifier = Modifier.fillMaxWidth().clickable { onNavigate("wifi") }.padding(vertical = 2.dp),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text("Wi-Fi AP · $apSsid", color = p.text, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
                    Text(apClients?.let { "接続端末 $it 台" } ?: "192.168.226.1", color = p.muted, fontSize = 11.sp)
                }
                NccTag(
                    text = if (apEnabled) "提供中" else "確認中",
                    color = if (apEnabled) p.ok else p.muted,
                    background = if (apEnabled) p.okSoft else p.bg,
                )
            }
            Row(
                modifier = Modifier.fillMaxWidth().clickable { onNavigate("wifi") }.padding(vertical = 2.dp),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text("Wi-Fi STA · $wifiSsid", color = p.text, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
                    Text(wifi.str("ipv4")?.ifBlank { "IP未取得" } ?: "—", color = p.muted, fontSize = 11.sp)
                }
                NccTag(
                    text = if (wifiConn) "接続済み" else "未接続",
                    color = if (wifiConn) p.ok else p.muted,
                    background = if (wifiConn) p.okSoft else p.bg,
                )
            }
            Row(
                modifier = Modifier.fillMaxWidth().clickable { onNavigate("vpn") }.padding(vertical = 2.dp),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text("Tailscale · $tsName", color = p.text, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
                    Text(tsIp, color = p.muted, fontSize = 11.sp, fontFamily = FontFamily.Monospace)
                }
                NccTag(
                    text = if (tsOnline) "オンライン" else "オフライン",
                    color = if (tsOnline) p.ok else p.muted,
                    background = if (tsOnline) p.okSoft else p.bg,
                )
            }
        }
    }

    val resourceCard: @Composable () -> Unit = {
        NccPanel(
            title = "リソースモニター",
            icon = Icons.Default.Memory,
            headerEnd = {
                IconButton(onClick = { onNavigate("system") }, modifier = Modifier.size(28.dp)) {
                    Icon(Icons.AutoMirrored.Filled.OpenInNew, contentDescription = "システム", tint = p.muted, modifier = Modifier.size(16.dp))
                }
            },
        ) {
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Column(modifier = Modifier.weight(1f)) {
                    Text("4G", color = p.text, fontSize = 11.sp, fontWeight = FontWeight.Bold)
                    Text(
                        "↓ ${Formatters.trafficSpeed(cellularNet?.rxBytesPerSecond)}  ↑ ${Formatters.trafficSpeed(cellularNet?.txBytesPerSecond)}",
                        color = p.accent,
                        fontSize = 11.sp,
                    )
                    SparklineCanvas(
                        series = listOf(p.accent to cellRxHistory.toList(), p.uploadOrange to cellTxHistory.toList()),
                        height = 34.dp,
                    )
                }
                Column(modifier = Modifier.weight(1f)) {
                    Text("Wi-Fi", color = p.text, fontSize = 11.sp, fontWeight = FontWeight.Bold)
                    Text(
                        "↓ ${Formatters.trafficSpeed(wifiNet?.rxBytesPerSecond)}  ↑ ${Formatters.trafficSpeed(wifiNet?.txBytesPerSecond)}",
                        color = p.accent,
                        fontSize = 11.sp,
                    )
                    SparklineCanvas(
                        series = listOf(p.accent to wifiRxHistory.toList(), p.uploadOrange to wifiTxHistory.toList()),
                        height = 34.dp,
                    )
                }
            }
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Column(modifier = Modifier.weight(1f)) {
                    Text("CPU", color = p.muted, fontSize = 10.sp)
                    Text(cpuPct?.let { "${it.toInt()} %" } ?: "—", color = p.text, fontSize = 16.sp, fontWeight = FontWeight.Bold)
                    SparklineCanvas(series = listOf(p.accent to cpuHistory.toList()), fixedMax = 100.0, height = 28.dp)
                }
                Column(modifier = Modifier.weight(1f)) {
                    Text("RAM", color = p.muted, fontSize = 10.sp)
                    Text(ramPct?.let { "${it.toInt()} %" } ?: "—", color = p.text, fontSize = 16.sp, fontWeight = FontWeight.Bold)
                    SparklineCanvas(series = listOf(p.ok to ramHistory.toList()), fixedMax = 100.0, height = 28.dp)
                }
                Column(modifier = Modifier.weight(1f)) {
                    Text("本体温度", color = p.muted, fontSize = 10.sp)
                    Text(maxTemp?.let { String.format("%.1f °C", it) } ?: "—", color = p.text, fontSize = 16.sp, fontWeight = FontWeight.Bold)
                    SparklineCanvas(series = listOf(p.uploadOrange to tempHistory.toList()), fixedMax = 110.0, height = 28.dp)
                }
            }
        }
    }

    val deviceCard: @Composable () -> Unit = {
        val sdUsedPct = sd.dbl("usedPercent")?.toFloat()?.div(100f)?.coerceIn(0f, 1f)
        val intUsedPct = storage.dbl("usedPercent")?.toFloat()?.div(100f)?.coerceIn(0f, 1f)
        NccPanel(
            title = "本体の状態",
            icon = Icons.Default.Speed,
            headerEnd = {
                IconButton(onClick = { onNavigate("system") }, modifier = Modifier.size(28.dp)) {
                    Icon(Icons.AutoMirrored.Filled.OpenInNew, contentDescription = "システム", tint = p.muted, modifier = Modifier.size(16.dp))
                }
            },
        ) {
            KeyValueRow("稼働時間", Formatters.uptime(uptimeSec))
            KeyValueRow(
                "Load average (1/5/15分)",
                loadParts.joinToString(" / ").ifBlank { "—" },
                mono = true,
            )
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                KeyValueRow(
                    "SDカード残り",
                    "${Formatters.bytes(sd.str("freeBytes"))} / ${Formatters.bytes(sd.str("totalBytes"))}",
                )
                if (sdUsedPct != null) {
                    LinearProgressIndicator(
                        progress = { sdUsedPct },
                        modifier = Modifier.fillMaxWidth().height(5.dp).clip(RoundedCornerShape(3.dp)),
                        color = if (sdUsedPct > 0.9f) p.warn else p.accent,
                        trackColor = p.bg,
                    )
                }
            }
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                KeyValueRow(
                    "本体空き容量",
                    "${Formatters.bytes(storage.str("freeBytes"))} / ${Formatters.bytes(storage.str("totalBytes"))}",
                )
                if (intUsedPct != null) {
                    LinearProgressIndicator(
                        progress = { intUsedPct },
                        modifier = Modifier.fillMaxWidth().height(5.dp).clip(RoundedCornerShape(3.dp)),
                        color = p.accent,
                        trackColor = p.bg,
                    )
                }
            }
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(if (isWide) 12.dp else 12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (isWide) {
            // Landscape / Echo Show 5 two-column balanced dashboard
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Box(modifier = Modifier.weight(1f)) { journeyCard() }
                Box(modifier = Modifier.weight(1f)) { gpsLiveCard() }
            }
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Box(modifier = Modifier.weight(1f)) { cellularCard() }
                Box(modifier = Modifier.weight(1f)) { networkCard() }
            }
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Box(modifier = Modifier.weight(1f)) { resourceCard() }
                Box(modifier = Modifier.weight(1f)) { deviceCard() }
            }
        } else {
            // Smartphone portrait single column
            journeyCard()
            gpsLiveCard()
            cellularCard()
            networkCard()
            resourceCard()
            deviceCard()
        }
        Spacer(modifier = Modifier.height(12.dp))
    }
}
