package com.np1control.app.ui.components

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.PlateRecord
import com.np1control.app.data.RadioPoint
import com.np1control.app.data.TrackPoint
import com.np1control.app.ui.theme.LocalNccPalette
import kotlin.math.hypot
import kotlin.math.max

data class JourneyBarItem(
    val key: String,
    val label: String,
    val distanceKm: Double?,
    val hours: Double?,
    val cumulativeDistanceKm: Double?,
    val cumulativeHours: Double?,
)

@Composable
fun JourneyChartCanvas(
    items: List<JourneyBarItem>,
    selectedKey: String?,
    onSelect: (JourneyBarItem) -> Unit,
    height: Dp = 160.dp,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    val distColor = p.accent
    val timeColor = p.uploadOrange
    val gridColor = p.line

    if (items.isEmpty()) {
        Box(
            modifier = modifier
                .fillMaxWidth()
                .height(height)
                .clip(RoundedCornerShape(10.dp))
                .background(p.bg),
            contentAlignment = Alignment.Center,
        ) {
            Text("この期間の走行データはありません", color = p.muted, fontSize = 12.sp)
        }
        return
    }

    val maxDist = max(1.0, items.mapNotNull { it.distanceKm }.maxOrNull() ?: 1.0) * 1.15
    val maxHours = max(0.5, items.mapNotNull { it.hours }.maxOrNull() ?: 0.5) * 1.15
    val maxCumDist = max(1.0, items.mapNotNull { it.cumulativeDistanceKm }.maxOrNull() ?: 1.0) * 1.1
    val maxCumHours = max(0.5, items.mapNotNull { it.cumulativeHours }.maxOrNull() ?: 0.5) * 1.1

    Column(modifier = modifier.fillMaxWidth()) {
        Canvas(
            modifier = Modifier
                .fillMaxWidth()
                .height(height)
                .pointerInput(items) {
                    detectTapGestures { offset ->
                        val step = size.width.toFloat() / items.size.coerceAtLeast(1)
                        val idx = (offset.x / step).toInt().coerceIn(0, items.lastIndex)
                        onSelect(items[idx])
                    }
                },
        ) {
            val topPad = 10f
            val bottomPad = 14f
            val plotH = (size.height - topPad - bottomPad).coerceAtLeast(20f)
            val baseY = topPad + plotH
            val step = size.width / items.size.coerceAtLeast(1)

            // Horizontal gridlines
            for (f in listOf(0f, 0.5f, 1f)) {
                val y = topPad + plotH * f
                drawLine(gridColor, Offset(0f, y), Offset(size.width, y), strokeWidth = 1f)
            }

            // Bars
            val barW = (step * 0.28f).coerceIn(3f, 18f)
            val gap = (barW * 0.25f).coerceAtLeast(1.5f)
            items.forEachIndexed { i, item ->
                val cx = (i + 0.5f) * step
                if (item.key == selectedKey) {
                    drawRect(
                        color = p.soft.copy(alpha = 0.6f),
                        topLeft = Offset(i * step, topPad),
                        size = Size(step, plotH),
                    )
                }
                item.distanceKm?.takeIf { it > 0.0 }?.let { d ->
                    val h = ((d / maxDist) * plotH).toFloat().coerceAtLeast(2f)
                    drawRoundRect(
                        color = distColor.copy(alpha = 0.85f),
                        topLeft = Offset(cx - barW - gap / 2f, baseY - h),
                        size = Size(barW, h),
                        cornerRadius = CornerRadius(3f, 3f),
                    )
                }
                item.hours?.takeIf { it > 0.0 }?.let { hrs ->
                    val h = ((hrs / maxHours) * plotH).toFloat().coerceAtLeast(2f)
                    drawRoundRect(
                        color = timeColor.copy(alpha = 0.85f),
                        topLeft = Offset(cx + gap / 2f, baseY - h),
                        size = Size(barW, h),
                        cornerRadius = CornerRadius(3f, 3f),
                    )
                }
            }

            // Cumulative lines
            val distPath = Path()
            val timePath = Path()
            var dStarted = false
            var tStarted = false
            items.forEachIndexed { i, item ->
                val cx = (i + 0.5f) * step
                item.cumulativeDistanceKm?.let { cd ->
                    val y = baseY - ((cd / maxCumDist) * plotH).toFloat()
                    if (!dStarted) {
                        distPath.moveTo(cx, y)
                        dStarted = true
                    } else {
                        distPath.lineTo(cx, y)
                    }
                }
                item.cumulativeHours?.let { ch ->
                    val y = baseY - ((ch / maxCumHours) * plotH).toFloat()
                    if (!tStarted) {
                        timePath.moveTo(cx, y)
                        tStarted = true
                    } else {
                        timePath.lineTo(cx, y)
                    }
                }
            }
            if (dStarted) {
                drawPath(distPath, color = distColor, style = Stroke(width = 2.5f, cap = StrokeCap.Round, join = StrokeJoin.Round))
            }
            if (tStarted) {
                drawPath(timePath, color = timeColor, style = Stroke(width = 2.5f, cap = StrokeCap.Round, join = StrokeJoin.Round))
            }
            if (items.size <= 31) {
                items.forEachIndexed { i, item ->
                    val cx = (i + 0.5f) * step
                    item.cumulativeDistanceKm?.let { cd ->
                        val y = baseY - ((cd / maxCumDist) * plotH).toFloat()
                        drawCircle(distColor, radius = 3.5f, center = Offset(cx, y))
                    }
                    item.cumulativeHours?.let { ch ->
                        val y = baseY - ((ch / maxCumHours) * plotH).toFloat()
                        drawCircle(timeColor, radius = 3.5f, center = Offset(cx, y))
                    }
                }
            }
        }
        // X-axis labels (first / middle / last)
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Text(items.first().label, color = p.muted, fontSize = 10.sp)
            if (items.size > 2) {
                Text(items[items.size / 2].label, color = p.muted, fontSize = 10.sp)
            }
            if (items.size > 1) {
                Text(items.last().label, color = p.muted, fontSize = 10.sp)
            }
        }
    }
}

