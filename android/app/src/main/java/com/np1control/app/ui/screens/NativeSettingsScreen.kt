package com.np1control.app.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Build
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.SdCard
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.AlexaData
import com.np1control.app.data.Formatters
import com.np1control.app.data.NativeActionMeta
import com.np1control.app.data.NativeActionReply
import com.np1control.app.data.NativeOpGroup
import com.np1control.app.data.NativeOpState
import com.np1control.app.data.NativeOperationsCatalog
import com.np1control.app.data.NativeSettingItem
import com.np1control.app.data.NativeSettingsData
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.EmptyState
import com.np1control.app.ui.components.FieldLabel
import com.np1control.app.ui.components.HintText
import com.np1control.app.ui.components.KeyValueRow
import com.np1control.app.ui.components.LoadingRow
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.PanelAction
import com.np1control.app.ui.components.ScreenColumn
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.components.StatusPill
import com.np1control.app.ui.components.SwitchRow
import com.np1control.app.ui.components.Tone
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull
import kotlinx.serialization.json.put
import java.net.URLEncoder

// Pages of stock settings, as grouped by the Web UI (frontend/src/services/native.ts).
private data class NativePage(val id: String, val label: String, val items: List<String>)

private val nativePages = listOf(
    NativePage("support", "運転サポート", listOf("driving-support", "tailgating-detection", "tailgating-sensitivity", "front-car-start", "signal-change")),
    NativePage("navigation", "ナビ・交通情報", listOf("traffic-check", "beginner-driver", "traffic-congestion", "traffic-regulation", "ferry", "toll-road", "smart-ic", "route-search")),
    NativePage("guidance", "案内・コエ", listOf("guidance-frequency", "daily-voice", "support-voice")),
    NativePage("privacy", "プライバシー", listOf("privacy-mode", "service-cabin-camera")),
    NativePage("installation", "車両・取付", listOf("vehicle-type", "mount-height", "mount-position", "mount-orientation")),
)

private val settingViews = listOf(
    Triple("device", "本体", emptyList<String>()),
    Triple("support", "運転支援", listOf("support")),
    Triple("navigation", "ナビ・案内", listOf("navigation", "guidance")),
    Triple("privacy", "プライバシー・取付", listOf("privacy", "installation")),
)

private val actionCategories = listOf(
    Triple("adjust", "調整・診断", listOf("取付の調整", "GNSS・センサー", "ライセンス・診断")),
    Triple("connect", "接続・アカウント", listOf("本体のナビ操作", "本体アカウントの操作", "純正Wi-Fi接続", "純正ローカルAP")),
    Triple("maintenance", "保守・セットアップ", listOf("保守・シリアル", "セットアップ", "純正ファームウェア")),
)

private val replyLabels = mapOf(
    "result" to "本体の処理結果", "mode" to "保守モード", "learningState" to "学習状態", "antennaConnected" to "アンテナ接続",
    "antennaLevel" to "受信レベル", "positioningStatus" to "測位状態", "satellites" to "受信衛星数", "usedSatellites" to "使用衛星数",
    "mounted" to "取付検出", "trip" to "センサートリップ", "ssid" to "SSID", "password" to "パスワード", "security" to "セキュリティ方式",
    "band" to "周波数帯", "output" to "シリアル出力", "input" to "コンソール入力", "routeSynchronized" to "ルート同期",
    "ownerKey" to "オーナーキー", "status" to "設定状態",
)

private fun replyValue(key: String, v: JsonElement): String {
    val prim = v as? JsonPrimitive
    val n = prim?.longOrNull?.toInt()
    return when {
        key == "mode" && n != null -> listOf("不明", "OFF", "Poseidon", "APP", "Sales", "Service").getOrNull(n) ?: n.toString()
        key == "learningState" && n != null -> listOf("不明", "未学習", "学習中", "学習済み").getOrNull(n) ?: n.toString()
        key == "positioningStatus" && n != null -> listOf("不明", "未測位", "1D", "2D", "3D", "測位エラー").getOrNull(n) ?: n.toString()
        prim?.booleanOrNull != null -> if (prim.booleanOrNull == true) "はい" else "いいえ"
        prim != null -> prim.contentOrNull ?: prim.toString()
        else -> v.toString()
    }
}

