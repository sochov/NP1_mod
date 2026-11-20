package com.np1control.app.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.material.icons.filled.Map
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Route
import androidx.compose.material3.Checkbox
import androidx.compose.material3.Icon
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.Formatters
import com.np1control.app.data.JourneyHistory
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.data.RadioHistoryResponse
import com.np1control.app.data.RadioPoint
import com.np1control.app.data.TrackData
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.JourneyBarItem
import com.np1control.app.ui.components.JourneyChartCanvas
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.NccTag
import com.np1control.app.ui.components.TrackMapCanvas
import com.np1control.app.ui.theme.LocalNccPalette

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun TripsScreen(
    apiClient: NccApiClient,
    onOpenRecordingAt: (String) -> Unit,
) {
    val p = LocalNccPalette.current
    var period by remember { mutableStateOf("7") }
    var history by remember { mutableStateOf<JourneyHistory?>(null) }
    var selectedDate by remember { mutableStateOf("") }
    var selectedBar by remember { mutableStateOf<JourneyBarItem?>(null) }
    var track by remember { mutableStateOf<TrackData?>(null) }
    var radioPoints by remember { mutableStateOf<List<RadioPoint>>(emptyList()) }
    var showRadio by remember { mutableStateOf(true) }
    var error by remember { mutableStateOf("") }

    LaunchedEffect(period) {
        error = ""
        runCatching {
            val obs = apiClient.get<Observation>("/trips?period=$period")
            val parsed = obs.data?.let { apiClient.json.decodeFromJsonElement(JourneyHistory.serializer(), it) }
            history = parsed
            if (parsed != null) {
                val latestObsDay = parsed.days.lastOrNull { it.observed }?.date ?: parsed.range.to
                if (selectedDate.isBlank() || selectedDate < parsed.range.from || selectedDate > parsed.range.to) {
                    selectedDate = latestObsDay
                }
            }
        }.onFailure { error = it.message ?: "走行履歴を取得できません" }
    }

    LaunchedEffect(selectedDate, showRadio) {
        if (selectedDate.isBlank()) return@LaunchedEffect
        runCatching {
            val obs = apiClient.get<Observation>("/recordings/track?date=$selectedDate")
            track = obs.data?.let { apiClient.json.decodeFromJsonElement(TrackData.serializer(), it) }
        }
        if (showRadio) {
            runCatching {
                val days = period.toIntOrNull()?.coerceIn(1, 365) ?: 7
                val res = apiClient.get<RadioHistoryResponse>("/radio/history?days=$days&metric=rsrp")
                radioPoints = res.points
            }
        } else {
            radioPoints = emptyList()
        }
    }

    val barItems = remember(history) {
        val h = history ?: return@remember emptyList()
        var cd = 0.0
        var ch = 0.0
        h.days.map { d ->
            val dist = d.distanceKm
            val hrs = d.drivingSeconds?.div(3600.0)
            if (dist != null) cd += dist
            if (hrs != null) ch += hrs
            JourneyBarItem(
                key = d.date,
                label = d.date.takeLast(5).replace('-', '/'),
                distanceKm = dist,
                hours = hrs,
                cumulativeDistanceKm = if (dist != null) cd else null,
                cumulativeHours = if (hrs != null) ch else null,
            )
        }
    }

    val recordedDays = remember(history) {
        history?.days?.filter { it.observed }?.reversed().orEmpty()
    }
    val dayTrips = remember(history, selectedDate) {
        history?.trips?.filter {
            val s = Formatters.isoDateJst(it.start)
            val e = Formatters.isoDateJst(it.end)
            s <= selectedDate && e >= selectedDate
        }.orEmpty()
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (error.isNotBlank()) NccBanner(error, BannerTone.ERROR, onDismiss = { error = "" })

        NccPanel(title = "走行の記録", icon = Icons.Default.Route) {
            Row(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
                Column {
                    Text("走行距離", color = p.muted, fontSize = 11.sp)
                    Text("${Formatters.distanceText(history?.period?.distanceKm)} km", color = p.accent, fontSize = 22.sp, fontWeight = FontWeight.Bold)
                }
                Column {
                    Text("走行時間", color = p.muted, fontSize = 11.sp)
                    Text(Formatters.duration(history?.period?.drivingSeconds), color = p.uploadOrange, fontSize = 20.sp, fontWeight = FontWeight.Bold)
                }
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                listOf("24h" to "24時間", "7" to "7日", "30" to "30日", "90" to "90日", "365" to "365日", "all" to "全期間").forEach { (id, lbl) ->
                    val sel = period == id
                    NccTag(
                        text = lbl,
                        color = if (sel) p.accent else p.muted,
                        background = if (sel) p.soft else p.bg,
                        modifier = Modifier.clickable { period = id },
                    )
                }
            }
            JourneyChartCanvas(
                items = barItems,
                selectedKey = selectedBar?.key ?: selectedDate,
                onSelect = {
                    selectedBar = it
                    selectedDate = it.key
                },
                height = 160.dp,
            )
        }

        NccPanel(
            title = "走行履歴・電波マップ",
            subtitle = if (selectedDate.isNotBlank()) Formatters.formatJpDate(selectedDate) else null,
            icon = Icons.Default.Map,
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Checkbox(checked = showRadio, onCheckedChange = { showRadio = it })
                Text("電波強度 (RSRP) を重ねて表示", color = p.text, fontSize = 12.sp)
            }
            TrackMapCanvas(
                points = track?.points.orEmpty(),
                radioPoints = if (showRadio) radioPoints else emptyList(),
                height = 210.dp,
            )
        }

        NccPanel(title = "日別の走行区間") {
            if (recordedDays.isNotEmpty()) {
                Row(
                    modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    recordedDays.take(30).forEach { day ->
                        val sel = day.date == selectedDate
                        NccTag(
                            text = "${day.date.takeLast(5)} (${Formatters.distanceText(day.distanceKm)}km)",
                            color = if (sel) p.accent else p.text,
                            background = if (sel) p.soft else p.bg,
                            modifier = Modifier.clickable { selectedDate = day.date },
                        )
                    }
                }
            }
            if (dayTrips.isEmpty()) {
                Text("選択した日の走行区間はありません。", color = p.muted, fontSize = 12.sp)
            } else {
                dayTrips.forEach { trip ->
                    Surface(
                        shape = RoundedCornerShape(8.dp),
                        color = p.bg,
                        border = BorderStroke(1.dp, p.line),
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Row(
                            modifier = Modifier.padding(10.dp),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Column(modifier = Modifier.weight(1f)) {
                                Text(
                                    "${Formatters.formatTime(trip.start)} → ${Formatters.formatTime(trip.end)}",
                                    color = p.text,
                                    fontWeight = FontWeight.SemiBold,
                                    fontSize = 13.sp,
                                )
                                Text(
                                    "距離 ${Formatters.distanceText(trip.distanceKm)} km · 走行 ${Formatters.duration(trip.drivingSeconds)}",
                                    color = p.muted,
                                    fontSize = 11.sp,
                                )
                            }
                            TextButton(onClick = { onOpenRecordingAt(trip.start) }) {
                                Icon(Icons.Default.PlayArrow, contentDescription = null)
                                Text("録画再生")
                            }
                        }
                    }
                }
            }
        }
        Spacer(modifier = Modifier.height(12.dp))
    }
}
