package com.np1control.app.ui.components

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.OpenInNew
import androidx.compose.material.icons.filled.ArrowDropDown
import androidx.compose.material.icons.filled.Route
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
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
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.Formatters
import com.np1control.app.data.JourneyDay
import com.np1control.app.data.JourneyHistory
import com.np1control.app.data.JourneyHour
import com.np1control.app.data.JourneyPeriod
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.data.TripStats
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import java.time.DayOfWeek
import java.time.LocalDate
import java.time.temporal.TemporalAdjusters
import kotlin.math.ceil
import kotlin.math.floor
import kotlin.math.log10
import kotlin.math.max
import kotlin.math.min
import kotlin.math.pow

// ── Aggregation (mirrors frontend/src/services/journey.ts) ─────────────────

data class JourneyBucket(
    val key: String,
    val from: String,
    val to: String,
    val distance: Double?,
    val hours: Double?,
    val observedDays: Int,
    val dayCount: Int,
    val cumulativeDistance: Double?,
    val cumulativeHours: Double?,
)

enum class JourneyGrouping(val id: String, val label: String, val unit: String) {
    DAY("day", "日毎", "日"),
    WEEK("week", "週毎", "週"),
    MONTH("month", "月毎", "月"),
}

private fun finite(v: Double?): Boolean = v != null && v.isFinite() && v >= 0

fun calendarDays(from: String, to: String, entries: List<JourneyDay>): List<JourneyDay> {
    val start = runCatching { LocalDate.parse(from) }.getOrNull() ?: return entries
    val end = runCatching { LocalDate.parse(to) }.getOrNull() ?: return entries
    if (start > end || java.time.temporal.ChronoUnit.DAYS.between(start, end) > 36600) return entries
    val known = entries.associateBy { it.date }
    val out = ArrayList<JourneyDay>()
    var d = start
    while (d <= end) {
        val key = d.toString()
        out.add(known[key] ?: JourneyDay(date = key))
        d = d.plusDays(1)
    }
    return out
}

fun groupJourney(days: List<JourneyDay>, grouping: JourneyGrouping): List<JourneyBucket> {
    data class Acc(val key: String, val from: String, var to: String, var distance: Double?, var hours: Double?, var observed: Int, var count: Int)
    val groups = LinkedHashMap<String, Acc>()
    for (day in days) {
        val date = runCatching { LocalDate.parse(day.date) }.getOrNull() ?: continue
        val key = when (grouping) {
            JourneyGrouping.MONTH -> day.date.take(7)
            JourneyGrouping.WEEK -> date.with(TemporalAdjusters.previousOrSame(DayOfWeek.MONDAY)).toString()
            JourneyGrouping.DAY -> day.date
        }
        val acc = groups.getOrPut(key) { Acc(key, day.date, day.date, null, null, 0, 0) }
        acc.to = day.date
        acc.count++
        val hasD = day.observed && finite(day.distanceKm)
        val hasT = day.observed && finite(day.drivingSeconds)
        if (hasD) acc.distance = (acc.distance ?: 0.0) + day.distanceKm!!
        if (hasT) acc.hours = (acc.hours ?: 0.0) + day.drivingSeconds!! / 3600.0
        if (hasD || hasT) acc.observed++
    }
    val list = groups.values.toList()
    val anyD = list.any { it.distance != null }
    val anyT = list.any { it.hours != null }
    var cd = 0.0
    var ch = 0.0
    return list.map { a ->
        a.distance?.let { cd += it }
        a.hours?.let { ch += it }
        JourneyBucket(a.key, a.from, a.to, a.distance, a.hours, a.observed, a.count, if (anyD) cd else null, if (anyT) ch else null)
    }
}

fun rollingJourney(hours: List<JourneyHour>): List<JourneyBucket> {
    val anyD = hours.any { it.observed && finite(it.distanceKm) }
    val anyT = hours.any { it.observed && finite(it.drivingSeconds) }
    var cd = 0.0
    var ch = 0.0
    return hours.map { h ->
        val d = if (h.observed && finite(h.distanceKm)) h.distanceKm else null
        val t = if (h.observed && finite(h.drivingSeconds)) h.drivingSeconds!! / 3600.0 else null
        d?.let { cd += it }
        t?.let { ch += it }
        JourneyBucket(h.from, h.from, h.to, d, t, if (h.observed) 1 else 0, 1, if (anyD) cd else null, if (anyT) ch else null)
    }
}

