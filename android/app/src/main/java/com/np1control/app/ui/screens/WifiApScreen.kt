package com.np1control.app.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
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
import androidx.compose.material.icons.filled.Public
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Router
import androidx.compose.material.icons.filled.Save
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Wifi
import androidx.compose.material3.Button
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
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.APData
import com.np1control.app.data.Formatters
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.data.UplinkView
import com.np1control.app.data.WiFiScanResult
import com.np1control.app.data.WiFiScanView
import com.np1control.app.data.WiFiTransaction
import com.np1control.app.data.WiFiView
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.KeyValueRow
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.NccTag
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

@Composable
fun WifiApScreen(
    apiClient: NccApiClient,
    onRefreshSummary: () -> Unit,
) {
    val p = LocalNccPalette.current
    val scope = rememberCoroutineScope()

    var uplink by remember { mutableStateOf<UplinkView?>(null) }
    var wifi by remember { mutableStateOf<WiFiView?>(null) }
    var ap by remember { mutableStateOf<APData?>(null) }
    var nearby by remember { mutableStateOf<List<WiFiScanResult>>(emptyList()) }

    var ssidInput by remember { mutableStateOf("") }
    var securityInput by remember { mutableStateOf("wpa2") }
    var passwordInput by remember { mutableStateOf("") }
    var showWifiSecret by remember { mutableStateOf(false) }

    var apEnabled by remember { mutableStateOf(true) }
    var apSsid by remember { mutableStateOf("") }
    var apPass by remember { mutableStateOf("") }
    var showApSecret by remember { mutableStateOf(false) }
    var apLoadedOnce by remember { mutableStateOf(false) }

    var busy by remember { mutableStateOf(false) }
    var scanning by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var notice by remember { mutableStateOf("") }

    suspend fun loadState() {
        runCatching { apiClient.get<UplinkView>("/network/uplink") }.onSuccess { uplink = it }
        runCatching { apiClient.get<WiFiView>("/network/wifi/networks") }.onSuccess { v ->
            wifi = v
            if (nearby.isEmpty() && v.lastScan != null) {
                nearby = v.lastScan.results.sortedByDescending { it.signalDBm }
            }
        }
        runCatching {
            val obs = apiClient.get<Observation>("/network/hotspot")
            obs.data?.let {
                val parsed = apiClient.json.decodeFromJsonElement(APData.serializer(), it)
                ap = parsed
                if (!apLoadedOnce && parsed.settings != null) {
                    apEnabled = parsed.settings.enabled
                    apSsid = parsed.settings.ssid
                    apPass = parsed.settings.passphrase
                    apLoadedOnce = true
                }
            }
        }
    }

    LaunchedEffect(Unit) {
        while (isActive) {
            loadState()
            delay(4000L)
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (error.isNotBlank()) NccBanner(error, BannerTone.ERROR, onDismiss = { error = "" })
        if (notice.isNotBlank()) NccBanner(notice, BannerTone.SUCCESS, onDismiss = { notice = "" })

        // Pending Wi-Fi confirmation transaction banner
        val pendingTx = wifi?.transaction?.takeIf { it.state == "pending" }
        if (pendingTx != null) {
            NccPanel(title = "このWi-Fi接続を確定しますか？", subtitle = "対象: ${pendingTx.targetSSID}") {
                Text(
                    if (pendingTx.canConfirm) "接続先を確認できました。確定するとNCCへ保存します。" else "接続先を確認しています…",
                    color = p.text,
                    fontSize = 12.sp,
                )
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Button(
                        enabled = !busy && pendingTx.canConfirm,
                        onClick = {
                            scope.launch {
                                busy = true
                                error = ""
                                runCatching {
                                    apiClient.post<WiFiTransaction>("/network/wifi/confirm", """{"transactionID":"${pendingTx.id}"}""")
                                }.onSuccess {
                                    notice = "Wi-Fi接続を確定しました"
                                    loadState()
                                    onRefreshSummary()
                                }.onFailure { error = it.message ?: "確定に失敗しました" }
                                busy = false
                            }
                        },
                    ) { Text("この接続を使う") }
                    OutlinedButton(
                        enabled = !busy,
                        onClick = {
                            scope.launch {
                                busy = true
                                error = ""
                                runCatching {
                                    apiClient.post<WiFiTransaction>("/network/wifi/rollback", """{"transactionID":"${pendingTx.id}"}""")
                                }.onSuccess {
                                    notice = "元のWi-Fi接続へ戻しました"
                                    loadState()
                                }.onFailure { error = it.message ?: "復旧に失敗しました" }
                                busy = false
                            }
                        },
                    ) { Text("元に戻す") }
                }
            }
        }

        // Uplink mode selector
        NccPanel(
            title = "インターネット回線",
            subtitle = "本体・純正アプリ・ホットスポットが使う上流回線",
            icon = Icons.Default.Public,
            headerEnd = {
                NccTag(
                    text = when (uplink?.status?.uplink) {
                        "wifi" -> "使用中: Wi-Fi"
                        "cellular" -> "使用中: 4G"
                        else -> "確認中"
                    },
                    color = p.accent,
                    background = p.soft,
                )
            },
        ) {
            SectionTabs(
                tabs = listOf("auto" to "自動", "wifi" to "Wi-Fi", "cellular" to "4G固定"),
                selectedId = uplink?.mode ?: "auto",
                onSelect = { nextMode ->
                    scope.launch {
                        busy = true
                        error = ""
                        runCatching { apiClient.put<UplinkView>("/network/uplink", """{"mode":"$nextMode"}""") }
                            .onSuccess {
                                uplink = it
                                notice = "インターネット回線モードを更新しました"
                            }
                            .onFailure { error = it.message ?: "切替に失敗しました" }
                        busy = false
                    }
                },
            )
            KeyValueRow("Wi-Fi STA", when (uplink?.status?.staOn) { true -> "オン"; false -> "オフ (4G)"; null -> "—" })
        }

        // Wi-Fi STA panel
        NccPanel(
            title = "Wi-Fi接続 (STA)",
            subtitle = if (wifi?.status?.state == "COMPLETED") "接続中: ${wifi?.status?.ssid} (${wifi?.status?.ipv4})" else "状態: ${wifi?.status?.state ?: "確認中"}",
            icon = Icons.Default.Wifi,
            headerEnd = {
                OutlinedButton(
                    enabled = !scanning && !busy && uplink?.mode != "cellular",
                    onClick = {
                        scope.launch {
                            scanning = true
                            error = ""
                            runCatching { apiClient.post<WiFiScanView>("/network/wifi/scan", "{}") }
                                .onSuccess { res ->
                                    nearby = res.results.sortedByDescending { it.signalDBm }
                                        .distinctBy { it.ssid to it.security }
                                    notice = "${nearby.size} 件のWi-Fiを検出しました"
                                }
                                .onFailure { error = it.message ?: "スキャンに失敗しました" }
                            scanning = false
                        }
                    },
                ) {
                    Icon(Icons.Default.Search, contentDescription = null)
                    Text(if (scanning) " 検索中…" else " 周辺を検索")
                }
            },
        ) {
            if (nearby.isNotEmpty()) {
                Text("周辺のWi-Fi（タップして入力）", color = p.muted, fontSize = 11.sp, fontWeight = FontWeight.SemiBold)
                nearby.take(10).forEach { net ->
                    Surface(
                        shape = RoundedCornerShape(8.dp),
                        color = p.bg,
                        border = BorderStroke(1.dp, p.line),
                        modifier = Modifier.fillMaxWidth().clickable {
                            ssidInput = net.ssid
                            securityInput = if (net.security == "open") "open" else "wpa2"
                        },
                    ) {
                        Row(
                            modifier = Modifier.padding(horizontal = 10.dp, vertical = 8.dp),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(net.ssid.ifBlank { "(非公開SSID)" }, color = p.text, fontSize = 13.sp, fontWeight = FontWeight.Medium)
                            Text("${net.security.uppercase()} · ${net.signalDBm} dBm", color = p.muted, fontSize = 11.sp)
                        }
                    }
                }
            }

            if (!wifi?.saved.isNullOrEmpty()) {
                Text("NCC保存済みWi-Fi（タップして接続）", color = p.muted, fontSize = 11.sp, fontWeight = FontWeight.SemiBold)
                wifi!!.saved.forEach { saved ->
                    Surface(
                        shape = RoundedCornerShape(8.dp),
                        color = p.bg,
                        border = BorderStroke(1.dp, p.line),
                        modifier = Modifier.fillMaxWidth().clickable(enabled = !busy) {
                            scope.launch {
                                busy = true
                                error = ""
                                runCatching {
                                    apiClient.post<WiFiTransaction>("/network/wifi/connect-saved", """{"profileID":"${saved.id}"}""")
                                }.onSuccess {
                                    notice = "${saved.ssid} への接続確認を開始しました"
                                    loadState()
                                }.onFailure { error = it.message ?: "接続開始に失敗しました" }
                                busy = false
                            }
                        },
                    ) {
                        Row(
                            modifier = Modifier.padding(horizontal = 10.dp, vertical = 8.dp),
                            horizontalArrangement = Arrangement.SpaceBetween,
                        ) {
                            Text(saved.ssid, color = p.text, fontSize = 13.sp, fontWeight = FontWeight.Medium)
                            Text(if (saved.security == "open") "認証なし" else "WPA2", color = p.muted, fontSize = 11.sp)
                        }
                    }
                }
            }

            Text("新しい接続先", color = p.text, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
            OutlinedTextField(
                value = ssidInput,
                onValueChange = { ssidInput = it },
                label = { Text("SSID") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            SectionTabs(
                tabs = listOf("wpa2" to "WPA2 Personal", "open" to "認証なし"),
                selectedId = securityInput,
                onSelect = { securityInput = it },
            )
            if (securityInput == "wpa2") {
                OutlinedTextField(
                    value = passwordInput,
                    onValueChange = { passwordInput = it },
                    label = { Text("パスワード") },
                    singleLine = true,
                    visualTransformation = if (showWifiSecret) VisualTransformation.None else PasswordVisualTransformation(),
                    modifier = Modifier.fillMaxWidth(),
                )
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(checked = showWifiSecret, onCheckedChange = { showWifiSecret = it })
                    Text("パスワードを表示", color = p.muted, fontSize = 12.sp)
                }
            }
            Button(
                enabled = !busy && ssidInput.isNotBlank() && uplink?.mode != "cellular",
                onClick = {
                    scope.launch {
                        busy = true
                        error = ""
                        val body = buildJsonObject {
                            put("ssid", ssidInput)
                            put("security", securityInput)
                            if (securityInput == "wpa2") put("password", passwordInput)
                        }.toString()
                        runCatching { apiClient.post<WiFiTransaction>("/network/wifi/connect-new", body) }
                            .onSuccess {
                                passwordInput = ""
                                notice = "Wi-Fi接続確認を開始しました（90秒以内に確定してください）"
                                loadState()
                            }
                            .onFailure { error = it.message ?: "接続に失敗しました" }
                        busy = false
                    }
                },
            ) {
                Text("このWi-Fiへ接続")
            }
        }

        // Hotspot AP panel
        NccPanel(
            title = "ホットスポット (NP1 AP)",
            subtitle = "管理画面: http://192.168.226.1",
            icon = Icons.Default.Router,
            headerEnd = {
                NccTag(
                    text = if (ap?.enabled == true) "稼働中" else "停止中",
                    color = if (ap?.enabled == true) p.ok else p.muted,
                    background = if (ap?.enabled == true) p.okSoft else p.bg,
                )
                IconButton(onClick = { scope.launch { loadState() } }) {
                    Icon(Icons.Default.Refresh, contentDescription = "更新", tint = p.muted)
                }
            },
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("ホットスポットを有効にする", color = p.text, fontSize = 13.sp, fontWeight = FontWeight.Medium)
                Switch(checked = apEnabled, onCheckedChange = { apEnabled = it })
            }
            OutlinedTextField(
                value = apSsid,
                onValueChange = { apSsid = it },
                label = { Text("ホットスポット SSID") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = apPass,
                onValueChange = { apPass = it },
                label = { Text("接続パスワード (8〜63文字)") },
                singleLine = true,
                visualTransformation = if (showApSecret) VisualTransformation.None else PasswordVisualTransformation(),
                modifier = Modifier.fillMaxWidth(),
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                Checkbox(checked = showApSecret, onCheckedChange = { showApSecret = it })
                Text("パスワードを表示", color = p.muted, fontSize = 12.sp)
            }
            Button(
                enabled = !busy && ap?.settings != null,
                onClick = {
                    scope.launch {
                        val st = ap?.settings ?: return@launch
                        busy = true
                        error = ""
                        val body = buildJsonObject {
                            put("expectedRevision", st.revision)
                            put("settings", buildJsonObject {
                                put("enabled", apEnabled)
                                put("ssid", apSsid)
                                if (apPass != st.passphrase && apPass.isNotEmpty()) {
                                    put("passphrase", apPass)
                                }
                            })
                        }.toString()
                        runCatching { apiClient.put<Observation>("/network/hotspot", body) }
                            .onSuccess {
                                notice = "ホットスポット設定を反映しました"
                                apLoadedOnce = false
                                loadState()
                                onRefreshSummary()
                            }
                            .onFailure { error = it.message ?: "ホットスポット反映に失敗しました" }
                        busy = false
                    }
                },
            ) {
                Icon(Icons.Default.Save, contentDescription = null)
                Text(" ホットスポットに反映")
            }

            KeyValueRow(
                "共有上流",
                if (ap?.sharing?.ready == true) {
                    if (ap?.sharing?.upstream?.kind == "cellular") "4G経由" else "Wi-Fi経由"
                } else "上流待ち / 停止",
            )
            KeyValueRow("アドレス / チャンネル", "${ap?.address ?: "192.168.226.1"} / ch.${ap?.channel ?: "—"}", mono = true)
            KeyValueRow("接続端末数", ap?.clientCount?.let { "$it 台" } ?: "未取得")

            val clients = ap?.clients.orEmpty()
            if (clients.isNotEmpty()) {
                Text("接続中の端末", color = p.muted, fontSize = 11.sp, fontWeight = FontWeight.SemiBold)
                clients.forEach { c ->
                    Surface(
                        shape = RoundedCornerShape(8.dp),
                        color = p.bg,
                        border = BorderStroke(1.dp, p.line),
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Column(modifier = Modifier.padding(10.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                                Text(c.hostname?.ifBlank { null } ?: "接続端末", color = p.text, fontWeight = FontWeight.SemiBold, fontSize = 13.sp)
                                Text(c.ipv4 ?: "IP未取得", color = p.accent, fontSize = 12.sp, fontFamily = FontFamily.Monospace)
                            }
                            Text(
                                "${c.mac} · ${c.signalDBm?.let { "$it dBm · " } ?: ""}↓${Formatters.trafficVolume(Formatters.trafficBigInt(c.receivedBytes))} ↑${Formatters.trafficVolume(Formatters.trafficBigInt(c.sentBytes))}",
                                color = p.muted,
                                fontSize = 11.sp,
                            )
                        }
                    }
                }
            }
        }
        Spacer(modifier = Modifier.height(12.dp))
    }
}
