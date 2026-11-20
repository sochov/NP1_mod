package com.np1control.app.ui.components

import androidx.compose.animation.animateColorAsState
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.ErrorOutline
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.WarningAmber
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.Formatters
import com.np1control.app.data.PlateReading
import com.np1control.app.ui.theme.LocalNccPalette
import com.np1control.app.ui.theme.NccPalette

// ── Tones ───────────────────────────────────────────────────────────────────

enum class Tone { NEUTRAL, ACCENT, OK, WARN, DANGER, ORANGE }

fun NccPalette.toneColor(tone: Tone): Color = when (tone) {
    Tone.NEUTRAL -> muted
    Tone.ACCENT -> accent
    Tone.OK -> ok
    Tone.WARN -> warn
    Tone.DANGER -> danger
    Tone.ORANGE -> uploadOrange
}

fun NccPalette.toneSoft(tone: Tone): Color = when (tone) {
    Tone.NEUTRAL -> raised
    Tone.ACCENT -> soft
    Tone.OK -> okSoft
    Tone.WARN -> warnSoft
    Tone.DANGER -> dangerSoft
    Tone.ORANGE -> uploadSoft
}

// ── Spacing tokens (one source for every screen) ──────────────────────────

object Spacing {
    /** Distance from the screen edge to cards and page text. */
    val gutter = 16.dp
    /** Inner padding of a card; titles, text and controls all start on this line. */
    val card = 16.dp
    /** Gap between cards. */
    val cardGap = 12.dp
    /** Gap between elements inside a card. */
    val item = 12.dp
}

// ── Page scaffolding ───────────────────────────────────────────────────────

/**
 * Scrollable page body with the same gutters on every screen. On wide screens the content keeps a
 * readable width; [wideContent] lets a page lay its cards out in two columns there.
 */
@Composable
fun ScreenColumn(
    modifier: Modifier = Modifier,
    maxWidth: Dp = 1100.dp,
    contentPadding: PaddingValues = PaddingValues(horizontal = Spacing.gutter, vertical = Spacing.gutter),
    content: @Composable ColumnScope.() -> Unit,
) {
    Box(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState()),
        contentAlignment = Alignment.TopCenter,
    ) {
        Column(
            modifier = Modifier
                .widthIn(max = maxWidth)
                .fillMaxWidth()
                .padding(contentPadding)
                .padding(bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(Spacing.cardGap),
            content = content,
        )
    }
}

/** Two equal columns when there is room, otherwise one. */
@Composable
fun AdaptiveColumns(
    modifier: Modifier = Modifier,
    minColumnWidth: Dp = 360.dp,
    spacing: Dp = Spacing.cardGap,
    left: @Composable ColumnScope.() -> Unit,
    right: @Composable ColumnScope.() -> Unit,
) {
    BoxWithConstraints(modifier = modifier.fillMaxWidth()) {
        if (maxWidth >= minColumnWidth * 2 + spacing) {
            Row(horizontalArrangement = Arrangement.spacedBy(spacing)) {
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(spacing), content = left)
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(spacing), content = right)
            }
        } else {
            Column(verticalArrangement = Arrangement.spacedBy(spacing)) {
                left()
                right()
            }
        }
    }
}

// ── Cards ──────────────────────────────────────────────────────────────────

