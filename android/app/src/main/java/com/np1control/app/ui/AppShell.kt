package com.np1control.app.ui

import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.tappableElement
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.CellTower
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Dashboard
import androidx.compose.material.icons.filled.Fullscreen
import androidx.compose.material.icons.filled.DirectionsCar
import androidx.compose.material.icons.filled.DocumentScanner
import androidx.compose.material.icons.filled.Map
import androidx.compose.material.icons.filled.Monitor
import androidx.compose.material.icons.filled.Movie
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.NotificationsActive
import androidx.compose.material.icons.filled.NotificationsOff
import androidx.compose.material.icons.filled.Router
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material.icons.filled.Videocam
import androidx.compose.material.icons.filled.ViewAgenda
import androidx.compose.material.icons.filled.Wifi
import androidx.compose.material.icons.outlined.Layers
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalView
import androidx.compose.runtime.DisposableEffect
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import android.app.Activity
import android.content.ContextWrapper
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.AppPreferences
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.PlatePopupFilter
import com.np1control.app.data.PlateRealtimeMonitor
import com.np1control.app.data.SystemSummary
import com.np1control.app.data.WebSettings
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.BrandMark
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.PlatePopupOverlay
import com.np1control.app.ui.screens.AppSettingsDialog
import com.np1control.app.ui.screens.CellularScreen
import com.np1control.app.ui.screens.DashboardScreen
import com.np1control.app.ui.screens.LandscapeDashboard
import com.np1control.app.ui.screens.landscapePaneWidth
import com.np1control.app.ui.screens.LiveCameraScreen
import com.np1control.app.ui.screens.NativeSettingsScreen
import com.np1control.app.ui.screens.PlatesScreen
import com.np1control.app.ui.screens.RecordingsScreen
import com.np1control.app.ui.screens.SystemScreen
import com.np1control.app.ui.screens.TailscaleScreen
import com.np1control.app.ui.screens.TripsScreen
import com.np1control.app.ui.screens.WifiApScreen
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

data class NavItem(
    val id: String,
    val label: String,
    val hint: String,
    val icon: ImageVector,
    val groupId: String,
)

data class NavGroup(
    val id: String,
    val label: String,
    val icon: ImageVector,
    val itemIds: List<String>,
)

private val allNavItems = listOf(
    NavItem("dashboard", "ダッシュボード", "走行・通信・本体の状態", Icons.Default.Dashboard, "home"),
    NavItem("internet", "4G", "回線・APN・通信量", Icons.Default.CellTower, "network"),
    NavItem("wifi", "Wi-Fi / AP", "Wi-Fi接続とホットスポット", Icons.Default.Wifi, "network"),
    NavItem("vpn", "Tailscale", "外出先からの接続", Icons.Default.Shield, "network"),
    NavItem("camera", "ライブ", "カメラのライブ映像", Icons.Default.Videocam, "drive"),
    NavItem("recordings", "ドラレコ再生", "録画を日時から再生", Icons.Default.Movie, "drive"),
    NavItem("trips", "走行履歴", "距離・経路・電波マップ", Icons.Default.Map, "drive"),
    NavItem("plates", "ナンバー", "ナンバー認識の記録と条件", Icons.Default.DocumentScanner, "drive"),
    NavItem("native", "純正設定", "NP1本体の設定とAlexa", Icons.Default.Tune, "manage"),
    NavItem("system", "システム", "本体・LED・バックアップなど", Icons.Default.Monitor, "manage"),
)

// Phone menu: the owner chose these four labels (not 「ホーム」) for the Web UI's mobile shell.
private val mobileNavGroups = listOf(
    NavGroup("home", "ダッシュボード", Icons.Default.Dashboard, listOf("dashboard")),
    NavGroup("network", "ネットワーク", Icons.Default.Router, listOf("internet", "wifi", "vpn")),
    NavGroup("drive", "ドラレコ", Icons.Default.DirectionsCar, listOf("camera", "recordings", "trips", "plates")),
    NavGroup("manage", "設定", Icons.Default.Settings, listOf("native", "system")),
)

/** Space the floating bars take, so pages can pad their content under them. */
data class ShellInsets(val top: Dp, val bottom: Dp, val start: Dp)

