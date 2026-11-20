package com.np1control.app.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Build
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import com.np1control.app.data.AlexaData
import com.np1control.app.data.Formatters
import com.np1control.app.data.NativeOpState
import com.np1control.app.data.NativeOperationsCatalog
import com.np1control.app.data.NativeSettingItem
import com.np1control.app.data.NativeSettingsData
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.KeyValueRow
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.NccTag
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull
import kotlinx.serialization.json.put
import java.net.URLEncoder

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun NativeSettingsScreen(
    apiClient: NccApiClient,
) {
    val p = LocalNccPalette.current
    val scope = rememberCoroutineScope()

    var subTab by remember { mutableStateOf("device") } // device | stock | actions
    var alexa by remember { mutableStateOf<AlexaData?>(null) }
    var nativeSettings by remember { mutableStateOf<NativeSettingsData?>(null) }
    var opsCatalog by remember { mutableStateOf<NativeOperationsCatalog?>(null) }
    val opStates = remember { mutableStateMapOf<String, NativeOpState>() }

    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var notice by remember { mutableStateOf("") }

    // SD format modal state
    var formatToken by remember { mutableStateOf<String?>(null) }
    var formatCapBytes by remember { mutableStateOf(0L) }
    var formatAck by remember { mutableStateOf(false) }
    var formatPhrase by remember { mutableStateOf("") }

    fun loadAll() {
        scope.launch {
            error = ""
            runCatching {
                val obs = apiClient.get<Observation>("/device/alexa")
                obs.data?.let { alexa = apiClient.json.decodeFromJsonElement(AlexaData.serializer(), it) }
            }
            runCatching {
                val obs = apiClient.get<Observation>("/device/native-settings")
                obs.data?.let { nativeSettings = apiClient.json.decodeFromJsonElement(NativeSettingsData.serializer(), it) }
            }
            runCatching {
                val cat = apiClient.get<NativeOperationsCatalog>("/device/native-operations")
                opsCatalog = cat
                if (cat.available) {
                    runCatching { apiClient.get<NativeOpState>("/device/native-operations/state/storage") }
                        .onSuccess { opStates["storage"] = it }
                    cat.groups.filter { it.id !in listOf("recorder", "parking", "volume") }.forEach { grp ->
                        runCatching { apiClient.get<NativeOpState>("/device/native-operations/state/${grp.id}") }
                            .onSuccess { opStates[grp.id] = it }
                    }
                }
            }
        }
    }

    LaunchedEffect(Unit) { loadAll() }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (error.isNotBlank()) NccBanner(error, BannerTone.ERROR, onDismiss = { error = "" })
        if (notice.isNotBlank()) NccBanner(notice, BannerTone.SUCCESS, onDismiss = { notice = "" })

        SectionTabs(
            tabs = listOf("device" to "本体・Alexa・SD", "stock" to "運転支援・ナビ・カメラ", "actions" to "操作・診断"),
            selectedId = subTab,
            onSelect = { subTab = it },
        )

        when (subTab) {
            "device" -> {
                // Alexa Wake word
                NccPanel(
                    title = "Alexa ウェイクワード",
                    icon = Icons.Default.Mic,
                    headerEnd = {
                        alexa?.let {
                            NccTag(text = if (it.wakeWord.fixed) "ON (連携固定)" else if (it.wakeWord.enabled) "ON" else "OFF")
                        }
                        IconButton(onClick = { loadAll() }) {
                            Icon(Icons.Default.Refresh, contentDescription = "更新", tint = p.muted)
                        }
                    },
                ) {
                    val ax = alexa
                    if (ax == null) {
                        Text("Alexa設定を取得できません（専用ヘルパー未導入または待機中）。", color = p.muted, fontSize = 12.sp)
                    } else {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text("「アレクサ」の呼びかけを認識する", color = p.text, fontSize = 13.sp)
                            Switch(
                                checked = ax.wakeWord.enabled,
                                enabled = !busy && ax.writable && !ax.wakeWord.fixed,
                                onCheckedChange = { nextEnabled ->
                                    scope.launch {
                                        busy = true
                                        error = ""
                                        runCatching {
                                            val obs = apiClient.put<Observation>(
                                                "/device/alexa",
                                                """{"expectedValue":${ax.wakeWord.value},"enabled":$nextEnabled}""",
                                            )
                                            obs.data?.let { alexa = apiClient.json.decodeFromJsonElement(AlexaData.serializer(), it) }
                                            notice = "Alexa設定を更新しました"
                                        }.onFailure { error = it.message ?: "Alexa設定の更新に失敗しました" }
                                        busy = false
                                    }
                                },
                            )
                        }
                        ax.reason?.let { Text(it, color = p.muted, fontSize = 11.sp) }
                    }
                }

                // SD card format
                val sdState = opStates["storage"]?.values
                NccPanel(title = "SDカード操作") {
                    KeyValueRow(
                        "挿入状態",
                        when (sdState?.get("inserted")) {
                            1L -> "挿入済み"
                            0L -> "未挿入"
                            else -> "未取得"
                        },
                    )
                    Button(
                        enabled = !busy && sdState?.get("inserted") == 1L && sdState["formatStatus"] == 2L,
                        colors = ButtonDefaults.buttonColors(containerColor = p.danger),
                        onClick = {
                            scope.launch {
                                busy = true
                                error = ""
                                runCatching {
                                    val raw = apiClient.requestRaw("/device/native-operations/sd-format/prepare", "POST", "{}")
                                    val obj = apiClient.json.parseToJsonElement(raw).jsonObject
                                    formatToken = obj["token"]?.jsonPrimitive?.content
                                    formatCapBytes = obj["capacityBytes"]?.jsonPrimitive?.longOrNull ?: 0L
                                    formatAck = false
                                    formatPhrase = ""
                                }.onFailure { error = it.message ?: "SDフォーマット準備に失敗しました" }
                                busy = false
                            }
                        },
                    ) {
                        Text("SDカードをフォーマット", color = Color.White)
                    }
                }

                // Other native operation groups (vehicle, installation, account, etc.)
                opsCatalog?.groups?.filter { it.id !in listOf("recorder", "parking", "volume") }?.forEach { grp ->
                    val vals = opStates[grp.id]?.values.orEmpty()
                    NccPanel(title = grp.label, icon = Icons.Default.Settings) {
                        grp.items.forEach { item ->
                            val curVal = vals[item.field]
                            val labelVal = if (curVal == null) "未取得"
                            else item.options?.firstOrNull { it.value.toLong() == curVal }?.label ?: curVal.toString()

                            if (item.writable && !item.options.isNullOrEmpty() && curVal != null) {
                                Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                    Text(item.label, color = p.text, fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
                                    FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                        item.options.forEach { opt ->
                                            val sel = opt.value.toLong() == curVal
                                            NccTag(
                                                text = opt.label,
                                                color = if (sel) p.accent else p.muted,
                                                background = if (sel) p.soft else p.bg,
                                                modifier = Modifier.clickable(enabled = !busy && !sel) {
                                                    scope.launch {
                                                        busy = true
                                                        error = ""
                                                        runCatching {
                                                            val st = apiClient.put<NativeOpState>(
                                                                "/device/native-operations/settings/${item.id}",
                                                                """{"expectedValue":$curVal,"value":${opt.value}}""",
                                                            )
                                                            opStates[grp.id] = st
                                                            notice = "${item.label}を変更しました"
                                                        }.onFailure { error = it.message ?: "変更に失敗しました" }
                                                        busy = false
                                                    }
                                                },
                                            )
                                        }
                                    }
                                }
                            } else {
                                KeyValueRow(item.label, labelVal)
                            }
                        }
                    }
                }
            }

            "stock" -> {
                val items = nativeSettings?.items.orEmpty()
                NccPanel(
                    title = "純正設定 (${items.size} 項目)",
                    icon = Icons.Default.Settings,
                    headerEnd = {
                        IconButton(onClick = { loadAll() }) {
                            Icon(Icons.Default.Refresh, contentDescription = "更新", tint = p.muted)
                        }
                    },
                ) {
                    if (items.isEmpty()) {
                        Text("純正設定を取得できません（対応ファームウェア・専用ヘルパーが必要です）。", color = p.muted, fontSize = 12.sp)
                    } else {
                        items.forEach { item: NativeSettingItem ->
                            val rawVal = item.value
                            val disp = item.valueLabel
                                ?: (rawVal as? JsonPrimitive)?.booleanOrNull?.let { if (it) "ON" else "OFF" }
                                ?: item.options?.firstOrNull { it.value == rawVal }?.label
                                ?: rawVal?.toString()
                                ?: "未取得"

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
                                            Text(item.label, color = p.text, fontWeight = FontWeight.SemiBold, fontSize = 13.sp)
                                            item.description?.let { Text(it, color = p.muted, fontSize = 11.sp) }
                                        }
                                        if (!item.writable || item.kind == "info") {
                                            NccTag(text = disp)
                                        }
                                    }
                                    if (item.writable && rawVal != null && !item.options.isNullOrEmpty()) {
                                        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                            item.options.forEach { opt ->
                                                val sel = opt.value == rawVal
                                                NccTag(
                                                    text = opt.label,
                                                    color = if (sel) p.accent else p.muted,
                                                    background = if (sel) p.soft else p.paper,
                                                    modifier = Modifier.clickable(enabled = !busy && !sel) {
                                                        scope.launch {
                                                            busy = true
                                                            error = ""
                                                            val expInt = (rawVal as? JsonPrimitive)?.intOrNull
                                                            val nextInt = (opt.value as? JsonPrimitive)?.intOrNull
                                                            if (expInt != null && nextInt != null) {
                                                                val enc = URLEncoder.encode(item.id, "UTF-8")
                                                                runCatching {
                                                                    val obs = apiClient.put<Observation>(
                                                                        "/device/native-settings/$enc",
                                                                        """{"expectedValue":$expInt,"value":$nextInt}""",
                                                                    )
                                                                    obs.data?.let {
                                                                        nativeSettings = apiClient.json.decodeFromJsonElement(NativeSettingsData.serializer(), it)
                                                                    }
                                                                    notice = "${item.label}を更新しました"
                                                                }.onFailure { error = it.message ?: "更新に失敗しました" }
                                                            }
                                                            busy = false
                                                        }
                                                    },
                                                )
                                            }
                                        }
                                    }
                                    item.reason?.let { Text(it, color = p.muted, fontSize = 11.sp) }
                                }
                            }
                        }
                    }
                }
            }

            "actions" -> {
                NccPanel(title = "操作・診断アクション", icon = Icons.Default.Build) {
                    val actions = opsCatalog?.actions.orEmpty()
                    if (actions.isEmpty()) {
                        Text("実行可能な操作アクションはありません。", color = p.muted, fontSize = 12.sp)
                    } else {
                        actions.forEach { act ->
                            Surface(
                                shape = RoundedCornerShape(8.dp),
                                color = p.bg,
                                border = BorderStroke(1.dp, p.line),
                                modifier = Modifier.fillMaxWidth(),
                            ) {
                                Row(
                                    modifier = Modifier.padding(10.dp),
                                    horizontalArrangement = Arrangement.SpaceBetween,
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    Column(modifier = Modifier.weight(1f)) {
                                        Text(act.label, color = p.text, fontWeight = FontWeight.SemiBold, fontSize = 13.sp)
                                        if (act.description.isNotBlank()) Text(act.description, color = p.muted, fontSize = 11.sp)
                                        act.reason?.let { Text(it, color = p.warn, fontSize = 11.sp) }
                                    }
                                    OutlinedButton(
                                        enabled = !busy && act.available,
                                        onClick = {
                                            scope.launch {
                                                busy = true
                                                error = ""
                                                val enc = URLEncoder.encode(act.id, "UTF-8")
                                                runCatching {
                                                    if (act.method.equals("GET", ignoreCase = true)) {
                                                        apiClient.requestRaw("/device/native-operations/actions/$enc", "GET")
                                                    } else {
                                                        val body = buildJsonObject {
                                                            if (!act.confirmation.isNullOrBlank()) put("confirmation", act.confirmation)
                                                            put("values", buildJsonObject {})
                                                        }.toString()
                                                        apiClient.requestRaw("/device/native-operations/actions/$enc", "POST", body)
                                                    }
                                                }.onSuccess { notice = "${act.label} を実行しました" }
                                                    .onFailure { error = it.message ?: "${act.label} に失敗しました" }
                                                busy = false
                                            }
                                        },
                                    ) { Text("実行") }
                                }
                            }
                        }
                    }
                }
            }
        }
        Spacer(modifier = Modifier.height(12.dp))
    }

    // SD Card Format confirmation dialog
    val tok = formatToken
    if (tok != null) {
        Dialog(onDismissRequest = { if (!busy) formatToken = null }) {
            Surface(shape = RoundedCornerShape(14.dp), color = p.paper, border = BorderStroke(1.dp, p.line)) {
                Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Text("SDカードをフォーマット", color = p.danger, fontWeight = FontWeight.Bold, fontSize = 16.sp)
                    Text("対象容量: ${Formatters.bytes(formatCapBytes)}\nSDカード内の録画・写真・位置情報はすべて消去されます。", color = p.text, fontSize = 12.sp)
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(checked = formatAck, onCheckedChange = { formatAck = it })
                        Text("全データ消去を確認しました", color = p.text, fontSize = 12.sp)
                    }
                    OutlinedTextField(
                        value = formatPhrase,
                        onValueChange = { formatPhrase = it },
                        label = { Text("「SDカードを消去」と入力") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        OutlinedButton(onClick = { formatToken = null }) { Text("キャンセル") }
                        Button(
                            enabled = !busy && formatAck && formatPhrase == "SDカードを消去",
                            colors = ButtonDefaults.buttonColors(containerColor = p.danger),
                            onClick = {
                                scope.launch {
                                    busy = true
                                    formatToken = null
                                    val body = buildJsonObject {
                                        put("token", tok)
                                        put("confirmation", "SDカードを消去")
                                        put("acknowledgeDataLoss", true)
                                    }.toString()
                                    runCatching { apiClient.requestRaw("/device/native-operations/sd-format/execute", "POST", body) }
                                        .onSuccess { notice = "SDカードフォーマット要求を送信しました" }
                                        .onFailure { error = it.message ?: "フォーマットに失敗しました" }
                                    busy = false
                                }
                            },
                        ) { Text("消去してフォーマット", color = Color.White) }
                    }
                }
            }
        }
    }
}
