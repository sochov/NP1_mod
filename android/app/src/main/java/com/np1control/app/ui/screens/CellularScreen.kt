package com.np1control.app.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CellTower
import androidx.compose.material.icons.filled.DataUsage
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.SettingsEthernet
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.APNJob
import com.np1control.app.data.APNSettings
import com.np1control.app.data.Formatters
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.NccApiException
import com.np1control.app.data.Observation
import com.np1control.app.data.SystemSummary
import com.np1control.app.data.TrafficUsageData
import com.np1control.app.data.WebSettings
import com.np1control.app.data.bool
import com.np1control.app.data.dataObj
import com.np1control.app.data.dbl
import com.np1control.app.data.interfaceIPv4
import com.np1control.app.data.str
import com.np1control.app.data.strings
import com.np1control.app.ui.components.AdaptiveColumns
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.FieldLabel
import com.np1control.app.ui.components.HintText
import com.np1control.app.ui.components.KeyValueRow
import com.np1control.app.ui.components.LoadingRow
import com.np1control.app.ui.components.MetricBlock
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.NccProgressBar
import com.np1control.app.ui.components.ScreenColumn
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.components.StatusPill
import com.np1control.app.ui.components.Tone
import com.np1control.app.ui.components.PanelAction
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import java.math.BigInteger

private data class ApnDraft(
    val id: String,
    val enabled: Boolean,
    val apn: String,
    val auth: Int,
    val usernameConfigured: Boolean,
    val passwordConfigured: Boolean,
    val username: String = "",
    val password: String = "",
    val clearUsername: Boolean = false,
    val clearPassword: Boolean = false,
) {
    fun payload() = listOf(id, enabled, apn, auth, username, password, clearUsername, clearPassword)
}

private fun apnError(e: Throwable): String {
    val code = (e as? NccApiException)?.code.orEmpty()
    val text = e.message.orEmpty()
    return when {
        code == "REVISION_CONFLICT" || text.contains("REVISION_CONFLICT") -> "別の操作でAPN設定が変わりました。保存内容を読み直してから編集してください。"
        code.contains("APN_CREDENTIAL_CHARACTERS_INVALID") -> "ユーザー名・パスワードは49文字以内の英数字と . _ - @ : + % = で入力してください。先頭のハイフンは使えません。"
        code.contains("APN_NAME_INVALID") -> "APNは49文字以内の英数字・ハイフン・ドットで入力してください。"
        code.contains("APN_NO_ENABLED_PROFILE") -> "接続に使うプロファイルを1つ以上有効にしてください。"
        else -> text.ifBlank { "APN設定を処理できませんでした" }
    }
}