fun niceMaximum(values: List<Double?>): Double {
    val v = values.maxOfOrNull { it ?: 0.0 }?.coerceAtLeast(0.0) ?: 0.0
    if (v == 0.0) return 1.0
    val step = 10.0.pow(floor(log10(v)))
    return ceil(v / step) * step
}

fun decimalText(v: Double?, digits: Int = 1): String {
    if (v == null || !v.isFinite()) return "—"
    val factor = 10.0.pow(digits)
    val rounded = Math.round(v * factor) / factor
    return if (rounded % 1.0 == 0.0) String.format("%,.0f", rounded) else String.format("%,.${digits}f", rounded).trimEnd('0').trimEnd('.')
}

// ── Chart ──────────────────────────────────────────────────────────────────

@Composable
fun JourneyChart(
    buckets: List<JourneyBucket>,
    rolling: Boolean,
    grouping: JourneyGrouping,
    selectedKey: String?,
    onSelect: (JourneyBucket) -> Unit,
    modifier: Modifier = Modifier,
    height: Dp = 170.dp,
) {
    val p = LocalNccPalette.current
    val distColor = p.accent
    val timeColor = p.uploadOrange
    val gridColor = p.line
    val density = LocalDensity.current
    val distMax = niceMaximum(buckets.map { it.distance })
    val hoursMax = niceMaximum(buckets.map { it.hours })
    val cumDistMax = niceMaximum(buckets.map { it.cumulativeDistance })
    val cumHoursMax = niceMaximum(buckets.map { it.cumulativeHours })
    val minStep = if (rolling) 9.dp else 30.dp

    Row(modifier = modifier.fillMaxWidth(), verticalAlignment = Alignment.Top) {
        // Left axis: per-bucket values
        AxisColumn(height, listOf(distMax, distMax / 2, 0.0), listOf(hoursMax, hoursMax / 2, 0.0), distColor, timeColor)
        BoxWithConstraints(modifier = Modifier.weight(1f)) {
            val needed = minStep * buckets.size
            val width = if (needed > maxWidth) needed else maxWidth
            val scroll = rememberScrollState()
            LaunchedEffect(buckets.size, width) { scroll.scrollTo(scroll.maxValue) }
            Column(modifier = Modifier.horizontalScroll(scroll)) {
                Canvas(
                    modifier = Modifier
                        .width(width)
                        .height(height)
                        .pointerInput(buckets) {
                            detectTapGestures { offset ->
                                if (buckets.isEmpty()) return@detectTapGestures
                                val step = size.width.toFloat() / buckets.size
                                val idx = (offset.x / step).toInt().coerceIn(0, buckets.lastIndex)
                                onSelect(buckets[idx])
                            }
                        },
                ) {
                    val top = 8f
                    val bottom = 6f
                    val plotH = size.height - top - bottom
                    val base = top + plotH
                    val step = size.width / buckets.size.coerceAtLeast(1)
                    fun y(v: Double, m: Double) = (base - (v / m) * plotH).toFloat()

                    val dash = PathEffect.dashPathEffect(floatArrayOf(4f, 6f))
                    for (f in listOf(0f, 0.5f)) {
                        val yy = top + plotH * f
                        drawLine(gridColor, Offset(0f, yy), Offset(size.width, yy), strokeWidth = 1f, pathEffect = dash)
                    }
                    drawLine(gridColor, Offset(0f, base), Offset(size.width, base), strokeWidth = 1.5f)

                    val barW = if (rolling) min(4.dp.toPx(), step / 3f) else min(10.dp.toPx(), step * 0.3f)
                    val gap = if (rolling) 1f else 2.dp.toPx()
                    buckets.forEachIndexed { i, b ->
                        val cx = (i + 0.5f) * step
                        if (b.key == selectedKey) {
                            drawRoundRect(p.soft, topLeft = Offset(i * step + 1f, top), size = Size(step - 2f, plotH), cornerRadius = CornerRadius(8f, 8f))
                        }
                        val missing = b.distance == null && b.hours == null
                        if (missing && !rolling) {
                            val s = 3.dp.toPx()
                            drawLine(p.faint, Offset(cx - s, base - 4f - s), Offset(cx + s, base - 4f + s), strokeWidth = 1.5f)
                            drawLine(p.faint, Offset(cx - s, base - 4f + s), Offset(cx + s, base - 4f - s), strokeWidth = 1.5f)
                            return@forEachIndexed
                        }
                        b.distance?.let { d ->
                            if (d > 0) {
                                val h = (base - y(d, distMax)).coerceAtLeast(2f)
                                drawRoundRect(distColor, topLeft = Offset(cx - barW - gap / 2, base - h), size = Size(barW, h), cornerRadius = CornerRadius(barW / 2.5f, barW / 2.5f))
                            } else if (!rolling) drawCircle(distColor, radius = 2.5.dp.toPx(), center = Offset(cx - barW / 2 - gap / 2, base))
                        }
                        b.hours?.let { t ->
                            if (t > 0) {
                                val h = (base - y(t, hoursMax)).coerceAtLeast(2f)
                                drawRoundRect(timeColor, topLeft = Offset(cx + gap / 2, base - h), size = Size(barW, h), cornerRadius = CornerRadius(barW / 2.5f, barW / 2.5f))
                            } else if (!rolling) drawCircle(timeColor, radius = 2.5.dp.toPx(), center = Offset(cx + barW / 2 + gap / 2, base))
                        }
                    }
                    // Cumulative lines (dashed so they read as a second scale)
                    fun line(values: List<Double?>, m: Double, color: Color) {
                        val path = Path()
                        var started = false
                        values.forEachIndexed { i, v ->
                            if (v == null) return@forEachIndexed
                            val x = (i + 0.5f) * step
                            if (!started) { path.moveTo(x, y(v, m)); started = true } else path.lineTo(x, y(v, m))
                        }
                        if (started) {
                            drawPath(path, color.copy(alpha = 0.85f), style = Stroke(width = 2.dp.toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round))
                        }
                        if (buckets.size <= 40) values.forEachIndexed { i, v ->
                            if (v != null) {
                                drawCircle(Color.White, radius = 3.5.dp.toPx(), center = Offset((i + 0.5f) * step, y(v, m)))
                                drawCircle(color, radius = 2.4.dp.toPx(), center = Offset((i + 0.5f) * step, y(v, m)))
                            }
                        }
                    }
                    line(buckets.map { it.cumulativeDistance }, cumDistMax, distColor)
                    line(buckets.map { it.cumulativeHours }, cumHoursMax, timeColor)
                }
                // X labels
                Box(modifier = Modifier.width(width).height(18.dp)) {
                    val labels = xLabels(buckets, rolling, grouping)
                    labels.forEach { (index, text) ->
                        val stepDp = width / buckets.size.coerceAtLeast(1)
                        val x = stepDp * (index + 0.5f) - 20.dp
                        Text(
                            text,
                            color = p.muted,
                            fontSize = 10.sp,
                            modifier = Modifier.padding(start = x.coerceAtLeast(0.dp)).width(40.dp),
                            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                            maxLines = 1,
                        )
                    }
                }
            }
        }
        AxisColumn(height, listOf(cumDistMax, cumDistMax / 2, 0.0), listOf(cumHoursMax, cumHoursMax / 2, 0.0), distColor, timeColor, end = true)
    }
}

