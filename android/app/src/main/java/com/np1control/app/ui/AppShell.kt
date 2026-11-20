package com.np1control.app.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CellTower
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Dashboard
import androidx.compose.material.icons.filled.DirectionsCar
import androidx.compose.material.icons.filled.DocumentScanner
import androidx.compose.material.icons.filled.Map
import androidx.compose.material.icons.filled.Monitor
import androidx.compose.material.icons.filled.Movie
import androidx.compose.material.icons.filled.NotificationsActive
import androidx.compose.material.icons.filled.NotificationsOff
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Router
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material.icons.filled.Videocam
import androidx.compose.material.icons.filled.Wifi
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.VerticalDivider
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.AppConfig
import com.np1control.app.data.AppPreferences
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.PlatePopupFilter
import com.np1control.app.data.PlateRealtimeMonitor
import com.np1control.app.data.SystemSummary
import com.np1control.app.data.WebSettings
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.BrandMark
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccTag
import com.np1control.app.ui.components.PlatePopupOverlay
import com.np1control.app.ui.components.StatusDot
import com.np1control.app.ui.screens.AppSettingsDialog
import com.np1control.app.ui.screens.CellularScreen
import com.np1control.app.ui.screens.DashboardScreen
import com.np1control.app.ui.screens.LiveCameraScreen
import com.np1control.app.ui.screens.NativeSettingsScreen
import com.np1control.app.ui.screens.PlatesScreen
import com.np1control.app.ui.screens.RecordingsScreen
import com.np1control.app.ui.screens.SystemScreen
import com.np1control.app.ui.screens.TailscaleScreen
import com.np1control.app.ui.screens.TripsScreen
import com.np1control.app.ui.screens.WifiApScreen
import com.np1control.app.ui.theme.LocalNccPalette
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
    NavItem("vpn", "Tailscale", "遠隔アクセス", Icons.Default.Shield, "network"),
    NavItem("camera", "ライブ", "カメラのライブ映像", Icons.Default.Videocam, "drive"),
    NavItem("recordings", "ドラレコ再生", "録画を日時から再生", Icons.Default.Movie, "drive"),
    NavItem("trips", "走行履歴", "距離・経路・電波マップ", Icons.Default.Map, "drive"),
    NavItem("plates", "ナンバー", "ナンバー認識の記録と条件", Icons.Default.DocumentScanner, "drive"),
    NavItem("native", "純正設定", "NP1本体の設定とAlexa", Icons.Default.Tune, "manage"),
    NavItem("system", "システム", "本体・LED・バックアップなど", Icons.Default.Monitor, "manage"),
)