private fun applyFailure(job: APNJob): String = when {
    job.result?.restored == true -> "接続を確認できなかったため、元のAPN設定へ戻しました。"
    job.result?.recoveryRequired == true -> "適用後の状態を確認できません。現在の回線状態を確認してください。"
    job.code == "REVISION_CONFLICT" -> "保存設定が変更されました。最新の設定を読み直してください。"
    job.code == "APN_APPLY_PROFILE2_UNSUPPORTED" || job.code == "APN_PROFILE2_MUST_BE_DISABLED" -> "プロファイル1を有効、プロファイル2を無効にして適用してください。"
    else -> "APNの反映を確認できませんでした。現在の回線状態と保存設定を確認してください。"
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun CellularScreen(
    summary: SystemSummary,
    apiClient: NccApiClient,
    onRefreshSummary: () -> Unit,
) {
    val p = LocalNccPalette.current
    val scope = rememberCoroutineScope()
    var apn by remember { mutableStateOf<APNSettings?>(null) }
    var drafts by remember { mutableStateOf<List<ApnDraft>>(emptyList()) }
    var savedSnapshot by remember { mutableStateOf<List<List<Any>>>(emptyList()) }
    var selected by remember { mutableStateOf("profile1") }
    var loading by remember { mutableStateOf(true) }
    var saving by remember { mutableStateOf(false) }
    var applying by remember { mutableStateOf(false) }
    var apnError by remember { mutableStateOf("") }
    var apnMessage by remember { mutableStateOf("") }
    var usage by remember { mutableStateOf<TrafficUsageData?>(null) }
    var usageFailed by remember { mutableStateOf(false) }
    var settings by remember { mutableStateOf<WebSettings?>(null) }
    var editingLimit by remember { mutableStateOf(false) }
    var planMessage by remember { mutableStateOf("") }

    fun install(s: APNSettings) {
        apn = s
        drafts = s.profiles.map { ApnDraft(it.id, it.enabled, it.apn, it.auth, it.usernameConfigured, it.passwordConfigured) }
        savedSnapshot = drafts.map { it.payload() }
    }

    suspend fun loadApn(keepError: Boolean = false) {
        if (saving || applying) return
        loading = true
        if (!keepError) apnError = ""
        runCatching { apiClient.get<APNSettings>("/network/cellular/apn") }
            .onSuccess { install(it) }
            .onFailure { apnError = apnError(it) }
        loading = false
    }

    suspend fun loadPlan() {
        runCatching { settings = apiClient.get<WebSettings>("/settings") }
    }

    LaunchedEffect(Unit) {
        launch { loadApn() }
        launch { loadPlan() }
        while (isActive) {
            runCatching {
                val obs = apiClient.get<Observation>("/traffic/usage")
                obs.data?.let { apiClient.json.decodeFromJsonElement(TrafficUsageData.serializer(), it) }
            }.onSuccess { usage = it; usageFailed = false }.onFailure { usageFailed = true }
            delay(2000)
        }
    }
    DisposableEffect(Unit) { onDispose { drafts = drafts.map { it.copy(username = "", password = "") } } }

    val modemObs = summary["modem"]
    val modem = summary.dataObj("modem")
    val modemStale = modemObs?.status == "stale"
    val carrier = when {
        !modem.str("carrier").isNullOrBlank() -> modem.str("carrier")!!
        modem.str("plmn") != null -> Formatters.carrierName(null, modem.str("plmn"))
        else -> "回線情報を確認中"
    }
    val registered = when {
        modemStale -> "前回の観測"
        modem.bool("registered") == true -> "回線登録済み"
        modem.bool("registered") == false -> "回線未登録"
        else -> "登録情報待ち"
    }
    val bands = modem.strings("bands").ifEmpty { listOfNotNull(modem.str("band")) }.joinToString(" / ") { "Band " + it.removePrefix("B") }.ifBlank { "—" }
    val current = summary.dataObj("apnCurrent")?.takeIf { it.bool("current") == true }
    val configured = apn?.configuredProfile1?.takeIf { (it.status == "available" || it.status == "stale") && !it.current }
    val cellular = usage?.networks?.cellular
    val stale = usageFailed || usage?.stale == true
    val month = networkTotal(cellular, true)
    val limit = Formatters.trafficBigInt(settings?.cellularLimitBytes)?.takeIf { it > BigInteger.ZERO }
    val remaining = if (limit != null && month != null) (limit - month).max(BigInteger.ZERO) else null
    val wan = current?.str("wanIPv4") ?: modem.str("wanIPv4") ?: summary.interfaceIPv4(cellular?.interfaceName ?: "usb0").joinToString(" / ").ifBlank { null }

    val dirty = drafts.map { it.payload() } != savedSnapshot
    val enabledCount = apn?.profiles?.count { it.enabled } ?: 0
    val profile1On = apn?.profiles?.firstOrNull { it.id == "profile1" }?.enabled == true
    val profile2On = apn?.profiles?.firstOrNull { it.id == "profile2" }?.enabled == true
    val applyDisabled = apn == null || apn?.applyAvailable != true || apn?.applyInProgress == true || !profile1On || profile2On ||
        enabledCount == 0 || dirty || saving || loading || applying
    val busy = saving || applying || loading

    ScreenColumn {
        // ── Connection ──
        NccPanel(
            title = carrier,
            subtitle = "${modem.str("rat") ?: "無線方式未取得"} · $bands",
            icon = Icons.Default.CellTower,
            headerEnd = {
                StatusPill(registered, if (modem.bool("registered") == true && !modemStale) Tone.OK else if (modemStale) Tone.WARN else Tone.NEUTRAL)
                PanelAction(onClick = onRefreshSummary) { Icon(Icons.Default.Refresh, contentDescription = "回線状態を更新", tint = p.muted) }
            },
        ) {
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                listOf(
                    "RSRP" to (modem.dbl("rsrp")?.let { "${decimal(it)} dBm" } ?: "—"),
                    "RSRQ" to (modem.dbl("rsrq")?.let { "${decimal(it)} dB" } ?: "—"),
                    "SNR" to (modem.dbl("snr")?.let { "${decimal(it)} dB" } ?: "—"),
                ).forEach { (k, v) ->
                    Column(
                        Modifier.weight(1f).clip(RoundedCornerShape(14.dp)).background(p.raised).padding(horizontal = 10.dp, vertical = 8.dp),
                    ) {
                        Text(k, color = p.muted, style = MaterialTheme.typography.labelSmall)
                        Text(v, color = p.text, style = MaterialTheme.typography.titleSmall)
                    }
                }
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                SignalBars(Formatters.signalLevel(modem.dbl("rsrp")))
                Text("電波は${Formatters.signalLabel(modem.dbl("rsrp"))}です", color = p.text, style = MaterialTheme.typography.bodyMedium)
            }
            KeyValueRow(
                if (current != null) "現在のAPN" else if (configured != null) "設定APN" else "APN",
                current?.str("apn") ?: configured?.apn?.ifBlank { null } ?: "未取得",
            )
            if (current == null && configured != null) HintText("本体に保存されているプロファイルの値です${if (configured.status == "stale") "（前回の確認）" else ""}。")
            KeyValueRow("データ通信", when (modem.bool("pdpConnected")) { true -> "接続済み"; false -> "未接続"; null -> "未取得" })
            KeyValueRow("WAN IPv4", wan ?: "未取得", mono = true)
            Text(
                (if (modemStale) "更新が止まっています" else "最終観測 ${Formatters.formatDateTime(modemObs?.observedAt)}") +
                    (modem.str("plmn")?.let { " · PLMN $it" } ?: ""),
                color = if (modemStale) p.warn else p.faint,
                style = MaterialTheme.typography.labelSmall,
            )
        }

        // ── Usage ──
        NccPanel(
            title = "通信量と月間上限",
            subtitle = "本日と今月の記録分、上限に対する残りの目安です。",
            icon = Icons.Default.DataUsage,
        ) {
            Row(horizontalArrangement = Arrangement.spacedBy(20.dp), verticalAlignment = Alignment.Bottom) {
                MetricBlock("今月の記録分", Formatters.trafficVolume(month), color = p.accent, size = 24)
                MetricBlock(
                    "残りの目安",
                    remaining?.let { Formatters.trafficVolume(it) } ?: "—",
                    caption = limit?.let { "上限 ${Formatters.trafficVolume(it)}" } ?: "上限未設定",
                    size = 20,
                )
            }
            if (limit != null && month != null) {
                val pct = (month.toDouble() / limit.toDouble()).toFloat()
                NccProgressBar(pct, color = if (pct > 0.85f) p.danger else p.accent, height = 8.dp)
            }
            Column(Modifier.clip(RoundedCornerShape(14.dp)).background(p.raised).padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Row { Text("", Modifier.weight(1.2f)); Text("↓ 受信", color = p.accent, fontSize = 12.sp, modifier = Modifier.weight(1f)); Text("↑ 送信", color = p.uploadOrange, fontSize = 12.sp, modifier = Modifier.weight(1f)) }
                UsageRow("本日", Formatters.trafficVolume(Formatters.trafficBigInt(cellular?.todayRX)), Formatters.trafficVolume(Formatters.trafficBigInt(cellular?.todayTX)))
                UsageRow("今月", Formatters.trafficVolume(Formatters.trafficBigInt(cellular?.monthRX)), Formatters.trafficVolume(Formatters.trafficBigInt(cellular?.monthTX)))
                UsageRow("現在の速度", speedText(cellular, true, stale), speedText(cellular, false, stale))
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    if (stale) "更新が止まっています。" else "最終更新 ${Formatters.formatTime(cellular?.sampledAt)}",
                    color = if (stale) p.warn else p.faint,
                    style = MaterialTheme.typography.labelSmall,
                    modifier = Modifier.weight(1f),
                )
                TextButton(onClick = { editingLimit = true }, enabled = settings != null) { Text(if (limit != null) "月間上限を変更" else "月間上限を設定") }
            }
            if (planMessage.isNotBlank()) NccBanner(planMessage, BannerTone.SUCCESS, onDismiss = { planMessage = "" })
            HintText("記録分はNP1で観測できた通信量です。通信会社の請求量とは異なることがあります。")
        }

        // ── APN ──
        NccPanel(
            title = "APN接続設定",
            subtitle = "2つの接続プロファイルを保存し、確認してから回線に適用します。",
            icon = Icons.Default.SettingsEthernet,
            headerEnd = {
                TextButton(enabled = !busy, onClick = { scope.launch { loadApn() } }) { Text("読み直す") }
            },
        ) {
            if (apnError.isNotBlank()) NccBanner(apnError, BannerTone.ERROR, onDismiss = { apnError = "" })
            if (apnMessage.isNotBlank()) NccBanner(apnMessage, if (applying) BannerTone.NEUTRAL else BannerTone.SUCCESS, onDismiss = if (applying) null else ({ apnMessage = "" }))
            val st = apn
            val editor = drafts.firstOrNull { it.id == selected }
            if (st == null || editor == null) {
                if (loading) LoadingRow("保存した接続設定を読み込んでいます…") else HintText("接続設定を読み直してから編集してください。")
                return@NccPanel
            }
            fun update(t: (ApnDraft) -> ApnDraft) { drafts = drafts.map { if (it.id == selected) t(it) else it } }
            AdaptiveColumns(
                minColumnWidth = 300.dp,
                left = {
                    SectionTabs(
                        tabs = drafts.map { it.id to "プロファイル ${if (it.id == "profile1") "1" else "2"}${if (it.enabled) "" else "（未使用）"}" },
                        selectedId = selected,
                        onSelect = { selected = it },
                        enabled = !busy,
                        fill = true,
                    )
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(checked = editor.enabled, enabled = !busy, onCheckedChange = { v -> update { it.copy(enabled = v) } })
                        Text("このプロファイルを接続に使う", color = p.text, style = MaterialTheme.typography.bodyMedium)
                    }
                    OutlinedTextField(
                        value = editor.apn,
                        onValueChange = { v -> update { it.copy(apn = v.take(49)) } },
                        label = { Text("APN") },
                        placeholder = { Text("例: provider.example") },
                        singleLine = true,
                        enabled = !busy,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    FieldLabel("認証方式")
                    SectionTabs(
                        tabs = listOf("0" to "なし", "1" to "PAP", "2" to "CHAP", "3" to "PAP または CHAP"),
                        selectedId = editor.auth.toString(),
                        onSelect = { v -> update { it.copy(auth = v.toInt()) } },
                        enabled = !busy,
                    )
                    CredentialField(
                        label = "ユーザー名",
                        configured = editor.usernameConfigured,
                        value = editor.username,
                        clear = editor.clearUsername,
                        enabled = !busy,
                        onValue = { v -> update { it.copy(username = v.take(49)) } },
                        onClear = { v -> update { it.copy(clearUsername = v, username = if (v) "" else it.username) } },
                    )
                    CredentialField(
                        label = "パスワード",
                        configured = editor.passwordConfigured,
                        value = editor.password,
                        clear = editor.clearPassword,
                        enabled = !busy,
                        onValue = { v -> update { it.copy(password = v.take(49)) } },
                        onClear = { v -> update { it.copy(clearPassword = v, password = if (v) "" else it.password) } },
                    )
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        Icon(Icons.Default.Lock, contentDescription = null, tint = p.faint, modifier = Modifier.size(14.dp))
                        HintText("保存済みのユーザー名・パスワードは表示しません。空欄のまま保存すると今の値を保ちます。")
                    }
                },
                right = {
                    Column(
                        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(p.raised).padding(14.dp),
                        verticalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        FieldLabel("保存している接続設定")
                        st.profiles.forEach { prof ->
                            KeyValueRow(
                                "プロファイル ${if (prof.id == "profile1") "1" else "2"}（${if (prof.enabled) "使用" else "未使用"}）",
                                prof.apn.ifBlank { "APN未設定" },
                            )
                        }
                        KeyValueRow("保存日時", Formatters.formatDateTime(st.savedAt))
                        KeyValueRow(
                            "回線への反映",
                            when {
                                st.appliedRevision == null -> "未適用"
                                st.appliedRevision == st.revision -> "保存設定を反映済み"
                                else -> "前の保存設定を反映済み"
                            },
                        )
                        when {
                            dirty -> Text("編集した内容は、まだ保存されていません。", color = p.warn, style = MaterialTheme.typography.bodySmall)
                            st.appliedRevision != st.revision -> Text("この保存設定は、まだ回線へ適用していません。", color = p.warn, style = MaterialTheme.typography.bodySmall)
                        }
                    }
                },
            )
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(
                    enabled = dirty && !busy,
                    onClick = {
                        scope.launch {
                            saving = true
                            apnError = ""
                            apnMessage = ""
                            val body = buildJsonObject {
                                put("expectedRevision", st.revision)
                                put("profiles", buildJsonArray {
                                    drafts.forEach { d ->
                                        add(buildJsonObject {
                                            put("id", d.id)
                                            put("enabled", d.enabled)
                                            put("apn", d.apn)
                                            put("auth", d.auth)
                                            if (d.username.isNotEmpty()) put("username", d.username)
                                            if (d.password.isNotEmpty()) put("password", d.password)
                                            if (d.clearUsername) put("clearUsername", true)
                                            if (d.clearPassword) put("clearPassword", true)
                                        })
                                    }
                                })
                            }.toString()
                            runCatching { apiClient.put<APNSettings>("/network/cellular/apn", body) }
                                .onSuccess { install(it); apnMessage = "接続設定を保存しました。回線への適用はまだ行っていません。" }
                                .onFailure { apnError = apnError(it) }
                            saving = false
                        }
                    },
                ) { Text(if (saving) "保存中…" else "接続設定を保存") }
                OutlinedButton(
                    enabled = !applyDisabled,
                    onClick = {
                        scope.launch {
                            applying = true
                            apnError = ""
                            apnMessage = ""
                            val revision = st.revision
                            runCatching {
                                var job = apiClient.post<APNJob>("/network/cellular/apn/apply", """{"expectedRevision":$revision}""")
                                val deadline = System.currentTimeMillis() + 150_000
                                while (job.status == "queued" || job.status == "running") {
                                    if (System.currentTimeMillis() > deadline) throw IllegalStateException("適用処理の完了をまだ確認できません。少し待ってから読み直してください。")
                                    apnMessage = if (job.status == "queued") "適用を準備しています…" else "APN設定と接続状態を確認しています…"
                                    delay(1000)
                                    job = apiClient.get<APNJob>("/network/cellular/apn/jobs/${job.id}")
                                }
                                if (job.status == "failed") throw IllegalStateException(applyFailure(job))
                                if (!job.verified || job.appliedRevision != revision) throw IllegalStateException("回線への反映を確認できません。現在の状態を読み直してください。")
                                job
                            }.onSuccess { job ->
                                apnMessage = if (job.result?.dataLink?.status == "available" && job.result.dataLink.linuxEgressConfigured == true) {
                                    "APN設定を本体へ反映しました。4Gの通信経路が設定されています。"
                                } else "APN設定を本体へ反映しました。4Gの通信経路は未接続です。"
                                onRefreshSummary()
                            }.onFailure { apnMessage = ""; apnError = apnError(it) }
                            applying = false
                            loadApn(keepError = true)
                        }
                    },
                ) { Text(if (applying) "回線への反映を確認中…" else "保存した設定を適用") }
            }
            HintText(
                when {
                    dirty -> "先に編集内容を保存してください。"
                    enabledCount == 0 -> "接続に使うプロファイルを有効にしてください。"
                    st.applyAvailable != true -> st.applyReason ?: "回線へ適用する機能の準備状況を確認しています。"
                    applying -> "接続が切り替わります。ホットスポットまたはUSBの管理接続を保ってください。"
                    else -> "適用すると、今の4G接続を切り替えます。保存だけでは回線を変更しません。"
                },
            )
        }
    }

    if (editingLimit) {
        LimitDialog(
            apiClient = apiClient,
            current = Formatters.trafficBigInt(settings?.cellularLimitBytes),
            onDismiss = { editingLimit = false },
            onSaved = {
                editingLimit = false
                planMessage = "月間上限を保存しました。"
                scope.launch { loadPlan() }
            },
        )
    }
}