@Composable
fun NativeSettingsScreen(apiClient: NccApiClient) {
    val p = LocalNccPalette.current
    val scope = rememberCoroutineScope()

    var view by remember { mutableStateOf("settings") }
    var settingsView by remember { mutableStateOf("device") }
    var alexa by remember { mutableStateOf<AlexaData?>(null) }
    var native by remember { mutableStateOf<Observation?>(null) }
    var catalog by remember { mutableStateOf<NativeOperationsCatalog?>(null) }
    val opStates = remember { mutableStateMapOf<String, NativeOpState>() }
    var loading by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf("") }
    var error by remember { mutableStateOf("") }
    var notice by remember { mutableStateOf("") }

    var formatToken by remember { mutableStateOf<String?>(null) }
    var formatCapBytes by remember { mutableStateOf(0L) }

    suspend fun loadAll() {
        loading = true
        error = ""
        runCatching { apiClient.get<Observation>("/device/alexa").data?.let { apiClient.json.decodeFromJsonElement(AlexaData.serializer(), it) } }
            .onSuccess { alexa = it }
        runCatching { apiClient.get<Observation>("/device/native-settings") }.onSuccess { native = it }
            .onFailure { error = "純正設定を取得できませんでした。" }
        runCatching { apiClient.get<NativeOperationsCatalog>("/device/native-operations") }.onSuccess { cat ->
            catalog = cat
            if (cat.available) {
                runCatching { apiClient.get<NativeOpState>("/device/native-operations/state/storage") }.onSuccess { opStates["storage"] = it }
                cat.groups.filter { it.id !in setOf("recorder", "parking", "volume") }.forEach { grp ->
                    runCatching { apiClient.get<NativeOpState>("/device/native-operations/state/${grp.id}") }.onSuccess { opStates[grp.id] = it }
                }
            }
        }
        loading = false
    }
    LaunchedEffect(Unit) { loadAll() }

    val nativeData = remember(native) { native?.data?.let { runCatching { apiClient.json.decodeFromJsonElement(NativeSettingsData.serializer(), it) }.getOrNull() } }

    ScreenColumn {
        SectionTabs(tabs = listOf("settings" to "設定", "actions" to "操作・診断"), selectedId = view, onSelect = { view = it }, fill = true)
        if (error.isNotBlank()) NccBanner(error, BannerTone.ERROR, onDismiss = { error = "" })
        if (notice.isNotBlank()) NccBanner(notice, BannerTone.SUCCESS, onDismiss = { notice = "" })

        if (view == "settings") {
            Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState())) {
                SectionTabs(tabs = settingViews.map { it.first to it.second }, selectedId = settingsView, onSelect = { settingsView = it })
            }
            if (settingsView == "device") {
                AlexaCard(alexa, busy.isNotEmpty(), onRefresh = { scope.launch { loadAll() } }) { next ->
                    val ax = alexa ?: return@AlexaCard
                    scope.launch {
                        busy = "alexa"
                        error = ""
                        runCatching {
                            apiClient.put<Observation>("/device/alexa", """{"expectedValue":${ax.wakeWord.value},"enabled":$next}""")
                                .data?.let { apiClient.json.decodeFromJsonElement(AlexaData.serializer(), it) }
                        }.onSuccess { alexa = it; notice = "Alexaの呼びかけ設定を変更しました。" }
                            .onFailure { error = it.message ?: "Alexaの設定を変更できませんでした" }
                        busy = ""
                    }
                }
                val sd = opStates["storage"]?.values
                NccPanel(
                    title = "SDカード",
                    icon = Icons.Default.SdCard,
                    headerEnd = {
                        StatusPill(
                            when (sd?.get("inserted")) { 1L -> "挿入済み"; 0L -> "未挿入"; else -> "確認中" },
                            if (sd?.get("inserted") == 1L) Tone.OK else Tone.NEUTRAL,
                        )
                    },
                ) {
                    HintText("フォーマットすると、SDカード内の録画・写真・走行の位置情報など、すべてのデータが消えます。")
                    OutlinedButton(
                        enabled = busy.isEmpty() && sd?.get("inserted") == 1L && sd["formatStatus"] == 2L,
                        colors = ButtonDefaults.outlinedButtonColors(contentColor = p.danger),
                        onClick = {
                            scope.launch {
                                busy = "format"
                                error = ""
                                runCatching {
                                    val obj = apiClient.json.parseToJsonElement(apiClient.requestRaw("/device/native-operations/sd-format/prepare", "POST", "{}")).jsonObject
                                    formatToken = obj["token"]?.jsonPrimitive?.content
                                    formatCapBytes = obj["capacityBytes"]?.jsonPrimitive?.longOrNull ?: 0L
                                }.onFailure { error = it.message ?: "フォーマットの準備ができませんでした" }
                                busy = ""
                            }
                        },
                    ) { Text(if (busy == "format") "対象を確認中…" else "SDカードをフォーマット") }
                }
                catalog?.groups?.filter { it.id !in setOf("recorder", "parking", "volume") }?.forEach { grp ->
                    OperationGroupCard(grp, opStates[grp.id], busy) { item, value ->
                        val cur = opStates[grp.id]?.values?.get(item.field) ?: return@OperationGroupCard
                        scope.launch {
                            busy = item.id
                            error = ""
                            runCatching {
                                apiClient.put<NativeOpState>("/device/native-operations/settings/${item.id}", """{"expectedValue":$cur,"value":$value}""")
                            }.onSuccess { st ->
                                opStates[grp.id] = st
                                notice = if (st.values[item.field] == value.toLong()) "${item.label}を変更し、本体から読み直しました。" else "${item.label}の変更を確認できません。状態を更新してください。"
                            }.onFailure { error = it.message ?: "変更できませんでした" }
                            busy = ""
                        }
                    }
                }
                if (catalog == null && loading) LoadingRow("本体の設定を読み込んでいます…")
            } else {
                val pages = settingViews.first { it.first == settingsView }.third.mapNotNull { id -> nativePages.firstOrNull { it.id == id } }
                val all = nativeData?.items.orEmpty()
                if (native == null) {
                    if (loading) LoadingRow("純正設定を読み込んでいます…") else EmptyState(Icons.Default.Tune, "純正設定を取得できません", "対応する本体ソフトウェアと補助プログラムが必要です。")
                }
                if (native != null) pages.forEach { page ->
                    NccPanel(title = page.label, icon = Icons.Default.Tune) {
                        if (page.id == "installation" && page.items.none { id -> all.any { it.id == id } }) {
                            HintText("この本体からは取得できません。取付の向きや高さは「本体」タブの「取付情報」で確認できます。")
                            return@NccPanel
                        }
                        page.items.forEachIndexed { index, id ->
                            val item = all.firstOrNull { it.id == id }
                            if (item == null) {
                                if (page.id == "installation") KeyValueRow(listOf("車種", "取付高さ", "取付位置", "取付方向")[index], "取得できません")
                                return@forEachIndexed
                            }
                            NativeSettingRow(item, editable = native?.status == "available" && busy.isEmpty()) { wanted ->
                                scope.launch {
                                    busy = item.id
                                    error = ""
                                    runCatching {
                                        val body = buildJsonObject {
                                            put("expectedValue", item.value ?: kotlinx.serialization.json.JsonNull)
                                            put("value", wanted)
                                        }.toString()
                                        apiClient.put<Observation>("/device/native-settings/${URLEncoder.encode(item.id, "UTF-8")}", body)
                                    }.onSuccess { obs ->
                                        native = obs
                                        val cur = obs.data?.let { runCatching { apiClient.json.decodeFromJsonElement(NativeSettingsData.serializer(), it) }.getOrNull() }
                                            ?.items?.firstOrNull { it.id == item.id }
                                        notice = if (obs.status == "available" && cur?.value == wanted) "${item.label}を変更し、本体から読み直しました。"
                                        else "変更後の値を確認できません。状態を更新してください。"
                                    }.onFailure { error = "変更を確認できませんでした。${it.message.orEmpty()}" }
                                    busy = ""
                                }
                            }
                        }
                    }
                }
            }
        } else {
            NativeActions(apiClient, catalog, loading, onReload = { scope.launch { loadAll() } })
        }
    }

    val tok = formatToken
    if (tok != null) {
        SdFormatDialog(capacity = formatCapBytes, onDismiss = { formatToken = null }) {
            formatToken = null
            scope.launch {
                busy = "format"
                val body = buildJsonObject {
                    put("token", tok)
                    put("confirmation", "SDカードを消去")
                    put("acknowledgeDataLoss", true)
                }.toString()
                runCatching { apiClient.requestRaw("/device/native-operations/sd-format/execute", "POST", body) }
                    .onSuccess { notice = "SDカードのフォーマットを本体に依頼しました。本体の案内を確認してください。" }
                    .onFailure { error = it.message ?: "フォーマットできませんでした" }
                busy = ""
            }
        }
    }
}