@Composable
private fun AxisColumn(height: Dp, dist: List<Double>, hrs: List<Double>, dc: Color, tc: Color, end: Boolean = false) {
    Column(
        modifier = Modifier.height(height).padding(top = 2.dp, bottom = 0.dp).width(36.dp),
        verticalArrangement = Arrangement.SpaceBetween,
        horizontalAlignment = if (end) Alignment.Start else Alignment.End,
    ) {
        dist.indices.forEach { i ->
            Column(horizontalAlignment = if (end) Alignment.Start else Alignment.End, modifier = Modifier.padding(horizontal = 3.dp)) {
                Text(decimalText(dist[i], 0), color = dc, fontSize = 9.sp, lineHeight = 10.sp, maxLines = 1)
                if (i < dist.lastIndex) Text(decimalText(hrs[i], 1) + "h", color = tc, fontSize = 9.sp, lineHeight = 10.sp, maxLines = 1)
            }
        }
    }
}

private fun xLabels(buckets: List<JourneyBucket>, rolling: Boolean, grouping: JourneyGrouping): List<Pair<Int, String>> {
    if (buckets.isEmpty()) return emptyList()
    if (rolling) {
        return buckets.mapIndexedNotNull { i, b ->
            val hour = Formatters.formatHourMinute(b.from).take(2).toIntOrNull() ?: return@mapIndexedNotNull null
            if (hour % 6 == 0) i to "${hour}時" else null
        }
    }
    val every = max(1, ceil(buckets.size / 7.0).toInt())
    return buckets.mapIndexedNotNull { i, b ->
        if (i % every == 0 || i == buckets.lastIndex && buckets.size <= 14) {
            i to if (grouping == JourneyGrouping.MONTH) "${b.from.substring(5, 7).toInt()}月" else Formatters.shortDate(b.from)
        } else null
    }
}

