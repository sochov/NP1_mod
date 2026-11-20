package com.np1control.app.ui.components

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.np1control.app.ui.theme.LocalNccPalette
import kotlin.math.max

/** A time-stamped sample for live sparklines. */
data class Sample(val atMs: Long, val value: Double?)

/**
 * Line chart of recent samples over a fixed time window, with a soft fill under the first series.
 * Gaps (null values or samples further apart than [gapMs]) break the line.
 */
@Composable
fun SparklineCanvas(
    series: List<Pair<Color, List<Sample>>>,
    windowMs: Long,
    nowMs: Long,
    fixedMax: Double? = null,
    height: Dp = 44.dp,
    gapMs: Long = 16_000L,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    val from = nowMs - windowMs
    val visible = series.flatMap { s -> s.second.filter { it.atMs >= from && it.value != null }.map { it.value!! } }
    val maxVal = fixedMax ?: max(1.0, (visible.maxOrNull() ?: 1.0) * 1.15)
    Canvas(modifier = modifier.fillMaxWidth().height(height)) {
        val pad = 3f
        val plotH = size.height - pad * 2
        val dash = PathEffect.dashPathEffect(floatArrayOf(3f, 5f))
        for (f in listOf(0f, 0.5f)) {
            val y = pad + plotH * f
            drawLine(p.line, Offset(0f, y), Offset(size.width, y), strokeWidth = 1f, pathEffect = dash)
        }
        drawLine(p.line, Offset(0f, pad + plotH), Offset(size.width, pad + plotH), strokeWidth = 1f)
        series.forEachIndexed { index, (color, values) ->
            val path = Path()
            val fill = Path()
            var drawing = false
            var prevAt = 0L
            var firstX = 0f
            var lastX = 0f
            for (pt in values) {
                if (pt.atMs < from || pt.atMs > nowMs) continue
                val v = pt.value
                if (v == null || !v.isFinite()) { drawing = false; continue }
                val x = ((pt.atMs - from).toFloat() / windowMs) * size.width
                val y = pad + plotH - ((v.coerceIn(0.0, maxVal) / maxVal) * plotH).toFloat()
                if (drawing && pt.atMs - prevAt <= gapMs) {
                    path.lineTo(x, y); fill.lineTo(x, y)
                } else {
                    path.moveTo(x, y)
                    if (index == 0) {
                        if (drawing) { fill.lineTo(lastX, pad + plotH); fill.lineTo(firstX, pad + plotH); fill.close() }
                        fill.moveTo(x, pad + plotH); fill.lineTo(x, y); firstX = x
                    }
                }
                drawing = true
                prevAt = pt.atMs
                lastX = x
            }
            if (index == 0 && drawing) {
                fill.lineTo(lastX, pad + plotH); fill.lineTo(firstX, pad + plotH); fill.close()
                drawPath(fill, Brush.verticalGradient(listOf(color.copy(alpha = 0.22f), color.copy(alpha = 0.02f))))
            }
            drawPath(path, color = color, style = Stroke(width = 2.dp.toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round))
        }
    }
}

/** Radio quality colour scale used by the trip map (RSRP, dBm). */
fun rsrpColor(rsrp: Double?): Color = when {
    rsrp == null -> Color(0xFF8892A2)
    rsrp >= -85 -> Color(0xFF1A7F55)
    rsrp >= -95 -> Color(0xFF4CAF50)
    rsrp >= -105 -> Color(0xFFF59E0B)
    rsrp >= -115 -> Color(0xFFEA580C)
    else -> Color(0xFFDC2626)
}