@Composable
private fun AlexaCard(alexa: AlexaData?, busy: Boolean, onRefresh: () -> Unit, onChange: (Boolean) -> Unit) {
    NccPanel(
        title = "Alexa",
        icon = Icons.Default.Mic,
        headerEnd = { PanelAction(onClick = onRefresh) { Icon(Icons.Default.Refresh, contentDescription = "本体の設定を読み直す") } },
    ) {
        if (alexa == null) {
            HintText("Alexaの設定を取得できません（補助プログラムが未導入か、起動を待っています）。")
        } else {
            SwitchRow(
                title = "「アレクサ」の呼びかけに反応する",
                description = if (alexa.wakeWord.fixed) "Alexa連携の都合で、今は変更できません。" else alexa.reason,
                checked = alexa.wakeWord.enabled,
                enabled = !busy && alexa.writable && !alexa.wakeWord.fixed,
                onCheckedChange = onChange,
            )
        }
    }
}

@Composable
private fun OperationGroupCard(group: NativeOpGroup, state: NativeOpState?, busy: String, onApply: (com.np1control.app.data.NativeOpItem, Int) -> Unit) {
    val p = LocalNccPalette.current
    NccPanel(
        title = group.label,
        icon = Icons.Default.Tune,
        headerEnd = { if (state == null) StatusPill("未取得", Tone.NEUTRAL, dot = false) },
    ) {
        group.items.forEach { item ->
            val cur = state?.values?.get(item.field)
            val shown = when {
                cur == null -> "未取得"
                cur >= 0x80000000L -> "未設定"
                else -> item.options?.firstOrNull { it.value.toLong() == cur }?.label ?: cur.toString()
            }
            if (item.writable && !item.options.isNullOrEmpty() && cur != null && cur < 0x80000000L) {
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    FieldLabel(item.label, trailing = if (busy == item.id) "確認中…" else null)
                    SectionTabs(
                        tabs = item.options.map { it.value.toString() to it.label },
                        selectedId = cur.toString(),
                        onSelect = { v -> if (v.toLong() != cur) onApply(item, v.toInt()) },
                        enabled = busy.isEmpty(),
                    )
                }
            } else {
                KeyValueRow(item.label, shown, valueColor = if (shown == "未取得" || shown == "未設定") p.muted else null)
            }
        }
    }
}