@Composable
fun AppShell(
    preferences: AppPreferences,
    apiClient: NccApiClient,
    plateMonitor: PlateRealtimeMonitor,
) {
    val p = LocalNccPalette.current
    val scope = rememberCoroutineScope()
    val config by preferences.config.collectAsState()
    val connection by apiClient.connectionState.collectAsState()
    val activePopups by plateMonitor.activePopups.collectAsState()

    val configuration = LocalConfiguration.current
    val isLandscape = configuration.screenWidthDp > configuration.screenHeightDp
    val isCompactLandscape = isLandscape && configuration.screenHeightDp < 540 // Echo Show 5 (960x480)
    val isWide = configuration.screenWidthDp >= 600 || isLandscape

    // Started in landscape → the full-screen landscape dashboard. Leaving it opens the normal app.
    var currentPage by rememberSaveable { mutableStateOf(if (isLandscape) "landscape" else "dashboard") }
    var systemSection by rememberSaveable { mutableStateOf("overview") }
    var recordingJumpIso by remember { mutableStateOf<String?>(null) }
    var plateHighlightId by remember { mutableStateOf<String?>(null) }
    var openMobileGroupId by remember { mutableStateOf<String?>(null) }
    var showAppSettings by remember { mutableStateOf(false) }
    var popupMenu by remember { mutableStateOf(false) }
    var landscapeMenu by remember { mutableStateOf(false) }

    var summary by remember { mutableStateOf<SystemSummary>(emptyMap()) }
    var webSettings by remember { mutableStateOf<WebSettings?>(null) }
    var globalError by remember { mutableStateOf("") }
    var everConnected by remember { mutableStateOf(false) }
    var failures by remember { mutableStateOf(0) }

    fun navigate(route: String) {
        openMobileGroupId = null
        if (route.startsWith("system/")) {
            currentPage = "system"
            systemSection = route.removePrefix("system/")
        } else {
            currentPage = route
            if (route == "system") systemSection = "overview"
        }
    }

    suspend fun loadSummary() {
        runCatching {
            apiClient.ensureSession(forceReprobe = !apiClient.connectionState.value.connected)
            summary = apiClient.get<SystemSummary>("/system/summary")
            if (webSettings == null) runCatching { webSettings = apiClient.get<WebSettings>("/settings") }
            globalError = ""
            failures = 0
            everConnected = true
        }.onFailure {
            failures++
            // One slow reply is common on 4G; report only a lasting failure.
            if (failures >= 2 || !everConnected) globalError = it.message ?: "NP1に接続できません"
        }
    }

    fun refreshSummary() {
        scope.launch { loadSummary() }
    }

    fun refreshWebSettings() {
        scope.launch { runCatching { webSettings = apiClient.get<WebSettings>("/settings") } }
    }

    // Boot: connect, follow the server's refresh events, poll as a fallback, run the plate monitor.
    LaunchedEffect(Unit) {
        loadSummary()
        plateMonitor.start(this)
        launch {
            apiClient.refreshEvents().collect {
                runCatching { summary = apiClient.get<SystemSummary>("/system/summary") }
            }
        }
        launch {
            while (isActive) {
                delay(if (apiClient.connectionState.value.connected) 15_000L else 6_000L)
                loadSummary()
            }
        }
    }

    // The landscape dashboard only exists in landscape; turned upright it shows the normal dashboard.
    val landscapeDash = currentPage == "landscape" && isLandscape
    val shellPage = if (currentPage == "landscape") "dashboard" else currentPage

    BackHandler(enabled = landscapeMenu || openMobileGroupId != null || shellPage != "dashboard") {
        when {
            landscapeMenu -> landscapeMenu = false
            openMobileGroupId != null -> openMobileGroupId = null
            else -> navigate("dashboard")
        }
    }

    ImmersiveMode(enabled = landscapeDash)

    val currentNavItem = allNavItems.firstOrNull { it.id == shellPage } ?: allNavItems.first()
    val mapStage = shellPage == "dashboard" && config.dashboardOverview

    val topBarHeight = if (isCompactLandscape) 52.dp else 60.dp
    val bottomBarHeight = if (isWide) 0.dp else 68.dp

    Box(modifier = Modifier.fillMaxSize().background(p.bg)) {
        if (landscapeDash) {
            LandscapeDashboard(
                summary = summary,
                webSettings = webSettings,
                apiClient = apiClient,
                connectionError = if (!connection.connected && globalError.isNotBlank()) globalError else null,
                onRetryConnection = { apiClient.resetSession(); refreshSummary() },
                onOpenMenu = { landscapeMenu = true },
                onOpenPlate = { recId -> plateHighlightId = recId; navigate("plates") },
            )
            if (landscapeMenu) {
                LandscapeMenu(
                    connected = connection.connected,
                    connectionLabel = if (connection.connected) "${connection.activeLabel}で接続中" else "未接続",
                    popupFilter = config.popupFilter,
                    onPopupFilter = { f -> preferences.update { it.copy(popupFilter = f) } },
                    onNavigate = { landscapeMenu = false; navigate(it) },
                    onOpenSettings = { landscapeMenu = false; showAppSettings = true },
                    onDismiss = { landscapeMenu = false },
                )
            }
        } else Row(modifier = Modifier.fillMaxSize()) {
            if (isWide) {
                NavRail(
                    compact = isCompactLandscape,
                    currentPage = shellPage,
                    connected = connection.connected,
                    connectionLabel = connection.activeLabel,
                    onNavigate = { navigate(it) },
                    onOpenSettings = { showAppSettings = true },
                )
            }
            Box(modifier = Modifier.weight(1f).fillMaxHeight()) {
                val insets = ShellInsets(
                    top = topBarHeight,
                    bottom = bottomBarHeight,
                    start = 0.dp,
                )
                // Page content (under the floating bars)
                Box(
                    modifier = Modifier
                        .fillMaxSize()
                        .then(
                            if (mapStage) Modifier
                            else Modifier
                                .windowInsetsPadding(WindowInsets.statusBars)
                                .padding(top = topBarHeight)
                                .then(if (isWide) Modifier.windowInsetsPadding(WindowInsets.navigationBars) else Modifier),
                        ),
                ) {
                    Column(Modifier.fillMaxSize()) {
                        if (!mapStage && !connection.connected && globalError.isNotBlank()) {
                            ConnectionBanner(
                                message = globalError,
                                firstTime = !everConnected,
                                onRetry = { apiClient.resetSession(); refreshSummary() },
                                onSettings = { showAppSettings = true },
                            )
                        }
                        Box(Modifier.weight(1f).fillMaxWidth()) {
                            PageContent(
                                page = shellPage,
                                summary = summary,
                                webSettings = webSettings,
                                apiClient = apiClient,
                                preferences = preferences,
                                plateMonitor = plateMonitor,
                                isWide = isWide,
                                isCompactLandscape = isCompactLandscape,
                                shellInsets = insets,
                                systemSection = systemSection,
                                recordingJumpIso = recordingJumpIso,
                                plateHighlightId = plateHighlightId,
                                connectionError = if (!connection.connected && globalError.isNotBlank()) globalError else null,
                                onRetryConnection = { apiClient.resetSession(); refreshSummary() },
                                onNavigate = { navigate(it) },
                                onOpenRecordingAt = { iso -> recordingJumpIso = iso; navigate("recordings") },
                                onRefreshSummary = { refreshSummary() },
                                onRefreshWebSettings = { refreshWebSettings() },
                                onOpenAppSettings = { showAppSettings = true },
                            )
                        }
                        if (!isWide) Spacer(Modifier.windowInsetsPadding(WindowInsets.navigationBars).height(bottomBarHeight))
                    }
                }

                // Floating top bar
                TopBar(
                    modifier = Modifier
                        .align(Alignment.TopCenter)
                        .windowInsetsPadding(WindowInsets.statusBars),
                    height = topBarHeight,
                    floating = mapStage,
                    title = currentNavItem.label,
                    showBrand = !isWide,
                    connected = connection.connected,
                    connectionLabel = if (connection.connected) connection.activeLabel else "未接続",
                    popupFilter = config.popupFilter,
                    popupMenu = popupMenu,
                    onPopupMenu = { popupMenu = it },
                    onPopupFilter = { f -> preferences.update { it.copy(popupFilter = f) } },
                    onTestPopup = { plateMonitor.emitTestPopup(it) },
                    showLayoutToggle = shellPage == "dashboard",
                    overview = config.dashboardOverview,
                    onToggleLayout = { preferences.update { it.copy(dashboardOverview = !it.dashboardOverview) } },
                    onOpenLandscape = if (isLandscape && shellPage == "dashboard") ({ navigate("landscape") }) else null,
                    onOpenSettings = { showAppSettings = true },
                )

                if (!isWide) {
                    BottomBar(
                        modifier = Modifier.align(Alignment.BottomCenter),
                        height = bottomBarHeight,
                        currentPage = shellPage,
                        openGroupId = openMobileGroupId,
                        onGroup = { grp ->
                            if (grp.itemIds.size == 1) navigate(grp.itemIds.first())
                            else openMobileGroupId = if (openMobileGroupId == grp.id) null else grp.id
                        },
                    )
                }
            }
        }

        // Phone group menu sheet
        val openGroup = mobileNavGroups.firstOrNull { it.id == openMobileGroupId }
        AnimatedVisibility(
            visible = !isWide && openGroup != null,
            enter = fadeIn(),
            exit = fadeOut(),
        ) {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .background(p.scrim)
                    .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null) { openMobileGroupId = null },
            )
        }
        AnimatedVisibility(
            visible = !isWide && openGroup != null,
            enter = slideInVertically { it / 3 } + fadeIn(),
            exit = slideOutVertically { it / 3 } + fadeOut(),
            modifier = Modifier
                .align(Alignment.BottomCenter)
                .windowInsetsPadding(WindowInsets.navigationBars)
                .padding(bottom = bottomBarHeight + 6.dp),
        ) {
            val grp = openGroup ?: mobileNavGroups.first()
            GroupSheet(group = grp, currentPage = shellPage, onNavigate = { navigate(it) })
        }

        // Real-time plate popup (always on top)
        PlatePopupOverlay(
            events = activePopups,
            config = config,
            apiClient = apiClient,
            compactLandscape = isCompactLandscape || isWide,
            onDismiss = { plateMonitor.dismiss(it) },
            onDismissAll = { plateMonitor.dismissAll() },
            onOpenPlates = { recId ->
                plateHighlightId = recId
                plateMonitor.dismissAll()
                navigate("plates")
            },
            modifier = if (landscapeDash) {
                // Over the map, clear of the camera and the detection list on the right.
                Modifier
                    .align(Alignment.TopStart)
                    .padding(top = 6.dp, end = landscapePaneWidth(configuration.screenWidthDp.dp))
            } else {
                Modifier
                    .align(Alignment.TopCenter)
                    .windowInsetsPadding(WindowInsets.statusBars)
                    .padding(top = topBarHeight)
            },
        )
    }

    if (showAppSettings) {
        AppSettingsDialog(
            config = config,
            apiClient = apiClient,
            connected = connection.connected,
            activeLabel = connection.activeLabel,
            activeUrl = connection.activeBaseUrl,
            onUpdateConfig = { preferences.update(it) },
            onReconnect = {
                apiClient.resetSession()
                plateMonitor.resetBaseline()
                refreshSummary()
            },
            onTestPopup = { matched -> plateMonitor.emitTestPopup(matched) },
            onDismiss = { showAppSettings = false },
        )
    }
}

