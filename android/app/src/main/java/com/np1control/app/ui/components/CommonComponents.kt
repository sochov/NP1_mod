package com.np1control.app.ui.components

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.ErrorOutline
import androidx.compose.material.icons.filled.Info
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.Formatters
import com.np1control.app.data.PlateReading
import com.np1control.app.ui.theme.LocalNccPalette

@Composable
fun NccPanel(
    modifier: Modifier = Modifier,
    title: String? = null,
    subtitle: String? = null,
    icon: ImageVector? = null,
    headerEnd: (@Composable RowScope.() -> Unit)? = null,
    contentPadding: PaddingValues = PaddingValues(14.dp),
    content: @Composable ColumnScope.() -> Unit,
) {
    val p = LocalNccPalette.current
    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = RoundedCornerShape(14.dp),
        color = p.paper,
        border = BorderStroke(1.dp, p.line),
        shadowElevation = if (p.isDark) 1.dp else 2.dp,
    ) {
        Column(modifier = Modifier.fillMaxWidth()) {
            if (title != null || headerEnd != null) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 14.dp, vertical = 11.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    if (icon != null) {
                        Icon(
                            imageVector = icon,
                            contentDescription = null,
                            tint = p.accent,
                            modifier = Modifier.size(18.dp),
                        )
                    }
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
                        if (!subtitle.isNullOrBlank()) {
                            Text(
                                text = subtitle,
                                style = MaterialTheme.typography.bodySmall,
                                color = p.muted,
                                maxLines = 2,
                                overflow = TextOverflow.Ellipsis,
                            )
                        }
                    }
                    if (headerEnd != null) {
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(6.dp),
                            content = headerEnd,
                        )
                    }
                }
                HorizontalDivider(color = p.line, thickness = 1.dp)
            }
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(contentPadding),
                verticalArrangement = Arrangement.spacedBy(10.dp),
                content = content,
            )
        }
    }
}

enum class BannerTone { ERROR, SUCCESS, NEUTRAL, WARN }

@Composable
fun NccBanner(
    message: String,
    tone: BannerTone = BannerTone.NEUTRAL,
    onDismiss: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    val (bg, fg, border, icon) = when (tone) {
        BannerTone.ERROR -> listOf(p.dangerSoft, p.danger, p.danger.copy(alpha = 0.35f), Icons.Default.ErrorOutline)
        BannerTone.SUCCESS -> listOf(p.okSoft, p.ok, p.ok.copy(alpha = 0.35f), Icons.Default.Check)
        BannerTone.WARN -> listOf(p.warnSoft, p.warn, p.warn.copy(alpha = 0.35f), Icons.Default.ErrorOutline)
        BannerTone.NEUTRAL -> listOf(p.soft, p.muted, p.line, Icons.Default.Info)
    }
    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = RoundedCornerShape(10.dp),
        color = bg as Color,
        border = BorderStroke(1.dp, border as Color),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 9.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Icon(
                imageVector = icon as ImageVector,
                contentDescription = null,
                tint = fg as Color,
                modifier = Modifier.size(16.dp),
            )
            Text(
                text = message,
                color = if (tone == BannerTone.NEUTRAL) p.text else fg,
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.weight(1f),
            )
            if (onDismiss != null) {
                IconButton(onClick = onDismiss, modifier = Modifier.size(26.dp)) {
                    Icon(Icons.Default.Close, contentDescription = "閉じる", tint = p.muted, modifier = Modifier.size(15.dp))
                }
            }
        }
    }
}

@Composable
fun NccTag(
    text: String,
    color: Color? = null,
    background: Color? = null,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    val fg = color ?: p.muted
    val bg = background ?: p.bg
    Box(
        modifier = modifier
            .clip(RoundedCornerShape(6.dp))
            .background(bg)
            .border(1.dp, color?.copy(alpha = 0.35f) ?: p.line, RoundedCornerShape(6.dp))
            .padding(horizontal = 7.dp, vertical = 3.dp),
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
                .background(if (online) p.ok else p.muted),
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
        modifier = modifier.fillMaxWidth(),
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
            style = MaterialTheme.typography.bodySmall.copy(
                fontFamily = if (mono) FontFamily.Monospace else FontFamily.Default,
                fontWeight = FontWeight.Medium,
            ),
            color = valueColor ?: p.text,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
fun BrandMark(size: Dp = 30.dp) {
    val p = LocalNccPalette.current
    Box(
        modifier = Modifier
            .size(size)
            .clip(RoundedCornerShape(8.dp))
            .background(if (p.isDark) Color(0xFFF4F5F7) else Color(0xFF202630)),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = "N",
            color = if (p.isDark) Color(0xFF111318) else Color(0xFFFFFFFF),
            fontWeight = FontWeight.ExtraBold,
            fontSize = (size.value * 0.56f).sp,
        )
    }
}

/**
 * Draws a Japanese license plate (330 x 165 mm, 2:1 aspect ratio) matching the web UI's
 * `.plate-badge` styling in frontend/src/plate-recognition.css.
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
        // Two mounting bolts at top corners
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
            // Top row: Region + Classification number
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

            // Bottom row: Kana + 4-digit Serial
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

@Composable
fun SectionTabs(
    tabs: List<Pair<String, String>>,
    selectedId: String,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val p = LocalNccPalette.current
    Row(
        modifier = modifier
            .fillMaxWidth()
            .border(width = 0.dp, color = Color.Transparent),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        tabs.forEach { (id, label) ->
            val selected = id == selectedId
            Surface(
                shape = RoundedCornerShape(9.dp),
                color = if (selected) p.soft else p.paper,
                border = BorderStroke(1.dp, if (selected) p.accent else p.line),
                modifier = Modifier.clickable { onSelect(id) },
            ) {
                Text(
                    text = label,
                    color = if (selected) p.accent else p.muted,
                    fontSize = 12.sp,
                    fontWeight = if (selected) FontWeight.Bold else FontWeight.Medium,
                    modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp),
                    maxLines = 1,
                )
            }
        }
    }
}