@Composable
private fun NativeSettingRow(item: NativeSettingItem, editable: Boolean, onApply: (JsonElement) -> Unit) {
    val p = LocalNccPalette.current
    val value = item.value
    val prim = value as? JsonPrimitive
    val canEdit = editable && item.writable && item.status == "available" && value != null
    val display = when {
        value == null -> "未取得"
        item.valueLabel != null -> item.valueLabel
        prim?.booleanOrNull != null -> if (prim.booleanOrNull == true) "ON" else "OFF"
        else -> item.options?.firstOrNull { it.value == value }?.label ?: prim?.contentOrNull ?: value.toString()
    }
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        when {
            canEdit && item.kind == "boolean" && prim?.booleanOrNull != null -> SwitchRow(
                title = item.label,
                description = item.description,
                checked = prim.booleanOrNull == true,
                onCheckedChange = { onApply(JsonPrimitive(it)) },
            )
            canEdit && !item.options.isNullOrEmpty() -> {
                FieldLabel(item.label)
                item.description?.let { HintText(it) }
                SectionTabs(
                    tabs = item.options.map { it.value.toString() to it.label },
                    selectedId = value.toString(),
                    onSelect = { sel -> item.options.firstOrNull { it.value.toString() == sel }?.let { if (it.value != value) onApply(it.value) } },
                )
            }
            else -> {
                KeyValueRow(item.label, display, valueColor = if (value == null) p.muted else null)
                item.description?.let { HintText(it) }
            }
        }
        if (item.status != null && item.status != "available") item.reason?.let { HintText(it, color = p.warn) }
    }
}

