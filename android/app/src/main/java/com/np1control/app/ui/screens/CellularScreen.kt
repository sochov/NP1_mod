package com.np1control.app.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CellTower
import androidx.compose.material.icons.filled.DataUsage
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Save
import androidx.compose.material.icons.filled.SettingsEthernet
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.APNSettings
import com.np1control.app.data.Formatters
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.data.SystemSummary
import com.np1control.app.data.TrafficUsageData
import com.np1control.app.data.WebSettings
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.KeyValueRow
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.NccTag
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.put
import java.math.BigDecimal
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
)

@Composable
fun CellularScreen(
    summary: SystemSummary,
    apiClient: NccApiClient,
    onRefreshSummary: () -> Unit,
) {
    val p = LocalNccPalette.current
    val scope = rememberCoroutineScope()
    var apnSettings by remember { mutableStateOf<APNSettings?>(null) }
    var drafts by remember { mutableStateOf<List<ApnDraft>>(emptyList()) }
    var selectedProfile by remember { mutableStateOf("profile1") }
    var usage by remember { mutableStateOf<TrafficUsageData?>(null) }
    var webSettings by remember { mutableStateOf<WebSettings?>(null) }
    var limitGbInput by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var notice by remember { mutableStateOf("") }

    fun loadAll() {
        scope.launch {
            error = ""
            runCatching { apiClient.get<APNSettings>("/network/cellular/apn") }
                .onSuccess { s ->
                    apnSettings = s
                    drafts = s.profiles.map { prof ->
                        ApnDraft(
                            id = prof.id,
                            enabled = prof.enabled,
                            apn = prof.apn,
                            auth = prof.auth,
                            usernameConfigured = prof.usernameConfigured,
                            passwordConfigured = prof.passwordConfigured,
                        )
                    }
                }
                .onFailure { error = it.message ?: "APN設定を取得できません" }
            runCatching {
                val obs = apiClient.get<Observation>("/traffic/usage")
                obs.data?.let { usage = apiClient.json.decodeFromJsonElement(TrafficUsageData.serializer(), it) }
            }
            runCatching {
                val st = apiClient.get<WebSettings>("/settings")
                webSettings = st
                val bi = Formatters.trafficBigInt(st.cellularLimitBytes)
                limitGbInput = if (bi != null && bi > BigInteger.ZERO) {
                    BigDecimal(bi).divide(BigDecimal("1000000000")).stripTrailingZeros().toPlainString()
                } else ""
            }
        }
    }

    LaunchedEffect(Unit) { loadAll() }

    val modem = summary["modem"]?.data as? JsonObject
    val apnCurrent = summary["apnCurrent"]?.data as? JsonObject
    val carrier = Formatters.carrierName(
        (modem?.get("carrier") as? JsonPrimitive)?.contentOrNull,
        (modem?.get("plmn") as? JsonPrimitive)?.contentOrNull,
    )
    val band = (modem?.get("band") as? JsonPrimitive)?.contentOrNull?.let { "Band ${it.removePrefix("B")}" } ?: "—"
    val rsrp = (modem?.get("rsrp") as? JsonPrimitive)?.doubleOrNull?.let { String.format("%.1f dBm", it) } ?: "—"
    val rsrq = (modem?.get("rsrq") as? JsonPrimitive)?.doubleOrNull?.let { String.format("%.1f dB", it) } ?: "—"
    val snr = (modem?.get("snr") as? JsonPrimitive)?.doubleOrNull?.let { String.format("%.1f dB", it) } ?: "—"
    val wanIp = (apnCurrent?.get("wanIPv4") as? JsonPrimitive)?.contentOrNull
        ?: (modem?.get("wanIPv4") as? JsonPrimitive)?.contentOrNull ?: "未取得"
    val curApnName = (apnCurrent?.get("apn") as? JsonPrimitive)?.contentOrNull ?: "未取得"
    val registered = (modem?.get("registered") as? JsonPrimitive)?.booleanOrNull

    val cellular = usage?.networks?.cellular
    val monthTotal = run {
        val rx = Formatters.trafficBigInt(cellular?.monthRX)
        val tx = Formatters.trafficBigInt(cellular?.monthTX)
        if (rx != null && tx != null) rx.add(tx) else null
    }
    val limitBytes = Formatters.trafficBigInt(webSettings?.cellularLimitBytes)?.takeIf { it > BigInteger.ZERO }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (error.isNotBlank()) NccBanner(error, BannerTone.ERROR, onDismiss = { error = "" })
        if (notice.isNotBlank()) NccBanner(notice, BannerTone.SUCCESS, onDismiss = { notice = "" })

        NccPanel(
            title = carrier,
            subtitle = "MOBILE NETWORK · ${(modem?.get("rat") as? JsonPrimitive)?.contentOrNull ?: "LTE"} $band",
            icon = Icons.Default.CellTower,
            headerEnd = {
                NccTag(
                    text = if (registered == true) "回線登録済み" else "状態確認中",
                    color = if (registered == true) p.ok else p.muted,
                    background = if (registered == true) p.okSoft else p.bg,
                )
                IconButton(onClick = { onRefreshSummary(); loadAll() }) {
                    Icon(Icons.Default.Refresh, contentDescription = "更新", tint = p.muted)
                }
            },
        ) {
            KeyValueRow("現在のAPN", curApnName)
            KeyValueRow("WAN IPv4", wanIp, mono = true)
            KeyValueRow("RSRP / RSRQ / SNR", "$rsrp / $rsrq / $snr", mono = true)
        }

        NccPanel(
            title = "通信量・月間上限",
            icon = Icons.Default.DataUsage,
            headerEnd = {
                NccTag(text = "今月 ${Formatters.trafficVolume(monthTotal)}", color = p.accent, background = p.soft)
            },
        ) {
            KeyValueRow(
                "本日 (受信 / 送信)",
                "${Formatters.trafficVolume(Formatters.trafficBigInt(cellular?.todayRX))} / ${Formatters.trafficVolume(Formatters.trafficBigInt(cellular?.todayTX))}",
            )
            KeyValueRow(
                "今月 (受信 / 送信)",
                "${Formatters.trafficVolume(Formatters.trafficBigInt(cellular?.monthRX))} / ${Formatters.trafficVolume(Formatters.trafficBigInt(cellular?.monthTX))}",
            )
            KeyValueRow(
                "現在の速度 (↓ / ↑)",
                "${Formatters.trafficSpeed(cellular?.rxBytesPerSecond)} / ${Formatters.trafficSpeed(cellular?.txBytesPerSecond)}",
            )
            if (limitBytes != null && monthTotal != null) {
                val pct = (monthTotal.toDouble() / limitBytes.toDouble()).toFloat().coerceIn(0f, 1f)
                val rem = (limitBytes - monthTotal).coerceAtLeast(BigInteger.ZERO)
                KeyValueRow("残り目安", "${Formatters.trafficVolume(rem)} / ${Formatters.trafficVolume(limitBytes)}")
                LinearProgressIndicator(
                    progress = { pct },
                    modifier = Modifier.fillMaxWidth().height(6.dp),
                    color = if (pct > 0.9f) p.danger else p.accent,
                )
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                OutlinedTextField(
                    value = limitGbInput,
                    onValueChange = { limitGbInput = it },
                    label = { Text("月間上限 (GB、0で解除)") },
                    singleLine = true,
                    modifier = Modifier.weight(1f),
                )
                Button(
                    enabled = !busy && webSettings != null,
                    onClick = {
                        scope.launch {
                            val current = webSettings ?: return@launch
                            val v = limitGbInput.trim().ifEmpty { "0" }.toBigDecimalOrNull()
                            if (v == null || v < BigDecimal.ZERO || v > BigDecimal("100000")) {
                                error = "0〜100,000 GB の数値で入力してください"
                                return@launch
                            }
                            busy = true
                            error = ""
                            notice = ""
                            val bytesStr = v.multiply(BigDecimal("1000000000")).toBigInteger().toString()
                            val body = buildJsonObject {
                                put("expectedRevision", current.revision)
                                put("settings", buildJsonObject {
                                    put("revision", current.revision)
                                    put("theme", current.theme)
                                    put("locale", current.locale)
                                    put("favorites", buildJsonArray { current.favorites.forEach { add(JsonPrimitive(it)) } })
                                    put("cellularLimitBytes", bytesStr)
                                })
                            }.toString()
                            runCatching { apiClient.put<WebSettings>("/settings", body) }
                                .onSuccess {
                                    webSettings = it
                                    notice = "月間上限を保存しました"
                                }
                                .onFailure { error = it.message ?: "保存に失敗しました" }
                            busy = false
                        }
                    },
                ) {
                    Text("上限を保存")
                }
            }
        }

        // APN Editor
        NccPanel(
            title = "APN接続設定",
            subtitle = "プロファイル1/2を編集・保存し、回線に適用します",
            icon = Icons.Default.SettingsEthernet,
            headerEnd = {
                apnSettings?.let { NccTag(text = "rev.${it.revision}") }
            },
        ) {
            SectionTabs(
                tabs = listOf("profile1" to "プロファイル 1", "profile2" to "プロファイル 2"),
                selectedId = selectedProfile,
                onSelect = { selectedProfile = it },
            )
            val currentDraft = drafts.firstOrNull { it.id == selectedProfile }
            if (currentDraft != null) {
                fun updateDraft(transform: (ApnDraft) -> ApnDraft) {
                    drafts = drafts.map { if (it.id == selectedProfile) transform(it) else it }
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(
                        checked = currentDraft.enabled,
                        onCheckedChange = { checked -> updateDraft { it.copy(enabled = checked) } },
                    )
                    Text("このプロファイルを接続に使用する", color = p.text, fontSize = 13.sp)
                }
                OutlinedTextField(
                    value = currentDraft.apn,
                    onValueChange = { v -> updateDraft { it.copy(apn = v) } },
                    label = { Text("APN") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                Text("認証方式", color = p.muted, fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
                SectionTabs(
                    tabs = listOf("0" to "なし", "1" to "PAP", "2" to "CHAP", "3" to "PAP/CHAP"),
                    selectedId = currentDraft.auth.toString(),
                    onSelect = { sel -> updateDraft { it.copy(auth = sel.toIntOrNull() ?: 0) } },
                )
                OutlinedTextField(
                    value = currentDraft.username,
                    onValueChange = { v -> updateDraft { it.copy(username = v) } },
                    label = { Text("ユーザー名 (${if (currentDraft.usernameConfigured) "保存済み・空欄で維持" else "未設定"})") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = currentDraft.password,
                    onValueChange = { v -> updateDraft { it.copy(password = v) } },
                    label = { Text("パスワード (${if (currentDraft.passwordConfigured) "保存済み・空欄で維持" else "未設定"})") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    modifier = Modifier.fillMaxWidth(),
                )
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Button(
                        enabled = !busy && apnSettings != null,
                        onClick = {
                            scope.launch {
                                val st = apnSettings ?: return@launch
                                busy = true
                                error = ""
                                notice = ""
                                val payload = buildJsonObject {
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
                                runCatching { apiClient.put<APNSettings>("/network/cellular/apn", payload) }
                                    .onSuccess {
                                        apnSettings = it
                                        notice = "APN接続設定を保存しました"
                                        loadAll()
                                    }
                                    .onFailure { error = it.message ?: "APNの保存に失敗しました" }
                                busy = false
                            }
                        },
                    ) {
                        Icon(Icons.Default.Save, contentDescription = null)
                        Text(" 保存")
                    }
                    OutlinedButton(
                        enabled = !busy && apnSettings?.applyAvailable == true,
                        onClick = {
                            scope.launch {
                                val st = apnSettings ?: return@launch
                                busy = true
                                error = ""
                                notice = "APNの回線適用を開始しました…"
                                val payload = """{"expectedRevision":${st.revision}}"""
                                runCatching { apiClient.requestRaw("/network/cellular/apn/apply", "POST", payload) }
                                    .onSuccess {
                                        delay(2000L)
                                        notice = "APN適用要求を送信しました。回線状態を確認してください。"
                                        onRefreshSummary()
                                        loadAll()
                                    }
                                    .onFailure { error = it.message ?: "APNの適用に失敗しました" }
                                busy = false
                            }
                        },
                    ) {
                        Text("保存設定を回線へ適用")
                    }
                }
                apnSettings?.applyReason?.takeIf { apnSettings?.applyAvailable != true }?.let {
                    Text(it, color = p.muted, fontSize = 11.sp)
                }
            }
        }
        Spacer(modifier = Modifier.height(12.dp))
    }
}