@Composable
fun SparklineCanvas(
    series: List<Pair<Color, List<Double?>>>,
    fixedMax: Double? = null,
    height: Dp = 44.dp,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    val allVals = series.flatMap { it.second }.filterNotNull()
    val maxVal = fixedMax ?: max(1.0, (allVals.maxOrNull() ?: 1.0) * 1.15)
    Canvas(
        modifier = modifier
            .fillMaxWidth()
            .height(height),
    ) {
        val pad = 4f
        val plotH = (size.height - pad * 2).coerceAtLeast(10f)
        for (f in listOf(0f, 0.5f, 1f)) {
            val y = pad + plotH * f
            drawLine(p.line, Offset(0f, y), Offset(size.width, y), strokeWidth = 1f)
        }
        for ((color, values) in series) {
            if (values.size < 2) continue
            val path = Path()
            var started = false
            val stepX = size.width / (values.size - 1).coerceAtLeast(1)
            values.forEachIndexed { idx, v ->
                if (v == null) {
                    started = false
                } else {
                    val x = idx * stepX
                    val y = pad + plotH - ((v.coerceIn(0.0, maxVal) / maxVal) * plotH).toFloat()
                    if (!started) {
                        path.moveTo(x, y)
                        started = true
                    } else {
                        path.lineTo(x, y)
                    }
                }
            }
            drawPath(path, color = color, style = Stroke(width = 2.2f, cap = StrokeCap.Round, join = StrokeJoin.Round))
        }
    }
}

/**
 * Lightweight self-contained GPS route & radio quality canvas map.
 * Works reliably offline (in-car NP1 hotspot) and on older Android / Echo Show 5 devices without MapLibre/WebGL issues.
 */