private val mobileNavGroups = listOf(
    NavGroup("home", "ホーム", Icons.Default.Dashboard, listOf("dashboard")),
    NavGroup("network", "ネットワーク", Icons.Default.Router, listOf("internet", "wifi", "vpn")),
    NavGroup("drive", "ドラレコ", Icons.Default.DirectionsCar, listOf("camera", "recordings", "trips", "plates")),
    NavGroup("manage", "設定", Icons.Default.Settings, listOf("native", "system")),
)

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
    val isCompactLandscape = isLandscape && configuration.screenHeightDp < 540 // Echo Show 5 (960x480 etc.)
    val isWide = configuration.screenWidthDp >= 600 || isLandscape

    var currentPage by remember { mutableStateOf("dashboard") }
    var systemSection by remember { mutableStateOf("overview") }
    var recordingJumpIso by remember { mutableStateOf<String?>(null) }
    var plateHighlightId by remember { mutableStateOf<String?>(null) }
    var openMobileGroupId by remember { mutableStateOf<String?>(null) }
    var showAppSettings by remember { mutableStateOf(false) }

    var summary by remember { mutableStateOf<SystemSummary>(emptyMap()) }
    var webSettings by remember { mutableStateOf<WebSettings?>(null) }
    var globalError by remember { mutableStateOf("") }

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

    fun refreshSummary() {
        scope.launch {
            runCatching {
                apiClient.ensureSession(forceReprobe = !connection.connected)
                summary = apiClient.get<SystemSummary>("/system/summary")
                if (webSettings == null) {
                    runCatching { webSettings = apiClient.get<WebSettings>("/settings") }
                }
                globalError = ""
            }.onFailure {
                globalError = it.message ?: "NP1に接続できません（接続設定を確認してください）"
            }
        }
    }

    // Boot: connect + listen to SSE refresh + run realtime plate monitor
    LaunchedEffect(Unit) {
        refreshSummary()
        plateMonitor.start(this)
        launch {
            apiClient.refreshEvents().collect {
                runCatching { summary = apiClient.get<SystemSummary>("/system/summary") }
            }
        }
    }

    fun cyclePopupFilter() {
        val next = when (config.popupFilter) {
            PlatePopupFilter.MATCHED_ONLY -> PlatePopupFilter.ALL
            PlatePopupFilter.ALL -> PlatePopupFilter.OFF
            PlatePopupFilter.OFF -> PlatePopupFilter.MATCHED_ONLY
        }
        preferences.update { it.copy(popupFilter = next) }
    }

    val currentNavItem = allNavItems.firstOrNull { it.id == currentPage } ?: allNavItems.first()

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(p.bg)
            .statusBarsPadding()
            .navigationBarsPadding(),
    ) {
        Row(modifier = Modifier.fillMaxSize()) {
            // ── Left Navigation Rail / Sidebar (Echo Show 5 landscape & tablets) ──
            if (isWide) {
                val railWidth = if (isCompactLandscape) 72.dp else 176.dp
                Column(
                    modifier = Modifier
                        .width(railWidth)
                        .fillMaxHeight()
                        .background(p.side)
                        .padding(vertical = 8.dp, horizontal = 6.dp),
                    horizontalAlignment = if (isCompactLandscape) Alignment.CenterHorizontally else Alignment.Start,
                ) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = if (isCompactLandscape) 0.dp else 8.dp, vertical = 4.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = if (isCompactLandscape) Arrangement.Center else Arrangement.spacedBy(8.dp),
                    ) {
                        BrandMark(size = if (isCompactLandscape) 28.dp else 30.dp)
                        if (!isCompactLandscape) {
                            Column {
                                Text("NP1", color = p.text, fontWeight = FontWeight.Bold, fontSize = 14.sp)
                                Text("Control Center", color = p.muted, fontSize = 10.sp)
                            }
                        }
                    }
                    Spacer(modifier = Modifier.height(6.dp))
                    Column(
                        modifier = Modifier
                            .weight(1f)
                            .verticalScroll(rememberScrollState()),
                        verticalArrangement = Arrangement.spacedBy(3.dp),
                    ) {
                        allNavItems.forEach { item ->
                            val active = item.id == currentPage
                            Surface(
                                shape = RoundedCornerShape(10.dp),
                                color = if (active) p.soft else Color.Transparent,
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clickable { navigate(item.id) },
                            ) {
                                if (isCompactLandscape) {
                                    Column(
                                        modifier = Modifier.padding(vertical = 6.dp, horizontal = 2.dp),
                                        horizontalAlignment = Alignment.CenterHorizontally,
                                        verticalArrangement = Arrangement.spacedBy(2.dp),
                                    ) {
                                        Icon(
                                            imageVector = item.icon,
                                            contentDescription = item.label,
                                            tint = if (active) p.accent else p.muted,
                                            modifier = Modifier.size(19.dp),
                                        )
                                        Text(
                                            text = item.label.take(5),
                                            color = if (active) p.accent else p.muted,
                                            fontSize = 9.sp,
                                            fontWeight = if (active) FontWeight.Bold else FontWeight.Normal,
                                            maxLines = 1,
                                            overflow = TextOverflow.Clip,
                                        )
                                    }
                                } else {
                                    Row(
                                        modifier = Modifier.padding(horizontal = 10.dp, vertical = 9.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(9.dp),
                                    ) {
                                        Icon(
                                            imageVector = item.icon,
                                            contentDescription = item.label,
                                            tint = if (active) p.accent else p.muted,
                                            modifier = Modifier.size(18.dp),
                                        )
                                        Text(
                                            text = item.label,
                                            color = if (active) p.accent else p.text,
                                            fontSize = 13.sp,
                                            fontWeight = if (active) FontWeight.Bold else FontWeight.Medium,
                                            maxLines = 1,
                                            overflow = TextOverflow.Ellipsis,
                                        )
                                    }
                                }
                            }
                        }
                    }
                    HorizontalDivider(color = p.line, modifier = Modifier.padding(vertical = 4.dp))
                    // Connection status footer in rail
                    Surface(
                        shape = RoundedCornerShape(8.dp),
                        color = p.paper,
                        border = BorderStroke(1.dp, p.line),
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable { showAppSettings = true },
                    ) {
                        Row(
                            modifier = Modifier.padding(horizontal = 8.dp, vertical = 7.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = if (isCompactLandscape) Arrangement.Center else Arrangement.spacedBy(6.dp),
                        ) {
                            StatusDot(
                                online = connection.connected,
                                label = if (isCompactLandscape) null else connection.activeLabel,
                            )
                        }
                    }
                }
                VerticalDivider(color = p.line, thickness = 1.dp)
            }

            // ── Main Content Area ─────────────────────────────────────────────
            Column(modifier = Modifier.weight(1f).fillMaxHeight()) {
                // Top toolbar (compact on Echo Show 5 & phones)
                Surface(color = p.paper, shadowElevation = 1.dp) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 12.dp, vertical = if (isCompactLandscape) 5.dp else 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        if (!isWide) {
                            BrandMark(size = 26.dp)
                        }
                        Text(
                            text = currentNavItem.label,
                            color = p.text,
                            fontWeight = FontWeight.Bold,
                            fontSize = if (isCompactLandscape) 14.sp else 16.sp,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f),
                        )

                        // Quick real-time plate popup mode toggle chip
                        val popupOn = config.popupFilter != PlatePopupFilter.OFF
                        Surface(
                            shape = RoundedCornerShape(999.dp),
                            color = if (popupOn) p.plateHitSoft else p.bg,
                            border = BorderStroke(1.dp, if (popupOn) p.plateHit else p.line),
                            modifier = Modifier.clickable { cyclePopupFilter() },
                        ) {
                            Row(
                                modifier = Modifier.padding(horizontal = 9.dp, vertical = 5.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(4.dp),
                            ) {
                                Icon(
                                    imageVector = if (popupOn) Icons.Default.NotificationsActive else Icons.Default.NotificationsOff,
                                    contentDescription = "ナンバー通知モード切替",
                                    tint = if (popupOn) p.plateHit else p.muted,
                                    modifier = Modifier.size(14.dp),
                                )
                                Text(
                                    text = "検出:${config.popupFilter.shortLabel}",
                                    color = if (popupOn) p.plateHit else p.muted,
                                    fontSize = 11.sp,
                                    fontWeight = FontWeight.Bold,
                                )
                            }
                        }

                        // Connection status button -> opens connection settings
                        Surface(
                            shape = RoundedCornerShape(999.dp),
                            color = p.bg,
                            border = BorderStroke(1.dp, p.line),
                            modifier = Modifier.clickable { showAppSettings = true },
                        ) {
                            Row(
                                modifier = Modifier.padding(horizontal = 8.dp, vertical = 5.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                StatusDot(
                                    online = connection.connected,
                                    label = if (connection.connected) connection.activeLabel else "未接続",
                                )
                            }
                        }

                        IconButton(onClick = { refreshSummary() }, modifier = Modifier.size(32.dp)) {
                            Icon(Icons.Default.Refresh, contentDescription = "更新", tint = p.muted, modifier = Modifier.size(18.dp))
                        }
                    }
                }
                HorizontalDivider(color = p.line, thickness = 1.dp)

                // Connection error hint banner (clickable to open settings)
                if (!connection.connected && globalError.isNotBlank()) {
                    Box(modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp).clickable { showAppSettings = true }) {
                        NccBanner(
                            message = "$globalError（タップして接続先設定を開く）",
                            tone = BannerTone.WARN,
                            onDismiss = { globalError = "" },
                        )
                    }
                }

                // Active screen content
                Box(modifier = Modifier.weight(1f).fillMaxWidth()) {
                    when (currentPage) {
                        "dashboard" -> DashboardScreen(
                            summary = summary,
                            webSettings = webSettings,
                            apiClient = apiClient,
                            isWide = isWide,
                            overviewMode = config.dashboardOverview,
                            onNavigate = { navigate(it) },
                        )
                        "internet" -> CellularScreen(
                            summary = summary,
                            apiClient = apiClient,
                            onRefreshSummary = { refreshSummary() },
                        )
                        "wifi" -> WifiApScreen(
                            apiClient = apiClient,
                            onRefreshSummary = { refreshSummary() },
                        )
                        "vpn" -> TailscaleScreen(
                            summary = summary,
                            apiClient = apiClient,
                            onRefreshSummary = { refreshSummary() },
                            onSaveAsTailscaleHost = { ip ->
                                preferences.update { it.copy(tailscaleHost = ip) }
                            },
                        )
                        "camera" -> LiveCameraScreen(
                            apiClient = apiClient,
                            isWide = isWide,
                        )
                        "recordings" -> RecordingsScreen(
                            apiClient = apiClient,
                            initialTimestampIso = recordingJumpIso,
                            isWide = isWide,
                        )
                        "trips" -> TripsScreen(
                            apiClient = apiClient,
                            onOpenRecordingAt = { iso ->
                                recordingJumpIso = iso
                                navigate("recordings")
                            },
                        )
                        "plates" -> PlatesScreen(
                            apiClient = apiClient,
                            config = config,
                            highlightRecordId = plateHighlightId,
                            isWide = isWide,
                            onPopupFilterChanged = { filter -> preferences.update { it.copy(popupFilter = filter) } },
                            onEmitTestPopup = { matched -> plateMonitor.emitTestPopup(matched) },
                            onOpenRecordingAt = { iso ->
                                recordingJumpIso = iso
                                navigate("recordings")
                            },
                        )
                        "native" -> NativeSettingsScreen(apiClient = apiClient)
                        "system" -> SystemScreen(
                            summary = summary,
                            apiClient = apiClient,
                            initialSection = systemSection,
                            onRefreshSummary = { refreshSummary() },
                            onOpenAppSettings = { showAppSettings = true },
                        )
                    }
                }

                // ── Smartphone Portrait Bottom Tab Bar (matches Web UI 4-group mobile shell) ──
                if (!isWide) {
                    HorizontalDivider(color = p.line, thickness = 1.dp)
                    Surface(color = p.paper, shadowElevation = 4.dp) {
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(horizontal = 4.dp, vertical = 4.dp),
                            horizontalArrangement = Arrangement.SpaceEvenly,
                        ) {
                            mobileNavGroups.forEach { grp ->
                                val isCurrentGroup = grp.itemIds.contains(currentPage)
                                val isSheetOpen = openMobileGroupId == grp.id
                                val active = isSheetOpen || (isCurrentGroup && openMobileGroupId == null)
                                Column(
                                    modifier = Modifier
                                        .weight(1f)
                                        .clip(RoundedCornerShape(10.dp))
                                        .clickable {
                                            if (grp.itemIds.size == 1) {
                                                navigate(grp.itemIds.first())
                                            } else {
                                                openMobileGroupId = if (openMobileGroupId == grp.id) null else grp.id
                                            }
                                        }
                                        .padding(vertical = 6.dp),
                                    horizontalAlignment = Alignment.CenterHorizontally,
                                    verticalArrangement = Arrangement.spacedBy(2.dp),
                                ) {
                                    Icon(
                                        imageVector = grp.icon,
                                        contentDescription = grp.label,
                                        tint = if (active) p.accent else p.muted,
                                        modifier = Modifier.size(21.dp),
                                    )
                                    Text(
                                        text = grp.label,
                                        color = if (active) p.accent else p.muted,
                                        fontSize = 10.sp,
                                        fontWeight = if (active) FontWeight.Bold else FontWeight.Medium,
                                    )
                                }
                            }
                        }
                    }
                }
            }
        }

        // ── Smartphone Group Sub-Menu Sheet (just like Web UI .tab-sheet) ──
        val openGroup = mobileNavGroups.firstOrNull { it.id == openMobileGroupId }
        if (!isWide && openGroup != null) {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .background(Color.Black.copy(alpha = 0.45f))
                    .clickable { openMobileGroupId = null },
                contentAlignment = Alignment.BottomCenter,
            ) {
                Surface(
                    shape = RoundedCornerShape(topStart = 18.dp, topEnd = 18.dp),
                    color = p.paper,
                    border = BorderStroke(1.dp, p.line),
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(bottom = 58.dp)
                        .clickable(enabled = false) {},
                ) {
                    Column(modifier = Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(openGroup.label, color = p.text, fontWeight = FontWeight.Bold, fontSize = 15.sp)
                            IconButton(onClick = { openMobileGroupId = null }, modifier = Modifier.size(28.dp)) {
                                Icon(Icons.Default.Close, contentDescription = "閉じる", tint = p.muted)
                            }
                        }
                        openGroup.itemIds.mapNotNull { id -> allNavItems.firstOrNull { it.id == id } }.forEach { item ->
                            val sel = item.id == currentPage
                            Surface(
                                shape = RoundedCornerShape(10.dp),
                                color = if (sel) p.soft else p.bg,
                                border = BorderStroke(1.dp, if (sel) p.accent else p.line),
                                modifier = Modifier.fillMaxWidth().clickable { navigate(item.id) },
                            ) {
                                Row(
                                    modifier = Modifier.padding(horizontal = 12.dp, vertical = 11.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                                ) {
                                    Icon(item.icon, contentDescription = null, tint = if (sel) p.accent else p.muted, modifier = Modifier.size(20.dp))
                                    Column(modifier = Modifier.weight(1f)) {
                                        Text(item.label, color = if (sel) p.accent else p.text, fontWeight = FontWeight.Bold, fontSize = 14.sp)
                                        Text(item.hint, color = p.muted, fontSize = 11.sp)
                                    }
                                    Icon(Icons.Default.ChevronRight, contentDescription = null, tint = p.muted, modifier = Modifier.size(18.dp))
                                }
                            }
                        }
                    }
                }
            }
        }

        // ── Real-time License Plate Detection Popup Overlay (always on top!) ──
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
            modifier = Modifier
                .align(Alignment.TopCenter)
                .padding(top = if (isCompactLandscape) 38.dp else 48.dp),
        )
    }

    if (showAppSettings) {
        AppSettingsDialog(
            config = config,
            apiClient = apiClient,
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