@Composable
private fun PageContent(
    page: String,
    summary: SystemSummary,
    webSettings: WebSettings?,
    apiClient: NccApiClient,
    preferences: AppPreferences,
    plateMonitor: PlateRealtimeMonitor,
    isWide: Boolean,
    isCompactLandscape: Boolean,
    shellInsets: ShellInsets,
    systemSection: String,
    recordingJumpIso: String?,
    plateHighlightId: String?,
    connectionError: String?,
    onRetryConnection: () -> Unit,
    onNavigate: (String) -> Unit,
    onOpenRecordingAt: (String) -> Unit,
    onRefreshSummary: () -> Unit,
    onRefreshWebSettings: () -> Unit,
    onOpenAppSettings: () -> Unit,
) {
    val config by preferences.config.collectAsState()
    when (page) {
        "dashboard" -> DashboardScreen(
            summary = summary,
            webSettings = webSettings,
            apiClient = apiClient,
            isWide = isWide,
            isCompactLandscape = isCompactLandscape,
            overviewMode = config.dashboardOverview,
            shellInsets = shellInsets,
            connectionError = connectionError,
            onRetryConnection = onRetryConnection,
            onOpenAppSettings = onOpenAppSettings,
            onNavigate = onNavigate,
            onWebSettingsChanged = onRefreshWebSettings,
        )
        "internet" -> CellularScreen(summary = summary, apiClient = apiClient, onRefreshSummary = onRefreshSummary)
        "wifi" -> WifiApScreen(summary = summary, apiClient = apiClient, onRefreshSummary = onRefreshSummary)
        "vpn" -> TailscaleScreen(
            summary = summary,
            apiClient = apiClient,
            onRefreshSummary = onRefreshSummary,
            onSaveAsTailscaleHost = { ip -> preferences.update { it.copy(tailscaleHost = ip) } },
            savedTailscaleHost = config.tailscaleHost,
        )
        "camera" -> LiveCameraScreen(apiClient = apiClient, isWide = isWide)
        "recordings" -> RecordingsScreen(
            apiClient = apiClient,
            initialTimestampIso = recordingJumpIso,
            isWide = isWide,
            mapThemes = webSettings?.mapThemes.orEmpty(),
        )
        "trips" -> TripsScreen(
            apiClient = apiClient,
            summary = summary,
            isWide = isWide,
            mapThemes = webSettings?.mapThemes.orEmpty(),
            onOpenRecordingAt = onOpenRecordingAt,
        )
        "plates" -> PlatesScreen(
            apiClient = apiClient,
            config = config,
            highlightRecordId = plateHighlightId,
            isWide = isWide,
            mapThemes = webSettings?.mapThemes.orEmpty(),
            onPopupFilterChanged = { filter -> preferences.update { it.copy(popupFilter = filter) } },
            onEmitTestPopup = { matched -> plateMonitor.emitTestPopup(matched) },
            onOpenRecordingAt = onOpenRecordingAt,
        )
        "native" -> NativeSettingsScreen(apiClient = apiClient)
        "system" -> SystemScreen(
            summary = summary,
            apiClient = apiClient,
            initialSection = systemSection,
            onRefreshSummary = onRefreshSummary,
            onOpenAppSettings = onOpenAppSettings,
            onWebSettingsChanged = onRefreshWebSettings,
        )
    }
}