@Composable
private fun NativeActions(apiClient: NccApiClient, catalog: NativeOperationsCatalog?, loading: Boolean, onReload: () -> Unit) {
    val p = LocalNccPalette.current
    val scope = rememberCoroutineScope()
    val actions = catalog?.actions.orEmpty().filter { it.id != "alignment-keepalive" }
    val groups = actions.map { it.group }.distinct()
    val known = actionCategories.flatMap { it.third }.toSet()
    val categories = actionCategories.map { (id, label, gs) -> Triple(id, label, gs.filter { it in groups }) }.filter { it.third.isNotEmpty() } +
        groups.filter { it !in known }.let { if (it.isEmpty()) emptyList() else listOf(Triple("other", "その他", it)) }
    var category by remember { mutableStateOf("adjust") }
    var busy by remember { mutableStateOf("") }
    var dialog by remember { mutableStateOf<NativeActionMeta?>(null) }
    val results = remember { mutableStateMapOf<String, NativeActionReply>() }
    val errors = remember { mutableStateMapOf<String, String>() }
    var alignment by remember { mutableStateOf(false) }

    LaunchedEffect(alignment) {
        if (!alignment) return@LaunchedEffect
        val keep = catalog?.actions?.firstOrNull { it.id == "alignment-keepalive" } ?: return@LaunchedEffect
        while (isActive && alignment) {
            delay(7000)
            val ok = runCatching {
                val body = buildJsonObject { keep.confirmation?.let { put("confirmation", it) }; put("values", buildJsonObject {}) }.toString()
                apiClient.post<NativeActionReply>("/device/native-operations/actions/${keep.id}", body).values?.get("result")?.jsonPrimitive?.booleanOrNull == true
            }.getOrDefault(false)
            if (!ok) {
                alignment = false
                errors[keep.group] = "取付調整の接続確認に失敗しました。本体の状態を確認し、もう一度開始してください。"
            }
        }
    }
    DisposableEffect(Unit) { onDispose { alignment = false } }

    fun execute(action: NativeActionMeta, values: Map<String, JsonElement>) {
        scope.launch {
            busy = action.id
            errors.remove(action.group)
            results.remove(action.group)
            dialog = null
            runCatching {
                val readGet = action.readOnly && action.inputs.isEmpty()
                val path = "/device/native-operations/actions/${URLEncoder.encode(action.id, "UTF-8")}"
                val reply = if (readGet) apiClient.get<NativeActionReply>(path) else {
                    val body = buildJsonObject {
                        action.confirmation?.let { put("confirmation", it) }
                        put("values", JsonObject(values))
                        if (action.id.startsWith("serial-") && action.id != "serial-status") {
                            val prior = (results[action.group]?.readback ?: results[action.group]?.values)?.get(action.id.removePrefix("serial-"))
                            val b = (prior as? JsonPrimitive)?.booleanOrNull ?: throw IllegalStateException("先にシリアルの状態を取得してください。")
                            put("expectedValue", b)
                        }
                    }.toString()
                    apiClient.post<NativeActionReply>(path, body)
                }
                if (!reply.sent || reply.values == null) throw IllegalStateException("本体の応答を確認できません。")
                reply
            }.onSuccess { r ->
                results[action.group] = r
                if (action.id == "alignment-start") alignment = true
                if (action.id == "alignment-cancel" || action.id == "alignment-finish") alignment = false
            }.onFailure { errors[action.group] = "${it.message ?: "操作できませんでした"} 操作は自動で再送しません。" }
            busy = ""
        }
    }

    if (catalog == null) {
        if (loading) LoadingRow("操作の一覧を読み込んでいます…") else EmptyState(Icons.Default.Build, "操作の一覧を取得できません", null) { TextButton(onClick = onReload) { Text("読み直す") } }
        return
    }
    if (!catalog.available) HintText("本体の操作機能を利用できません。補助プログラムの状態を確認してください。", color = p.warn)
    Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState())) {
        SectionTabs(tabs = categories.map { it.first to "${it.second}" }, selectedId = category, onSelect = { category = it })
    }
    val shownGroups = categories.firstOrNull { it.first == category }?.third ?: categories.firstOrNull()?.third.orEmpty()
    shownGroups.forEach { group ->
        NccPanel(title = group, icon = Icons.Default.Build) {
            if (group == "取付の調整" && alignment) {
                NccBanner("取付調整中です。この画面を開いたまま、停車して調整してください。画面を離れると接続の維持が止まります。", BannerTone.WARN)
            }
            actions.filter { it.group == group }.forEach { action ->
                Column(
                    Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(p.raised).padding(12.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    if (action.description.isNotBlank()) Text(action.description, color = p.text, style = MaterialTheme.typography.bodySmall)
                    action.unavailableReason?.let { HintText(it, color = p.warn) }
                    val enabled = catalog.available && busy.isEmpty() && action.unavailableReason == null
                    val onClick = { if (action.readOnly && action.inputs.isEmpty()) execute(action, emptyMap()) else dialog = action }
                    if (action.readOnly) {
                        OutlinedButton(enabled = enabled, onClick = onClick) { Text(if (busy == action.id) "本体に確認中…" else action.label) }
                    } else {
                        FilledTonalButton(enabled = enabled, onClick = onClick) { Text(if (busy == action.id) "本体に確認中…" else action.label) }
                    }
                }
            }
            errors[group]?.let { NccBanner(it, BannerTone.ERROR, onDismiss = { errors.remove(group) }) }
            results[group]?.let { r ->
                Column(
                    Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(p.okSoft).padding(12.dp),
                    verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Icon(Icons.Default.CheckCircle, contentDescription = null, tint = p.ok, modifier = Modifier.size(16.dp))
                        Text(" ${r.message.ifBlank { "本体が応答しました。" }}", color = p.ok, style = MaterialTheme.typography.labelLarge)
                    }
                    if (!r.completionConfirmed) HintText("要求の送信・応答と、動作の完了は別です。本体の案内や状態を確認してください。")
                    r.values?.forEach { (k, v) -> KeyValueRow(replyLabels[k] ?: k, replyValue(k, v)) }
                    r.readback?.let { rb ->
                        FieldLabel("本体から読み直した値")
                        rb.forEach { (k, v) -> KeyValueRow(replyLabels[k] ?: k, replyValue(k, v)) }
                    }
                    TextButton(onClick = { results.remove(group) }) { Text("結果を閉じる") }
                }
            }
        }
    }

    dialog?.let { action -> ActionDialog(action, onDismiss = { dialog = null }, onRun = { execute(action, it) }) }
}

