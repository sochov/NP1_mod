package com.np1control.app.ui.screens

import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Memory
import androidx.compose.material.icons.filled.Palette
import androidx.compose.material.icons.filled.RecordVoiceOver
import androidx.compose.material.icons.filled.Route
import androidx.compose.material.icons.filled.Usb
import androidx.compose.material3.MaterialTheme
import com.np1control.app.data.dataDouble
import com.np1control.app.data.dataObj
import com.np1control.app.data.dataString
import com.np1control.app.data.dbl
import com.np1control.app.data.str
import androidx.compose.material.icons.filled.Settings
import androidx.compose.foundation.layout.width
import com.np1control.app.ui.components.ConfirmDialog
import com.np1control.app.ui.components.FieldLabel
import com.np1control.app.ui.components.HintText
import com.np1control.app.ui.components.ListRow
import com.np1control.app.ui.components.PanelAction
import com.np1control.app.ui.components.ScreenColumn
import com.np1control.app.ui.components.StatusPill
import com.np1control.app.ui.components.Tone
import kotlinx.serialization.json.JsonObject
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowUpward
import androidx.compose.material.icons.filled.Backup
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.Lightbulb
import androidx.compose.material.icons.filled.Movie
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Save
import androidx.compose.material.icons.filled.Security
import androidx.compose.material.icons.filled.Storage
import androidx.compose.material.icons.automirrored.filled.VolumeUp
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Slider
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.FileEntry
import com.np1control.app.data.Formatters
import com.np1control.app.data.LEDObservationData
import com.np1control.app.data.LEDSettingsData
import com.np1control.app.data.NasStateData
import com.np1control.app.data.NativeAudioData
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.data.OtaPolicyData
import com.np1control.app.data.RecorderAdvancedConfig
import com.np1control.app.data.RecorderAdvancedState
import com.np1control.app.data.StockService
import com.np1control.app.data.SystemSummary
import com.np1control.app.data.VoicevoxCheckResponse
import com.np1control.app.data.VoicevoxConfig
import com.np1control.app.data.WebSettings
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.KeyValueRow
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.NccTag
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.components.VideoPlayerBox
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.launch
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.put
import java.net.URLEncoder

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun SystemScreen(
    summary: SystemSummary,
    apiClient: NccApiClient,
    initialSection: String = "overview",
    onRefreshSummary: () -> Unit,
    onOpenAppSettings: () -> Unit,
    onWebSettingsChanged: () -> Unit = {},
) {
    val p = LocalNccPalette.current
    val scope = rememberCoroutineScope()
    val context = LocalContext.current

    var section by remember(initialSection) { mutableStateOf(initialSection) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var notice by remember { mutableStateOf("") }

    // Overview / stock services
    var services by remember { mutableStateOf<List<StockService>>(emptyList()) }

    // Storage browser
    var currentPath by remember { mutableStateOf(".") }
    var files by remember { mutableStateOf<List<FileEntry>>(emptyList()) }
    var fileQuery by remember { mutableStateOf("") }
    var previewVideoUrl by remember { mutableStateOf<String?>(null) }

    // Logs
    var logsText by remember { mutableStateOf("") }

    // Recorder advanced
    var recState by remember { mutableStateOf<RecorderAdvancedState?>(null) }
    var recDraft by remember { mutableStateOf<RecorderAdvancedConfig?>(null) }
    var recApplyNow by remember { mutableStateOf(false) }

    // LED / Audio / Voicevox
    var ledData by remember { mutableStateOf<LEDObservationData?>(null) }
    var ledMode by remember { mutableStateOf("solid") }
    var ledBrightness by remember { mutableIntStateOf(180) }
    var ledRgb by remember { mutableStateOf(listOf(64, 160, 255)) }
    var ledTestSec by remember { mutableIntStateOf(5) }
    var ledContinuous by remember { mutableStateOf(false) }
    var nativeAudio by remember { mutableStateOf<NativeAudioData?>(null) }
    var vvConfig by remember { mutableStateOf<VoicevoxConfig?>(null) }
    var vvEndpoint by remember { mutableStateOf("") }
    var vvKey by remember { mutableStateOf("") }
    var vvPoints by remember { mutableStateOf("") }

    // NAS backup
    var nasState by remember { mutableStateOf<NasStateData?>(null) }

    // OTA & Web settings
    var otaPolicy by remember { mutableStateOf<OtaPolicyData?>(null) }
    var webSettings by remember { mutableStateOf<WebSettings?>(null) }
    var webThemeDraft by remember { mutableStateOf("system") }

    fun loadFiles(path: String) {
        scope.launch {
            error = ""
            val enc = URLEncoder.encode(path, "UTF-8")
            runCatching { apiClient.get<List<FileEntry>>("/storage/files?path=$enc") }
                .onSuccess {
                    files = it
                    currentPath = path
                    previewVideoUrl = null
                }
                .onFailure { error = it.message ?: "SDカードを読み取れません" }
        }
    }

    LaunchedEffect(section) {
        error = ""
        when (section) {
            "overview" -> runCatching {
                val obs = apiClient.get<Observation>("/system/services")
                obs.data?.let { services = apiClient.json.decodeFromJsonElement(ListSerializer(StockService.serializer()), it) }
            }
            "storage" -> loadFiles(".")
            "logs" -> runCatching { logsText = apiClient.requestRaw("/logs", "GET") }
                .onFailure { error = it.message ?: "操作履歴を取得できません" }
            "recorder" -> runCatching {
                val st = apiClient.get<RecorderAdvancedState>("/device/recorder-advanced")
                recState = st
                recDraft = st.settings
            }.onFailure { error = it.message ?: "録画詳細設定を取得できません" }
            "led" -> {
                runCatching {
                    val obs = apiClient.get<Observation>("/device/led")
                    obs.data?.let {
                        val d = apiClient.json.decodeFromJsonElement(LEDObservationData.serializer(), it)
                        ledData = d
                        d.settings?.let { s ->
                            ledMode = if (s.mode == "off") "solid" else s.mode
                            ledBrightness = s.brightness
                            if (s.rgb.size == 3) ledRgb = s.rgb
                        }
                    }
                }
                runCatching {
                    val obs = apiClient.get<Observation>("/device/native-audio")
                    obs.data?.let { nativeAudio = apiClient.json.decodeFromJsonElement(NativeAudioData.serializer(), it) }
                }
                runCatching {
                    val vv = apiClient.get<VoicevoxConfig>("/voicevox")
                    vvConfig = vv
                    vvEndpoint = vv.endpoint.ifBlank { vv.defaultEndpoint }
                    vvKey = vv.apiKey
                }
            }
            "nas" -> runCatching {
                val obs = apiClient.get<Observation>("/nas-backup")
                obs.data?.let { nasState = apiClient.json.decodeFromJsonElement(NasStateData.serializer(), it) }
            }.onFailure { error = it.message ?: "NAS設定を取得できません" }
            "settings" -> {
                runCatching {
                    val obs = apiClient.get<Observation>("/device/ota-policy")
                    obs.data?.let { otaPolicy = apiClient.json.decodeFromJsonElement(OtaPolicyData.serializer(), it) }
                }
                runCatching {
                    val st = apiClient.get<WebSettings>("/settings")
                    webSettings = st
                    webThemeDraft = st.theme
                }
            }
        }
    }

    val tabs = listOf(
        "overview" to "本体",
        "routing" to "ルーティング",
        "storage" to "SDカード",
        "recorder" to "録画設定",
        "led" to "LED / 音声",
        "usb" to "USB",
        "logs" to "操作履歴",
        "nas" to "録画バックアップ",
        "settings" to "設定・更新",
    )
    var confirmService by remember { mutableStateOf<Pair<StockService, String>?>(null) }
    var openService by remember { mutableStateOf<String?>(null) }

    suspend fun reloadServices() {
        runCatching {
            apiClient.get<Observation>("/system/services").data?.let { services = apiClient.json.decodeFromJsonElement(ListSerializer(StockService.serializer()), it) }
        }
    }

    ScreenColumn {
        Row(modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState())) {
            SectionTabs(tabs = tabs, selectedId = section, onSelect = { section = it })
        }
        if (error.isNotBlank()) NccBanner(error, BannerTone.ERROR, onDismiss = { error = "" })
        if (notice.isNotBlank()) NccBanner(notice, BannerTone.SUCCESS, onDismiss = { notice = "" })

        when (section) {
            "overview" -> {
                NccPanel(title = "本体情報", icon = Icons.Default.Memory) {
                    val ram = summary.dataObj("ram", allowStale = false)
                    val loadParts = summary.dataString("load")?.trim()?.split(Regex("\\s+"))?.take(3).orEmpty()
                    KeyValueRow("稼働時間", Formatters.uptime(summary.dataDouble("uptime")))
                    KeyValueRow("負荷（1 / 5 / 15分）", loadParts.joinToString(" / ").ifBlank { "—" })
                    KeyValueRow("メモリー", ram?.let { "${Formatters.bytes(it.str("usedBytes"))} / ${Formatters.bytes(it.str("totalBytes"))}（${it.dbl("usedPercent")?.toInt() ?: "—"}%）" } ?: "—")
                    KeyValueRow("温度（最高）", temperatures(summary).maxOfOrNull { it.value }?.let { String.format("%.1f °C", it) } ?: "—")
                    KeyValueRow("カーネル", summary.dataString("kernel") ?: "—", mono = true)
                }
                NccPanel(
                    title = "純正サービス",
                    subtitle = if (services.isEmpty()) "サービスの状態を読み込んでいます…" else "${services.size} 件中 ${services.count { it.state == "active" }} 件が稼働中",
                    icon = Icons.Default.Settings,
                    contentPadding = PaddingValues(start = 10.dp, end = 10.dp, top = 0.dp, bottom = 10.dp),
                    headerEnd = { PanelAction(onClick = { scope.launch { reloadServices() } }) { Icon(Icons.Default.Refresh, contentDescription = "サービスの状態を更新") } },
                ) {
                    services.forEach { svc ->
                        val open = openService == svc.id
                        ListRow(
                            title = svc.label.ifBlank { svc.name },
                            subtitle = svc.description.ifBlank { null },
                            trailing = {
                                StatusPill(
                                    when (svc.state) { "active" -> "稼働中"; "failed" -> "異常"; "inactive" -> "停止中"; "activating" -> "起動中"; else -> svc.state },
                                    when (svc.state) { "active" -> Tone.OK; "failed" -> Tone.DANGER; else -> Tone.NEUTRAL },
                                    dot = false,
                                )
                            },
                            onClick = { openService = if (open) null else svc.id },
                        )
                        if (open) {
                            Column(
                                Modifier.fillMaxWidth().padding(start = 6.dp, end = 6.dp, bottom = 8.dp).clip(RoundedCornerShape(14.dp)).background(p.raised).padding(12.dp),
                                verticalArrangement = Arrangement.spacedBy(8.dp),
                            ) {
                                if (svc.impact.isNotBlank()) Text(svc.impact, color = p.warn, style = MaterialTheme.typography.bodySmall)
                                Text("${svc.name}${if (svc.mainPID > 0) " · PID ${svc.mainPID}" else ""}", color = p.faint, fontSize = 11.sp, fontFamily = FontFamily.Monospace)
                                svc.controlReason?.let { HintText(it) }
                                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                    listOf("start" to "起動", "restart" to "再起動", "stop" to "停止").forEach { (act, lbl) ->
                                        val enabled = !busy && svc.loadState == "loaded" && when (act) {
                                            "start" -> svc.canStart && svc.state != "active"
                                            "stop" -> svc.canStop && svc.state != "inactive"
                                            else -> svc.canRestart
                                        }
                                        OutlinedButton(
                                            enabled = enabled,
                                            colors = if (act == "stop") ButtonDefaults.outlinedButtonColors(contentColor = p.danger) else ButtonDefaults.outlinedButtonColors(),
                                            onClick = { confirmService = svc to act },
                                        ) { Text(lbl, fontSize = 12.sp) }
                                    }
                                }
                            }
                        }
                    }
                }
                ListRow(
                    title = "このアプリの接続先と表示",
                    subtitle = "接続先、通知、テーマなどアプリ側の設定",
                    icon = Icons.Default.Settings,
                    onClick = onOpenAppSettings,
                )
            }

            "routing" -> {
                NccPanel(title = "ルーティング", subtitle = "本体の通信経路です（表示のみ）。", icon = Icons.Default.Route) {
                    val routes = summary.dataString("routes")?.trim().orEmpty()
                    if (routes.isEmpty()) HintText("経路を取得できません。")
                    routes.lines().filter { it.isNotBlank() }.forEach { line ->
                        val dev = Regex("dev (\\S+)").find(line)?.groupValues?.get(1)
                        Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(p.raised).padding(10.dp)) {
                            Text(
                                when {
                                    line.startsWith("default") -> "インターネットへの既定の経路"
                                    else -> line.substringBefore(' ')
                                } + (dev?.let { "（${ifaceLabel(it)}）" } ?: ""),
                                color = p.text,
                                style = MaterialTheme.typography.labelLarge,
                            )
                            Text(line, color = p.muted, fontSize = 11.sp, fontFamily = FontFamily.Monospace)
                        }
                    }
                }
            }

            "storage" -> {
                NccPanel(
                    title = "SDカード",
                    subtitle = if (currentPath == ".") "最上位のフォルダー" else currentPath,
                    icon = Icons.Default.Storage,
                    headerEnd = {
                        if (currentPath != ".") {
                            OutlinedButton(onClick = {
                                val parent = currentPath.substringBeforeLast('/', ".").ifBlank { "." }
                                loadFiles(parent)
                            }) {
                                Icon(Icons.Default.ArrowUpward, contentDescription = null, modifier = Modifier.size(15.dp))
                                Text(" 上へ")
                            }
                        }
                    },
                ) {
                    OutlinedTextField(
                        value = fileQuery,
                        onValueChange = { fileQuery = it },
                        label = { Text("フォルダー内を検索") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    previewVideoUrl?.let { url ->
                        VideoPlayerBox(startOffsetMs = 0L, streamUrl = url, apiClient = apiClient)
                        OutlinedButton(onClick = { previewVideoUrl = null }) { Text("プレビューを閉じる") }
                    }
                    val filtered = files.filter { it.name.contains(fileQuery, ignoreCase = true) }
                    filtered.forEach { f ->
                        Surface(
                            shape = RoundedCornerShape(8.dp),
                            color = p.bg,
                            border = BorderStroke(1.dp, p.line),
                            modifier = Modifier.fillMaxWidth().clickable {
                                if (f.directory) loadFiles(f.path)
                                else if (f.media == "video-candidate") {
                                    previewVideoUrl = apiClient.fullUrl("/storage/file?path=${URLEncoder.encode(f.path, "UTF-8")}")
                                }
                            },
                        ) {
                            Row(
                                modifier = Modifier.padding(10.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                            ) {
                                Icon(
                                    imageVector = if (f.directory) Icons.Default.Folder else Icons.Default.Movie,
                                    contentDescription = null,
                                    tint = if (f.directory) p.accent else p.muted,
                                    modifier = Modifier.size(18.dp),
                                )
                                Column(modifier = Modifier.weight(1f)) {
                                    Text(f.name, color = p.text, fontSize = 12.sp, fontWeight = FontWeight.Medium)
                                    Text(
                                        "${Formatters.formatDateTime(f.modified)} · ${if (f.directory) "フォルダー" else Formatters.bytes(f.size)}",
                                        color = p.muted,
                                        fontSize = 10.sp,
                                    )
                                }
                                if (!f.directory) {
                                    IconButton(onClick = {
                                        val url = apiClient.fullUrl("/storage/file?path=${URLEncoder.encode(f.path, "UTF-8")}&download=1")
                                        runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url))) }
                                    }) {
                                        Icon(Icons.Default.Download, contentDescription = "ダウンロード", tint = p.accent, modifier = Modifier.size(18.dp))
                                    }
                                }
                            }
                        }
                    }
                }
            }

            "recorder" -> {
                NccPanel(title = "録画の詳細設定", subtitle = "画質と1ファイルの長さを変更します（H.264）。", icon = Icons.Default.Movie) {
                    val d = recDraft
                    val st = recState
                    if (d == null || st == null) {
                        HintText("録画の詳細設定を取得できません。")
                    } else {
                        KeyValueRow("設定の反映", when { st.applied == true -> "反映済み"; st.hookLoaded -> "確認中"; st.available -> "利用できます"; else -> "利用できません" })
                        OutlinedTextField(
                            value = d.segmentDurationSeconds.toString(),
                            onValueChange = { v -> v.toIntOrNull()?.let { recDraft = d.copy(segmentDurationSeconds = it) } },
                            label = { Text("1ファイルの長さ（秒・純正は180）") },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            OutlinedTextField(
                                value = d.frontBitrateKbps.toString(),
                                onValueChange = { v -> v.toIntOrNull()?.let { recDraft = d.copy(frontBitrateKbps = it) } },
                                label = { Text("前方の画質（kbps）") },
                                singleLine = true,
                                modifier = Modifier.weight(1f),
                            )
                            OutlinedTextField(
                                value = d.cabinBitrateKbps.toString(),
                                onValueChange = { v -> v.toIntOrNull()?.let { recDraft = d.copy(cabinBitrateKbps = it) } },
                                label = { Text("車内の画質（kbps）") },
                                singleLine = true,
                                modifier = Modifier.weight(1f),
                            )
                        }
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.SpaceBetween, modifier = Modifier.fillMaxWidth()) {
                            Text("車内・後方カメラを録画する", color = p.text, fontSize = 13.sp)
                            Switch(checked = d.cabinRecording, onCheckedChange = { recDraft = d.copy(cabinRecording = it) })
                        }
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Checkbox(checked = recApplyNow, onCheckedChange = { recApplyNow = it })
                            Text("すぐに反映する（数秒間、録画が止まります）", color = p.warn, fontSize = 12.sp)
                        }
                        Button(
                            enabled = !busy && st.available,
                            onClick = {
                                scope.launch {
                                    busy = true
                                    error = ""
                                    val cfgEl = apiClient.json.encodeToJsonElement(RecorderAdvancedConfig.serializer(), d)
                                    val body = buildJsonObject {
                                        put("expectedGeneration", st.generation)
                                        put("settings", cfgEl)
                                        put("applyNow", recApplyNow)
                                        put("confirmation", if (recApplyNow) "即時適用する" else "")
                                        put("acknowledgeRecordingGap", recApplyNow)
                                    }.toString()
                                    runCatching { apiClient.put<RecorderAdvancedState>("/device/recorder-advanced", body) }
                                        .onSuccess {
                                            recState = it
                                            recDraft = it.settings
                                            recApplyNow = false
                                            notice = it.message ?: "録画設定を保存しました"
                                        }
                                        .onFailure { error = it.message ?: "保存に失敗しました" }
                                    busy = false
                                }
                            },
                        ) {
                            Icon(Icons.Default.Save, contentDescription = null)
                            Text(" 録画設定を保存")
                        }
                    }
                }
            }

            "led" -> {
                // Front LED control
                NccPanel(title = "前面LED（5か所のライト）", icon = Icons.Default.Lightbulb) {
                    SectionTabs(
                        tabs = listOf("solid" to "単色", "rainbow" to "レインボー"),
                        selectedId = ledMode,
                        onSelect = { ledMode = it },
                    )
                    if (ledMode == "solid") {
                        val presets = listOf(
                            listOf(255, 255, 255) to Color.White,
                            listOf(64, 160, 255) to Color(0xFF40A0FF),
                            listOf(46, 204, 113) to Color(0xFF2ECC71),
                            listOf(255, 160, 32) to Color(0xFFFFA020),
                            listOf(231, 76, 60) to Color(0xFFE74C3C),
                            listOf(180, 90, 255) to Color(0xFFB45AFF),
                        )
                        Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                            Text("色", color = p.muted, fontSize = 12.sp)
                            presets.forEach { (rgb, c) ->
                                Box(
                                    modifier = Modifier
                                        .size(30.dp)
                                        .clip(CircleShape)
                                        .background(c)
                                        .clickable { ledRgb = rgb },
                                )
                            }
                        }
                    }
                    FieldLabel("明るさ", trailing = "${(ledBrightness * 100 / 255)}%")
                    Slider(
                        value = ledBrightness.toFloat(),
                        onValueChange = { ledBrightness = it.toInt() },
                        valueRange = 1f..255f,
                    )
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(checked = ledContinuous, onCheckedChange = { ledContinuous = it })
                        Text("点灯し続ける（通常設定として保存）", color = p.text, fontSize = 12.sp)
                    }
                    if (!ledContinuous) {
                        SectionTabs(
                            tabs = listOf("5" to "5秒", "10" to "10秒", "30" to "30秒"),
                            selectedId = ledTestSec.toString(),
                            onSelect = { ledTestSec = it.toIntOrNull() ?: 5 },
                        )
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Button(
                            enabled = !busy,
                            onClick = {
                                scope.launch {
                                    busy = true
                                    error = ""
                                    runCatching {
                                        if (ledContinuous) {
                                            val rev = ledData?.settings?.revision ?: 1
                                            apiClient.requestRaw("/device/led/test/stop", "POST", "{}")
                                            val settingsEl = apiClient.json.encodeToJsonElement(
                                                LEDSettingsData.serializer(),
                                                (ledData?.settings ?: LEDSettingsData()).copy(
                                                    mode = ledMode,
                                                    brightness = ledBrightness,
                                                    rgb = ledRgb,
                                                ),
                                            )
                                            val body = buildJsonObject {
                                                put("expectedRevision", rev)
                                                put("settings", settingsEl)
                                            }.toString()
                                            apiClient.requestRaw("/device/led", "PUT", body)
                                            notice = "前面LEDを連続点灯に設定しました"
                                        } else {
                                            val body = buildJsonObject {
                                                put("settings", buildJsonObject {
                                                    put("mode", ledMode)
                                                    put("effect", "steady")
                                                    put("rgb", buildJsonArray { ledRgb.forEach { add(JsonPrimitive(it)) } })
                                                    put("brightness", ledBrightness)
                                                })
                                                put("durationMs", ledTestSec * 1000)
                                            }.toString()
                                            apiClient.requestRaw("/device/led/test", "POST", body)
                                            notice = "${ledTestSec}秒間LEDを点灯します"
                                        }
                                    }.onFailure { error = it.message ?: "LED操作に失敗しました" }
                                    busy = false
                                }
                            },
                        ) { Text("点灯") }
                        OutlinedButton(
                            enabled = !busy,
                            onClick = {
                                scope.launch {
                                    busy = true
                                    runCatching { apiClient.requestRaw("/device/led/test/stop", "POST", "{}") }
                                        .onSuccess { notice = "前面LEDを消灯しました" }
                                        .onFailure { error = it.message ?: "消灯に失敗しました" }
                                    busy = false
                                }
                            },
                        ) { Text("消灯") }
                    }
                }

                // Native audio
                nativeAudio?.let { aud ->
                    NccPanel(title = "本体の音声設定", icon = Icons.AutoMirrored.Filled.VolumeUp) {
                        KeyValueRow("今の音量", aud.masterVolume.toString())
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text("消音（ミュート）", color = p.text, fontSize = 13.sp, modifier = Modifier.weight(1f))
                            Switch(
                                checked = aud.mute.enabled,
                                enabled = !busy && aud.writable,
                                onCheckedChange = { muteOn ->
                                    scope.launch {
                                        busy = true
                                        val nextVal = if (muteOn) 0 else 1
                                        runCatching {
                                            val obs = apiClient.put<Observation>(
                                                "/device/native-audio/mute",
                                                """{"expectedValue":${aud.mute.value},"value":$nextVal}""",
                                            )
                                            obs.data?.let { nativeAudio = apiClient.json.decodeFromJsonElement(NativeAudioData.serializer(), it) }
                                            notice = "消音設定を更新しました"
                                        }.onFailure { error = it.message ?: "消音設定に失敗しました" }
                                        busy = false
                                    }
                                },
                            )
                        }
                        SectionTabs(
                            tabs = listOf("0" to "補正なし", "1" to "補正 弱", "2" to "補正 強"),
                            fill = true,
                            selectedId = aud.correction.value.toString(),
                            onSelect = { sel ->
                                val nextVal = sel.toIntOrNull() ?: return@SectionTabs
                                if (!aud.writable || nextVal == aud.correction.value) return@SectionTabs
                                scope.launch {
                                    busy = true
                                    runCatching {
                                        val obs = apiClient.put<Observation>(
                                            "/device/native-audio/volume-correction",
                                            """{"expectedValue":${aud.correction.value},"value":$nextVal}""",
                                        )
                                        obs.data?.let { nativeAudio = apiClient.json.decodeFromJsonElement(NativeAudioData.serializer(), it) }
                                        notice = "音量補正を更新しました"
                                    }.onFailure { error = it.message ?: "音量補正に失敗しました" }
                                    busy = false
                                }
                            },
                        )
                    }
                }

                // VOICEVOX API settings
                NccPanel(title = "音声合成（VOICEVOX）", subtitle = "ナンバー検知の読み上げに使います。", icon = Icons.Default.RecordVoiceOver) {
                    OutlinedTextField(
                        value = vvEndpoint,
                        onValueChange = { vvEndpoint = it },
                        label = { Text("接続先URL") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = vvKey,
                        onValueChange = { vvKey = it },
                        label = { Text("APIキー（${if (vvConfig?.keyConfigured == true) "保存済み" else "未設定"}）") },
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Button(
                            enabled = !busy && vvEndpoint.isNotBlank(),
                            onClick = {
                                scope.launch {
                                    busy = true
                                    error = ""
                                    val body = buildJsonObject {
                                        put("endpoint", vvEndpoint.trim())
                                        if (vvKey != vvConfig?.apiKey) put("key", vvKey.trim())
                                    }.toString()
                                    runCatching { apiClient.put<VoicevoxConfig>("/voicevox", body) }
                                        .onSuccess { vvConfig = it; notice = "VOICEVOX設定を保存しました" }
                                        .onFailure { error = it.message ?: "保存に失敗しました" }
                                    busy = false
                                }
                            },
                        ) { Text("保存") }
                        OutlinedButton(
                            enabled = !busy && vvConfig?.keyConfigured == true,
                            onClick = {
                                scope.launch {
                                    busy = true
                                    runCatching { apiClient.post<VoicevoxCheckResponse>("/voicevox/check", "{}") }
                                        .onSuccess { vvPoints = "残り ${it.points} ポイント（約${it.resetInHours.toInt()}時間後に回復）" }
                                        .onFailure { error = it.message ?: "接続確認に失敗しました" }
                                    busy = false
                                }
                            },
                        ) { Text("残りポイントを確認") }
                    }
                    if (vvPoints.isNotBlank()) Text(vvPoints, color = p.ok, fontSize = 12.sp)
                }
            }

            "usb" -> {
                NccPanel(title = "USB", icon = Icons.Default.Usb) {
                    val role = summary.dataString("usb")
                    KeyValueRow("USBの動作", when (role) { "peripheral" -> "周辺機器モード（PCとつなぐ）"; "host" -> "ホストモード（機器をつなぐ）"; null -> "未取得"; else -> role })
                    HintText("ホストモードへの切り替えはPCとの接続（ADB）が切れるため、この画面からは行えません。")
                }
            }

            "logs" -> {
                NccPanel(
                    title = "操作履歴",
                    subtitle = "この管理機能で行った操作の記録です（新しい順）。",
                    icon = Icons.Default.History,
                    headerEnd = { PanelAction(onClick = { scope.launch { runCatching { logsText = apiClient.requestRaw("/logs", "GET") } } }) { Icon(Icons.Default.Refresh, contentDescription = "操作履歴を更新") } },
                ) {
                    val entries = logsText.lines().filter { it.isNotBlank() }.takeLast(200).reversed().map { line ->
                        runCatching { apiClient.json.parseToJsonElement(line) as JsonObject }.getOrNull()
                    }
                    if (entries.isEmpty()) HintText("操作履歴はありません。")
                    entries.forEach { e ->
                        Row(Modifier.fillMaxWidth().padding(vertical = 2.dp), verticalAlignment = Alignment.CenterVertically) {
                            Text(Formatters.formatDateTimeShort(e.str("at")), color = p.muted, fontSize = 11.sp, modifier = Modifier.width(78.dp))
                            Text(e.str("action") ?: "—", color = p.text, fontSize = 12.sp, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f), maxLines = 1)
                            Text(e.str("result").orEmpty(), color = if (e.str("result") in setOf("failed", "error", "denied")) p.danger else p.muted, fontSize = 11.sp)
                        }
                    }
                }
            }

            "nas" -> {
                val ns = nasState
                NccPanel(title = "録画バックアップ", subtitle = "録画をNASへ自動で保存します。詳細な設定はWeb UIで行えます。", icon = Icons.Default.Backup) {
                    if (ns == null) {
                        HintText("録画バックアップの設定を取得できません。")
                    } else {
                        ns.job?.let { job ->
                            val cur = job.progress.bytes.toDoubleOrNull() ?: 0.0
                            val tot = job.progress.totalBytes.toDoubleOrNull()?.coerceAtLeast(1.0) ?: 1.0
                            KeyValueRow("転送中", job.originalName)
                            LinearProgressIndicator(progress = { (cur / tot).toFloat().coerceIn(0f, 1f) }, modifier = Modifier.fillMaxWidth())
                            if (job.active && job.canCancel) {
                                OutlinedButton(onClick = {
                                    scope.launch {
                                        runCatching { apiClient.requestRaw("/nas-backup/jobs/${job.id}", "DELETE") }
                                            .onSuccess { notice = "転送を中断しました" }
                                    }
                                }) { Text("転送を中断") }
                            }
                        }
                        listOf("video" to "通常の録画", "event" to "イベント録画").forEach { (kind, lbl) ->
                            val pol = ns.settings.policies[kind]
                            if (pol != null) {
                                Surface(
                                    shape = RoundedCornerShape(8.dp),
                                    color = p.bg,
                                    border = BorderStroke(1.dp, p.line),
                                    modifier = Modifier.fillMaxWidth(),
                                ) {
                                    Column(modifier = Modifier.padding(10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
                                            Text(lbl, color = p.text, fontWeight = FontWeight.Bold, fontSize = 13.sp)
                                            StatusPill(if (pol.enabled) "自動でバックアップ（${routeLabel(pol.route)}）" else "停止", if (pol.enabled) Tone.OK else Tone.NEUTRAL, dot = false)
                                        }
                                        val tgt = pol.targets[pol.route]
                                        KeyValueRow("保存先", tgt?.host?.ifBlank { "未設定" } ?: "未設定")
                                        ns.automatic[kind]?.reason?.let { Text(it, color = p.muted, fontSize = 11.sp) }
                                        if (tgt?.configured == true) {
                                            OutlinedButton(onClick = {
                                                scope.launch {
                                                    busy = true
                                                    val body = """{"expectedRevision":${ns.settings.revision},"kind":"$kind","route":"${pol.route}"}"""
                                                    runCatching { apiClient.requestRaw("/nas-backup/test", "POST", body) }
                                                        .onSuccess { notice = "${lbl}のNAS接続を確認しました" }
                                                        .onFailure { error = it.message ?: "NAS接続確認に失敗しました" }
                                                    busy = false
                                                }
                                            }) { Text("接続確認") }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }

            "settings" -> {
                // OTA guard
                NccPanel(
                    title = "本体ソフトウェアの自動更新",
                    subtitle = "純正の自動更新でこの管理機能が使えなくなるのを防ぎます。",
                    icon = Icons.Default.Security,
                    headerEnd = {
                        otaPolicy?.let { ota ->
                            NccTag(
                                text = if (ota.disabled) "更新を止めています" else "更新が有効",
                                color = if (ota.disabled) p.ok else p.danger,
                                background = if (ota.disabled) p.okSoft else p.dangerSoft,
                            )
                        }
                    },
                ) {
                    val ota = otaPolicy
                    if (ota != null) {
                        KeyValueRow("設定の保護", if (ota.protected == true) "保護されています" else "未確認")
                        ota.guard?.let { g ->
                            KeyValueRow("更新の通信", if (g.network.active) "遮断中（${g.network.rejectedPackets}件）" else "遮断していません")
                            KeyValueRow("本体ソフトウェア", if (g.staging.firmwareKnown) "変化なし" else "変化を検出")
                        }
                        if (ota.writable) {
                            OutlinedButton(
                                enabled = !busy,
                                onClick = {
                                    scope.launch {
                                        busy = true
                                        val nextDisabled = !ota.disabled
                                        runCatching {
                                            val obs = apiClient.put<Observation>(
                                                "/device/ota-policy",
                                                """{"expectedToken":"${ota.token}","disabled":$nextDisabled}""",
                                            )
                                            obs.data?.let { otaPolicy = apiClient.json.decodeFromJsonElement(OtaPolicyData.serializer(), it) }
                                            notice = if (nextDisabled) "更新防止を有効にしました" else "更新許可へ変更しました"
                                        }.onFailure { error = it.message ?: "OTA設定の変更に失敗しました" }
                                        busy = false
                                    }
                                },
                            ) {
                                Text(if (ota.disabled) "自動更新を許可する（おすすめしません）" else "自動更新を止める")
                            }
                        }
                    }
                }

                // Map colours (shared with the Web UI) and the Web UI theme. Saved without touching other settings.
                NccPanel(title = "地図と表示", subtitle = "地図の色はこのアプリとWeb UIで共通です。", icon = Icons.Default.Palette) {
                    val ws = webSettings
                    listOf("dashboard" to "ダッシュボード", "journey" to "走行履歴", "playback" to "録画再生", "plates" to "ナンバー記録").forEach { (view, label) ->
                        FieldLabel(label)
                        SectionTabs(
                            tabs = listOf("auto" to "自動", "mist" to "青灰", "night" to "濃紺", "sage" to "緑灰", "colorful" to "カラー"),
                            selectedId = ws?.mapThemes?.get(view) ?: "auto",
                            enabled = ws != null && !busy,
                            onSelect = { theme ->
                                scope.launch {
                                    busy = true
                                    val next = (ws?.mapThemes.orEmpty() + (view to theme))
                                    runCatching {
                                        apiClient.updateSettings(mapOf("mapThemes" to JsonObject(next.mapValues { JsonPrimitive(it.value) })))
                                    }.onSuccess {
                                        webSettings = apiClient.get<WebSettings>("/settings")
                                        onWebSettingsChanged()
                                        notice = "${label}の地図の色を変更しました"
                                    }.onFailure { error = it.message ?: "保存できませんでした" }
                                    busy = false
                                }
                            },
                        )
                    }
                    HintText("自動はライト・ダークの表示に合わせます。")
                    FieldLabel("Web UIのテーマ")
                    SectionTabs(
                        tabs = listOf("system" to "端末に合わせる", "light" to "ライト", "dark" to "ダーク"),
                        selectedId = ws?.theme ?: "system",
                        enabled = ws != null && !busy,
                        onSelect = { theme ->
                            scope.launch {
                                busy = true
                                runCatching { apiClient.updateSettings(mapOf("theme" to JsonPrimitive(theme))) }
                                    .onSuccess { webSettings = apiClient.get<WebSettings>("/settings"); notice = "Web UIのテーマを変更しました" }
                                    .onFailure { error = it.message ?: "保存できませんでした" }
                                busy = false
                            }
                        },
                        fill = true,
                    )
                    HintText("このアプリのテーマは「このアプリの接続先と表示」で変更できます。")
                }
            }
        }
    }

    confirmService?.let { (svc, act) ->
        val verb = when (act) { "start" -> "起動"; "stop" -> "停止"; else -> "再起動" }
        ConfirmDialog(
            title = "${svc.label.ifBlank { svc.name }}を${verb}しますか？",
            message = listOfNotNull(svc.description.ifBlank { null }, svc.impact.ifBlank { null }).joinToString("\n").ifBlank { "本体のサービスを${verb}します。" },
            confirmLabel = verb,
            destructive = act != "start",
            onDismiss = { confirmService = null },
            onConfirm = {
                scope.launch {
                    busy = true
                    error = ""
                    val body = """{"action":"$act","expectedInvocationID":"${svc.invocationID}","expectedState":"${svc.state}"}"""
                    runCatching { apiClient.requestRaw("/system/services/${svc.id}/actions", "POST", body) }
                        .onSuccess { notice = "${svc.label.ifBlank { svc.name }}を${verb}しました"; onRefreshSummary() }
                        .onFailure { error = it.message ?: "サービスを操作できませんでした" }
                    reloadServices()
                    busy = false
                }
            },
        )
    }
}

private fun ifaceLabel(dev: String) = when {
    dev.startsWith("usb0") -> "4G"
    dev.startsWith("wlan") -> "Wi-Fi"
    dev.startsWith("nccap") -> "ホットスポット"
    dev.startsWith("tailscale") -> "Tailscale"
    dev.startsWith("bridge") -> "純正の内部ネットワーク"
    else -> dev
}

private fun routeLabel(route: String) = when (route) { "wifi" -> "Wi-Fi"; "cellular" -> "4G"; "tailscale" -> "Tailscale"; else -> route }