@Composable
private fun ConnectionBanner(message: String, firstTime: Boolean, onRetry: () -> Unit, onSettings: () -> Unit) {
    Box(Modifier.padding(horizontal = 14.dp, vertical = 6.dp)) {
        NccBanner(
            message = if (firstTime) "NP1に接続できません。接続先の設定と、ホットスポットまたはTailscaleへの接続を確認してください。" else message,
            tone = BannerTone.WARN,
            action = {
                Row {
                    TextButton(onClick = onRetry) { Text("再接続") }
                    TextButton(onClick = onSettings) { Text("接続先") }
                }
            },
        )
    }
}

// ── Bars ───────────────────────────────────────────────────────────────────

@Composable
private fun TopBar(
    modifier: Modifier,
    height: Dp,
    floating: Boolean,
    title: String,
    showBrand: Boolean,
    connected: Boolean,
    connectionLabel: String,
    popupFilter: PlatePopupFilter,
    popupMenu: Boolean,
    onPopupMenu: (Boolean) -> Unit,
    onPopupFilter: (PlatePopupFilter) -> Unit,
    onTestPopup: (Boolean) -> Unit,
    showLayoutToggle: Boolean,
    overview: Boolean,
    onToggleLayout: () -> Unit,
    onOpenLandscape: (() -> Unit)?,
    onOpenSettings: () -> Unit,
) {
    val p = LocalNccPalette.current
    Surface(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = if (floating) 12.dp else 0.dp, vertical = if (floating) 6.dp else 0.dp)
            .height(if (floating) height - 12.dp else height),
        shape = if (floating) RoundedCornerShape(18.dp) else RoundedCornerShape(0.dp),
        color = if (floating) p.glass else p.bg,
        shadowElevation = if (floating) 6.dp else 0.dp,
    ) {
        Row(
            modifier = Modifier.fillMaxSize().padding(start = if (floating) 12.dp else 16.dp, end = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            if (showBrand) BrandMark(size = 28.dp)
            Text(
                text = title,
                color = p.text,
                style = MaterialTheme.typography.titleLarge,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f).padding(start = 4.dp),
            )
            // Connection
            Row(
                modifier = Modifier
                    .clip(RoundedCornerShape(999.dp))
                    .clickable(onClick = onOpenSettings)
                    .background(if (connected) p.okSoft else p.warnSoft)
                    .padding(horizontal = 10.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                Box(Modifier.size(7.dp).clip(CircleShape).background(if (connected) p.ok else p.warn))
                Text(
                    connectionLabel,
                    color = if (connected) p.ok else p.warn,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    modifier = Modifier.widthIn(max = 110.dp),
                    overflow = TextOverflow.Ellipsis,
                )
            }
            // Plate notifications
            Box {
                IconButton(onClick = { onPopupMenu(true) }) {
                    Icon(
                        imageVector = when (popupFilter) {
                            PlatePopupFilter.OFF -> Icons.Default.NotificationsOff
                            PlatePopupFilter.ALL -> Icons.Default.NotificationsActive
                            PlatePopupFilter.MATCHED_ONLY -> Icons.Default.Notifications
                        },
                        contentDescription = "ナンバー検知の通知",
                        tint = if (popupFilter == PlatePopupFilter.OFF) p.faint else p.plateHit,
                    )
                }
                DropdownMenu(expanded = popupMenu, onDismissRequest = { onPopupMenu(false) }, containerColor = p.paper) {
                    Text(
                        "ナンバー検知の通知",
                        color = p.muted,
                        style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp),
                    )
                    listOf(
                        PlatePopupFilter.MATCHED_ONLY to "条件に一致したときだけ",
                        PlatePopupFilter.ALL to "すべてのナンバー",
                        PlatePopupFilter.OFF to "通知しない",
                    ).forEach { (f, label) ->
                        DropdownMenuItem(
                            text = { Text(label) },
                            leadingIcon = {
                                if (f == popupFilter) Icon(Icons.Default.Check, contentDescription = null, tint = p.accent)
                                else Spacer(Modifier.size(24.dp))
                            },
                            onClick = { onPopupFilter(f); onPopupMenu(false) },
                        )
                    }
                    HorizontalDivider(color = p.line)
                    DropdownMenuItem(
                        text = { Text("通知の表示を試す", color = p.muted) },
                        onClick = { onTestPopup(true); onPopupMenu(false) },
                    )
                }
            }
            if (showLayoutToggle) {
                IconButton(onClick = onToggleLayout) {
                    Icon(
                        if (overview) Icons.Default.ViewAgenda else Icons.Outlined.Layers,
                        contentDescription = if (overview) "ダッシュボードを詳細表示にする" else "ダッシュボードを地図表示にする",
                        tint = p.muted,
                    )
                }
            }
            if (onOpenLandscape != null) {
                IconButton(onClick = onOpenLandscape) {
                    Icon(Icons.Default.Fullscreen, contentDescription = "横画面ダッシュボードにする", tint = p.muted)
                }
            }
        }
    }
}

