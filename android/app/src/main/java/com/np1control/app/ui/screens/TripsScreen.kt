package com.np1control.app.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.material.icons.filled.CalendarMonth
import androidx.compose.material.icons.filled.FitScreen
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Map
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Schedule
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
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
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.Formatters
import com.np1control.app.data.JourneyHistory
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.data.RadioHistoryResponse
import com.np1control.app.data.RadioLegend
import com.np1control.app.data.SystemSummary
import com.np1control.app.data.TrackData
import com.np1control.app.data.TripStats
import com.np1control.app.ui.components.AdaptiveColumns
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.EmptyState
import com.np1control.app.ui.components.HintText
import com.np1control.app.ui.components.JourneyPanel
import com.np1control.app.ui.components.KeyValueRow
import com.np1control.app.ui.components.LoadingRow
import com.np1control.app.ui.components.MetricBlock
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.ScreenColumn
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.components.SwitchRow
import com.np1control.app.ui.components.decimalText
import com.np1control.app.ui.map.GeoPoint
import com.np1control.app.ui.map.MapFocus
import com.np1control.app.ui.map.MapPoint
import com.np1control.app.ui.map.MapPolygon
import com.np1control.app.ui.map.NccMap
import com.np1control.app.ui.theme.LocalNccPalette
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset

fun parseHexColor(hex: String?, fallback: Color = Color(0xFFA8AFB8)): Color = runCatching {
    val h = hex!!.trim().removePrefix("#")
    val v = h.toLong(16)
    when (h.length) {
        6 -> Color(0xFF000000 or v)
        8 -> Color(v)
        else -> fallback
    }
}.getOrDefault(fallback)