@Composable
fun NccPanel(
    modifier: Modifier = Modifier,
    title: String? = null,
    subtitle: String? = null,
    icon: ImageVector? = null,
    headerEnd: (@Composable RowScope.() -> Unit)? = null,
    contentPadding: PaddingValues = PaddingValues(start = Spacing.card, end = Spacing.card, top = 4.dp, bottom = Spacing.card),
    glass: Boolean = false,
    content: @Composable ColumnScope.() -> Unit,
) {
    val p = LocalNccPalette.current
    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = RoundedCornerShape(20.dp),
        color = if (glass) p.glass else p.paper,
        border = BorderStroke(1.dp, if (p.isDark) p.line else p.line.copy(alpha = 0.7f)),
        shadowElevation = if (glass) 6.dp else if (p.isDark) 0.dp else 1.dp,
    ) {
        Column(modifier = Modifier.fillMaxWidth()) {
            if (title != null || headerEnd != null) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = 52.dp)
                        .padding(start = Spacing.card, end = Spacing.card, top = 10.dp, bottom = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    if (icon != null) IconBadge(icon = icon, size = 30.dp)
                    Column(modifier = Modifier.weight(1f)) {
                        if (title != null) {
                            Text(
                                text = title,
                                style = MaterialTheme.typography.titleMedium,
                                color = p.text,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        }
                    }
                    if (headerEnd != null) {
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(4.dp),
                            content = headerEnd,
                        )
                    }
                }
                if (!subtitle.isNullOrBlank()) {
                    Text(
                        text = subtitle,
                        style = MaterialTheme.typography.bodySmall,
                        color = p.muted,
                        modifier = Modifier.padding(start = Spacing.card, end = Spacing.card, bottom = 6.dp),
                    )
                }
            }
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(contentPadding),
                verticalArrangement = Arrangement.spacedBy(Spacing.item),
                content = content,
            )
        }
    }
}

/**
 * Icon button for card headers. The touch area overhangs the card padding so that the icon itself
 * lines up with the card's right content edge, like the text below it.
 */
@Composable
fun PanelAction(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    content: @Composable () -> Unit,
) {
    Box(
        modifier = modifier
            .offset(x = 8.dp)
            .size(40.dp)
            .clip(CircleShape)
            .clickable(enabled = enabled, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        androidx.compose.runtime.CompositionLocalProvider(
            androidx.compose.material3.LocalContentColor provides LocalNccPalette.current.muted,
        ) { content() }
    }
}

/** Small round tinted badge holding a section or row icon. */
@Composable
fun IconBadge(
    icon: ImageVector,
    tone: Tone = Tone.ACCENT,
    size: Dp = 34.dp,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    Box(
        modifier = modifier
            .size(size)
            .clip(RoundedCornerShape(size * 0.32f))
            .background(p.toneSoft(tone)),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = null, tint = p.toneColor(tone), modifier = Modifier.size(size * 0.56f))
    }
}

// ── Messages ───────────────────────────────────────────────────────────────

enum class BannerTone { ERROR, SUCCESS, NEUTRAL, WARN }

@Composable
fun NccBanner(
    message: String,
    tone: BannerTone = BannerTone.NEUTRAL,
    onDismiss: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
    action: (@Composable () -> Unit)? = null,
) {
    val p = LocalNccPalette.current
    val t = when (tone) {
        BannerTone.ERROR -> Tone.DANGER
        BannerTone.SUCCESS -> Tone.OK
        BannerTone.WARN -> Tone.WARN
        BannerTone.NEUTRAL -> Tone.ACCENT
    }
    val icon = when (tone) {
        BannerTone.ERROR -> Icons.Default.ErrorOutline
        BannerTone.SUCCESS -> Icons.Default.CheckCircle
        BannerTone.WARN -> Icons.Default.WarningAmber
        BannerTone.NEUTRAL -> Icons.Default.Info
    }
    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = RoundedCornerShape(14.dp),
        color = p.toneSoft(t),
    ) {
        Row(
            modifier = Modifier.padding(start = 12.dp, end = 4.dp, top = 10.dp, bottom = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Icon(icon, contentDescription = null, tint = p.toneColor(t), modifier = Modifier.size(18.dp))
            Text(
                text = message,
                color = if (tone == BannerTone.NEUTRAL) p.text else p.toneColor(t),
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.weight(1f).padding(end = 6.dp),
            )
            action?.invoke()
            if (onDismiss != null) {
                IconButton(onClick = onDismiss, modifier = Modifier.size(32.dp)) {
                    Icon(Icons.Default.Close, contentDescription = "閉じる", tint = p.muted, modifier = Modifier.size(16.dp))
                }
            }
        }
    }
}

