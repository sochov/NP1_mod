package com.np1control.app.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Devices
import androidx.compose.material.icons.filled.Language
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Save
import androidx.compose.material.icons.filled.SignalCellularAlt
import androidx.compose.material.icons.filled.Wifi
import androidx.compose.material.icons.filled.WifiTethering
import androidx.compose.material.icons.filled.Bolt
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.APData
import com.np1control.app.data.Formatters
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.NccApiException
import com.np1control.app.data.Observation
import com.np1control.app.data.SystemSummary
import com.np1control.app.data.TrafficUsageData
import com.np1control.app.data.UplinkView
import com.np1control.app.data.WiFiScanResult
import com.np1control.app.data.WiFiScanView
import com.np1control.app.data.WiFiTransaction
import com.np1control.app.data.WiFiView
import com.np1control.app.data.parseInstantMs
import com.np1control.app.ui.components.AdaptiveColumns
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.ChoiceCard
import com.np1control.app.ui.components.EmptyState
import com.np1control.app.ui.components.FieldLabel
import com.np1control.app.ui.components.HintText
import com.np1control.app.ui.components.IconBadge
import com.np1control.app.ui.components.KeyValueRow
import com.np1control.app.ui.components.ListRow
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.ScreenColumn
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.components.StatusPill
import com.np1control.app.ui.components.SwitchRow
import com.np1control.app.ui.components.Tone
import com.np1control.app.ui.components.PanelAction
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

private fun wifiReason(e: Throwable): String {
    val code = (e as? NccApiException)?.code.orEmpty()
    return when {
        code.contains("WIFI_TARGET_NOT_CONNECTED") -> "接続先からIPアドレスを取得するまでお待ちください。"
        code.contains("WIFI_TRANSACTION_PENDING") -> "今の接続を確定するか、元に戻してください。"
        code.contains("WIFI_SETUP_PENDING") -> "回線の切り替え中です。少し待ってからもう一度操作してください。"
        code.contains("WIFI_CELLULAR_FIXED") -> "「インターネット回線」を「Wi-Fi」か「自動」にすると、検索や接続ができます。"
        code.contains("WIFI_SCAN_TIMEOUT") || code.contains("WIFI_SCAN_REJECTED") -> "周辺のWi-Fi検索が完了しませんでした。もう一度検索するか、SSIDを入力してください。"
        code.contains("WIFI_CONNECTION_INVALID") -> "SSIDは1〜32バイト、WPA2パスワードは半角8〜63文字か64桁の16進数で入力してください。"
        code == "REVISION_CONFLICT" -> "別の操作で設定が変わりました。最新状態を読み直してください。"
        code.contains("WIFI_ROLLBACK") -> "元の接続への復旧を確認できません。NP1のホットスポットから状態を確認してください。"
        code.contains("WIFI_STATION_UNAVAILABLE") || code.contains("WIFI_CONTROL_UNAVAILABLE") || code.contains("WIFI_SETUP_UNAVAILABLE") ->
            "本体のWi-Fiを利用できません。インターネット回線の状態を確認してください。"
        else -> e.message ?: "操作できませんでした"
    }
}

private val uplinkReasons = mapOf(
    "wifi-unavailable" to "Wi-Fiが圏外になったため4Gへ",
    "saved-network-seen" to "保存済みWi-Fiが見えたためWi-Fiへ",
    "near-last-connected-place" to "Wi-Fiの近くに戻ったためWi-Fiへ",
    "owner-manual" to "手動の設定に合わせて",
    "auto-disabled" to "自動切替を止めたためWi-Fiへ",
    "periodic-recheck" to "保存済みWi-Fiへの再接続を試すため",
    "probe-timeout" to "Wi-Fi回線を確認できなかったため4Gへ",
    "wifi-setup-ended" to "周辺検索・接続確認を終えたため4Gへ",
)

