package com.np1control.app

import android.os.Bundle
import android.view.WindowManager
import android.graphics.Color
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.isSystemInDarkTheme
import com.np1control.app.data.AppThemeMode
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import com.np1control.app.data.AppPreferences
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.PlateRealtimeMonitor
import com.np1control.app.data.PlateRecordsResponse
import com.np1control.app.ui.AppShell
import com.np1control.app.ui.theme.NccTheme

class MainActivity : ComponentActivity() {
    private lateinit var preferences: AppPreferences
    private lateinit var apiClient: NccApiClient
    private lateinit var plateMonitor: PlateRealtimeMonitor

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        preferences = AppPreferences(applicationContext)
        apiClient = NccApiClient { preferences.config.value }
        plateMonitor = PlateRealtimeMonitor(
            fetchRecords = { from, to, matchedOnly, limit ->
                val matchedParam = if (matchedOnly) "&matched=true" else ""
                apiClient.get<PlateRecordsResponse>("/plate/records?from=$from&to=$to$matchedParam&limit=$limit")
            },
            configProvider = { preferences.config.value },
        )

        enableEdgeToEdge()
        setContent {
            val config by preferences.config.collectAsState()
            val systemDark = isSystemInDarkTheme()
            val dark = when (config.themeMode) {
                AppThemeMode.DARK -> true
                AppThemeMode.LIGHT -> false
                AppThemeMode.SYSTEM -> systemDark
            }
            LaunchedEffect(dark) {
                val style = if (dark) SystemBarStyle.dark(Color.TRANSPARENT) else SystemBarStyle.light(Color.TRANSPARENT, Color.TRANSPARENT)
                enableEdgeToEdge(statusBarStyle = style, navigationBarStyle = style)
            }

            LaunchedEffect(config.keepScreenOn) {
                if (config.keepScreenOn) {
                    window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                } else {
                    window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                }
            }

            NccTheme(themeMode = config.themeMode) {
                AppShell(
                    preferences = preferences,
                    apiClient = apiClient,
                    plateMonitor = plateMonitor,
                )
            }
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        if (::plateMonitor.isInitialized) {
            plateMonitor.stop()
        }
    }
}