@OptIn(ExperimentalLayoutApi::class, ExperimentalMaterial3Api::class)
@Composable
fun TripsScreen(
    apiClient: NccApiClient,
    summary: SystemSummary,
    isWide: Boolean,
    mapThemes: Map<String, String>,
    onOpenRecordingAt: (String) -> Unit,
) {
    val p = LocalNccPalette.current
    var history by remember { mutableStateOf<JourneyHistory?>(null) }
    var selectedDate by remember { mutableStateOf("") }
    var shownDays by remember { mutableIntStateOf(30) }
    val tripStats = remember(summary["tripStats"]) {
        summary["tripStats"]?.data?.let { runCatching { apiClient.json.decodeFromJsonElement(TripStats.serializer(), it) }.getOrNull() }
    }

    fun adoptHistory(h: JourneyHistory?) {
        history = h
        if (h != null && (selectedDate.isBlank() || selectedDate < h.range.from || selectedDate > h.range.to)) {
            selectedDate = h.days.lastOrNull { it.observed }?.date ?: h.range.to
        }
    }

    val recordedDays = remember(history) { history?.days?.filter { it.observed }?.reversed().orEmpty() }
    val selectedDay = history?.days?.firstOrNull { it.date == selectedDate }
    val dayTrips = remember(history, selectedDate) {
        history?.trips?.filter { Formatters.isoDateJst(it.start) <= selectedDate && Formatters.isoDateJst(it.end) >= selectedDate }.orEmpty()
    }

    ScreenColumn {
        JourneyPanel(
            apiClient = apiClient,
            tripStats = tripStats,
            onLoaded = { h, _ -> adoptHistory(h) },
            selectedDate = selectedDate,
            onSelectDate = { selectedDate = it },
        )
        AdaptiveColumns(
            minColumnWidth = 420.dp,
            left = {
                TripMapCard(
                    apiClient = apiClient,
                    date = selectedDate.ifBlank { Formatters.jstDay(0) },
                    minDate = history?.range?.from,
                    maxDate = history?.range?.to ?: Formatters.jstDay(0),
                    mapThemes = mapThemes,
                    onDateChange = { selectedDate = it },
                )
            },
            right = {
                NccPanel(
                    title = "日別の詳細",
                    subtitle = "連続して測位できた区間ごとに確認できます。",
                    icon = Icons.Default.Schedule,
                ) {
                    val h = history
                    when {
                        h == null -> LoadingRow("走行区間を読み込み中…")
                        !h.period.hasData -> EmptyState(
                            Icons.Default.Map,
                            "距離・走行時間は未計測です",
                            h.reason ?: "有効な連続測位データがありません。",
                        )
                        else -> {
                            Row(
                                modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                            ) {
                                recordedDays.take(shownDays).forEach { day ->
                                    val sel = day.date == selectedDate
                                    Column(
                                        modifier = Modifier
                                            .clip(RoundedCornerShape(14.dp))
                                            .background(if (sel) p.accent else p.raised)
                                            .clickable { selectedDate = day.date }
                                            .padding(horizontal = 12.dp, vertical = 8.dp),
                                    ) {
                                        Text(Formatters.shortDate(day.date), color = if (sel) p.onAccent else p.text, style = MaterialTheme.typography.labelLarge)
                                        Text("${decimalText(day.distanceKm)} km", color = if (sel) p.onAccent.copy(alpha = 0.85f) else p.muted, fontSize = 11.sp)
                                    }
                                }
                                if (recordedDays.size > shownDays) {
                                    TextButton(onClick = { shownDays += 30 }) { Text("さらに表示") }
                                }
                            }
                            Text(
                                if (selectedDate.isBlank()) "日付を選択" else Formatters.formatJpDate(selectedDate),
                                color = p.text,
                                style = MaterialTheme.typography.titleMedium,
                            )
                            Row(horizontalArrangement = Arrangement.spacedBy(28.dp)) {
                                MetricBlock("この日の距離", decimalText(selectedDay?.distanceKm), unit = "km", color = p.accent, size = 22)
                                MetricBlock("走行時間", Formatters.duration(selectedDay?.drivingSeconds), color = p.uploadOrange, size = 18)
                            }
                            when {
                                selectedDay?.observed != true -> HintText("この日に有効な連続測位の記録はありません。")
                                dayTrips.isEmpty() -> HintText("この日の集計はありますが、区間の詳細は表示できる範囲外です。")
                                else -> dayTrips.forEach { trip ->
                                    Surface(shape = RoundedCornerShape(14.dp), color = p.raised, modifier = Modifier.fillMaxWidth()) {
                                        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                            Row(verticalAlignment = Alignment.CenterVertically) {
                                                Text(
                                                    "${Formatters.formatTime(trip.start)} → ${Formatters.formatTime(trip.end)}",
                                                    color = p.text,
                                                    style = MaterialTheme.typography.titleSmall,
                                                    modifier = Modifier.weight(1f),
                                                )
                                                FilledTonalButton(
                                                    onClick = { onOpenRecordingAt(trip.start) },
                                                    contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                                                ) {
                                                    Icon(Icons.Default.PlayArrow, contentDescription = null, modifier = Modifier.size(16.dp))
                                                    Text(" 録画を見る", fontSize = 12.sp)
                                                }
                                            }
                                            FlowRow(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                                                Text("距離 ${decimalText(trip.distanceKm)} km", color = p.muted, fontSize = 12.sp)
                                                Text("走行 ${Formatters.duration(trip.drivingSeconds)}", color = p.muted, fontSize = 12.sp)
                                                Text("測位 ${Formatters.duration(trip.observedSeconds)}", color = p.muted, fontSize = 12.sp)
                                            }
                                            if (trip.drivingSeconds == 0.0) Text("停止中の測位", color = p.faint, fontSize = 11.sp)
                                            if (Formatters.isoDateJst(trip.start) != Formatters.isoDateJst(trip.end)) {
                                                Text("日付をまたぐ区間全体の距離・時間です。", color = p.faint, fontSize = 11.sp)
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
                history?.let { h ->
                    NccPanel(title = "取得元と測位の状態", icon = Icons.Default.Info) {
                        KeyValueRow("取得元", if (h.source == "ncc-retained-measured-intervals") "保存した走行記録（純正の測位から集計）" else "純正録画の測位情報")
                        KeyValueRow("読み取ったファイル", "${String.format("%,d", h.sourceFiles)} 件")
                        KeyValueRow("連続測位のある日", "${h.period.validObservedDays} 日")
                        KeyValueRow("測位できなかった記録", "${String.format("%,d", h.noFixSentences)} 件")
                        if (h.unreadableFiles > 0 || h.badChecksums > 0) KeyValueRow("読み取れない / 破損", "${h.unreadableFiles} / ${h.badChecksums}")
                        if (h.detailsTruncated) HintText("区間の詳細は新しいものから表示しています（全${h.totalTrips}区間）。")
                    }
                }
            },
        )
    }
}

@OptIn(ExperimentalLayoutApi::class, ExperimentalMaterial3Api::class)
@Composable
private fun TripMapCard(
    apiClient: NccApiClient,
    date: String,
    minDate: String?,
    maxDate: String,
    mapThemes: Map<String, String>,
    onDateChange: (String) -> Unit,
) {
    val p = LocalNccPalette.current
    var track by remember { mutableStateOf<TrackData?>(null) }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var overlay by remember { mutableStateOf(false) }
    var metric by remember { mutableStateOf("rsrp") }
    var mode by remember { mutableStateOf("grid") }
    var radio by remember { mutableStateOf<RadioHistoryResponse?>(null) }
    var legend by remember { mutableStateOf<RadioLegend?>(null) }
    var radioError by remember { mutableStateOf("") }
    var fitKey by remember { mutableIntStateOf(0) }
    var picker by remember { mutableStateOf(false) }

    LaunchedEffect(date) {
        loading = true
        error = ""
        if (track?.date != date) track = null
        runCatching {
            val obs = apiClient.get<Observation>("/recordings/track?date=$date")
            obs.data?.let { apiClient.json.decodeFromJsonElement(TrackData.serializer(), it) }
        }.onSuccess { track = it; fitKey++ }.onFailure { error = it.message ?: "走行経路を取得できません" }
        loading = false
    }
    LaunchedEffect(date, overlay, metric) {
        radio = null
        radioError = ""
        if (!overlay) return@LaunchedEffect
        runCatching {
            radio = apiClient.get<RadioHistoryResponse>("/radio/history?date=$date&metric=$metric&cellSize=50&includeUnknownAccuracy=true&tripWeighted=true")
            legend = apiClient.get<RadioLegend>("/radio/heatmap/legend?metric=$metric")
        }.onFailure { radioError = it.message ?: "電波履歴を取得できません" }
    }

    val segments = remember(track) {
        track?.points.orEmpty().groupBy { it.segment }.values.map { seg -> seg.map { GeoPoint(it.lat, it.lon) } }
    }
    val cells = remember(radio, mode) {
        if (mode != "grid") emptyList() else radio?.cells.orEmpty().mapIndexedNotNull { i, c ->
            if (c.bounds.size < 2 || c.bounds[0].size < 2) return@mapIndexedNotNull null
            val (s, w) = c.bounds[0][0] to c.bounds[0][1]
            val (n, e) = c.bounds[1][0] to c.bounds[1][1]
            MapPolygon("cell$i", listOf(GeoPoint(s, w), GeoPoint(s, e), GeoPoint(n, e), GeoPoint(n, w)), parseHexColor(c.color), if (c.lowConfidence) 0.25f else 0.5f)
        }
    }
    val radioPoints = remember(radio, mode) {
        val pts = radio?.points.orEmpty().let { list -> if (mode == "grid" && radio?.cells?.isNotEmpty() == true) list.filter { it.accuracyUnknown } else list }
        pts.mapIndexed { i, pt ->
            MapPoint(
                "r$i", pt.gps.lat, pt.gps.lon, parseHexColor(pt.color), radius = 5f,
                stroke = parseHexColor(pt.color), strokeWidth = if (pt.accuracyUnknown) 1f else 2f,
                opacity = when { pt.state == "missing" -> 0.2f; pt.accuracyUnknown -> 0.35f; else -> 0.9f },
            )
        }
    }
    val day = runCatching { LocalDate.parse(date) }.getOrNull()

    NccPanel(
        title = "走行履歴マップ",
        icon = Icons.Default.Map,
        contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 0.dp, bottom = 16.dp),
    ) {
        // Date navigation
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconButton(
                enabled = day != null && (minDate == null || day.minusDays(1).toString() >= minDate),
                onClick = { day?.let { onDateChange(it.minusDays(1).toString()) } },
            ) { Icon(Icons.AutoMirrored.Filled.KeyboardArrowLeft, contentDescription = "前の日") }
            Row(
                modifier = Modifier.weight(1f).clip(RoundedCornerShape(12.dp)).clickable { picker = true }.padding(vertical = 8.dp),
                horizontalArrangement = Arrangement.Center,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(Icons.Default.CalendarMonth, contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                Text("  ${Formatters.formatJpDate(date)}", color = p.text, style = MaterialTheme.typography.titleSmall)
            }
            IconButton(
                enabled = day != null && day.plusDays(1).toString() <= maxDate,
                onClick = { day?.let { onDateChange(it.plusDays(1).toString()) } },
            ) { Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, contentDescription = "次の日") }
        }
        Box(Modifier.fillMaxWidth().height(320.dp).clip(RoundedCornerShape(16.dp)).background(p.raised)) {
            NccMap(
                modifier = Modifier.fillMaxSize(),
                view = "journey",
                scrollFriendly = true,
                mapThemes = mapThemes,
                routes = segments,
                points = if (overlay) radioPoints else emptyList(),
                polygons = if (overlay) cells else emptyList(),
                focus = MapFocus.FIT,
                focusKey = "$date-$fitKey-${overlay && (cells.isNotEmpty() || radioPoints.isNotEmpty())}",
            )
            if (segments.isEmpty() && !(overlay && (cells.isNotEmpty() || radioPoints.isNotEmpty()))) {
                Column(
                    Modifier.align(Alignment.Center).clip(RoundedCornerShape(14.dp)).background(p.glass).padding(horizontal = 16.dp, vertical = 12.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Text(if (loading) "走行経路を読み込み中…" else "この日の位置記録はありません", color = p.text, style = MaterialTheme.typography.titleSmall)
                    Text(track?.reason ?: "録画に保存された測位から走行経路を表示します。", color = p.muted, style = MaterialTheme.typography.bodySmall)
                }
            } else {
                Surface(
                    shape = CircleShape,
                    color = p.glass,
                    shadowElevation = 4.dp,
                    onClick = { fitKey++ },
                    modifier = Modifier.align(Alignment.TopEnd).padding(8.dp).size(40.dp),
                ) { Box(contentAlignment = Alignment.Center) { Icon(Icons.Default.FitScreen, contentDescription = "全体を表示", tint = p.accent) } }
            }
        }
        if (error.isNotBlank()) NccBanner(error, BannerTone.ERROR)
        Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            Text("測位点 ${track?.points?.size?.let { String.format("%,d", it) } ?: "—"}", color = p.muted, fontSize = 12.sp)
            Text("走行区間 ${if (track != null) segments.size else "—"}", color = p.muted, fontSize = 12.sp)
        }
        if (track?.sourceTruncated == true) HintText("一部の位置記録は読み取り範囲に含まれていません。", color = p.warn)
        SwitchRow(
            title = "電波の強さを重ねる",
            description = "4Gの電波状況を、走った場所ごとに色で表示します。",
            checked = overlay,
            onCheckedChange = { overlay = it },
        )
        if (overlay) {
            SectionTabs(
                tabs = listOf("rsrp" to "受信強度 RSRP", "sinr" to "品質 SINR", "rsrq" to "受信品質 RSRQ"),
                selectedId = metric,
                onSelect = { metric = it },
            )
            SectionTabs(tabs = listOf("grid" to "エリア集計", "points" to "測定点"), selectedId = mode, onSelect = { mode = it }, fill = true)
            if (radioError.isNotBlank()) NccBanner(radioError, BannerTone.ERROR)
            radio?.let { r ->
                FlowRow(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                    Text("観測 ${String.format("%,d", r.total)}", color = p.muted, fontSize = 12.sp)
                    Text("位置付き ${String.format("%,d", r.points.size)}", color = p.muted, fontSize = 12.sp)
                    if (r.unplaced > 0) Text("位置なし ${String.format("%,d", r.unplaced)}", color = p.muted, fontSize = 12.sp)
                }
                if (r.points.isEmpty() && r.cells.isEmpty()) HintText(r.reason ?: "この日の表示条件に合う電波観測はありません。")
            }
            legend?.let { lg ->
                FlowRow(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    lg.thresholds.forEach { t ->
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                            Box(Modifier.size(10.dp).clip(CircleShape).background(parseHexColor(t.color)))
                            Text("${t.label}${t.minimum?.let { " ≥${it.toInt()}" } ?: ""}", color = p.muted, fontSize = 11.sp)
                        }
                    }
                }
                if (lg.note.isNotBlank()) HintText("${lg.unit} · ${lg.note}")
            }
        }
    }

    if (picker) {
        val initial = day?.atStartOfDay()?.toInstant(ZoneOffset.UTC)?.toEpochMilli()
        val state = rememberDatePickerState(initialSelectedDateMillis = initial)
        DatePickerDialog(
            onDismissRequest = { picker = false },
            confirmButton = {
                TextButton(onClick = {
                    state.selectedDateMillis?.let { ms ->
                        val d = Instant.ofEpochMilli(ms).atZone(ZoneOffset.UTC).toLocalDate().toString()
                        if (d <= maxDate) onDateChange(d)
                    }
                    picker = false
                }) { Text("表示") }
            },
            dismissButton = { TextButton(onClick = { picker = false }) { Text("キャンセル") } },
        ) { DatePicker(state = state, showModeToggle = false) }
    }
}
