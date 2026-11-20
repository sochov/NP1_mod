package com.np1control.app.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.AppThemeMode

/**
 * Design tokens. The colours follow the Web UI (frontend/src/style.css): neutral grey surfaces,
 * cobalt #2f5bd8 as the accent, blue for distance and orange for time.
 */
@Immutable
data class NccPalette(
    val isDark: Boolean,
    val bg: Color,
    val paper: Color,
    val raised: Color,
    val side: Color,
    val line: Color,
    val lineStrong: Color,
    val text: Color,
    val muted: Color,
    val faint: Color,
    val accent: Color,
    val accentStrong: Color,
    val onAccent: Color,
    val soft: Color,
    val danger: Color,
    val dangerSoft: Color,
    val ok: Color,
    val okSoft: Color,
    val warn: Color,
    val warnSoft: Color,
    val uploadOrange: Color,
    val uploadSoft: Color,
    val glass: Color,
    val scrim: Color,
    val plateHit: Color,
    val plateHitLine: Color,
    val plateHitSoft: Color,
    val plateHitBorder: Color,
    val plateBadgeBg: Color,
    val plateBadgeText: Color,
    val plateBadgeBorder: Color,
)

val LightNccPalette = NccPalette(
    isDark = false,
    bg = Color(0xFFF2F3F6),
    paper = Color(0xFFFFFFFF),
    raised = Color(0xFFF7F8FA),
    side = Color(0xFFF7F8FA),
    line = Color(0xFFE4E7EC),
    lineStrong = Color(0xFFD0D5DD),
    text = Color(0xFF1C2230),
    muted = Color(0xFF636C7C),
    faint = Color(0xFF98A1B0),
    accent = Color(0xFF2F5BD8),
    accentStrong = Color(0xFF2549B4),
    onAccent = Color(0xFFFFFFFF),
    soft = Color(0xFFEBF0FD),
    danger = Color(0xFFB42318),
    dangerSoft = Color(0xFFFDECEA),
    ok = Color(0xFF16794F),
    okSoft = Color(0xFFE4F4EC),
    warn = Color(0xFF8A5B10),
    warnSoft = Color(0xFFFFF4DE),
    uploadOrange = Color(0xFFD97706),
    uploadSoft = Color(0xFFFFF1DF),
    glass = Color(0xEBFFFFFF),
    scrim = Color(0x66101418),
    plateHit = Color(0xFFE8590C),
    plateHitLine = Color(0xFF8F2D14),
    plateHitSoft = Color(0xFFFFF1E8),
    plateHitBorder = Color(0xFFF3C4A6),
    plateBadgeBg = Color(0xFFFBFBF8),
    plateBadgeText = Color(0xFF1F3A2C),
    plateBadgeBorder = Color(0xFF3D5A46),
)

val DarkNccPalette = NccPalette(
    isDark = true,
    bg = Color(0xFF0F1115),
    paper = Color(0xFF191C23),
    raised = Color(0xFF20242C),
    side = Color(0xFF14171C),
    line = Color(0xFF2A2F38),
    lineStrong = Color(0xFF3A404B),
    text = Color(0xFFE8EBF0),
    muted = Color(0xFFA0A9B8),
    faint = Color(0xFF6E7787),
    accent = Color(0xFF8CABFF),
    accentStrong = Color(0xFFA8C0FF),
    onAccent = Color(0xFF0F1830),
    soft = Color(0xFF212B45),
    danger = Color(0xFFF09991),
    dangerSoft = Color(0xFF3A2220),
    ok = Color(0xFF6FCF9F),
    okSoft = Color(0xFF1A3126),
    warn = Color(0xFFE5C98C),
    warnSoft = Color(0xFF302A1E),
    uploadOrange = Color(0xFFF59E42),
    uploadSoft = Color(0xFF33281A),
    glass = Color(0xE6191C23),
    scrim = Color(0x99000000),
    plateHit = Color(0xFFFF8A4C),
    plateHitLine = Color(0xFFFFD9C2),
    plateHitSoft = Color(0xFF3D2A20),
    plateHitBorder = Color(0xFF6D4330),
    plateBadgeBg = Color(0xFFE9EDE5),
    plateBadgeText = Color(0xFF1F3A2C),
    plateBadgeBorder = Color(0xFF3D5A46),
)

