package com.np1control.app.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.unit.sp
import com.np1control.app.data.AppThemeMode

/**
 * Exact design tokens mirrored from frontend/src/style.css and frontend/src/plate-recognition.css.
 */
@Immutable
data class NccPalette(
    val isDark: Boolean,
    val bg: Color,
    val paper: Color,
    val side: Color,
    val line: Color,
    val lineStrong: Color,
    val text: Color,
    val muted: Color,
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
    bg = Color(0xFFF4F5F7),
    paper = Color(0xFFFFFFFF),
    side = Color(0xFFF4F5F7),
    line = Color(0xFFE3E6EB),
    lineStrong = Color(0xFFD3D8E0),
    text = Color(0xFF202630),
    muted = Color(0xFF667080),
    accent = Color(0xFF2F5BD8),
    accentStrong = Color(0xFF2549B4),
    onAccent = Color(0xFFFFFFFF),
    soft = Color(0xFFEDF1FD),
    danger = Color(0xFFB42318),
    dangerSoft = Color(0xFFFDECEA),
    ok = Color(0xFF1A7F55),
    okSoft = Color(0xFFE6F5EE),
    warn = Color(0xFF8A5B10),
    warnSoft = Color(0xFFFFF6E3),
    uploadOrange = Color(0xFFD97706),
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
    bg = Color(0xFF111318),
    paper = Color(0xFF1B1F26),
    side = Color(0xFF111318),
    line = Color(0xFF2D333C),
    lineStrong = Color(0xFF3B424D),
    text = Color(0xFFE6E9EE),
    muted = Color(0xFFA2ACBB),
    accent = Color(0xFF8CABFF),
    accentStrong = Color(0xFFA8C0FF),
    onAccent = Color(0xFF0F1830),
    soft = Color(0xFF222C47),
    danger = Color(0xFFF09991),
    dangerSoft = Color(0xFF3A2220),
    ok = Color(0xFF6FCF9F),
    okSoft = Color(0xFF1C3327),
    warn = Color(0xFFE5C98C),
    warnSoft = Color(0xFF302B20),
    uploadOrange = Color(0xFFF59E42),
    plateHit = Color(0xFFFF8A4C),
    plateHitLine = Color(0xFFFFD9C2),
    plateHitSoft = Color(0xFF3D2A20),
    plateHitBorder = Color(0xFF6D4330),
    plateBadgeBg = Color(0xFFE9EDE5),
    plateBadgeText = Color(0xFF1F3A2C),
    plateBadgeBorder = Color(0xFF3D5A46),
)

val LocalNccPalette = staticCompositionLocalOf { DarkNccPalette }

private val NccTypography = Typography(
    headlineSmall = TextStyle(fontSize = 20.sp, fontWeight = FontWeight.SemiBold, letterSpacing = (-0.3).sp),
    titleLarge = TextStyle(fontSize = 18.sp, fontWeight = FontWeight.SemiBold),
    titleMedium = TextStyle(fontSize = 15.sp, fontWeight = FontWeight.SemiBold),
    titleSmall = TextStyle(fontSize = 13.sp, fontWeight = FontWeight.SemiBold),
    bodyMedium = TextStyle(fontSize = 13.sp, lineHeight = 19.sp),
    bodySmall = TextStyle(fontSize = 12.sp, lineHeight = 17.sp),
    labelMedium = TextStyle(fontSize = 11.sp, fontWeight = FontWeight.SemiBold),
    labelSmall = TextStyle(fontSize = 10.sp, fontWeight = FontWeight.Medium),
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
    val colorScheme = if (dark) {
        darkColorScheme(
            primary = palette.accent,
            onPrimary = palette.onAccent,
            primaryContainer = palette.soft,
            onPrimaryContainer = palette.accent,
            secondary = palette.muted,
            background = palette.bg,
            onBackground = palette.text,
            surface = palette.paper,
            onSurface = palette.text,
            surfaceVariant = palette.side,
            onSurfaceVariant = palette.muted,
            outline = palette.lineStrong,
            outlineVariant = palette.line,
            error = palette.danger,
            errorContainer = palette.dangerSoft,
        )
    } else {
        lightColorScheme(
            primary = palette.accent,
            onPrimary = palette.onAccent,
            primaryContainer = palette.soft,
            onPrimaryContainer = palette.accent,
            secondary = palette.muted,
            background = palette.bg,
            onBackground = palette.text,
            surface = palette.paper,
            onSurface = palette.text,
            surfaceVariant = palette.side,
            onSurfaceVariant = palette.muted,
            outline = palette.lineStrong,
            outlineVariant = palette.line,
            error = palette.danger,
            errorContainer = palette.dangerSoft,
        )
    }

    CompositionLocalProvider(LocalNccPalette provides palette) {
        MaterialTheme(
            colorScheme = colorScheme,
            typography = NccTypography,
            content = content,
        )
    }
}
