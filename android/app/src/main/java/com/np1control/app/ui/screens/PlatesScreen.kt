package com.np1control.app.ui.screens

import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.material.icons.filled.CloudUpload
import androidx.compose.material.icons.filled.DocumentScanner
import androidx.compose.material.icons.filled.FilterAlt
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Place
import androidx.compose.material.icons.filled.Share
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material3.MaterialTheme
import androidx.compose.ui.text.input.ImeAction
import com.np1control.app.ui.components.EmptyState
import com.np1control.app.ui.components.FieldLabel
import com.np1control.app.ui.components.HintText
import com.np1control.app.ui.components.PanelAction
import com.np1control.app.ui.components.ScreenColumn
import com.np1control.app.ui.components.SwitchRow
import com.np1control.app.ui.components.Tone
import com.np1control.app.ui.components.toneColor
import com.np1control.app.ui.components.toneSoft
import com.np1control.app.ui.map.MapFocus
import com.np1control.app.ui.map.MapPoint
import com.np1control.app.ui.map.NccMap
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Image
import androidx.compose.material.icons.filled.NotificationsActive
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Save
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.automirrored.filled.VolumeUp
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Slider
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import coil.ImageLoader
import coil.compose.AsyncImage
import coil.request.ImageRequest
import com.np1control.app.data.AppConfig
import com.np1control.app.data.Formatters
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.NumberShareSightingsResponse
import com.np1control.app.data.NumberShareStatus
import com.np1control.app.data.NumberShareUploadResponse
import com.np1control.app.data.NumberShareVehiclesResponse
import com.np1control.app.data.PlateLED
import com.np1control.app.data.PlatePopupFilter
import com.np1control.app.data.PlateRecord
import com.np1control.app.data.PlateRecordEnvelope
import com.np1control.app.data.PlateRecordsResponse
import com.np1control.app.data.PlateRule
import com.np1control.app.data.PlateSettings
import com.np1control.app.data.PlateSpeechResponse
import com.np1control.app.data.PlateStatus
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.JapanesePlateBadge
import com.np1control.app.ui.components.KeyValueRow
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.NccTag
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.launch
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import java.net.URLEncoder
import kotlin.random.Random

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun PlatesScreen(
    apiClient: NccApiClient,
    config: AppConfig,
    highlightRecordId: String? = null,
    isWide: Boolean,
    mapThemes: Map<String, String> = emptyMap(),
    onPopupFilterChanged: (PlatePopupFilter) -> Unit,
    onEmitTestPopup: (Boolean) -> Unit,
    onOpenRecordingAt: (String) -> Unit,
) {
    val p = LocalNccPalette.current
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val imageLoader = remember(apiClient) {
        ImageLoader.Builder(context).okHttpClient(apiClient.okHttpClient).build()
    }

    var tab by remember { mutableStateOf("records") } // records | share | rules | status
    var status by remember { mutableStateOf<PlateStatus?>(null) }
    var speech by remember { mutableStateOf<PlateSpeechResponse?>(null) }
    var settings by remember { mutableStateOf<PlateSettings?>(null) }
    var draft by remember { mutableStateOf<PlateSettings?>(null) }

    // Records tab state
    var fromDay by remember { mutableStateOf(Formatters.jstDay(-6)) }
    var toDay by remember { mutableStateOf(Formatters.jstDay(0)) }
    var query by remember { mutableStateOf("") }
    var matchedOnly by remember { mutableStateOf(false) }
    var rangeDays by remember { mutableStateOf(6) }
    var truncated by remember { mutableStateOf(false) }
    var listReason by remember { mutableStateOf("") }
    var records by remember { mutableStateOf<List<PlateRecordEnvelope>>(emptyList()) }
    var selectedId by remember(highlightRecordId) { mutableStateOf(highlightRecordId.orEmpty()) }
    var imageModalRecord by remember { mutableStateOf<PlateRecord?>(null) }

    // NumberShare tab state
    var shareStatus by remember { mutableStateOf<NumberShareStatus?>(null) }
    var shareSightings by remember { mutableStateOf<NumberShareSightingsResponse?>(null) }
    var shareVehicles by remember { mutableStateOf<NumberShareVehiclesResponse?>(null) }
    var shareEndpoint by remember { mutableStateOf("https://nmch.jp/app/numbershare/api/") }
    var shareEnabled by remember { mutableStateOf(false) }
    var shareKey by remember { mutableStateOf("") }
    var shareUserHandle by remember { mutableStateOf("") }
    var shareBootstrapKey by remember { mutableStateOf("") }
    var shareQuery by remember { mutableStateOf("") }
    var shareScope by remember { mutableStateOf("all") }
    var vehicleSerial by remember { mutableStateOf("") }

    var loading by remember { mutableStateOf(false) }
    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var notice by remember { mutableStateOf("") }

    fun loadStatusAndSettings() {
        scope.launch {
            runCatching { apiClient.get<PlateStatus>("/plate/status") }.onSuccess { status = it }
            runCatching { apiClient.get<PlateSpeechResponse>("/plate/speech") }.onSuccess { speech = it }
            runCatching { apiClient.get<PlateSettings>("/plate/settings") }.onSuccess {
                settings = it
                if (draft == null) draft = it
            }
        }
    }

    fun loadRecords() {
        scope.launch {
            loading = true
            error = ""
            val params = buildList {
                add("from=${URLEncoder.encode(fromDay, "UTF-8")}")
                add("to=${URLEncoder.encode(toDay, "UTF-8")}")
                if (matchedOnly) add("matched=true")
                if (query.isNotBlank()) add("q=${URLEncoder.encode(query.trim(), "UTF-8")}")
            }.joinToString("&")
            runCatching { apiClient.get<PlateRecordsResponse>("/plate/records?$params&limit=500") }
                .onSuccess { records = it.records; truncated = it.truncated; listReason = it.reason }
                .onFailure { error = it.message ?: "ナンバー記録を取得できません" }
            loading = false
        }
    }

    fun loadShare() {
        scope.launch {
            runCatching { apiClient.get<NumberShareStatus>("/plate/share/status") }
                .onSuccess { st ->
                    shareStatus = st
                    if (st.config.endpoint.isNotBlank()) shareEndpoint = st.config.endpoint
                    shareEnabled = st.config.enabled
                    if (st.config.keyConfigured) {
                        runCatching {
                            val qs = buildList {
                                if (shareQuery.isNotBlank()) add("q=${URLEncoder.encode(shareQuery.trim(), "UTF-8")}")
                                if (shareScope != "all") add("scope=$shareScope")
                            }.joinToString("&")
                            apiClient.get<NumberShareSightingsResponse>("/plate/share/sightings?$qs")
                        }.onSuccess { shareSightings = it }
                        runCatching {
                            val vq = if (vehicleSerial.isNotBlank()) "?serialNumber=${URLEncoder.encode(vehicleSerial.trim(), "UTF-8")}" else ""
                            apiClient.get<NumberShareVehiclesResponse>("/plate/share/vehicles$vq")
                        }.onSuccess { shareVehicles = it }
                    }
                }
                .onFailure { error = it.message ?: "NumberShare状態を取得できません" }
        }
    }

    LaunchedEffect(Unit) {
        loadStatusAndSettings()
        loadRecords()
    }
    LaunchedEffect(tab) {
        if (tab == "share") loadShare()
        if (tab == "rules" || tab == "status") loadStatusAndSettings()
    }

    val matchedCount = records.count { it.record.matches.isNotEmpty() }
    val placed = records.filter { it.record.location != null }
    val groups = remember(records) { records.groupBy { Formatters.isoDateJst(it.record.firstSeen) }.toSortedMap(compareByDescending { it }) }

    ScreenColumn {
        if (error.isNotBlank()) NccBanner(error, BannerTone.ERROR, onDismiss = { error = "" })
        if (notice.isNotBlank()) NccBanner(notice, BannerTone.SUCCESS, onDismiss = { notice = "" })

        NccPanel(
            title = "ナンバー",
            subtitle = "前方カメラで読み取ったナンバーと場所を記録し、条件に合う車両を目立たせます。",
            icon = Icons.Default.DocumentScanner,
        ) {
            status?.let { st ->
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    StatusTile("記録", if (st.enabled) "有効" else "停止中", if (st.enabled) Tone.OK else Tone.NEUTRAL, Modifier.weight(1f))
                    StatusTile("認識", Formatters.sensorLabel(st.sensor.state), if (st.sensor.state == "running") Tone.OK else Tone.WARN, Modifier.weight(1f))
                    StatusTile("読み上げ", if (st.speech.available) "利用可能" else "未設定", if (st.speech.available) Tone.OK else Tone.WARN, Modifier.weight(1f))
                }
                if (st.sensor.state != "running") {
                    HintText("${st.sensor.reason ?: Formatters.sensorLabel(st.sensor.state)}。認識が動くまで新しい記録は増えません。", color = p.warn)
                }
            }
            FieldLabel("このアプリでの通知")
            SectionTabs(
                tabs = listOf(
                    PlatePopupFilter.MATCHED_ONLY.id to "条件に一致",
                    PlatePopupFilter.ALL.id to "すべて",
                    PlatePopupFilter.OFF.id to "通知しない",
                ),
                selectedId = config.popupFilter.id,
                onSelect = { id -> onPopupFilterChanged(PlatePopupFilter.fromId(id)) },
                fill = true,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                TextButton(onClick = { onEmitTestPopup(true) }) { Text("通知を試す（条件に一致）") }
                TextButton(onClick = { onEmitTestPopup(false) }) { Text("通知を試す（通常）") }
            }
        }

        SectionTabs(
            tabs = listOf("records" to "記録", "share" to "共有", "rules" to "条件と設定", "status" to "状態"),
            selectedId = tab,
            onSelect = { tab = it },
            fill = true,
        )

        when (tab) {
            "records" -> {
                NccPanel(
                    title = "認識したナンバー",
                    icon = Icons.Default.History,
                    headerEnd = {
                        PanelAction(onClick = { loadRecords() }, enabled = !loading) { Icon(Icons.Default.Refresh, contentDescription = "記録を更新") }
                    },
                ) {
                    SectionTabs(
                        tabs = listOf("0" to "今日", "2" to "3日", "6" to "7日", "29" to "30日"),
                        selectedId = rangeDays.toString(),
                        onSelect = {
                            rangeDays = it.toInt()
                            fromDay = Formatters.jstDay(-rangeDays.toLong())
                            toDay = Formatters.jstDay(0)
                            loadRecords()
                        },
                        fill = true,
                    )
                    OutlinedTextField(
                        value = query,
                        onValueChange = { query = it.take(20) },
                        placeholder = { Text("ナンバーで検索（例: 12-34、品川）") },
                        leadingIcon = { Icon(Icons.Default.Search, contentDescription = null) },
                        trailingIcon = {
                            if (query.isNotEmpty()) IconButton(onClick = { query = ""; loadRecords() }) { Icon(Icons.Default.Close, contentDescription = "検索をクリア") }
                        },
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                        keyboardActions = KeyboardActions(onSearch = { loadRecords() }),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    SwitchRow("条件に合ったものだけ表示", matchedOnly, { matchedOnly = it; loadRecords() })
                    Text(
                        if (loading) "読み込み中…" else "${records.size}件（条件に一致 ${matchedCount}件 · 地図に表示 ${placed.size}件）${if (truncated) " · 新しい順に上限まで表示" else ""}",
                        color = p.muted,
                        style = MaterialTheme.typography.labelMedium,
                    )
                    Box(Modifier.fillMaxWidth().height(240.dp).clip(RoundedCornerShape(14.dp)).background(p.raised)) {
                        if (placed.isEmpty()) {
                            EmptyState(
                                Icons.Default.Place,
                                "地図に表示できる記録はありません",
                                if (records.isNotEmpty()) "測位できた記録がないため、地図には表示できません。" else "位置のある記録を取得すると、ここに表示します。",
                                modifier = Modifier.align(Alignment.Center),
                            )
                        } else {
                            NccMap(
                                modifier = Modifier.fillMaxSize(),
                                view = "plates",
                scrollFriendly = true,
                                mapThemes = mapThemes,
                                points = placed.map { env ->
                                    val r = env.record
                                    val hit = r.matches.isNotEmpty()
                                    val sel = r.id == selectedId
                                    MapPoint(
                                        r.id, r.location!!.lat, r.location.lon,
                                        color = if (hit) p.plateHit else Color(0xFF397FD7),
                                        radius = if (sel) 9f else if (hit) 7f else 5f,
                                        stroke = if (sel) Color(0xFF1C2230) else Color.White,
                                        strokeWidth = if (sel) 3f else 1.5f,
                                        opacity = if (r.location.estimated == true) 0.5f else 0.95f,
                                        sortKey = if (sel) 2f else if (hit) 1f else 0f,
                                    )
                                },
                                focus = MapFocus.FIT,
                                focusKey = "$fromDay-$toDay-$matchedOnly-$query-${placed.size}",
                                onPointClick = { selectedId = it },
                            )
                        }
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                        LegendChip(Color(0xFF397FD7), "記録")
                        LegendChip(p.plateHit, "条件に一致")
                        LegendChip(Color(0xFF397FD7).copy(alpha = 0.5f), "位置は推定")
                    }
                    if (records.isEmpty() && !loading) {
                        EmptyState(Icons.Default.DocumentScanner, "この期間の記録はありません", listReason.ifBlank { null })
                    }
                    groups.forEach { (day, items) ->
                        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 4.dp)) {
                            Text(Formatters.formatJpDate(day), color = p.text, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
                            Text("${items.size}件", color = p.muted, style = MaterialTheme.typography.labelSmall)
                        }
                        items.take(200).forEach { env ->
                            val rec = env.record
                            val hit = rec.matches.isNotEmpty()
                            val danger = env.numberShare?.status == "exact" && env.numberShare.category == "危険"
                            val sel = rec.id == selectedId
                            Surface(
                                shape = RoundedCornerShape(16.dp),
                                color = when {
                                    danger -> p.dangerSoft
                                    hit -> p.plateHitSoft
                                    else -> p.raised
                                },
                                border = if (sel) BorderStroke(2.dp, p.accent) else null,
                                modifier = Modifier.fillMaxWidth().clickable { selectedId = rec.id },
                            ) {
                                Row(
                                    modifier = Modifier.padding(12.dp),
                                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    JapanesePlateBadge(reading = rec.reading, matched = hit, danger = danger, width = 120.dp)
                                    Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                        Text(Formatters.formatTime(rec.firstSeen), color = p.text, style = MaterialTheme.typography.titleSmall)
                                        Text(
                                            "${rec.sightings}回検出 · 確からしさ ${(rec.reading.confidence * 100).toInt()}% · ${Formatters.plateLocationLabel(rec)}",
                                            color = p.muted,
                                            fontSize = 11.sp,
                                        )
                                        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                            rec.matches.forEach { m -> NccTag(text = m.ruleName, color = Color.White, background = p.plateHit) }
                                            env.numberShare?.takeIf { it.status == "exact" }?.let { ns ->
                                                NccTag(
                                                    text = "NumberShare: ${ns.category ?: "登録済み"}",
                                                    color = if (danger) Color.White else p.accent,
                                                    background = if (danger) p.danger else p.soft,
                                                )
                                            }
                                            if (!rec.speech.isNullOrBlank()) NccTag(text = Formatters.speechLabel(rec.speech))
                                        }
                                        env.numberShare?.comment?.takeIf { env.numberShare.status == "exact" && it.isNotBlank() }?.let {
                                            Text(it, color = p.text, fontSize = 11.sp)
                                        }
                                        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                                            TextButton(onClick = { onOpenRecordingAt(rec.firstSeen) }, contentPadding = PaddingValues(start = 0.dp, end = 10.dp)) {
                                                Icon(Icons.Default.PlayArrow, contentDescription = null, modifier = Modifier.size(16.dp))
                                                Text(" 録画を見る", fontSize = 12.sp)
                                            }
                                            if (env.imageAvailable) {
                                                TextButton(onClick = { imageModalRecord = rec }, contentPadding = PaddingValues(start = 0.dp, end = 10.dp)) {
                                                    Icon(Icons.Default.Image, contentDescription = null, modifier = Modifier.size(16.dp))
                                                    Text(" 画像", fontSize = 12.sp)
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }

            "share" -> {
                NccPanel(
                    title = "NumberShare（ナンバー共有）",
                    subtitle = "検知したナンバーを共有サービスに送り、他の利用者が登録した車両と照合します。",
                    icon = Icons.Default.Share,
                    headerEnd = {
                        NccTag(
                            text = if (shareStatus?.config?.enabled == true) "送信中" else "送信停止",
                            color = if (shareStatus?.config?.enabled == true) p.accent else p.muted,
                            background = p.soft,
                        )
                    },
                ) {
                    OutlinedTextField(
                        value = shareEndpoint,
                        onValueChange = { shareEndpoint = it },
                        label = { Text("送信先URL（https）") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = shareKey,
                        onValueChange = { shareKey = it },
                        label = { Text("APIキー（${if (shareStatus?.config?.keyConfigured == true) "保存済み・空欄なら変更しません" else "未設定"}）") },
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.SpaceBetween, modifier = Modifier.fillMaxWidth()) {
                        Text("検知したナンバーをすぐに送信する", color = p.text, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
                        Switch(checked = shareEnabled, onCheckedChange = { shareEnabled = it })
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Button(
                            enabled = !saving && shareStatus != null,
                            onClick = {
                                scope.launch {
                                    val st = shareStatus ?: return@launch
                                    saving = true
                                    error = ""
                                    val body = buildJsonObject {
                                        put("expectedRevision", st.config.revision)
                                        put("enabled", shareEnabled)
                                        put("endpoint", shareEndpoint.trim())
                                        if (shareKey.isNotBlank()) put("key", shareKey.trim())
                                    }.toString()
                                    runCatching { apiClient.requestRaw("/plate/share/config", "PUT", body) }
                                        .onSuccess {
                                            shareKey = ""
                                            notice = "NumberShare設定を保存しました"
                                            loadShare()
                                        }
                                        .onFailure { error = it.message ?: "保存に失敗しました" }
                                    saving = false
                                }
                            },
                        ) { Text("設定を保存") }
                        OutlinedButton(
                            enabled = !saving && shareStatus?.config?.keyConfigured == true,
                            onClick = {
                                scope.launch {
                                    saving = true
                                    error = ""
                                    runCatching { apiClient.requestRaw("/plate/share/check", "POST", "{}") }
                                        .onSuccess { notice = "NumberShare APIへの接続を確認しました" }
                                        .onFailure { error = it.message ?: "接続確認に失敗しました" }
                                    saving = false
                                }
                            },
                        ) { Text("接続確認") }
                    }

                    if (shareStatus?.config?.keyConfigured != true) {
                        HorizontalDivider(color = p.line)
                        FieldLabel("はじめて使うときの登録")
                        HintText("匿名のユーザーIDと、サービスから受け取った接続用キーを入力すると、APIキーを取得して保存します。")
                        OutlinedTextField(
                            value = shareUserHandle,
                            onValueChange = { shareUserHandle = it },
                            label = { Text("匿名ユーザーID（例: sky_4821）") },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        OutlinedTextField(
                            value = shareBootstrapKey,
                            onValueChange = { shareBootstrapKey = it },
                            label = { Text("接続用キー") },
                            singleLine = true,
                            visualTransformation = PasswordVisualTransformation(),
                            modifier = Modifier.fillMaxWidth(),
                        )
                        Button(
                            enabled = !saving && shareStatus != null && shareUserHandle.isNotBlank() && shareBootstrapKey.isNotBlank(),
                            onClick = {
                                scope.launch {
                                    val st = shareStatus ?: return@launch
                                    saving = true
                                    error = ""
                                    val body = buildJsonObject {
                                        put("expectedRevision", st.config.revision)
                                        put("endpoint", shareEndpoint.trim())
                                        put("userHandle", shareUserHandle.trim())
                                        put("bootstrapKey", shareBootstrapKey.trim())
                                    }.toString()
                                    runCatching { apiClient.requestRaw("/plate/share/register", "POST", body) }
                                        .onSuccess {
                                            shareBootstrapKey = ""
                                            notice = "初回登録が完了しました"
                                            loadShare()
                                        }
                                        .onFailure { error = it.message ?: "初回登録に失敗しました" }
                                    saving = false
                                }
                            },
                        ) { Text("APIキーを取得して保存") }
                    }
                }

                // Queue & manual upload
                shareStatus?.let { st ->
                    NccPanel(title = "送信状況", icon = Icons.Default.CloudUpload) {
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            StatusTile("送信待ち", "${st.pending}件", Tone.NEUTRAL, Modifier.weight(1f))
                            StatusTile("送信済み", "${st.sent}件", Tone.OK, Modifier.weight(1f))
                            StatusTile("要確認", "${st.blocked}件", if (st.blocked > 0) Tone.WARN else Tone.NEUTRAL, Modifier.weight(1f))
                        }
                        OutlinedButton(
                            enabled = !saving && st.config.keyConfigured,
                            onClick = {
                                scope.launch {
                                    saving = true
                                    val body = """{"from":"$fromDay","to":"$toDay"}"""
                                    runCatching { apiClient.post<NumberShareUploadResponse>("/plate/share/upload", body) }
                                        .onSuccess { r ->
                                            notice = "追加 ${r.queued}件・再送待ち ${r.requeued}件（対象 ${r.total}件）"
                                            loadShare()
                                        }
                                        .onFailure { error = it.message ?: "アップロードに失敗しました" }
                                    saving = false
                                }
                            },
                        ) { Text("${Formatters.shortDate(fromDay)}〜${Formatters.shortDate(toDay)} の記録をまとめて送信") }

                        st.items.take(15).forEach { qItem ->
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                horizontalArrangement = Arrangement.SpaceBetween,
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Column(modifier = Modifier.weight(1f)) {
                                    Text(
                                        "${qItem.payload.plate.region.orEmpty()} ${Formatters.plateSerial(qItem.payload.plate.serial_number)} · ${queueLabel(qItem.state)}",
                                        color = p.text,
                                        fontSize = 12.sp,
                                    )
                                    qItem.lastError?.let { Text(it, color = p.danger, fontSize = 11.sp) }
                                }
                                if (qItem.state == "blocked") {
                                    TextButton(onClick = {
                                        scope.launch {
                                            runCatching { apiClient.requestRaw("/plate/share/retry/${qItem.eventId}", "POST", "{}") }
                                                .onSuccess { notice = "再送待ちに戻しました"; loadShare() }
                                        }
                                    }) { Text("再送") }
                                }
                            }
                        }
                    }
                }

                // Shared sightings & vehicles
                NccPanel(title = "共有された検知と登録車両", icon = Icons.Default.Groups) {
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                        OutlinedTextField(
                            value = shareQuery,
                            onValueChange = { shareQuery = it },
                            label = { Text("検知履歴を検索") },
                            singleLine = true,
                            modifier = Modifier.weight(1f),
                        )
                        Button(onClick = { loadShare() }) { Text("取得") }
                    }
                    SectionTabs(
                        tabs = listOf("all" to "すべて", "mine" to "自分", "others" to "他の利用者"),
                        fill = true,
                        selectedId = shareScope,
                        onSelect = { shareScope = it; loadShare() },
                    )
                    shareSightings?.items?.take(20)?.forEach { s ->
                        Surface(
                            shape = RoundedCornerShape(8.dp),
                            color = p.bg,
                            border = BorderStroke(1.dp, p.line),
                            modifier = Modifier.fillMaxWidth(),
                        ) {
                            Column(modifier = Modifier.padding(8.dp)) {
                                Text(
                                    "${s.plate.region.orEmpty()} ${s.plate.classification_number.orEmpty()} ${s.plate.hiragana.orEmpty()} ${Formatters.plateSerial(s.plate.serial_number)}".trim(),
                                    color = p.text,
                                    fontWeight = FontWeight.Bold,
                                    fontSize = 13.sp,
                                )
                                Text(
                                    "${if (s.mine) "自分" else "他の利用者"} · ${Formatters.formatDateTime(s.observed_at)} · ${matchLabel(s.match_status)}",
                                    color = p.muted,
                                    fontSize = 11.sp,
                                )
                            }
                        }
                    }
                    HorizontalDivider(color = p.line)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                        OutlinedTextField(
                            value = vehicleSerial,
                            onValueChange = { vehicleSerial = it },
                            label = { Text("登録車両を番号で検索（例: 1234）") },
                            singleLine = true,
                            modifier = Modifier.weight(1f),
                        )
                        OutlinedButton(onClick = { loadShare() }) { Text("検索") }
                    }
                    shareVehicles?.items?.take(15)?.forEach { v ->
                        Text(
                            "${v.region} ${v.classification_number} ${v.hiragana} ${Formatters.plateSerial(v.serial_number)} [${v.category}] ${v.comment}",
                            color = p.text,
                            fontSize = 12.sp,
                        )
                    }
                }
            }

            "rules" -> {
                val d = draft
                if (d == null) {
                    NccPanel(title = "設定を読み込み中…") {
                        OutlinedButton(onClick = { loadStatusAndSettings() }) { Text("再読み込み") }
                    }
                } else {
                    NccPanel(title = "記録の設定", icon = Icons.Default.Tune) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Column(modifier = Modifier.weight(1f)) {
                                Text("ナンバーを記録する", color = p.text, style = MaterialTheme.typography.bodyMedium)
                                Text("オフの間は読み取ったナンバーを保存しません", color = p.muted, style = MaterialTheme.typography.bodySmall)
                            }
                            Switch(checked = d.enabled, onCheckedChange = { draft = d.copy(enabled = it) })
                        }
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            OutlinedTextField(
                                value = d.retentionDays.toString(),
                                onValueChange = { v -> v.toIntOrNull()?.let { draft = d.copy(retentionDays = it) } },
                                label = { Text("保存する日数") },
                                singleLine = true,
                                modifier = Modifier.weight(1f),
                            )
                            OutlinedTextField(
                                value = d.imageQuotaMB.toString(),
                                onValueChange = { v -> v.toIntOrNull()?.let { draft = d.copy(imageQuotaMB = it) } },
                                label = { Text("画像の上限（MB）") },
                                singleLine = true,
                                modifier = Modifier.weight(1f),
                            )
                        }
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            OutlinedTextField(
                                value = d.minSightings.toString(),
                                onValueChange = { v -> v.toIntOrNull()?.let { draft = d.copy(minSightings = it) } },
                                label = { Text("記録に必要な検出回数") },
                                singleLine = true,
                                modifier = Modifier.weight(1f),
                            )
                            OutlinedTextField(
                                value = d.minConfidence.toString(),
                                onValueChange = { v -> v.toDoubleOrNull()?.let { draft = d.copy(minConfidence = it) } },
                                label = { Text("確からしさの下限（0〜1）") },
                                singleLine = true,
                                modifier = Modifier.weight(1f),
                            )
                        }
                    }

                    NccPanel(title = "本体での読み上げ", subtitle = "条件に一致したとき、NP1のスピーカーで読み上げます（VOICEVOX）。", icon = Icons.AutoMirrored.Filled.VolumeUp) {
                        FieldLabel("音量", trailing = "${d.voice.volume}%")
                        Slider(
                            value = d.voice.volume.toFloat(),
                            onValueChange = { draft = d.copy(voice = d.voice.copy(volume = it.toInt())) },
                            valueRange = 10f..100f,
                        )
                        OutlinedTextField(
                            value = d.voice.phrase,
                            onValueChange = { draft = d.copy(voice = d.voice.copy(phrase = it)) },
                            label = { Text("読み上げの定型文") },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        speech?.styles?.takeIf { it.isNotEmpty() }?.let { styles ->
                            FieldLabel("声")
                            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                styles.take(16).forEach { st ->
                                    val sel = st.id == d.voice.style
                                    NccTag(
                                        text = "${st.speaker}(${st.name})",
                                        color = if (sel) p.accent else p.muted,
                                        background = if (sel) p.soft else p.bg,
                                        modifier = Modifier.clickable {
                                            draft = d.copy(voice = d.voice.copy(style = st.id))
                                        },
                                    )
                                }
                            }
                        }
                    }

                    NccPanel(
                        title = "条件（${d.rules.size} / 100）",
                        icon = Icons.Default.FilterAlt,
                        headerEnd = {
                            OutlinedButton(
                                enabled = d.rules.size < 100,
                                onClick = {
                                    val newId = "r" + (1..10).map { "0123456789abcdef"[Random.nextInt(16)] }.joinToString("")
                                    draft = d.copy(
                                        rules = d.rules + PlateRule(
                                            id = newId,
                                            name = "条件 ${d.rules.size + 1}",
                                            enabled = true,
                                            saveImage = true,
                                        ),
                                    )
                                },
                            ) {
                                Icon(Icons.Default.Add, contentDescription = null)
                                Text("追加")
                            }
                        },
                    ) {
                        HintText("空欄はどれでも一致、* は任意の1文字です（例: 12**）。")
                        d.rules.forEachIndexed { idx, rule ->
                            fun updateRule(newRule: PlateRule) {
                                draft = d.copy(rules = d.rules.mapIndexed { i, r -> if (i == idx) newRule else r })
                            }
                            Surface(
                                shape = RoundedCornerShape(10.dp),
                                color = p.bg,
                                border = BorderStroke(1.dp, p.line),
                                modifier = Modifier.fillMaxWidth(),
                            ) {
                                Column(modifier = Modifier.padding(10.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                        OutlinedTextField(
                                            value = rule.name,
                                            onValueChange = { updateRule(rule.copy(name = it)) },
                                            label = { Text("条件名") },
                                            singleLine = true,
                                            modifier = Modifier.weight(1f),
                                        )
                                        Switch(checked = rule.enabled, onCheckedChange = { updateRule(rule.copy(enabled = it)) })
                                        IconButton(onClick = {
                                            draft = d.copy(rules = d.rules.filterIndexed { i, _ -> i != idx })
                                        }) {
                                            Icon(Icons.Default.Delete, contentDescription = "削除", tint = p.danger)
                                        }
                                    }
                                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                        OutlinedTextField(
                                            value = rule.region,
                                            onValueChange = { updateRule(rule.copy(region = it)) },
                                            label = { Text("地名") },
                                            singleLine = true,
                                            modifier = Modifier.weight(1.2f),
                                        )
                                        OutlinedTextField(
                                            value = rule.classNum,
                                            onValueChange = { updateRule(rule.copy(classNum = it)) },
                                            label = { Text("分類") },
                                            singleLine = true,
                                            modifier = Modifier.weight(1f),
                                        )
                                        OutlinedTextField(
                                            value = rule.kana,
                                            onValueChange = { updateRule(rule.copy(kana = it)) },
                                            label = { Text("かな") },
                                            singleLine = true,
                                            modifier = Modifier.weight(0.8f),
                                        )
                                        OutlinedTextField(
                                            value = rule.number,
                                            onValueChange = { updateRule(rule.copy(number = it)) },
                                            label = { Text("番号") },
                                            singleLine = true,
                                            modifier = Modifier.weight(1.2f),
                                        )
                                    }
                                    Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                                        Row(verticalAlignment = Alignment.CenterVertically) {
                                            Checkbox(checked = rule.saveImage, onCheckedChange = { updateRule(rule.copy(saveImage = it)) })
                                            Text("画像を保存", color = p.text, fontSize = 12.sp)
                                        }
                                        Row(verticalAlignment = Alignment.CenterVertically) {
                                            Checkbox(checked = rule.speak, onCheckedChange = { updateRule(rule.copy(speak = it)) })
                                            Text("読み上げる", color = p.text, fontSize = 12.sp)
                                        }
                                    }
                                    if (rule.speak) {
                                        OutlinedTextField(
                                            value = rule.message,
                                            onValueChange = { updateRule(rule.copy(message = it)) },
                                            label = { Text("読み上げる言葉") },
                                            singleLine = true,
                                            modifier = Modifier.fillMaxWidth(),
                                        )
                                    }
                                    SectionTabs(
                                        tabs = listOf("off" to "LEDなし", "solid" to "LED単色", "rainbow" to "LED虹色"),
                                        fill = true,
                                        selectedId = rule.led.mode,
                                        onSelect = { m -> updateRule(rule.copy(led = rule.led.copy(mode = m))) },
                                    )
                                    if (settings?.rules?.any { it.id == rule.id } == true && (rule.speak || rule.led.mode != "off")) {
                                        TextButton(onClick = {
                                            scope.launch {
                                                runCatching {
                                                    apiClient.requestRaw("/plate/speech/preview", "POST", """{"ruleId":"${rule.id}"}""")
                                                }.onSuccess { notice = "本体で読み上げ・LEDを試しました" }
                                                    .onFailure { error = it.message ?: "テストに失敗しました" }
                                            }
                                        }) {
                                            Icon(Icons.AutoMirrored.Filled.VolumeUp, contentDescription = null, modifier = Modifier.size(16.dp))
                                            Text(" 本体で試す")
                                        }
                                    }
                                }
                            }
                        }

                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Button(
                                enabled = !saving && settings != null,
                                onClick = {
                                    scope.launch {
                                        val base = settings ?: return@launch
                                        saving = true
                                        error = ""
                                        val settingsEl = apiClient.json.encodeToJsonElement(PlateSettings.serializer(), d)
                                        val payload = buildJsonObject {
                                            put("expectedRevision", base.revision)
                                            put("settings", settingsEl)
                                        }.toString()
                                        runCatching { apiClient.put<PlateSettings>("/plate/settings", payload) }
                                            .onSuccess {
                                                settings = it
                                                draft = it
                                                notice = "ナンバー認識設定を保存しました"
                                                loadStatusAndSettings()
                                            }
                                            .onFailure { error = it.message ?: "設定の保存に失敗しました" }
                                        saving = false
                                    }
                                },
                            ) {
                                Icon(Icons.Default.Save, contentDescription = null)
                                Text(if (saving) " 保存中…" else " 設定を保存")
                            }
                            OutlinedButton(onClick = { draft = settings }) { Text("取り消し") }
                        }
                    }
                }
            }

            "status" -> {
                NccPanel(
                    title = "認識と保存の状態",
                    icon = Icons.Default.Info,
                    headerEnd = {
                        PanelAction(onClick = { loadStatusAndSettings() }) { Icon(Icons.Default.Refresh, contentDescription = "状態を更新") }
                    },
                ) {
                    val st = status
                    if (st != null) {
                        KeyValueRow("記録", if (st.enabled) "有効" else "停止中")
                        KeyValueRow("認識", Formatters.sensorLabel(st.sensor.state))
                        st.sensor.reason?.let { KeyValueRow("理由", it) }
                        st.sensor.engine?.let { KeyValueRow("認識エンジン", "$it ${st.sensor.backend.orEmpty()}") }
                        st.sensor.fps?.let { KeyValueRow("処理速度", String.format("%.1f fps", it)) }
                        KeyValueRow("最終フレーム", Formatters.formatDateTime(st.sensor.lastFrameAt))
                        KeyValueRow("最終取り込み", Formatters.formatDateTime(st.collector.lastPollAt))
                        KeyValueRow("読み上げ", if (st.speech.available) "利用可能" else st.speech.reason.ifBlank { "未設定" })
                        KeyValueRow("記録の容量", "${st.usage.recordDays}日分 · ${Formatters.bytes(st.usage.recordBytes)}")
                        KeyValueRow("画像の容量", "${st.usage.imageCount}枚 · ${Formatters.bytes(st.usage.imageBytes)} / ${Formatters.bytes(st.imageQuotaBytes)}")
                    }
                }
            }
        }
    }

    // Detection image modal with bounding box
    val modalRec = imageModalRecord
    if (modalRec != null) {
        Dialog(onDismissRequest = { imageModalRecord = null }) {
            Surface(
                shape = RoundedCornerShape(14.dp),
                color = p.paper,
                border = BorderStroke(1.dp, p.line),
                modifier = Modifier.fillMaxWidth(),
            ) {
                Column(modifier = Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(Formatters.plateDisplay(modalRec), color = p.text, fontWeight = FontWeight.Bold, fontSize = 16.sp)
                        IconButton(onClick = { imageModalRecord = null }) {
                            Icon(Icons.Default.Close, contentDescription = "閉じる", tint = p.muted)
                        }
                    }
                    val encId = URLEncoder.encode(modalRec.id, "UTF-8")
                    val imgUrl = apiClient.fullUrl("/plate/records/$encId/image")
                    Box(
                        modifier = Modifier
                            .fillMaxWidth()
                            .aspectRatio(16f / 9f)
                            .clip(RoundedCornerShape(8.dp))
                            .background(Color(0xFF10141A)),
                    ) {
                        AsyncImage(
                            model = ImageRequest.Builder(context).data(imgUrl).build(),
                            imageLoader = imageLoader,
                            contentDescription = "検出画像",
                            contentScale = ContentScale.Fit,
                            modifier = Modifier.fillMaxSize(),
                        )
                        if (modalRec.box.size >= 4 && modalRec.box[2] > 0) {
                            Canvas(modifier = Modifier.fillMaxSize()) {
                                val left = (modalRec.box[0] / 1920.0 * size.width).toFloat()
                                val top = (modalRec.box[1] / 1080.0 * size.height).toFloat()
                                val bw = (modalRec.box[2] / 1920.0 * size.width).toFloat()
                                val bh = (modalRec.box[3] / 1080.0 * size.height).toFloat()
                                drawRect(
                                    color = p.plateHit,
                                    topLeft = Offset(left, top),
                                    size = Size(bw, bh),
                                    style = Stroke(width = 3f),
                                )
                            }
                        }
                    }
                    Text(
                        "${Formatters.formatDateTime(modalRec.firstSeen)} · ${modalRec.matches.joinToString("、") { it.ruleName }}",
                        color = p.muted,
                        fontSize = 12.sp,
                    )
                }
            }
        }
    }
}


private fun queueLabel(state: String) = when (state) {
    "pending" -> "送信待ち"
    "sent" -> "送信済み"
    "blocked" -> "要確認"
    "sending" -> "送信中"
    else -> state
}

private fun matchLabel(state: String) = when (state) {
    "exact" -> "登録車両と一致"
    "partial" -> "一部一致"
    "none" -> "一致なし"
    else -> state
}

@Composable
private fun StatusTile(label: String, value: String, tone: Tone, modifier: Modifier = Modifier) {
    val p = LocalNccPalette.current
    Column(modifier.clip(RoundedCornerShape(14.dp)).background(p.toneSoft(tone)).padding(horizontal = 10.dp, vertical = 8.dp)) {
        Text(label, color = p.muted, style = MaterialTheme.typography.labelSmall)
        Text(value, color = p.toneColor(tone), style = MaterialTheme.typography.titleSmall, maxLines = 1)
    }
}

@Composable
private fun LegendChip(color: Color, label: String) {
    val p = LocalNccPalette.current
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(9.dp).clip(CircleShape).background(color))
        Text(" $label", color = p.muted, fontSize = 11.sp)
    }
}
