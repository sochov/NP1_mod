package com.np1control.app.data

import android.content.Context
import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

enum class ConnectionMode(val id: String, val label: String) {
    AUTO("auto", "自動（ホットスポット優先 → Tailscale）"),
    HOTSPOT("hotspot", "NP1ホットスポット (192.168.226.1)"),
    TAILSCALE("tailscale", "Tailscale IP"),
    CUSTOM("custom", "カスタムURL");

    companion object {
        fun fromId(id: String?): ConnectionMode = entries.firstOrNull { it.id == id } ?: AUTO
    }
}

enum class PlatePopupFilter(val id: String, val label: String, val shortLabel: String) {
    MATCHED_ONLY("matched", "条件一致のみ", "条件一致"),
    ALL("all", "すべてのナンバー", "すべて"),
    OFF("off", "ポップアップOFF", "OFF");

    companion object {
        fun fromId(id: String?): PlatePopupFilter = entries.firstOrNull { it.id == id } ?: MATCHED_ONLY
    }
}

enum class AppThemeMode(val id: String, val label: String) {
    SYSTEM("system", "システムに合わせる"),
    LIGHT("light", "ライト"),
    DARK("dark", "ダーク");

    companion object {
        fun fromId(id: String?): AppThemeMode = entries.firstOrNull { it.id == id } ?: SYSTEM
    }
}

data class AppConfig(
    val connectionMode: ConnectionMode = ConnectionMode.AUTO,
    val hotspotUrl: String = DEFAULT_HOTSPOT_URL,
    val tailscaleHost: String = "",
    val customUrl: String = "",
    val popupFilter: PlatePopupFilter = PlatePopupFilter.MATCHED_ONLY,
    val popupPollIntervalMs: Long = 1000L,
    val popupDisplaySeconds: Int = 8,
    val popupSoundEnabled: Boolean = true,
    val popupShowImage: Boolean = true,
    val themeMode: AppThemeMode = AppThemeMode.SYSTEM,
    val keepScreenOn: Boolean = true,
    val dashboardOverview: Boolean = true,
) {
    fun normalizedHotspotUrl(): String = normalizeBaseUrl(hotspotUrl.ifBlank { DEFAULT_HOTSPOT_URL })

    fun normalizedTailscaleUrl(): String? {
        val trimmed = tailscaleHost.trim()
        if (trimmed.isEmpty()) return null
        return normalizeBaseUrl(trimmed)
    }

    fun normalizedCustomUrl(): String? {
        val trimmed = customUrl.trim()
        if (trimmed.isEmpty()) return null
        return normalizeBaseUrl(trimmed)
    }

    fun candidateUrls(): List<String> = when (connectionMode) {
        ConnectionMode.HOTSPOT -> listOf(normalizedHotspotUrl())
        ConnectionMode.TAILSCALE -> listOfNotNull(normalizedTailscaleUrl(), normalizedHotspotUrl()).distinct()
        ConnectionMode.CUSTOM -> listOfNotNull(normalizedCustomUrl(), normalizedHotspotUrl()).distinct()
        ConnectionMode.AUTO -> listOfNotNull(normalizedHotspotUrl(), normalizedTailscaleUrl(), normalizedCustomUrl()).distinct()
    }

    companion object {
        const val DEFAULT_HOTSPOT_URL = "http://192.168.226.1"

        fun normalizeBaseUrl(raw: String): String {
            val trimmed = raw.trim().trimEnd('/')
            if (trimmed.isEmpty()) return DEFAULT_HOTSPOT_URL
            return if (trimmed.startsWith("http://", ignoreCase = true) ||
                trimmed.startsWith("https://", ignoreCase = true)
            ) {
                trimmed
            } else {
                "http://$trimmed"
            }
        }
    }
}

class AppPreferences(context: Context) {
    private val prefs: SharedPreferences =
        context.getSharedPreferences("np1_control_prefs", Context.MODE_PRIVATE)

    private val _config = MutableStateFlow(load())
    val config: StateFlow<AppConfig> = _config.asStateFlow()

    private fun load(): AppConfig = AppConfig(
        connectionMode = ConnectionMode.fromId(prefs.getString("connection_mode", ConnectionMode.AUTO.id)),
        hotspotUrl = prefs.getString("hotspot_url", AppConfig.DEFAULT_HOTSPOT_URL) ?: AppConfig.DEFAULT_HOTSPOT_URL,
        tailscaleHost = prefs.getString("tailscale_host", "") ?: "",
        customUrl = prefs.getString("custom_url", "") ?: "",
        popupFilter = PlatePopupFilter.fromId(prefs.getString("popup_filter", PlatePopupFilter.MATCHED_ONLY.id)),
        popupPollIntervalMs = prefs.getLong("popup_poll_ms", 1000L).coerceIn(500L, 10000L),
        popupDisplaySeconds = prefs.getInt("popup_display_sec", 8).coerceIn(0, 60),
        popupSoundEnabled = prefs.getBoolean("popup_sound", true),
        popupShowImage = prefs.getBoolean("popup_show_image", true),
        themeMode = AppThemeMode.fromId(prefs.getString("theme_mode", AppThemeMode.SYSTEM.id)),
        keepScreenOn = prefs.getBoolean("keep_screen_on", true),
        dashboardOverview = prefs.getBoolean("dashboard_overview", true),
    )

    fun update(transform: (AppConfig) -> AppConfig) {
        val next = transform(_config.value)
        _config.value = next
        prefs.edit()
            .putString("connection_mode", next.connectionMode.id)
            .putString("hotspot_url", next.hotspotUrl)
            .putString("tailscale_host", next.tailscaleHost)
            .putString("custom_url", next.customUrl)
            .putString("popup_filter", next.popupFilter.id)
            .putLong("popup_poll_ms", next.popupPollIntervalMs)
            .putInt("popup_display_sec", next.popupDisplaySeconds)
            .putBoolean("popup_sound", next.popupSoundEnabled)
            .putBoolean("popup_show_image", next.popupShowImage)
            .putString("theme_mode", next.themeMode.id)
            .putBoolean("keep_screen_on", next.keepScreenOn)
            .putBoolean("dashboard_overview", next.dashboardOverview)
            .apply()
    }
}