@Composable
fun TrackMapCanvas(
    points: List<TrackPoint>,
    currentPoint: TrackPoint? = null,
    radioPoints: List<RadioPoint> = emptyList(),
    height: Dp = 200.dp,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    val mapBg = if (p.isDark) Color(0xFF141820) else Color(0xFFECEFF4)
    val gridColor = if (p.isDark) Color(0xFF232936) else Color(0xFFDDE2EB)

    Box(
        modifier = modifier
            .fillMaxWidth()
            .height(height)
            .clip(RoundedCornerShape(10.dp))
            .background(mapBg)
            .border(1.dp, p.line, RoundedCornerShape(10.dp)),
    ) {
        if (points.isEmpty() && currentPoint == null && radioPoints.isEmpty()) {
            Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                Text("測位データがありません", color = p.muted, fontSize = 12.sp)
            }
            return
        }

        val allLat = buildList {
            points.forEach { add(it.lat) }
            currentPoint?.let { add(it.lat) }
            radioPoints.forEach { add(it.lat) }
        }
        val allLon = buildList {
            points.forEach { add(it.lon) }
            currentPoint?.let { add(it.lon) }
            radioPoints.forEach { add(it.lon) }
        }
        val minLat = allLat.minOrNull() ?: 35.68
        val maxLat = allLat.maxOrNull() ?: 35.68
        val minLon = allLon.minOrNull() ?: 139.76
        val maxLon = allLon.maxOrNull() ?: 139.76
        val latSpan = max(0.002, maxLat - minLat)
        val lonSpan = max(0.002, maxLon - minLon)

        Canvas(modifier = Modifier.fillMaxSize()) {
            // Subtle map grid
            val gridStep = 44f
            var x = gridStep
            while (x < size.width) {
                drawLine(gridColor, Offset(x, 0f), Offset(x, size.height), strokeWidth = 1f)
                x += gridStep
            }
            var y = gridStep
            while (y < size.height) {
                drawLine(gridColor, Offset(0f, y), Offset(size.width, y), strokeWidth = 1f)
                y += gridStep
            }

            val pad = 26f
            val w = (size.width - pad * 2).coerceAtLeast(10f)
            val h = (size.height - pad * 2).coerceAtLeast(10f)
            fun project(lat: Double, lon: Double): Offset {
                val px = pad + ((lon - minLon) / lonSpan).toFloat() * w
                val py = pad + ((maxLat - lat) / latSpan).toFloat() * h
                return Offset(px, py)
            }

            // Radio points overlay (colored by RSRP)
            for (rp in radioPoints) {
                val c = rsrpColor(rp.rsrp)
                drawCircle(color = c.copy(alpha = 0.72f), radius = 5f, center = project(rp.lat, rp.lon))
            }

            // Route polylines split by segment
            val grouped = points.groupBy { it.segment }
            for ((_, segPoints) in grouped) {
                if (segPoints.size < 2) continue
                val path = Path()
                segPoints.forEachIndexed { idx, pt ->
                    val o = project(pt.lat, pt.lon)
                    if (idx == 0) path.moveTo(o.x, o.y) else path.lineTo(o.x, o.y)
                }
                drawPath(
                    path = path,
                    color = p.accent,
                    style = Stroke(width = 4f, cap = StrokeCap.Round, join = StrokeJoin.Round),
                )
            }

            // Current / latest position marker
            val focus = currentPoint ?: points.lastOrNull()
            if (focus != null) {
                val pos = project(focus.lat, focus.lon)
                drawCircle(color = p.accent.copy(alpha = 0.25f), radius = 15f, center = pos)
                drawCircle(color = Color.White, radius = 8f, center = pos)
                drawCircle(color = p.accent, radius = 5.5f, center = pos)
            }
        }

        val focus = currentPoint ?: points.lastOrNull()
        if (focus != null) {
            Box(
                modifier = Modifier
                    .align(Alignment.BottomStart)
                    .padding(8.dp)
                    .clip(RoundedCornerShape(6.dp))
                    .background(p.paper.copy(alpha = 0.88f))
                    .padding(horizontal = 8.dp, vertical = 4.dp),
            ) {
                Text(
                    text = String.format("%.5f, %.5f", focus.lat, focus.lon),
                    color = p.text,
                    fontSize = 11.sp,
                    fontWeight = FontWeight.Medium,
                )
            }
        }
    }
}

private fun rsrpColor(rsrp: Double?): Color = when {
    rsrp == null -> Color(0xFF8892A2)
    rsrp >= -85 -> Color(0xFF1A7F55)
    rsrp >= -95 -> Color(0xFF4CAF50)
    rsrp >= -105 -> Color(0xFFF59E0B)
    rsrp >= -115 -> Color(0xFFEA580C)
    else -> Color(0xFFDC2626)
}