@Composable
private fun BottomBar(
    modifier: Modifier,
    height: Dp,
    currentPage: String,
    openGroupId: String?,
    onGroup: (NavGroup) -> Unit,
) {
    val p = LocalNccPalette.current
    Surface(
        modifier = modifier.fillMaxWidth(),
        color = p.paper,
        shadowElevation = 12.dp,
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .windowInsetsPadding(WindowInsets.navigationBars)
                .height(height)
                .padding(horizontal = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            mobileNavGroups.forEach { grp ->
                val inGroup = grp.itemIds.contains(currentPage)
                val active = openGroupId == grp.id || (inGroup && openGroupId == null)
                Column(
                    modifier = Modifier
                        .weight(1f)
                        .clip(RoundedCornerShape(14.dp))
                        .clickable { onGroup(grp) }
                        .padding(vertical = 6.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.spacedBy(3.dp),
                ) {
                    Box(
                        modifier = Modifier
                            .clip(RoundedCornerShape(999.dp))
                            .background(if (active) p.soft else Color.Transparent)
                            .padding(horizontal = 16.dp, vertical = 4.dp),
                    ) {
                        Icon(grp.icon, contentDescription = null, tint = if (active) p.accent else p.muted, modifier = Modifier.size(22.dp))
                    }
                    Text(
                        grp.label,
                        color = if (active) p.accent else p.muted,
                        fontSize = 11.sp,
                        fontWeight = if (active) FontWeight.Bold else FontWeight.Medium,
                        maxLines = 1,
                    )
                }
            }
        }
    }
}

@Composable
private fun GroupSheet(group: NavGroup, currentPage: String, onNavigate: (String) -> Unit) {
    val p = LocalNccPalette.current
    Surface(
        shape = RoundedCornerShape(24.dp),
        color = p.paper,
        shadowElevation = 16.dp,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 10.dp)
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null) {},
    ) {
        Column(modifier = Modifier.padding(10.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(
                group.label,
                color = p.muted,
                style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.padding(start = 10.dp, top = 6.dp, bottom = 4.dp),
            )
            group.itemIds.mapNotNull { id -> allNavItems.firstOrNull { it.id == id } }.forEach { item ->
                val sel = item.id == currentPage
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(16.dp))
                        .background(if (sel) p.soft else Color.Transparent)
                        .clickable { onNavigate(item.id) }
                        .padding(horizontal = 10.dp, vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Box(
                        Modifier.size(38.dp).clip(RoundedCornerShape(12.dp)).background(if (sel) p.accent else p.raised),
                        contentAlignment = Alignment.Center,
                    ) {
                        Icon(item.icon, contentDescription = null, tint = if (sel) p.onAccent else p.text, modifier = Modifier.size(20.dp))
                    }
                    Column(modifier = Modifier.weight(1f)) {
                        Text(item.label, color = if (sel) p.accent else p.text, style = MaterialTheme.typography.titleSmall)
                        Text(item.hint, color = p.muted, style = MaterialTheme.typography.bodySmall)
                    }
                    Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, contentDescription = null, tint = p.faint)
                }
            }
        }
    }
}