val LocalNccPalette = staticCompositionLocalOf { DarkNccPalette }

/** Tabular figures keep changing numbers (speeds, distances, clocks) from jittering. */
val Tabular = TextStyle(fontFeatureSettings = "tnum")

private val NccTypography = Typography(
    displaySmall = TextStyle(fontSize = 30.sp, fontWeight = FontWeight.Bold, letterSpacing = (-0.5).sp, fontFeatureSettings = "tnum"),
    headlineMedium = TextStyle(fontSize = 24.sp, fontWeight = FontWeight.Bold, letterSpacing = (-0.4).sp, fontFeatureSettings = "tnum"),
    headlineSmall = TextStyle(fontSize = 20.sp, fontWeight = FontWeight.Bold, letterSpacing = (-0.3).sp),
    titleLarge = TextStyle(fontSize = 18.sp, fontWeight = FontWeight.Bold),
    titleMedium = TextStyle(fontSize = 15.sp, fontWeight = FontWeight.Bold),
    titleSmall = TextStyle(fontSize = 13.sp, fontWeight = FontWeight.SemiBold),
    bodyLarge = TextStyle(fontSize = 15.sp, lineHeight = 22.sp),
    bodyMedium = TextStyle(fontSize = 13.sp, lineHeight = 20.sp),
    bodySmall = TextStyle(fontSize = 12.sp, lineHeight = 18.sp),
    labelLarge = TextStyle(fontSize = 14.sp, fontWeight = FontWeight.SemiBold),
    labelMedium = TextStyle(fontSize = 12.sp, fontWeight = FontWeight.SemiBold),
    labelSmall = TextStyle(fontSize = 11.sp, fontWeight = FontWeight.Medium),
)

private val NccShapes = Shapes(
    extraSmall = RoundedCornerShape(8.dp),
    small = RoundedCornerShape(10.dp),
    medium = RoundedCornerShape(14.dp),
    large = RoundedCornerShape(20.dp),
    extraLarge = RoundedCornerShape(28.dp),
)

@Composable
fun NccTheme(
    themeMode: AppThemeMode,
    content: @Composable () -> Unit,
) {
    val dark = when (themeMode) {
        AppThemeMode.DARK -> true
        AppThemeMode.LIGHT -> false
        AppThemeMode.SYSTEM -> isSystemInDarkTheme()
    }
    val palette = if (dark) DarkNccPalette else LightNccPalette
    val base = if (dark) darkColorScheme() else lightColorScheme()
    val colorScheme = base.copy(
        primary = palette.accent,
        onPrimary = palette.onAccent,
        primaryContainer = palette.soft,
        onPrimaryContainer = palette.accent,
        secondary = palette.muted,
        secondaryContainer = palette.soft,
        onSecondaryContainer = palette.accent,
        background = palette.bg,
        onBackground = palette.text,
        surface = palette.paper,
        onSurface = palette.text,
        surfaceVariant = palette.raised,
        onSurfaceVariant = palette.muted,
        surfaceContainerLowest = palette.paper,
        surfaceContainerLow = palette.paper,
        surfaceContainer = palette.paper,
        surfaceContainerHigh = palette.paper,
        surfaceContainerHighest = palette.raised,
        outline = palette.lineStrong,
        outlineVariant = palette.line,
        error = palette.danger,
        errorContainer = palette.dangerSoft,
        inverseSurface = if (dark) Color(0xFFE8EBF0) else Color(0xFF1C2230),
        inverseOnSurface = if (dark) Color(0xFF1C2230) else Color(0xFFF2F3F6),
    )

    CompositionLocalProvider(LocalNccPalette provides palette) {
        MaterialTheme(
            colorScheme = colorScheme,
            typography = NccTypography,
            shapes = NccShapes,
            content = content,
        )
    }
}