/** Quiet explanatory note under a control. */
@Composable
fun HintText(text: String, modifier: Modifier = Modifier, color: Color? = null) {
    val p = LocalNccPalette.current
    Text(text, color = color ?: p.muted, style = MaterialTheme.typography.bodySmall, modifier = modifier)
}

@Composable
fun EmptyState(
    icon: ImageVector,
    title: String,
    message: String? = null,
    modifier: Modifier = Modifier,
    action: (@Composable () -> Unit)? = null,
) {
    val p = LocalNccPalette.current
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(p.raised)
            .padding(horizontal = 18.dp, vertical = 22.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Icon(icon, contentDescription = null, tint = p.faint, modifier = Modifier.size(28.dp))
        Text(title, color = p.text, style = MaterialTheme.typography.titleSmall, textAlign = TextAlign.Center)
        if (!message.isNullOrBlank()) {
            Text(message, color = p.muted, style = MaterialTheme.typography.bodySmall, textAlign = TextAlign.Center)
        }
        action?.invoke()
    }
}

@Composable
fun LoadingRow(text: String, modifier: Modifier = Modifier) {
    val p = LocalNccPalette.current
    Row(
        modifier = modifier.fillMaxWidth().padding(vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.Center,
    ) {
        CircularProgressIndicator(strokeWidth = 2.dp, modifier = Modifier.size(16.dp), color = p.accent)
        Spacer(Modifier.width(10.dp))
        Text(text, color = p.muted, style = MaterialTheme.typography.bodySmall)
    }
}

// ── Tags, pills and dots ───────────────────────────────────────────────────

@Composable
fun NccTag(
    text: String,
    color: Color? = null,
    background: Color? = null,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    val fg = color ?: p.muted
    val bg = background ?: p.raised
    Box(
        modifier = modifier
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 4.dp),
    ) {
        Text(
            text = text,
            color = fg,
            fontSize = 11.sp,
            fontWeight = FontWeight.SemiBold,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/** Status label with a leading dot, e.g. 「● 録画中」. */
@Composable
fun StatusPill(
    text: String,
    tone: Tone,
    modifier: Modifier = Modifier,
    dot: Boolean = true,
    onClick: (() -> Unit)? = null,
) {
    val p = LocalNccPalette.current
    Row(
        modifier = modifier
            .clip(RoundedCornerShape(999.dp))
            .background(p.toneSoft(tone))
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(horizontal = 10.dp, vertical = 5.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        if (dot) Box(Modifier.size(7.dp).clip(CircleShape).background(p.toneColor(tone)))
        Text(text, color = p.toneColor(tone), fontSize = 12.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
fun StatusDot(
    online: Boolean,
    label: String? = null,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    Row(
        modifier = modifier,
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Box(
            modifier = Modifier
                .size(8.dp)
                .clip(CircleShape)
                .background(if (online) p.ok else p.faint),
        )
        if (label != null) {
            Text(
                text = label,
                style = MaterialTheme.typography.labelMedium,
                color = if (online) p.text else p.muted,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

// ── Values ─────────────────────────────────────────────────────────────────

@Composable
fun KeyValueRow(
    label: String,
    value: String,
    mono: Boolean = false,
    valueColor: Color? = null,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    Row(
        modifier = modifier.fillMaxWidth().padding(vertical = 1.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.bodySmall,
            color = p.muted,
            modifier = Modifier.padding(end = 12.dp),
        )
        Text(
            text = value,
            style = MaterialTheme.typography.bodyMedium.copy(
                fontFamily = if (mono) FontFamily.Monospace else FontFamily.Default,
                fontWeight = FontWeight.SemiBold,
                fontFeatureSettings = "tnum",
            ),
            color = valueColor ?: p.text,
            textAlign = TextAlign.End,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/** Big number with a caption above and an optional unit. */
@Composable
fun MetricBlock(
    label: String,
    value: String,
    modifier: Modifier = Modifier,
    unit: String? = null,
    color: Color? = null,
    size: Int = 26,
    caption: String? = null,
) {
    val p = LocalNccPalette.current
    Column(modifier = modifier, verticalArrangement = Arrangement.spacedBy(1.dp)) {
        Text(label, color = p.muted, style = MaterialTheme.typography.labelSmall, maxLines = 1)
        Row(verticalAlignment = Alignment.Bottom) {
            Text(
                value,
                color = color ?: p.text,
                fontSize = size.sp,
                fontWeight = FontWeight.Bold,
                letterSpacing = (-0.5).sp,
                style = MaterialTheme.typography.headlineMedium.copy(fontSize = size.sp),
                maxLines = 1,
            )
            if (unit != null) {
                Text(
                    unit,
                    color = p.muted,
                    fontSize = (size * 0.46f).coerceAtLeast(11f).sp,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.padding(start = 3.dp, bottom = (size * 0.12f).dp),
                )
            }
        }
        if (caption != null) Text(caption, color = p.muted, style = MaterialTheme.typography.labelSmall, maxLines = 2)
    }
}

/** Rounded progress bar (Compose's LinearProgressIndicator draws a gap marker we do not want). */
@Composable
fun NccProgressBar(
    fraction: Float,
    modifier: Modifier = Modifier,
    color: Color? = null,
    track: Color? = null,
    height: Dp = 6.dp,
) {
    val p = LocalNccPalette.current
    val fg = color ?: p.accent
    val bg = track ?: p.raised.let { if (p.isDark) p.line else Color(0xFFE8EBF0) }
    Canvas(modifier = modifier.fillMaxWidth().height(height)) {
        val r = CornerRadius(size.height / 2, size.height / 2)
        drawRoundRect(bg, cornerRadius = r)
        val w = size.width * fraction.coerceIn(0f, 1f)
        if (w > 0f) drawRoundRect(fg, size = Size(w.coerceAtLeast(size.height), size.height), cornerRadius = r)
    }
}

// ── Rows and controls ──────────────────────────────────────────────────────

/** Tappable list row: icon, title, detail and something at the end (status or chevron). */
@Composable
fun ListRow(
    title: String,
    modifier: Modifier = Modifier,
    subtitle: String? = null,
    icon: ImageVector? = null,
    iconTone: Tone = Tone.NEUTRAL,
    overline: String? = null,
    enabled: Boolean = true,
    showChevron: Boolean = true,
    trailing: (@Composable RowScope.() -> Unit)? = null,
    onClick: (() -> Unit)? = null,
) {
    val p = LocalNccPalette.current
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .then(if (onClick != null) Modifier.clickable(enabled = enabled, onClick = onClick) else Modifier)
            .padding(horizontal = 6.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (icon != null) IconBadge(icon, tone = iconTone, size = 36.dp)
        Column(modifier = Modifier.weight(1f)) {
            if (overline != null) Text(overline, color = p.muted, style = MaterialTheme.typography.labelSmall, maxLines = 1)
            Text(
                title,
                color = if (enabled) p.text else p.muted,
                style = MaterialTheme.typography.titleSmall,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (!subtitle.isNullOrBlank()) {
                Text(subtitle, color = p.muted, style = MaterialTheme.typography.bodySmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        trailing?.invoke(this)
        if (onClick != null && showChevron) {
            Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, contentDescription = null, tint = p.faint, modifier = Modifier.size(20.dp))
        }
    }
}

@Composable
fun SwitchRow(
    title: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
    description: String? = null,
    enabled: Boolean = true,
) {
    val p = LocalNccPalette.current
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .clickable(enabled = enabled) { onCheckedChange(!checked) }
            .padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Column(Modifier.weight(1f)) {
            Text(title, color = if (enabled) p.text else p.muted, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
            if (!description.isNullOrBlank()) Text(description, color = p.muted, style = MaterialTheme.typography.bodySmall)
        }
        Switch(
            checked = checked,
            onCheckedChange = onCheckedChange,
            enabled = enabled,
            colors = SwitchDefaults.colors(checkedTrackColor = p.accent, checkedThumbColor = p.onAccent),
        )
    }
}

/** Small heading inside a card. */
@Composable
fun FieldLabel(text: String, modifier: Modifier = Modifier, trailing: String? = null) {
    val p = LocalNccPalette.current
    Row(modifier = modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(text, color = p.text, style = MaterialTheme.typography.labelLarge, modifier = Modifier.weight(1f))
        if (trailing != null) Text(trailing, color = p.muted, style = MaterialTheme.typography.labelSmall)
    }
}

/**
 * Pill-shaped segmented control. Kept under its old name so every screen picks up the new look.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun SectionTabs(
    tabs: List<Pair<String, String>>,
    selectedId: String,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    fill: Boolean = false,
) {
    val p = LocalNccPalette.current
    val container = if (p.isDark) p.raised else Color(0xFFECEEF2)
    if (fill) {
        Row(
            modifier = modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(12.dp))
                .background(container)
                .padding(3.dp),
            horizontalArrangement = Arrangement.spacedBy(3.dp),
        ) {
            tabs.forEach { (id, label) -> SegmentChip(label, id == selectedId, enabled, Modifier.weight(1f)) { onSelect(id) } }
        }
    } else {
        FlowRow(
            modifier = modifier,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            tabs.forEach { (id, label) ->
                val selected = id == selectedId
                val bg by animateColorAsState(if (selected) p.accent else container, label = "seg")
                Box(
                    modifier = Modifier
                        .clip(RoundedCornerShape(999.dp))
                        .background(bg)
                        .clickable(enabled = enabled) { onSelect(id) }
                        .padding(horizontal = 14.dp, vertical = 7.dp),
                ) {
                    Text(
                        text = label,
                        color = if (selected) p.onAccent else if (enabled) p.text else p.faint,
                        fontSize = 13.sp,
                        fontWeight = if (selected) FontWeight.Bold else FontWeight.Medium,
                        maxLines = 1,
                    )
                }
            }
        }
    }
}

@Composable
private fun SegmentChip(label: String, selected: Boolean, enabled: Boolean, modifier: Modifier, onClick: () -> Unit) {
    val p = LocalNccPalette.current
    val bg by animateColorAsState(if (selected) p.paper else Color.Transparent, label = "segchip")
    Box(
        modifier = modifier
            .then(if (selected) Modifier.shadow(1.dp, RoundedCornerShape(10.dp)) else Modifier)
            .clip(RoundedCornerShape(10.dp))
            .background(bg)
            .clickable(enabled = enabled, onClick = onClick)
            .padding(vertical = 8.dp, horizontal = 6.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            label,
            color = if (selected) p.accent else if (enabled) p.muted else p.faint,
            fontSize = 13.sp,
            fontWeight = if (selected) FontWeight.Bold else FontWeight.Medium,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/** Selectable card for choices that need an explanation (e.g. the uplink mode). */
@Composable
fun ChoiceCard(
    title: String,
    description: String,
    selected: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    icon: ImageVector? = null,
    enabled: Boolean = true,
) {
    val p = LocalNccPalette.current
    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = RoundedCornerShape(14.dp),
        color = if (selected) p.soft else p.paper,
        border = BorderStroke(if (selected) 1.5.dp else 1.dp, if (selected) p.accent else p.line),
        onClick = onClick,
        enabled = enabled,
    ) {
        Row(
            modifier = Modifier.padding(12.dp),
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (icon != null) Icon(icon, contentDescription = null, tint = if (selected) p.accent else p.muted, modifier = Modifier.size(20.dp))
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(title, color = if (selected) p.accent else p.text, style = MaterialTheme.typography.titleSmall)
                Text(description, color = p.muted, style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}

@Composable
fun ConfirmDialog(
    title: String,
    message: String,
    confirmLabel: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
    destructive: Boolean = false,
) {
    val p = LocalNccPalette.current
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = p.paper,
        title = { Text(title, style = MaterialTheme.typography.titleLarge) },
        text = { Text(message, style = MaterialTheme.typography.bodyMedium, color = p.text) },
        confirmButton = {
            TextButton(onClick = { onConfirm(); onDismiss() }) {
                Text(confirmLabel, color = if (destructive) p.danger else p.accent, fontWeight = FontWeight.Bold)
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("キャンセル", color = p.muted) } },
    )
}

// ── Brand ──────────────────────────────────────────────────────────────────

@Composable
fun BrandMark(size: Dp = 30.dp) {
    val p = LocalNccPalette.current
    Box(
        modifier = Modifier
            .size(size)
            .clip(RoundedCornerShape(size * 0.3f))
            .background(p.accent),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = "N",
            color = p.onAccent,
            fontWeight = FontWeight.Black,
            fontSize = (size.value * 0.54f).sp,
        )
    }
}

// ── Japanese licence plate ─────────────────────────────────────────────────

/**
 * Draws a Japanese licence plate (330 x 165 mm, 2:1) like the Web UI's `.plate-badge`.
 */
@Composable
fun JapanesePlateBadge(
    reading: PlateReading,
    matched: Boolean = false,
    danger: Boolean = false,
    width: Dp = 136.dp,
    onClick: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    val scale = width.value / 136f
    val borderColor = when {
        danger -> p.danger
        matched -> p.plateHit
        else -> p.plateBadgeBorder
    }
    val borderWidth = if (matched || danger) (2.5f * scale).coerceAtLeast(2f).dp else (1.8f * scale).coerceAtLeast(1.5f).dp
    val shape = RoundedCornerShape((7f * scale).dp)

    Box(
        modifier = modifier
            .width(width)
            .aspectRatio(2f)
            .then(
                if (matched || danger) {
                    Modifier.shadow(elevation = (4f * scale).dp, shape = shape, ambientColor = borderColor, spotColor = borderColor)
                } else {
                    Modifier
                },
            )
            .clip(shape)
            .background(p.plateBadgeBg)
            .border(borderWidth, borderColor, shape)
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(horizontal = (7f * scale).dp, vertical = (3f * scale).dp),
    ) {
        Canvas(modifier = Modifier.fillMaxSize()) {
            val boltR = size.width * 0.022f
            val boltY = size.height * 0.16f
            val boltColor = p.plateBadgeText.copy(alpha = 0.22f)
            drawCircle(color = boltColor, radius = boltR, center = Offset(size.width * 0.14f, boltY))
            drawCircle(color = boltColor, radius = boltR, center = Offset(size.width * 0.86f, boltY))
        }

        Column(
            modifier = Modifier.fillMaxSize(),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.SpaceBetween,
        ) {
            Row(
                modifier = Modifier.padding(top = (1f * scale).dp),
                horizontalArrangement = Arrangement.spacedBy((4f * scale).dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = reading.region.ifBlank { "？？" },
                    color = if (reading.region.isBlank()) p.plateBadgeText.copy(alpha = 0.28f) else p.plateBadgeText,
                    fontSize = (13f * scale).sp,
                    fontWeight = FontWeight.Bold,
                    maxLines = 1,
                )
                Text(
                    text = reading.classNum.ifBlank { "？？？" },
                    color = if (reading.classNum.isBlank()) p.plateBadgeText.copy(alpha = 0.28f) else p.plateBadgeText,
                    fontSize = (13f * scale).sp,
                    fontWeight = FontWeight.Bold,
                    maxLines = 1,
                )
            }
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = reading.kana.ifBlank { "？" },
                    color = if (reading.kana.isBlank()) p.plateBadgeText.copy(alpha = 0.28f) else p.plateBadgeText,
                    fontSize = (14.5f * scale).sp,
                    fontWeight = FontWeight.Bold,
                    modifier = Modifier.padding(start = (2f * scale).dp, end = (4f * scale).dp),
                )
                Box(modifier = Modifier.weight(1f), contentAlignment = Alignment.Center) {
                    val serial = Formatters.plateSerial(reading.number).ifBlank { "・・・・" }
                    Text(
                        text = serial,
                        color = p.plateBadgeText,
                        fontSize = (31f * scale).sp,
                        fontWeight = FontWeight.Bold,
                        letterSpacing = (0.5f * scale).sp,
                        maxLines = 1,
                    )
                }
            }
        }
    }
}