@Composable
private fun NavRail(
    compact: Boolean,
    currentPage: String,
    connected: Boolean,
    connectionLabel: String,
    onNavigate: (String) -> Unit,
    onOpenSettings: () -> Unit,
) {
    val p = LocalNccPalette.current
    val width = if (compact) 92.dp else 196.dp
    Column(
        modifier = Modifier
            .width(width)
            .fillMaxHeight()
            .background(p.side)
            .windowInsetsPadding(WindowInsets.statusBars)
            .windowInsetsPadding(WindowInsets.tappableElement)
            .padding(vertical = if (compact) 4.dp else 10.dp, horizontal = 8.dp),
        horizontalAlignment = if (compact) Alignment.CenterHorizontally else Alignment.Start,
    ) {
        // No brand mark here: in landscape it only pushed the menu down (owner's request).
        if (!compact) {
            Column(Modifier.padding(horizontal = 12.dp, vertical = 4.dp)) {
                Text("NP1", color = p.text, fontWeight = FontWeight.Bold, fontSize = 15.sp)
                Text("Control Center", color = p.muted, fontSize = 11.sp)
            }
            Spacer(Modifier.height(10.dp))
        }
        Column(
            modifier = Modifier.weight(1f).verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            var lastGroup = ""
            allNavItems.forEach { item ->
                if (!compact && item.groupId != lastGroup && item.groupId != "home") {
                    Text(
                        when (item.groupId) { "network" -> "ネットワーク"; "drive" -> "ドラレコ"; else -> "設定" },
                        color = p.faint,
                        style = MaterialTheme.typography.labelSmall,
                        modifier = Modifier.padding(start = 12.dp, top = 10.dp, bottom = 2.dp),
                    )
                }
                lastGroup = item.groupId
                val active = item.id == currentPage
                if (compact) {
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(14.dp))
                            .clickable { onNavigate(item.id) }
                            .padding(vertical = 1.dp),
                        horizontalAlignment = Alignment.CenterHorizontally,
                    ) {
                        Box(
                            Modifier.clip(RoundedCornerShape(999.dp)).background(if (active) p.soft else Color.Transparent).padding(horizontal = 14.dp, vertical = 2.dp),
                        ) {
                            Icon(item.icon, contentDescription = item.label, tint = if (active) p.accent else p.muted, modifier = Modifier.size(18.dp))
                        }
                        Text(
                            item.label.replace(" / AP", ""),
                            color = if (active) p.accent else p.muted,
                            fontSize = 9.5.sp,
                            lineHeight = 11.sp,
                            softWrap = false,
                            fontWeight = if (active) FontWeight.Bold else FontWeight.Medium,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                } else {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(12.dp))
                            .background(if (active) p.soft else Color.Transparent)
                            .clickable { onNavigate(item.id) }
                            .padding(horizontal = 12.dp, vertical = 9.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        Icon(item.icon, contentDescription = null, tint = if (active) p.accent else p.muted, modifier = Modifier.size(19.dp))
                        Text(
                            item.label,
                            color = if (active) p.accent else p.text,
                            fontSize = 14.sp,
                            fontWeight = if (active) FontWeight.Bold else FontWeight.Medium,
                            maxLines = 1,
                        )
                    }
                }
            }
        }
        Spacer(Modifier.height(6.dp))
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(12.dp))
                .clickable(onClick = onOpenSettings)
                .padding(horizontal = if (compact) 4.dp else 12.dp, vertical = if (compact) 6.dp else 9.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = if (compact) Arrangement.Center else Arrangement.spacedBy(10.dp),
        ) {
            Icon(Icons.Default.Settings, contentDescription = "アプリの設定", tint = p.muted, modifier = Modifier.size(19.dp))
            if (!compact) {
                Column(Modifier.weight(1f)) {
                    Text("アプリの設定", color = p.text, fontSize = 13.sp, fontWeight = FontWeight.Medium)
                    Text(if (connected) connectionLabel else "未接続", color = if (connected) p.ok else p.warn, fontSize = 11.sp, maxLines = 1)
                }
            }
        }
    }
}