@Composable
fun ChartLegend(rolling: Boolean, grouping: JourneyGrouping) {
    val p = LocalNccPalette.current
    Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
        LegendSwatch(p.accent, "距離 km")
        LegendSwatch(p.uploadOrange, "時間 h")
        Text("棒: ${if (rolling) "1時間" else grouping.unit}ごと · 線: 累計", color = p.muted, fontSize = 10.sp)
    }
}

@Composable
private fun LegendSwatch(color: Color, label: String) {
    val p = LocalNccPalette.current
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        Box(Modifier.size(8.dp).clip(CircleShape).background(color))
        Text(label, color = p.muted, fontSize = 10.sp)
    }
}

// ── Panel with periods, totals and the chart ───────────────────────────────

val journeyPeriods = listOf("24h" to "24時間", "7" to "7日", "30" to "30日", "90" to "90日", "365" to "1年", "all" to "全期間")

/**
 * 「走行の記録」card shared by the dashboard and the trip history. Totals appear at once from the
 * summary's precomputed `tripStats`; the detailed series then loads from `/trips` and refreshes every
 * 30 seconds. Failures keep the last good data and offer a retry.
 */
@Composable
fun JourneyPanel(
    apiClient: NccApiClient,
    tripStats: TripStats?,
    modifier: Modifier = Modifier,
    chartHeight: Dp = 170.dp,
    compact: Boolean = false,
    glass: Boolean = false,
    onOpenTrips: (() -> Unit)? = null,
    onLoaded: (JourneyHistory?, String) -> Unit = { _, _ -> },
    selectedDate: String? = null,
    onSelectDate: ((String) -> Unit)? = null,
) {
    val p = LocalNccPalette.current
    var period by remember { mutableStateOf("7") }
    var grouping by remember { mutableStateOf(JourneyGrouping.DAY) }
    var history by remember { mutableStateOf<JourneyHistory?>(null) }
    var loadedPeriod by remember { mutableStateOf("") }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var selected by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    var groupMenu by remember { mutableStateOf(false) }

    LaunchedEffect(period, reload) {
        if (loadedPeriod != period) {
            history = null
            onLoaded(null, period)
        }
        while (isActive) {
            loading = history == null
            runCatching {
                val obs = apiClient.get<Observation>("/trips?period=$period")
                val data = obs.data ?: throw IllegalStateException(obs.reason ?: "走行履歴を取得できません")
                apiClient.json.decodeFromJsonElement(JourneyHistory.serializer(), data)
            }.onSuccess {
                history = it
                loadedPeriod = period
                error = ""
                onLoaded(it, period)
            }.onFailure {
                error = if (history != null) "更新できませんでした。前回の記録を表示しています。" else (it.message ?: "走行履歴を取得できません")
            }
            loading = false
            delay(30_000L)
        }
    }

    val rolling = period == "24h"
    // The summary's tripStats only covers the NMEA still on the SD card and under-reports longer
    // periods, so totals come from /trips alone (it answers in well under a second).
    val fallback: JourneyPeriod? = null
    val periodData = history?.period ?: fallback
    val buckets = remember(history, fallback, grouping, period) {
        val h = history
        if (rolling) rollingJourney(h?.hours.orEmpty())
        else {
            val days = h?.days ?: fallback?.days.orEmpty()
            val from = h?.range?.from ?: days.firstOrNull()?.date
            val to = h?.range?.to ?: days.lastOrNull()?.date
            val cal = if (from != null && to != null) calendarDays(from, to, days) else days
            groupJourney(cal, grouping)
        }
    }
    val populated = buckets.any { it.distance != null || it.hours != null }
    val detail = buckets.firstOrNull { it.key == selected }
    val estimated = history?.boundaryEstimated == true

    NccPanel(
        modifier = modifier,
        title = "走行の記録",
        subtitle = if (rolling) "過去24時間" else null,
        icon = Icons.Default.Route,
        glass = glass,
        headerEnd = if (onOpenTrips != null) {
            {
                PanelAction(onClick = onOpenTrips) {
                    Icon(Icons.AutoMirrored.Filled.OpenInNew, contentDescription = "走行履歴を開く", tint = p.muted, modifier = Modifier.size(18.dp))
                }
            }
        } else null,
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(22.dp), verticalAlignment = Alignment.Bottom) {
            MetricBlock(
                label = "走行距離",
                value = (if (estimated) "約" else "") + decimalText(periodData?.distanceKm),
                unit = "km",
                color = p.accent,
                size = if (compact) 26 else 30,
            )
            MetricBlock(
                label = "走行時間",
                value = (if (estimated) "約" else "") + Formatters.duration(periodData?.drivingSeconds),
                color = p.uploadOrange,
                size = if (compact) 20 else 22,
                modifier = Modifier.weight(1f),
            )
            if (!rolling) {
                Box {
                    TextButton(onClick = { groupMenu = true }, contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 8.dp)) {
                        Text(grouping.label, fontSize = 12.sp, color = p.text)
                        Icon(Icons.Default.ArrowDropDown, contentDescription = "グラフの集計単位", tint = p.muted)
                    }
                    DropdownMenu(expanded = groupMenu, onDismissRequest = { groupMenu = false }) {
                        JourneyGrouping.entries.forEach { g ->
                            DropdownMenuItem(text = { Text(g.label) }, onClick = { grouping = g; selected = null; groupMenu = false })
                        }
                    }
                }
            }
        }
        Row(modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState())) {
            SectionTabs(
                tabs = journeyPeriods,
                selectedId = period,
                onSelect = {
                    period = it
                    selected = null
                    grouping = when (it) {
                        "90" -> JourneyGrouping.WEEK
                        "365", "all" -> JourneyGrouping.MONTH
                        else -> JourneyGrouping.DAY
                    }
                },
            )
        }
        when {
            error.isNotBlank() && history == null && fallback == null -> NccBanner(
                error,
                BannerTone.ERROR,
                action = { TextButton(onClick = { reload++ }) { Text("再試行") } },
            )
            (loading || history == null) && !populated -> LoadingRow("走行履歴を読み込み中…", Modifier.height(chartHeight))
            !populated -> EmptyState(
                Icons.Default.Route,
                "この期間の走行記録はありません",
                history?.reason ?: "距離と時間は、連続して測位できた記録から集計します。",
            )
            else -> {
                JourneyChart(
                    buckets = buckets,
                    rolling = rolling,
                    grouping = grouping,
                    selectedKey = selected ?: selectedDate,
                    onSelect = {
                        selected = it.key
                        if (!rolling && grouping == JourneyGrouping.DAY) onSelectDate?.invoke(it.key)
                    },
                    height = chartHeight,
                )
                if (!compact || detail != null) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(10.dp))
                            .background(p.raised)
                            .padding(horizontal = 12.dp, vertical = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        if (detail != null) {
                            Column(Modifier.weight(1f)) {
                                Text(bucketLabel(detail, rolling), color = p.text, style = MaterialTheme.typography.labelLarge)
                                Text(
                                    "距離 ${decimalText(detail.distance)} km · 時間 ${Formatters.duration(detail.hours?.times(3600))}" +
                                        if (!rolling && detail.dayCount > 1) " · 計測 ${detail.observedDays}/${detail.dayCount}日" else "",
                                    color = p.muted,
                                    style = MaterialTheme.typography.bodySmall,
                                )
                            }
                        } else {
                            Text("棒をタップすると、その${if (rolling) "時間" else grouping.unit}の値を確認できます。", color = p.muted, style = MaterialTheme.typography.bodySmall)
                        }
                    }
                }
                if (!compact) ChartLegend(rolling, grouping)
                if (error.isNotBlank()) HintText(error, color = p.warn)
            }
        }
        if (history?.sourceTruncated == true || tripStats?.sourceTruncated == true) {
            HintText("一部の区間は集計に含まれていません。", color = p.warn)
        }
    }
}

private fun bucketLabel(b: JourneyBucket, rolling: Boolean): String = when {
    rolling -> "${Formatters.formatHourMinute(b.from)}〜${Formatters.formatHourMinute(b.to)}"
    b.from == b.to -> Formatters.formatJpDate(b.from)
    else -> "${Formatters.shortDate(b.from)}〜${Formatters.shortDate(b.to)}"
}
