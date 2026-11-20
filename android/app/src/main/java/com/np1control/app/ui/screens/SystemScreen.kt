package com.np1control.app.ui.screens

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
        "logs" to "ログ",
        "nas" to "NASバックアップ",
        "settings" to "OTA・表示設定",
    )

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (error.isNotBlank()) NccBanner(error, BannerTone.ERROR, onDismiss = { error = "" })
        if (notice.isNotBlank()) NccBanner(notice, BannerTone.SUCCESS, onDismiss = { notice = "" })

        Row(
            modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            tabs.forEach { (id, label) ->
                val sel = section == id
                NccTag(
                    text = label,
                    color = if (sel) p.accent else p.muted,
                    background = if (sel) p.soft else p.paper,
                    modifier = Modifier.clickable { section = id },
                )
            }
        }

        when (section) {
            "overview" -> {
                NccPanel(
                    title = "本体情報",
                    headerEnd = {
                        OutlinedButton(onClick = onOpenAppSettings) { Text("アプリ接続設定") }
                    },
                ) {
                    val uptimeSec = (summary["uptime"]?.data as? JsonPrimitive)?.doubleOrNull
                    val kernel = (summary["kernel"]?.data as? JsonPrimitive)?.contentOrNull ?: "—"
                    val load = (summary["load"]?.data as? JsonPrimitive)?.contentOrNull ?: "—"
                    val mem = (summary["memory"]?.data as? JsonPrimitive)?.contentOrNull.orEmpty()
                    KeyValueRow("稼働時間", Formatters.uptime(uptimeSec))
                    KeyValueRow("カーネル", kernel, mono = true)
                    KeyValueRow("Load average", load.trim().split(Regex("\\s+")).take(3).joinToString(" / "), mono = true)
                    if (mem.isNotBlank()) {
                        Text(mem.lines().take(5).joinToString("\n"), color = p.muted, fontSize = 11.sp, fontFamily = FontFamily.Monospace)
                    }
                }

                NccPanel(
                    title = "純正サービス (${services.count { it.state == "active" }} / ${services.size} 稼働中)",
                    headerEnd = {
                        IconButton(onClick = {
                            scope.launch {
                                runCatching {
                                    val obs = apiClient.get<Observation>("/system/services")
                                    obs.data?.let { services = apiClient.json.decodeFromJsonElement(ListSerializer(StockService.serializer()), it) }
                                }
                            }
                        }) {
                            Icon(Icons.Default.Refresh, contentDescription = "更新", tint = p.muted)
                        }
                    },
                ) {
                    services.forEach { svc ->
                        Surface(
                            shape = RoundedCornerShape(8.dp),
                            color = p.bg,
                            border = BorderStroke(1.dp, p.line),
                            modifier = Modifier.fillMaxWidth(),
                        ) {
                            Column(modifier = Modifier.padding(10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                Row(
                                    modifier = Modifier.fillMaxWidth(),
                                    horizontalArrangement = Arrangement.SpaceBetween,
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    Column(modifier = Modifier.weight(1f)) {
                                        Text(svc.label.ifBlank { svc.name }, color = p.text, fontWeight = FontWeight.SemiBold, fontSize = 13.sp)
                                        Text("${svc.name} · PID ${svc.mainPID}", color = p.muted, fontSize = 11.sp, fontFamily = FontFamily.Monospace)
                                    }
                                    NccTag(
                                        text = if (svc.state == "active") "稼働中" else svc.state,
                                        color = if (svc.state == "active") p.ok else if (svc.state == "failed") p.danger else p.muted,
                                        background = if (svc.state == "active") p.okSoft else p.paper,
                                    )
                                }
                                if (svc.description.isNotBlank()) Text(svc.description, color = p.muted, fontSize = 11.sp)
                                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                    listOf("start" to "起動", "stop" to "停止", "restart" to "再起動").forEach { (act, lbl) ->
                                        val enabled = !busy && svc.loadState == "loaded" && when (act) {
                                            "start" -> svc.canStart && svc.state != "active"
                                            "stop" -> svc.canStop && svc.state != "inactive"
                                            else -> svc.canRestart
                                        }
                                        OutlinedButton(
                                            enabled = enabled,
                                            onClick = {
                                                scope.launch {
                                                    busy = true
                                                    error = ""
                                                    val body = """{"action":"$act","expectedInvocationID":"${svc.invocationID}","expectedState":"${svc.state}"}"""
                                                    runCatching { apiClient.requestRaw("/system/services/${svc.id}/actions", "POST", body) }
                                                        .onSuccess {
                                                            notice = "${svc.label} を${lbl}しました"
                                                            onRefreshSummary()
                                                            val obs = apiClient.get<Observation>("/system/services")
                                                            obs.data?.let { services = apiClient.json.decodeFromJsonElement(ListSerializer(StockService.serializer()), it) }
                                                        }
                                                        .onFailure { error = it.message ?: "サービス操作に失敗しました" }
                                                    busy = false
                                                }
                                            },
                                        ) { Text(lbl, fontSize = 12.sp) }
                                    }
                                }
                            }
                        }
                    }
                }
            }

            "routing" -> {
                NccPanel(title = "ルーティングテーブル (READ ONLY)") {
                    val routes = (summary["routes"]?.data as? JsonPrimitive)?.contentOrNull ?: "未取得"
                    Text(routes, color = p.muted, fontSize = 11.sp, fontFamily = FontFamily.Monospace)
                }
            }

            "storage" -> {
                NccPanel(
                    title = "SDカード / ${if (currentPath == ".") "" else currentPath}",
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
                        VideoPlayerBox(streamUrl = url, apiClient = apiClient)
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
                NccPanel(title = "H.264詳細録画設定", icon = Icons.Default.Movie) {
                    val d = recDraft
                    val st = recState
                    if (d == null || st == null) {
                        Text("録画詳細設定を取得できません。", color = p.muted, fontSize = 12.sp)
                    } else {
                        KeyValueRow("フック状態", if (st.applied == true) "適用済み" else if (st.hookLoaded) "確認中" else "利用可能=${st.available}")
                        OutlinedTextField(
                            value = d.segmentDurationSeconds.toString(),
                            onValueChange = { v -> v.toIntOrNull()?.let { recDraft = d.copy(segmentDurationSeconds = it) } },
                            label = { Text("ファイル分割時間 (秒, 純正180)") },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            OutlinedTextField(
                                value = d.frontBitrateKbps.toString(),
                                onValueChange = { v -> v.toIntOrNull()?.let { recDraft = d.copy(frontBitrateKbps = it) } },
                                label = { Text("前方ビットレート(kbps)") },
                                singleLine = true,
                                modifier = Modifier.weight(1f),
                            )
                            OutlinedTextField(
                                value = d.cabinBitrateKbps.toString(),
                                onValueChange = { v -> v.toIntOrNull()?.let { recDraft = d.copy(cabinBitrateKbps = it) } },
                                label = { Text("車内ビットレート(kbps)") },
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
                            Text("即時適用する（数秒間録画が中断します）", color = p.warn, fontSize = 12.sp)
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
                            Text("色:", color = p.muted, fontSize = 12.sp)
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
                    Text("明るさ: ${(ledBrightness * 100 / 255)}%", color = p.text, fontSize = 12.sp)
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
                        KeyValueRow("現在の音量", aud.masterVolume.toString())
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text("消音 (ミュート)", color = p.text, fontSize = 13.sp)
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
                            tabs = listOf("0" to "補正 OFF", "1" to "補正 弱", "2" to "補正 強"),
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
                NccPanel(title = "音声合成 (VOICEVOX API)") {
                    OutlinedTextField(
                        value = vvEndpoint,
                        onValueChange = { vvEndpoint = it },
                        label = { Text("エンドポイントURL") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = vvKey,
                        onValueChange = { vvKey = it },
                        label = { Text("APIキー (${if (vvConfig?.keyConfigured == true) "設定済み" else "未設定"})") },
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
                        ) { Text("ポイント確認") }
                    }
                    if (vvPoints.isNotBlank()) Text(vvPoints, color = p.ok, fontSize = 12.sp)
                }
            }

            "usb" -> {
                NccPanel(title = "USB Role") {
                    Text(summary["usb"]?.data?.toString() ?: "未取得", color = p.muted, fontSize = 12.sp, fontFamily = FontFamily.Monospace)
                    NccBanner("USB host切替はADBを切断するため無効です。", BannerTone.NEUTRAL)
                }
            }

            "logs" -> {
                NccPanel(
                    title = "操作履歴 (audit.jsonl)",
                    headerEnd = {
                        IconButton(onClick = {
                            scope.launch { runCatching { logsText = apiClient.requestRaw("/logs", "GET") } }
                        }) {
                            Icon(Icons.Default.Refresh, contentDescription = "更新", tint = p.muted)
                        }
                    },
                ) {
                    Text(
                        logsText.ifBlank { "操作履歴がありません" },
                        color = p.muted,
                        fontSize = 11.sp,
                        fontFamily = FontFamily.Monospace,
                    )
                }
            }

            "nas" -> {
                val ns = nasState
                NccPanel(title = "録画のNASバックアップ", icon = Icons.Default.Backup) {
                    if (ns == null) {
                        Text("NASバックアップ設定を取得できません。", color = p.muted, fontSize = 12.sp)
                    } else {
                        ns.job?.let { job ->
                            val cur = job.progress.bytes.toDoubleOrNull() ?: 0.0
                            val tot = job.progress.totalBytes.toDoubleOrNull()?.coerceAtLeast(1.0) ?: 1.0
                            KeyValueRow("転送中ジョブ", "${job.originalName} (${job.status})")
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
                        listOf("video" to "常時録画", "event" to "緊急録画").forEach { (kind, lbl) ->
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
                                            NccTag(text = if (pol.enabled) "自動ON (${pol.route})" else "OFF")
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
                    title = "本体の自動更新 (OTA抑止)",
                    icon = Icons.Default.Security,
                    headerEnd = {
                        otaPolicy?.let { ota ->
                            NccTag(
                                text = if (ota.disabled) "更新を防止中" else "更新を許可",
                                color = if (ota.disabled) p.ok else p.danger,
                                background = if (ota.disabled) p.okSoft else p.dangerSoft,
                            )
                        }
                    },
                ) {
                    val ota = otaPolicy
                    if (ota != null) {
                        KeyValueRow("停止設定の保護", if (ota.protected == true) "保護済み" else "未確認")
                        ota.guard?.let { g ->
                            KeyValueRow("更新通信の遮断", if (g.network.active) "有効 (遮断 ${g.network.rejectedPackets}件)" else "無効")
                            KeyValueRow("ファームウェア変化", if (g.staging.firmwareKnown) "変化なし" else "変化あり")
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
                                Text(if (ota.disabled) "更新を許可する（非推奨）" else "更新を防止する")
                            }
                        }
                    }
                }

                // Web settings
                NccPanel(title = "NP1本体側の表示設定") {
                    SectionTabs(
                        tabs = listOf("system" to "システム", "light" to "ライト", "dark" to "ダーク"),
                        selectedId = webThemeDraft,
                        onSelect = { webThemeDraft = it },
                    )
                    Button(
                        enabled = !busy && webSettings != null && webThemeDraft != webSettings?.theme,
                        onClick = {
                            scope.launch {
                                val cur = webSettings ?: return@launch
                                busy = true
                                val body = buildJsonObject {
                                    put("expectedRevision", cur.revision)
                                    put("settings", buildJsonObject {
                                        put("revision", cur.revision)
                                        put("theme", webThemeDraft)
                                        put("locale", cur.locale)
                                        put("favorites", buildJsonArray { cur.favorites.forEach { add(JsonPrimitive(it)) } })
                                        put("cellularLimitBytes", cur.cellularLimitBytes)
                                    })
                                }.toString()
                                runCatching { apiClient.put<WebSettings>("/settings", body) }
                                    .onSuccess { webSettings = it; notice = "表示設定を保存しました" }
                                    .onFailure { error = it.message ?: "保存に失敗しました" }
                                busy = false
                            }
                        },
                    ) { Text("テーマ設定を本体へ保存") }
                }
            }
        }
        Spacer(modifier = Modifier.height(12.dp))
    }
}