// ── Landscape dashboard support ────────────────────────────────────────────

/** Hides the status and navigation bars while [enabled]; a swipe from the edge shows them briefly. */
@Composable
private fun ImmersiveMode(enabled: Boolean) {
    val view = LocalView.current
    DisposableEffect(enabled) {
        var ctx = view.context
        while (ctx is ContextWrapper && ctx !is Activity) ctx = ctx.baseContext
        val window = (ctx as? Activity)?.window
        val controller = window?.let { WindowCompat.getInsetsController(it, view) }
        if (enabled) {
            controller?.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
            controller?.hide(WindowInsetsCompat.Type.systemBars())
        } else {
            controller?.show(WindowInsetsCompat.Type.systemBars())
        }
        onDispose { controller?.show(WindowInsetsCompat.Type.systemBars()) }
    }
}

private data class MenuTile(val label: String, val icon: ImageVector, val onClick: () -> Unit)

/** Menu opened from the small button at the bottom left of the landscape dashboard. */
@Composable
private fun LandscapeMenu(
    connected: Boolean,
    connectionLabel: String,
    popupFilter: PlatePopupFilter,
    onPopupFilter: (PlatePopupFilter) -> Unit,
    onNavigate: (String) -> Unit,
    onOpenSettings: () -> Unit,
    onDismiss: () -> Unit,
) {
    val p = LocalNccPalette.current
    Box(
        Modifier
            .fillMaxSize()
            .background(p.scrim)
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onDismiss),
    ) {
        Surface(
            shape = RoundedCornerShape(20.dp),
            color = p.paper,
            shadowElevation = 16.dp,
            modifier = Modifier
                .align(Alignment.BottomStart)
                .padding(12.dp)
                .width(392.dp)
                .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null) {},
        ) {
            Column(
                Modifier.verticalScroll(rememberScrollState()).padding(12.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Box(Modifier.size(8.dp).clip(CircleShape).background(if (connected) p.ok else p.warn))
                    Text(
                        connectionLabel,
                        color = if (connected) p.ok else p.warn,
                        fontSize = 12.sp,
                        fontWeight = FontWeight.SemiBold,
                        modifier = Modifier.weight(1f),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    IconButton(onClick = onDismiss, modifier = Modifier.size(32.dp)) {
                        Icon(Icons.Default.Close, contentDescription = "閉じる", tint = p.muted, modifier = Modifier.size(18.dp))
                    }
                }
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text("ナンバー検知の通知", color = p.muted, style = MaterialTheme.typography.labelMedium)
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        listOf(
                            PlatePopupFilter.MATCHED_ONLY to "条件一致のみ",
                            PlatePopupFilter.ALL to "すべて",
                            PlatePopupFilter.OFF to "通知しない",
                        ).forEach { (f, label) ->
                            val sel = f == popupFilter
                            Box(
                                Modifier
                                    .weight(1f)
                                    .clip(RoundedCornerShape(10.dp))
                                    .background(if (sel) p.accent else p.raised)
                                    .clickable { onPopupFilter(f) }
                                    .padding(vertical = 8.dp),
                                contentAlignment = Alignment.Center,
                            ) {
                                Text(label, color = if (sel) p.onAccent else p.text, fontSize = 12.sp, fontWeight = FontWeight.Medium, maxLines = 1)
                            }
                        }
                    }
                }
                HorizontalDivider(color = p.line)
                val tiles = allNavItems.map { item -> MenuTile(item.label, item.icon) { onNavigate(item.id) } } +
                    MenuTile("アプリの設定", Icons.Default.Settings, onOpenSettings)
                tiles.chunked(4).forEach { row ->
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        row.forEach { tile ->
                            Column(
                                Modifier
                                    .weight(1f)
                                    .clip(RoundedCornerShape(12.dp))
                                    .background(p.raised)
                                    .clickable(onClick = tile.onClick)
                                    .padding(vertical = 8.dp, horizontal = 4.dp),
                                horizontalAlignment = Alignment.CenterHorizontally,
                                verticalArrangement = Arrangement.spacedBy(4.dp),
                            ) {
                                Icon(tile.icon, contentDescription = null, tint = p.accent, modifier = Modifier.size(20.dp))
                                Text(tile.label, color = p.text, fontSize = 11.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            }
                        }
                        repeat(4 - row.size) { Spacer(Modifier.weight(1f)) }
                    }
                }
            }
        }
    }
}