@Composable
fun PlateMapCanvas(
    records: List<PlateRecord>,
    selectedId: String?,
    onSelect: (PlateRecord) -> Unit,
    height: Dp = 220.dp,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    val located = records.filter { it.location != null }
    val mapBg = if (p.isDark) Color(0xFF141820) else Color(0xFFECEFF4)
    val gridColor = if (p.isDark) Color(0xFF232936) else Color(0xFFDDE2EB)

    Box(
        modifier = modifier
            .fillMaxWidth()
            .height(height)
            .clip(RoundedCornerShape(10.dp))
            .background(mapBg)
            .border(1.dp, p.line, RoundedCornerShape(10.dp)),
    ) {
        if (located.isEmpty()) {
            Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                Text("位置のあるナンバー記録はありません", color = p.muted, fontSize = 12.sp)
            }
            return
        }

        val minLat = located.minOf { it.location!!.lat }
        val maxLat = located.maxOf { it.location!!.lat }
        val minLon = located.minOf { it.location!!.lon }
        val maxLon = located.maxOf { it.location!!.lon }
        val latSpan = max(0.002, maxLat - minLat)
        val lonSpan = max(0.002, maxLon - minLon)

        Canvas(
            modifier = Modifier
                .fillMaxSize()
                .pointerInput(located) {
                    detectTapGestures { tap ->
                        val pad = 28f
                        val w = (size.width - pad * 2).coerceAtLeast(10f)
                        val h = (size.height - pad * 2).coerceAtLeast(10f)
                        var best: PlateRecord? = null
                        var bestDist = 48f
                        for (rec in located) {
                            val loc = rec.location!!
                            val px = pad + ((loc.lon - minLon) / lonSpan).toFloat() * w
                            val py = pad + ((maxLat - loc.lat) / latSpan).toFloat() * h
                            val d = hypot(tap.x - px, tap.y - py)
                            if (d < bestDist) {
                                bestDist = d
                                best = rec
                            }
                        }
                        if (best != null) onSelect(best)
                    }
                },
        ) {
            val gridStep = 44f
            var x = gridStep
            while (x < size.width) {
                drawLine(gridColor, Offset(x, 0f), Offset(x, size.height), strokeWidth = 1f)
                x += gridStep
            }
            var y = gridStep
            while (y < size.height) {
                drawLine(gridColor, Offset(0f, y), Offset(size.width, y), strokeWidth = 1f)
                y += gridStep
            }

            val pad = 28f
            val w = (size.width - pad * 2).coerceAtLeast(10f)
            val h = (size.height - pad * 2).coerceAtLeast(10f)

            // Draw non-hit first, then hit on top
            val sorted = located.sortedBy { it.matches.isNotEmpty() }
            for (rec in sorted) {
                val loc = rec.location!!
                val hit = rec.matches.isNotEmpty()
                val sel = rec.id == selectedId
                val alpha = if (loc.estimated == true) 0.45f else 0.92f
                val pos = Offset(
                    pad + ((loc.lon - minLon) / lonSpan).toFloat() * w,
                    pad + ((maxLat - loc.lat) / latSpan).toFloat() * h,
                )
                val radius = when {
                    sel && hit -> 13f
                    sel -> 10f
                    hit -> 9f
                    else -> 6f
                }
                drawCircle(
                    color = if (hit) p.plateHitLine else Color.White,
                    radius = radius + if (sel) 3f else 1.8f,
                    center = pos,
                )
                drawCircle(
                    color = (if (hit) p.plateHit else Color(0xFF397FD7)).copy(alpha = alpha),
                    radius = radius,
                    center = pos,
                )
            }
        }

        // Legend
        Row(
            modifier = Modifier
                .align(Alignment.BottomStart)
                .padding(8.dp)
                .clip(RoundedCornerShape(6.dp))
                .background(p.paper.copy(alpha = 0.88f))
                .padding(horizontal = 8.dp, vertical = 4.dp),
            horizontalArrangement = Arrangement.spacedBy(10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                Box(modifier = Modifier.size(8.dp).clip(CircleShape).background(Color(0xFF397FD7)))
                Text("記録", fontSize = 10.sp, color = p.muted)
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                Box(modifier = Modifier.size(10.dp).clip(CircleShape).background(p.plateHit))
                Text("条件一致", fontSize = 10.sp, color = p.muted)
            }
        }
    }
}