@Composable
private fun ActionDialog(action: NativeActionMeta, onDismiss: () -> Unit, onRun: (Map<String, JsonElement>) -> Unit) {
    val p = LocalNccPalette.current
    val fields = remember(action.id) { mutableStateMapOf<String, String>().apply { action.inputs.forEach { put(it.id, if (it.kind == "number") (it.min ?: 0).toString() else "") } } }
    var ack by remember(action.id) { mutableStateOf(false) }
    var phrase by remember(action.id) { mutableStateOf("") }
    val needsPhrase = action.id == "factory-reset"
    val valid = (action.readOnly || ack) && (!needsPhrase || phrase == "本体を初期化") && action.inputs.all { f ->
        val v = fields[f.id].orEmpty()
        if (f.kind == "number") v.toLongOrNull()?.let { it >= (f.min ?: 0) && it <= (f.max ?: 0xffffffffL) } == true
        else (v.isNotEmpty() || f.kind == "optional-secret") && v.toByteArray().size <= (f.max ?: 1024)
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = p.paper,
        title = { Text(action.label) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                if (action.description.isNotBlank()) Text(action.description, style = MaterialTheme.typography.bodyMedium)
                action.inputs.forEach { f ->
                    OutlinedTextField(
                        value = fields[f.id].orEmpty(),
                        onValueChange = { fields[f.id] = it },
                        label = { Text(f.label) },
                        singleLine = true,
                        visualTransformation = if (f.kind.contains("secret")) PasswordVisualTransformation() else VisualTransformation.None,
                        keyboardOptions = KeyboardOptions(keyboardType = if (f.kind == "number") KeyboardType.Number else KeyboardType.Text),
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
                if (needsPhrase) {
                    OutlinedTextField(
                        value = phrase,
                        onValueChange = { phrase = it },
                        label = { Text("確認のため「本体を初期化」と入力") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
                if (!action.readOnly) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(checked = ack, onCheckedChange = { ack = it })
                        Text("操作の内容と本体への影響を確認しました", style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        },
        confirmButton = {
            Button(enabled = valid, onClick = {
                onRun(action.inputs.associate { f ->
                    f.id to if (f.kind == "number") JsonPrimitive(fields[f.id].orEmpty().toLong()) else JsonPrimitive(fields[f.id].orEmpty())
                })
            }) { Text(action.label) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("キャンセル") } },
    )
}

@Composable
private fun SdFormatDialog(capacity: Long, onDismiss: () -> Unit, onConfirm: () -> Unit) {
    val p = LocalNccPalette.current
    var ack by remember { mutableStateOf(false) }
    var phrase by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = p.paper,
        title = { Text("SDカードをフォーマット", color = p.danger) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("対象の容量: ${Formatters.bytes(capacity)}\nSDカード内の録画・写真・位置情報はすべて消去され、元に戻せません。", style = MaterialTheme.typography.bodyMedium)
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(checked = ack, onCheckedChange = { ack = it })
                    Text("すべてのデータが消えることを確認しました", style = MaterialTheme.typography.bodySmall)
                }
                OutlinedTextField(
                    value = phrase,
                    onValueChange = { phrase = it },
                    label = { Text("確認のため「SDカードを消去」と入力") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        confirmButton = {
            Button(
                enabled = ack && phrase == "SDカードを消去",
                colors = ButtonDefaults.buttonColors(containerColor = p.danger),
                onClick = onConfirm,
            ) { Text("消去してフォーマット") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("キャンセル") } },
    )
}