private fun decimal(v: Double): String = if (v % 1.0 == 0.0) v.toInt().toString() else String.format("%.1f", v)

@Composable
private fun UsageRow(label: String, rx: String, tx: String) {
    val p = LocalNccPalette.current
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(label, color = p.muted, style = MaterialTheme.typography.bodySmall, modifier = Modifier.weight(1.2f))
        Text(rx, color = p.text, style = MaterialTheme.typography.labelLarge, modifier = Modifier.weight(1f))
        Text(tx, color = p.text, style = MaterialTheme.typography.labelLarge, modifier = Modifier.weight(1f))
    }
}

@Composable
private fun CredentialField(
    label: String,
    configured: Boolean,
    value: String,
    clear: Boolean,
    enabled: Boolean,
    onValue: (String) -> Unit,
    onClear: (Boolean) -> Unit,
) {
    val p = LocalNccPalette.current
    Column {
        OutlinedTextField(
            value = value,
            onValueChange = onValue,
            label = { Text("$label（${if (configured) "保存済み" else "未設定"}）") },
            placeholder = { Text(if (configured) "変更する場合のみ入力" else "プロバイダー指定の$label") },
            singleLine = true,
            enabled = enabled && !clear,
            visualTransformation = PasswordVisualTransformation(),
            modifier = Modifier.fillMaxWidth(),
        )
        if (configured) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Checkbox(checked = clear, enabled = enabled, onCheckedChange = onClear)
                Text("保存済みの${label}を消去", color = p.muted, style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}