@Composable
fun WifiApScreen(
    summary: SystemSummary,
    apiClient: NccApiClient,
    onRefreshSummary: () -> Unit,
) {
    val p = LocalNccPalette.current
    val scope = rememberCoroutineScope()

    var uplink by remember { mutableStateOf<UplinkView?>(null) }
    var uplinkBusy by remember { mutableStateOf(false) }
    var uplinkError by remember { mutableStateOf("") }
    var wifi by remember { mutableStateOf<WiFiView?>(null) }
    var ap by remember { mutableStateOf<APData?>(null) }
    var usage by remember { mutableStateOf<TrafficUsageData?>(null) }
    var nearby by remember { mutableStateOf<List<WiFiScanResult>>(emptyList()) }
    var scanAt by remember { mutableStateOf("") }
    var scanDone by remember { mutableStateOf(false) }
    var now by remember { mutableLongStateOf(System.currentTimeMillis()) }

    var ssid by remember { mutableStateOf("") }
    var security by remember { mutableStateOf("wpa2") }
    var password by remember { mutableStateOf("") }
    var showSecrets by remember { mutableStateOf(false) }

    var apInstalled by remember { mutableStateOf(false) }
    var apEnabled by remember { mutableStateOf(false) }
    var apSsid by remember { mutableStateOf("") }
    var apPass by remember { mutableStateOf("") }
    var showApSecret by remember { mutableStateOf(false) }

    var busy by remember { mutableStateOf(false) }
    var scanning by remember { mutableStateOf(false) }
    var apBusy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var message by remember { mutableStateOf("") }
    var loadError by remember { mutableStateOf("") }
    var apError by remember { mutableStateOf("") }
    var apMessage by remember { mutableStateOf("") }

    fun installAp(data: APData?, force: Boolean = false) {
        ap = data
        val st = data?.settings
        if (st != null && (!apInstalled || force)) {
            apEnabled = st.enabled
            apSsid = st.ssid
            apPass = st.passphrase
            apInstalled = true
        }
    }

    suspend fun load() {
        runCatching { apiClient.get<WiFiView>("/network/wifi/networks") }
            .onSuccess { v ->
                wifi = v
                loadError = ""
                val recent = v.lastScan
                if (recent != null && !scanning && recent.scannedAt != scanAt) {
                    scanAt = recent.scannedAt
                    nearby = recent.results
                    scanDone = true
                }
            }
            .onFailure { loadError = wifiReason(it) }
        runCatching {
            val obs = apiClient.get<Observation>("/network/hotspot")
            obs.data?.let { apiClient.json.decodeFromJsonElement(APData.serializer(), it) }
        }.onSuccess { installAp(it) }.onFailure { if (ap == null) apError = wifiReason(it) }
    }

    suspend fun loadUplink() {
        runCatching { apiClient.get<UplinkView>("/network/uplink") }
            .onSuccess { uplink = it; uplinkError = "" }
            .onFailure { uplinkError = "回線の設定を取得できません。" }
    }

    LaunchedEffect(Unit) {
        launch { while (isActive) { load(); delay(2000) } }
        launch { while (isActive) { loadUplink(); delay(5000) } }
        launch {
            while (isActive) {
                runCatching {
                    apiClient.get<Observation>("/traffic/usage").data?.let { apiClient.json.decodeFromJsonElement(TrafficUsageData.serializer(), it) }
                }.onSuccess { usage = it }
                now = System.currentTimeMillis()
                delay(1000)
            }
        }
    }
    DisposableEffect(Unit) { onDispose { password = ""; apPass = "" } }

    val pending = wifi?.transaction?.takeIf { it.state == "pending" }
    val remaining = pending?.let { tx -> parseInstantMs(tx.expiresAt)?.let { ((it - now) / 1000).coerceAtLeast(0) } } ?: 0L
    val connected = wifi?.status?.state == "COMPLETED"
    val stationOff = wifi?.status?.state == "STA_OFF" || uplink?.status?.staOn == false
    val cellularFixed = uplink?.mode == "cellular"
    val controlsDisabled = cellularFixed || scanning || busy || pending != null
    val wifiUsage = usage?.networks?.wifi

    fun connect(body: String, saved: Boolean) {
        if (controlsDisabled) return
        scope.launch {
            busy = true
            error = ""
            message = ""
            runCatching { apiClient.post<WiFiTransaction>("/network/wifi/${if (saved) "connect-saved" else "connect-new"}", body) }
                .onSuccess { tx ->
                    if (tx.state != "pending") error = "接続確認の開始を確認できません。"
                    password = ""
                    load()
                }
                .onFailure { error = wifiReason(it) }
            busy = false
        }
    }

    fun finish(confirm: Boolean) {
        val tx = pending ?: return
        scope.launch {
            busy = true
            error = ""
            message = ""
            runCatching { apiClient.post<WiFiTransaction>("/network/wifi/${if (confirm) "confirm" else "rollback"}", """{"transactionID":"${tx.id}"}""") }
                .onSuccess { v ->
                    if (v.state != (if (confirm) "confirmed" else "rolled-back")) error = "操作の完了を確認できません。"
                    else message = if (confirm) "Wi-Fi接続を確定しました。" else "元のWi-Fi接続へ戻しました。"
                    load()
                    onRefreshSummary()
                }
                .onFailure { error = wifiReason(it) }
            busy = false
        }
    }

    ScreenColumn {
        if (loadError.isNotBlank()) NccBanner(loadError, BannerTone.ERROR)
        if (pending != null) {
            Surface(shape = RoundedCornerShape(20.dp), color = p.warnSoft, border = BorderStroke(1.5.dp, p.warn.copy(alpha = 0.5f))) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                        Box(Modifier.size(56.dp).clip(CircleShape).background(p.paper), contentAlignment = Alignment.Center) {
                            Text("${remaining}秒", color = p.warn, style = MaterialTheme.typography.titleMedium)
                        }
                        Column(Modifier.weight(1f)) {
                            Text("このWi-Fi接続を使いますか？", color = p.text, style = MaterialTheme.typography.titleMedium)
                            Text(
                                "${pending.targetSSID} への接続を確認してください。時間内に確定しないと、元の接続へ自動で戻します。",
                                color = p.text,
                                style = MaterialTheme.typography.bodySmall,
                            )
                        }
                    }
                    Text(if (pending.canConfirm) "接続先を確認できました" else "接続先を確認しています…", color = p.warn, style = MaterialTheme.typography.labelMedium)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Button(enabled = !busy && pending.canConfirm && remaining > 0, onClick = { finish(true) }) {
                            Icon(Icons.Default.Check, contentDescription = null, modifier = Modifier.size(16.dp))
                            Text(" この接続を使う")
                        }
                        OutlinedButton(enabled = !busy, onClick = { finish(false) }) { Text("元に戻す") }
                    }
                }
            }
        }
        if (error.isNotBlank()) NccBanner(error, BannerTone.ERROR, onDismiss = { error = "" })
        if (message.isNotBlank()) NccBanner(message, BannerTone.SUCCESS, onDismiss = { message = "" })

        // ── Uplink ──
        val st = uplink?.status
        NccPanel(
            title = "インターネット回線",
            subtitle = "本体・純正アプリ・ホットスポットにつないだ端末が使う回線です。",
            icon = Icons.Default.Language,
            headerEnd = { PanelAction(onClick = { scope.launch { loadUplink() } }) { Icon(Icons.Default.Refresh, contentDescription = "回線の状態を更新", tint = p.muted) } },
        ) {
            listOf(
                Triple("auto", "自動", "Wi-Fiが圏外になったら4Gへ。保存済みWi-Fiの近くに戻ったらWi-Fiへ。") to Icons.Default.Bolt,
                Triple("wifi", "Wi-Fi", "常にWi-Fiを使います。圏外の間はインターネットにつながりません。") to Icons.Default.Wifi,
                Triple("cellular", "4G", "常に4Gを使います。Wi-Fi接続は止まります。") to Icons.Default.SignalCellularAlt,
            ).forEach { (opt, icon) ->
                ChoiceCard(
                    title = opt.second,
                    description = opt.third,
                    icon = icon,
                    selected = uplink?.mode == opt.first,
                    enabled = !uplinkBusy && uplink != null,
                    onClick = {
                        if (uplink?.mode == opt.first) return@ChoiceCard
                        scope.launch {
                            uplinkBusy = true
                            runCatching { apiClient.put<UplinkView>("/network/uplink", """{"mode":"${opt.first}"}""") }
                                .onSuccess { uplink = it; uplinkError = if (it.mode != opt.first) "設定を保存できません。もう一度お試しください。" else "" }
                                .onFailure { uplinkError = "設定を保存できません。もう一度お試しください。" }
                            uplinkBusy = false
                        }
                    },
                )
            }
            Row(horizontalArrangement = Arrangement.spacedBy(24.dp)) {
                Column {
                    Text("使用中の回線", color = p.muted, style = MaterialTheme.typography.labelSmall)
                    Text(
                        when (st?.uplink) { "wifi" -> "Wi-Fi"; "cellular" -> "4G"; "none" -> "未接続"; null -> "確認中"; else -> "その他" },
                        color = when (st?.uplink) { "wifi" -> p.accent; "cellular" -> p.uploadOrange; else -> p.text },
                        style = MaterialTheme.typography.titleMedium,
                    )
                }
                Column {
                    Text("本体のWi-Fi接続", color = p.muted, style = MaterialTheme.typography.labelSmall)
                    Text(when (st?.staOn) { true -> "オン"; false -> "オフ（4Gを使用）"; null -> "—" }, color = p.text, style = MaterialTheme.typography.titleMedium)
                }
            }
            st?.state?.lastReason?.let { r -> KeyValueRow("直近の切り替え", uplinkReasons[r] ?: r) }
            if (uplink != null && st != null && st.mode != uplink?.mode) HintText("設定を反映しています…（10秒ほどかかります）")
            if (st != null && !st.helperAvailable) HintText("切り替えに必要な部品が本体にないため、回線を切り替えられません。", color = p.danger)
            else if (st?.error != null) HintText("直前の切り替えを確認できませんでした。しばらくすると再試行します。", color = p.warn)
            if (uplinkError.isNotBlank()) HintText(uplinkError, color = p.danger)
        }

        AdaptiveColumns(
            minColumnWidth = 380.dp,
            left = {
                // ── Wi-Fi station ──
                NccPanel(
                    title = "Wi-Fi接続",
                    subtitle = "NP1からインターネットへ接続するWi-Fiを選びます。",
                    icon = Icons.Default.Wifi,
                ) {
                    Row(
                        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(p.raised).padding(12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        IconBadge(Icons.Default.Wifi, tone = if (connected) Tone.OK else Tone.NEUTRAL, size = 44.dp)
                        Column(Modifier.weight(1f)) {
                            Text(
                                when {
                                    connected -> "接続中"
                                    scanning -> "接続先を検索中"
                                    stationOff -> "Wi-Fi停止中"
                                    wifi != null -> "未接続"
                                    else -> "状態を確認中"
                                },
                                color = if (connected) p.ok else p.muted,
                                style = MaterialTheme.typography.labelMedium,
                            )
                            Text(wifi?.status?.ssid?.ifBlank { null } ?: "接続先なし", color = p.text, style = MaterialTheme.typography.titleMedium)
                            Text("IPアドレス ${wifi?.status?.ipv4?.ifBlank { null } ?: "未取得"}", color = p.muted, style = MaterialTheme.typography.bodySmall)
                        }
                        Column(horizontalAlignment = Alignment.End) {
                            Text("本日 ${Formatters.trafficVolume(networkTotal(wifiUsage, false))}", color = p.muted, fontSize = 11.sp)
                            Text("今月 ${Formatters.trafficVolume(networkTotal(wifiUsage, true))}", color = p.muted, fontSize = 11.sp)
                        }
                    }
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        FieldLabel("接続先を選ぶ", Modifier.weight(1f))
                        OutlinedButton(
                            enabled = !controlsDisabled,
                            onClick = {
                                scope.launch {
                                    scanning = true
                                    scanDone = false
                                    nearby = emptyList()
                                    error = ""
                                    message = ""
                                    runCatching { apiClient.post<WiFiScanView>("/network/wifi/scan", "{}") }
                                        .onSuccess { scanAt = it.scannedAt; nearby = it.results; scanDone = true }
                                        .onFailure { error = wifiReason(it) }
                                    scanning = false
                                }
                            },
                        ) {
                            Icon(Icons.Default.Refresh, contentDescription = null, modifier = Modifier.size(16.dp))
                            Text(if (scanning) " 検索中…" else " 周辺を検索")
                        }
                    }
                    if (cellularFixed) HintText("「インターネット回線」を「Wi-Fi」か「自動」にすると検索・接続できます。", color = p.warn)
                    else if (stationOff) HintText("検索・接続のときに一時的にWi-Fiを起動します。Tailscaleが切れた場合は、NP1のホットスポットからつなぎ直してください。")
                    val unique = nearby.sortedByDescending { it.signalDBm }.distinctBy { it.ssid to it.security }
                    if (unique.isEmpty()) {
                        HintText(
                            when {
                                scanning -> "周辺のWi-Fiを検索しています…"
                                scanDone -> "周辺のWi-Fiが見つかりませんでした。"
                                else -> "周辺を検索するか、下でSSIDを入力してください。"
                            },
                        )
                    }
                    unique.forEach { net ->
                        ListRow(
                            title = net.ssid.ifBlank { "非公開SSID" },
                            subtitle = "${when (net.security) { "open" -> "認証なし"; "wpa2" -> "WPA2 Personal"; else -> "未対応の認証方式" }} · ${net.signalDBm} dBm",
                            icon = Icons.Default.Wifi,
                            iconTone = if (net.signalDBm > -65) Tone.OK else if (net.signalDBm > -78) Tone.ACCENT else Tone.NEUTRAL,
                            enabled = !controlsDisabled && net.security != "unsupported",
                            trailing = { if (net.security != "open") Icon(Icons.Default.Lock, contentDescription = "パスワードあり", tint = p.faint, modifier = Modifier.size(14.dp)) },
                            onClick = { ssid = net.ssid; security = if (net.security == "open") "open" else "wpa2"; password = ""; message = "" },
                        )
                    }
                    if (wifi?.networks?.isNotEmpty() == true) {
                        FieldLabel("本体が保持している接続先")
                        wifi!!.networks.forEach { n ->
                            ListRow(
                                title = n.ssid.ifBlank { "非公開SSID" },
                                subtitle = if (n.current) "接続中" else if (n.managed) "NCCで管理している接続先" else "本体の接続先",
                                icon = Icons.Default.Wifi,
                                iconTone = if (n.current) Tone.OK else Tone.NEUTRAL,
                                enabled = !controlsDisabled && !n.current,
                                trailing = { if (n.current) Icon(Icons.Default.Check, contentDescription = null, tint = p.ok) },
                                showChevron = !n.current,
                                onClick = { connect("""{"networkID":${n.id}}""", saved = true) },
                            )
                        }
                    }
                    if (wifi?.saved?.isNotEmpty() == true) {
                        FieldLabel("NCCに保存した接続先")
                        wifi!!.saved.forEach { s ->
                            ListRow(
                                title = s.ssid,
                                subtitle = if (s.security == "open") "認証なし" else if (showSecrets) s.password else "••••••••",
                                icon = Icons.Default.Save,
                                enabled = !controlsDisabled,
                                onClick = { connect("""{"profileID":"${s.id}"}""", saved = true) },
                            )
                        }
                    }
                }
            },
            right = {
                NccPanel(title = "新しい接続先", icon = Icons.Default.Wifi) {
                    OutlinedTextField(
                        value = ssid,
                        onValueChange = { ssid = it.take(32) },
                        label = { Text("SSID") },
                        singleLine = true,
                        enabled = !controlsDisabled,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    SectionTabs(
                        tabs = listOf("wpa2" to "WPA2 Personal", "open" to "認証なし"),
                        selectedId = security,
                        onSelect = { security = it },
                        enabled = !controlsDisabled,
                        fill = true,
                    )
                    if (security == "wpa2") {
                        OutlinedTextField(
                            value = password,
                            onValueChange = { password = it.take(64) },
                            label = { Text("パスワード") },
                            singleLine = true,
                            enabled = !controlsDisabled,
                            visualTransformation = if (showSecrets) VisualTransformation.None else PasswordVisualTransformation(),
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                    SwitchRow("パスワードを表示する", showSecrets, { showSecrets = it })
                    HintText("接続を始めてから90秒以内に確定すると、NCCへ保存します。確認中に画面が切れたら、ホットスポットかTailscaleからつなぎ直してください。")
                    Button(
                        enabled = !controlsDisabled && ssid.isNotBlank(),
                        modifier = Modifier.fillMaxWidth(),
                        onClick = {
                            val body = buildJsonObject {
                                put("ssid", ssid)
                                put("security", security)
                                put("password", if (security == "open") "" else password)
                            }.toString()
                            connect(body, saved = false)
                        },
                    ) { Text(if (busy) "接続操作中…" else "このWi-Fiへ接続") }
                }

                // ── Hotspot ──
                val data = ap
                NccPanel(
                    title = "ホットスポット",
                    subtitle = "スマートフォンやPCをつなぎ、NP1の回線を共有します。つないだ端末からは http://192.168.226.1 で管理画面を開けます。",
                    icon = Icons.Default.WifiTethering,
                    headerEnd = {
                        StatusPill(
                            when (data?.enabled) { true -> "稼働中"; false -> "停止中"; null -> "未取得" },
                            if (data?.enabled == true) Tone.OK else Tone.NEUTRAL,
                        )
                    },
                ) {
                    if (apError.isNotBlank()) NccBanner(apError, BannerTone.ERROR, onDismiss = { apError = "" })
                    if (apMessage.isNotBlank()) NccBanner(apMessage, BannerTone.SUCCESS, onDismiss = { apMessage = "" })
                    val apDisabled = apBusy || pending != null || !apInstalled
                    SwitchRow("ホットスポットを有効にする", apEnabled, { apEnabled = it }, enabled = !apDisabled)
                    OutlinedTextField(
                        value = apSsid,
                        onValueChange = { apSsid = it.take(32) },
                        label = { Text("SSID") },
                        singleLine = true,
                        enabled = !apDisabled,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = apPass,
                        onValueChange = { apPass = it.take(63) },
                        label = { Text("接続パスワード（${if (data?.settings?.passphraseConfigured == true) "保存済み" else "未設定"}）") },
                        placeholder = { Text("8〜63文字の半角英数字・記号") },
                        singleLine = true,
                        enabled = !apDisabled,
                        visualTransformation = if (showApSecret) VisualTransformation.None else PasswordVisualTransformation(),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    SwitchRow("パスワードを表示する", showApSecret, { showApSecret = it })
                    HintText("変更すると、接続中の端末はいったん切断されます。")
                    Button(
                        enabled = !apDisabled,
                        modifier = Modifier.fillMaxWidth(),
                        onClick = {
                            val settings = data?.settings ?: return@Button
                            scope.launch {
                                apBusy = true
                                apError = ""
                                apMessage = ""
                                val body = buildJsonObject {
                                    put("expectedRevision", settings.revision)
                                    put("settings", buildJsonObject {
                                        put("enabled", apEnabled)
                                        put("ssid", apSsid)
                                        if (apPass != settings.passphrase) put("passphrase", apPass)
                                    })
                                }.toString()
                                runCatching {
                                    val obs = apiClient.put<Observation>("/network/hotspot", body)
                                    obs.data?.let { apiClient.json.decodeFromJsonElement(APData.serializer(), it) }
                                }.onSuccess { v ->
                                    if (v?.enabled != apEnabled || v.settings?.ssid != apSsid || (apEnabled && v.dhcpReady != true)) {
                                        apError = "ホットスポットの反映を確認できません。"
                                    } else {
                                        installAp(v, force = true)
                                        apMessage = "ホットスポットの反映を確認しました。"
                                        onRefreshSummary()
                                    }
                                }.onFailure { apError = wifiReason(it) }
                                apBusy = false
                            }
                        },
                    ) {
                        Icon(Icons.Default.Save, contentDescription = null, modifier = Modifier.size(16.dp))
                        Text(if (apBusy) " 反映中…" else " ホットスポットに反映")
                    }
                    if (pending != null) HintText("Wi-Fiの接続確認中は、ホットスポットの変更を待ってください。")

                    FieldLabel("接続の詳細")
                    KeyValueRow(
                        "インターネット共有",
                        when {
                            data?.sharing?.ready == true -> if (data.sharing.upstream?.kind == "cellular") "4G経由" else "Wi-Fi経由"
                            data?.sharing?.state == "error" -> "共有を確認できません"
                            data?.enabled == true -> "上流の接続待ち"
                            else -> "停止中"
                        },
                    )
                    KeyValueRow("管理画面", data?.address?.substringBefore('/')?.ifBlank { null } ?: "未取得", mono = true)
                    KeyValueRow("チャンネル", data?.channel?.toString() ?: "未取得")
                    KeyValueRow("アドレス配布", when (data?.dhcpReady) { true -> "利用可能"; false -> "停止中"; null -> "未取得" })

                    FieldLabel("接続中の端末", trailing = data?.clientCount?.let { "$it 台" })
                    val clients = data?.clients.orEmpty()
                    if (clients.isEmpty()) {
                        EmptyState(Icons.Default.Devices, if (data?.clientCount == 0) "接続している端末はありません" else "端末の詳細はまだ取得できていません")
                    } else {
                        clients.forEach { c ->
                            Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(p.raised).padding(12.dp)) {
                                Row {
                                    Text(c.hostname?.ifBlank { null } ?: "接続端末", color = p.text, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
                                    Text(c.ipv4 ?: "IP未取得", color = p.accent, fontFamily = FontFamily.Monospace, fontSize = 12.sp)
                                }
                                Text(
                                    "${c.mac}${c.signalDBm?.let { " · $it dBm" } ?: ""}",
                                    color = p.muted,
                                    fontSize = 11.sp,
                                )
                                Text(
                                    "↓ 受信 ${Formatters.trafficVolume(Formatters.trafficBigInt(c.receivedBytes))}   ↑ 送信 ${Formatters.trafficVolume(Formatters.trafficBigInt(c.sentBytes))}",
                                    color = p.muted,
                                    fontSize = 11.sp,
                                )
                            }
                        }
                        HintText("通信量は接続してからの分です（再接続でリセット）。")
                    }
                }
            },
        )
    }
}
