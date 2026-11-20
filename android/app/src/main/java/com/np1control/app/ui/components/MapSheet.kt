package com.np1control.app.ui.components

import androidx.compose.animation.core.animate
import androidx.compose.animation.core.spring
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.Orientation
import androidx.compose.foundation.gestures.draggable
import androidx.compose.foundation.gestures.rememberDraggableState
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.nestedscroll.NestedScrollConnection
import androidx.compose.ui.input.nestedscroll.NestedScrollSource
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.Velocity
import androidx.compose.ui.unit.dp
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.launch
import kotlin.math.abs

/**
 * Sheet pulled up over the dashboard map on a phone. It rests at a peek, about half (default) and
 * full height; only the full sheet scrolls its content, a shorter one is dragged as a whole.
 * [onRestHeight] reports the resting height (capped at half) so the map keeps the car in view.
 */
@Composable
fun MapSheet(
    maxHeight: Dp,
    modifier: Modifier = Modifier,
    onRestHeight: (Dp) -> Unit = {},
    content: @Composable ColumnScope.() -> Unit,
) {
    val p = LocalNccPalette.current
    val density = LocalDensity.current
    val scope = rememberCoroutineScope()
    val maxPx = with(density) { maxHeight.toPx() }
    val stops = remember(maxPx) {
        listOf(
            minOf(with(density) { 170.dp.toPx() }, maxPx * 0.3f),
            maxPx * 0.46f,
            maxPx,
        )
    }
    var level by remember { mutableIntStateOf(1) }
    var heightPx by remember { mutableFloatStateOf(stops[1]) }
    LaunchedEffect(stops) { heightPx = stops[level] }
    LaunchedEffect(level, stops) { onRestHeight(with(density) { stops[minOf(level, 1)].toDp() }) }
    val scroll = rememberScrollState()

    fun settle(velocity: Float) {
        val nearest = stops.indices.minBy { abs(stops[it] - heightPx) }
        var next = nearest
        if (abs(velocity) > 1200f) next = (level + if (velocity < 0) 1 else -1).coerceIn(0, 2)
        else if (next == level && abs(heightPx - stops[level]) > 40f) next = (level + if (heightPx > stops[level]) 1 else -1).coerceIn(0, 2)
        level = next
        scope.launch {
            animate(heightPx, stops[next], animationSpec = spring(dampingRatio = 0.85f, stiffness = 500f)) { v, _ -> heightPx = v }
            if (next < 2) scroll.scrollTo(0)
        }
    }

    val connection = remember(stops) {
        object : NestedScrollConnection {
            override fun onPreScroll(available: Offset, source: NestedScrollSource): Offset {
                val dy = available.y
                if (dy < 0 && heightPx < stops[2]) {
                    val take = maxOf(dy, -(stops[2] - heightPx))
                    heightPx -= take
                    return Offset(0f, take)
                }
                return Offset.Zero
            }

            override fun onPostScroll(consumed: Offset, available: Offset, source: NestedScrollSource): Offset {
                val dy = available.y
                if (dy > 0 && heightPx > stops[0]) {
                    val take = minOf(dy, heightPx - stops[0])
                    heightPx -= take
                    return Offset(0f, take)
                }
                return Offset.Zero
            }

            override suspend fun onPreFling(available: Velocity): Velocity {
                val atRest = stops.any { abs(it - heightPx) < 1f }
                if (!atRest || (heightPx < stops[2] - 1f)) {
                    settle(available.y)
                    return available
                }
                return Velocity.Zero
            }
        }
    }

    Surface(
        modifier = modifier
            .fillMaxWidth()
            .height(with(density) { heightPx.toDp() }),
        shape = RoundedCornerShape(topStart = 26.dp, topEnd = 26.dp),
        color = p.bg,
        shadowElevation = 18.dp,
    ) {
        Column(Modifier.fillMaxWidth()) {
            // Grip: drag or tap to move between heights
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(26.dp)
                    .draggable(
                        orientation = Orientation.Vertical,
                        state = rememberDraggableState { delta -> heightPx = (heightPx - delta).coerceIn(stops[0], stops[2]) },
                        onDragStopped = { v -> settle(v) },
                    )
                    .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null) {
                        level = if (level == 2) 0 else level + 1
                        scope.launch { animate(heightPx, stops[level]) { v, _ -> heightPx = v } }
                    },
                contentAlignment = Alignment.Center,
            ) {
                Box(Modifier.width(40.dp).height(5.dp).clip(RoundedCornerShape(99.dp)).background(p.lineStrong))
            }
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f)
                    .nestedScroll(connection)
                    .verticalScroll(scroll)
                    .padding(start = 12.dp, end = 12.dp, bottom = 16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
                content = content,
            )
        }
    }
}
