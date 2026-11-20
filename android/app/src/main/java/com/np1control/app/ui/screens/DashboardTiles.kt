package com.np1control.app.ui.screens

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.OpenInNew
import androidx.compose.material.icons.filled.CellTower
import androidx.compose.material.icons.filled.Memory
import androidx.compose.material.icons.filled.Router
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material.icons.filled.Speed
import androidx.compose.material.icons.filled.Wifi
import androidx.compose.material.icons.filled.WifiTethering
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
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
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.Formatters
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.NetworkUsage
import com.np1control.app.data.SystemSummary
import com.np1control.app.data.bool
import com.np1control.app.data.dataDouble
import com.np1control.app.data.dataElement
import com.np1control.app.data.dataObj
import com.np1control.app.data.dataString
import com.np1control.app.data.dbl
import com.np1control.app.data.int
import com.np1control.app.data.parseInstantMs
import com.np1control.app.data.str
import com.np1control.app.data.strings
import com.np1control.app.ui.components.KeyValueRow
import com.np1control.app.ui.components.ListRow
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.NccProgressBar
import com.np1control.app.ui.components.Sample
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.components.SparklineCanvas
import com.np1control.app.ui.components.StatusPill
import com.np1control.app.ui.components.Tone
import com.np1control.app.ui.components.PanelAction
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.math.BigDecimal
import java.math.BigInteger

// ── Shared helpers ─────────────────────────────────────────────────────────

fun networkTotal(n: NetworkUsage?, month: Boolean): BigInteger? {
    if (n == null || n.status != "available") return null
    val rx = Formatters.trafficBigInt(if (month) n.monthRX else n.todayRX)
    val tx = Formatters.trafficBigInt(if (month) n.monthTX else n.todayTX)
    return if (rx != null && tx != null) rx + tx else null
}

fun speedText(n: NetworkUsage?, rx: Boolean, stale: Boolean): String {
    if (n == null || n.stale || n.speedStatus != "available") return "—"
    return Formatters.trafficSpeed(if (rx) n.rxBytesPerSecond else n.txBytesPerSecond, stale)
}

/** Live resource samples kept for the whole app session (like the Web UI's resource history). */
object ResourceHistoryStore {
    private val data = HashMap<String, ArrayDeque<Sample>>()

    @Synchronized
    fun remember(key: String, atMs: Long, value: Double?) {
        val list = data.getOrPut(key) { ArrayDeque() }
        val last = list.lastOrNull()
        if (last != null && atMs < last.atMs) return
        if (last != null && last.atMs == atMs) {
            list.removeLast()
        }
        list.addLast(Sample(atMs, value))
        while (list.isNotEmpty() && list.first().atMs < atMs - 3_600_000L) list.removeFirst()
        while (list.size > 2000) list.removeFirst()
    }

    @Synchronized
    fun get(key: String): List<Sample> = data[key]?.toList().orEmpty()
}

data class TemperatureReading(val key: String, val label: String, val value: Double)

fun temperatures(summary: SystemSummary): List<TemperatureReading> {
    val list = (summary.dataElement("temperatures", allowStale = false) as? JsonArray)
        ?.mapNotNull { it as? JsonObject }
        ?.filter { it.str("name") != null && (it.dbl("celsius") ?: -999.0) in -40.0..150.0 }
        .orEmpty()
    fun find(vararg names: String) = names.firstNotNullOfOrNull { n -> list.firstOrNull { it.str("name") == n }?.dbl("celsius") }
    val cpu = list.filter { Regex("^(cpu|cpuss)", RegexOption.IGNORE_CASE).containsMatchIn(it.str("name")!!) && !it.str("name")!!.endsWith("-step") }
        .mapNotNull { it.dbl("celsius") }.maxOrNull()
    return listOfNotNull(
        cpu?.let { TemperatureReading("cpu", "CPU", it) },
        find("gpu-usr", "gpu-step")?.let { TemperatureReading("gpu", "GPU", it) },
        find("mdm-core-usr", "mdm-core-step")?.let { TemperatureReading("modem", "モデム", it) },
        find("emmc_ufs-therm")?.let { TemperatureReading("storage", "eMMC", it) },
    )
}

// ── Recording status ───────────────────────────────────────────────────────

data class RecordingState(val label: String, val tone: Tone, val detail: String?, val clips: Int?)

fun recordingState(summary: SystemSummary, now: Long = System.currentTimeMillis()): RecordingState {
    val obs = summary["recording"]
    val data = obs?.data as? JsonObject
    val at = parseInstantMs(data.str("writingObservedAt") ?: obs?.observedAt)
    val clips = data.int("finalizedClips")
    val unknown = obs?.status != "available" || at == null || now - at > 30_000 || at - now > 2_000 || data.str("writingReason") != null
    if (unknown) return RecordingState("録画状態を取得できません", Tone.NEUTRAL, null, clips)
    val writing = data.bool("writing")
    val cams = data?.get("cameras") as? JsonObject
    fun cam(id: String, label: String): String {
        val w = (cams?.get(id) as? JsonPrimitive)?.let { if (it.content == "true") true else if (it.content == "false") false else null }
        return "$label ${when (w) { true -> "録画中"; false -> "更新なし"; null -> "確認中" }}"
    }
    return when (writing) {
        true -> RecordingState("録画中", Tone.DANGER, "${cam("front", "前方")} · ${cam("in", "車内")}", clips)
        false -> RecordingState("録画停止中", Tone.WARN, "直近の録画ファイル更新がありません", clips)
        null -> RecordingState("録画を確認中", Tone.NEUTRAL, "録画ファイルの更新を確認しています", clips)
    }
}

@Composable
fun RecordingChip(summary: SystemSummary, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val st = recordingState(summary)
    StatusPill(
        text = st.label + (st.clips?.let { " · ${String.format("%,d", it)}件" } ?: ""),
        tone = st.tone,
        onClick = onClick,
        modifier = modifier,
    )
}

// ── 4G tile ────────────────────────────────────────────────────────────────

@Composable
fun CellularTile(
    summary: SystemSummary,
    cellular: NetworkUsage?,
    stale: Boolean,
    limitBytes: BigInteger?,
    apiClient: NccApiClient,
    onOpen: () -> Unit,
    onLimitSaved: () -> Unit,
    modifier: Modifier = Modifier,
    glass: Boolean = false,
) {
    val p = LocalNccPalette.current
    val modem = summary.dataObj("modem")
    val carrier = when {
        !modem.str("carrier").isNullOrBlank() -> modem.str("carrier")!!
        modem.str("plmn") != null -> Formatters.carrierName(null, modem.str("plmn"))
        else -> "回線情報未取得"
    }
    val rsrp = modem.dbl("rsrp")
    val level = Formatters.signalLevel(rsrp)
    val month = networkTotal(cellular, true)
    val today = networkTotal(cellular, false)
    val knownLimit = limitBytes != null && limitBytes > BigInteger.ZERO
    val remaining = if (knownLimit && month != null) (limitBytes!! - month).max(BigInteger.ZERO) else null
    val ratio = if (remaining != null) remaining.toDouble() / limitBytes!!.toDouble() else null
    var editing by remember { mutableStateOf(false) }

    NccPanel(
        modifier = modifier,
        title = "4G データ通信",
        icon = Icons.Default.CellTower,
        glass = glass,
        headerEnd = {
            PanelAction(onClick = onOpen) { Icon(Icons.AutoMirrored.Filled.OpenInNew, contentDescription = "4Gを開く", tint = p.muted, modifier = Modifier.size(18.dp)) }
        },
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(carrier, color = p.text, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(
                    "${Formatters.bandLabel(modem.str("band"))} · ${rsrp?.let { "${it.toInt()} dBm" } ?: "—"}",
                    color = p.muted,
                    style = MaterialTheme.typography.bodySmall,
                )
            }
            SignalBars(level)
        }
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
            Box(Modifier.size(84.dp), contentAlignment = Alignment.Center) {
                val ringColor = when {
                    ratio == null -> p.line
                    ratio <= 0.15 -> p.danger
                    ratio <= 0.3 -> Color(0xFFBF8A36)
                    else -> p.accent
                }
                Canvas(Modifier.size(84.dp)) {
                    val stroke = 8.dp.toPx()
                    val inset = stroke / 2
                    drawArc(p.raised.takeIf { !p.isDark } ?: p.line, -90f, 360f, false, topLeft = Offset(inset, inset), size = Size(size.width - stroke, size.height - stroke), style = Stroke(stroke))
                    if (ratio != null) drawArc(ringColor, -90f, (360 * ratio).toFloat(), false, topLeft = Offset(inset, inset), size = Size(size.width - stroke, size.height - stroke), style = Stroke(stroke, cap = StrokeCap.Round))
                }
                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(if (knownLimit) "残り" else "月間上限", color = p.muted, fontSize = 10.sp)
                    if (knownLimit) {
                        Text(remaining?.let { Formatters.decimalGB(it) } ?: "—", color = p.text, fontSize = 17.sp, fontWeight = FontWeight.Bold, style = MaterialTheme.typography.titleMedium)
                        Text("/ ${Formatters.decimalGB(limitBytes!!, 0)} GB", color = p.muted, fontSize = 9.sp)
                    } else {
                        Text("未設定", color = p.muted, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
                    }
                }
            }
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                KeyValueRow("今月", Formatters.trafficVolume(month))
                KeyValueRow("本日", Formatters.trafficVolume(today))
                TextButton(onClick = { editing = true }, contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 0.dp)) {
                    Text(if (knownLimit) "月間上限を変更" else "月間上限を設定", fontSize = 12.sp)
                }
            }
        }
        if (month != null && knownLimit && month > limitBytes) {
            Text("設定上限を${Formatters.trafficVolume(month - limitBytes!!)}超過", color = p.danger, style = MaterialTheme.typography.bodySmall)
        }
        Text(
            if (stale) "更新が止まっています" else "最終更新 ${Formatters.formatTime(cellular?.sampledAt)} · 受信＋送信",
            color = if (stale) p.warn else p.faint,
            style = MaterialTheme.typography.labelSmall,
        )
    }
    if (editing) {
        LimitDialog(apiClient = apiClient, current = limitBytes, onDismiss = { editing = false }, onSaved = { editing = false; onLimitSaved() })
    }
}

@Composable
fun SignalBars(level: Int?) {
    val p = LocalNccPalette.current
    Row(verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(2.dp)) {
        for (n in 1..5) {
            Box(
                Modifier
                    .width(4.dp)
                    .height((5 + n * 3).dp)
                    .clip(RoundedCornerShape(2.dp))
                    .background(if (level != null && n <= level) p.accent else p.line),
            )
        }
    }
}

/** Monthly 4G allowance editor; writes `cellularLimitBytes` without touching other settings. */
@Composable
fun LimitDialog(apiClient: NccApiClient, current: BigInteger?, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val p = LocalNccPalette.current
    val scope = rememberCoroutineScope()
    var draft by remember {
        mutableStateOf(current?.takeIf { it > BigInteger.ZERO }?.let { BigDecimal(it).divide(BigDecimal("1000000000")).stripTrailingZeros().toPlainString() } ?: "")
    }
    var error by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    fun save(value: String) {
        val v = value.trim().ifEmpty { "0" }
        if (!Regex("^(0|[1-9][0-9]{0,5})(\\.[0-9]{1,3})?$").matches(v)) {
            error = "0〜100,000 GBの範囲で、小数点以下3桁まで入力してください。"
            return
        }
        val bytes = BigDecimal(v).multiply(BigDecimal("1000000000")).toBigInteger()
        busy = true
        error = ""
        scope.launch {
            runCatching { apiClient.updateSettings(mapOf("cellularLimitBytes" to JsonPrimitive(bytes.toString()))) }
                .onSuccess { onSaved() }
                .onFailure { error = it.message ?: "保存できませんでした" }
            busy = false
        }
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = p.paper,
        title = { Text("4Gの月間上限") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = draft,
                    onValueChange = { draft = it },
                    singleLine = true,
                    suffix = { Text("GB") },
                    placeholder = { Text("20") },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    enabled = !busy,
                    modifier = Modifier.fillMaxWidth(),
                )
                Text("毎月1日からの記録で集計します。上限で通信を止める機能ではありません。", color = p.muted, style = MaterialTheme.typography.bodySmall)
                if (error.isNotBlank()) Text(error, color = p.danger, style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = { TextButton(enabled = !busy, onClick = { save(draft) }) { Text(if (busy) "保存中…" else "保存") } },
        dismissButton = {
            Row {
                if (current != null && current > BigInteger.ZERO) TextButton(enabled = !busy, onClick = { save("0") }) { Text("上限なしにする", color = p.muted) }
                TextButton(onClick = onDismiss) { Text("キャンセル", color = p.muted) }
            }
        },
    )
}

// ── Network tile ───────────────────────────────────────────────────────────

@Composable
fun NetworkTile(
    summary: SystemSummary,
    wifiUsage: NetworkUsage?,
    onNavigate: (String) -> Unit,
    modifier: Modifier = Modifier,
    glass: Boolean = false,
) {
    val p = LocalNccPalette.current
    val hotspot = summary.dataObj("hotspot")
    val wifi = summary.dataObj("wifi")
    val vpn = summary.dataObj("tailscale")
    NccPanel(
        modifier = modifier,
        title = "ネットワーク",
        icon = Icons.Default.Router,
        glass = glass,
        contentPadding = androidx.compose.foundation.layout.PaddingValues(start = 10.dp, end = 10.dp, top = 0.dp, bottom = 10.dp),
        headerEnd = {
            PanelAction(onClick = { onNavigate("wifi") }) { Icon(Icons.AutoMirrored.Filled.OpenInNew, contentDescription = "Wi-Fi / APを開く", tint = p.muted, modifier = Modifier.size(18.dp)) }
        },
    ) {
        val apOn = hotspot.bool("enabled")
        ListRow(
            overline = "ホットスポット",
            title = hotspot.str("ssid") ?: (hotspot?.get("settings") as? JsonObject).str("ssid") ?: "接続情報待ち",
            subtitle = hotspot.int("clientCount")?.let { "接続端末 $it 台" } ?: "接続端末 —",
            icon = Icons.Default.WifiTethering,
            iconTone = if (apOn == true) Tone.OK else Tone.NEUTRAL,
            trailing = { StatusPill(when (apOn) { true -> "提供中"; false -> "停止中"; null -> "取得待ち" }, if (apOn == true) Tone.OK else Tone.NEUTRAL, dot = false) },
            onClick = { onNavigate("wifi") },
            showChevron = false,
        )
        val wpa = wifi.str("wpa_state")
        val staOn = wpa == "COMPLETED"
        ListRow(
            overline = "Wi-Fi接続",
            title = when {
                wifi == null -> "状態を取得できません"
                staOn -> wifi.str("ssid")?.takeIf { it.isNotBlank() } ?: "接続中"
                else -> "未接続"
            },
            subtitle = "今月 ${Formatters.trafficVolume(networkTotal(wifiUsage, true))}",
            icon = Icons.Default.Wifi,
            iconTone = if (staOn) Tone.OK else Tone.NEUTRAL,
            trailing = {
                StatusPill(
                    when {
                        staOn -> "接続済み"
                        wpa in setOf("DISCONNECTED", "INACTIVE", "INTERFACE_DISABLED") -> "未接続"
                        else -> "取得待ち"
                    },
                    if (staOn) Tone.OK else Tone.NEUTRAL,
                    dot = false,
                )
            },
            onClick = { onNavigate("wifi") },
            showChevron = false,
        )
        val online = vpn.bool("online")
        ListRow(
            overline = "Tailscale",
            title = vpn.str("deviceName") ?: "接続情報待ち",
            subtitle = vpn.strings("ips").firstOrNull { it.contains('.') } ?: "アドレス —",
            icon = Icons.Default.Shield,
            iconTone = if (online == true) Tone.OK else Tone.NEUTRAL,
            trailing = {
                StatusPill(
                    when {
                        online == true -> "オンライン"
                        vpn.str("backendState") == "NeedsLogin" -> "ログインが必要"
                        online == false -> "オフライン"
                        else -> "取得待ち"
                    },
                    if (online == true) Tone.OK else if (vpn.str("backendState") == "NeedsLogin") Tone.WARN else Tone.NEUTRAL,
                    dot = false,
                )
            },
            onClick = { onNavigate("vpn") },
            showChevron = false,
        )
    }
}

// ── Resource tile ──────────────────────────────────────────────────────────

@Composable
fun ResourceTile(
    summary: SystemSummary,
    cellular: NetworkUsage?,
    wifi: NetworkUsage?,
    stale: Boolean,
    nowMs: Long,
    onOpen: () -> Unit,
    modifier: Modifier = Modifier,
    glass: Boolean = false,
) {
    val p = LocalNccPalette.current
    var minutes by remember { mutableStateOf("5") }
    val window = (minutes.toLongOrNull() ?: 5L) * 60_000L
    val cpu = summary.dataObj("cpu", allowStale = false).dbl("usedPercent")
    val ram = summary.dataObj("ram", allowStale = false)
    val temps = temperatures(summary)
    NccPanel(
        modifier = modifier,
        title = "リソースモニター",
        icon = Icons.Default.Memory,
        glass = glass,
        headerEnd = {
            PanelAction(onClick = onOpen) { Icon(Icons.AutoMirrored.Filled.OpenInNew, contentDescription = "システムを開く", tint = p.muted, modifier = Modifier.size(18.dp)) }
        },
    ) {
        SectionTabs(tabs = listOf("5" to "5分", "15" to "15分", "60" to "60分"), selectedId = minutes, onSelect = { minutes = it }, fill = true)
        Row(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
            listOf("cellular" to ("4G" to cellular), "wifi" to ("Wi-Fi" to wifi)).forEach { (key, pair) ->
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(pair.first, color = p.text, style = MaterialTheme.typography.labelLarge)
                    Text("↓ ${speedText(pair.second, true, stale)}", color = p.accent, style = MaterialTheme.typography.labelMedium, maxLines = 1)
                    Text("↑ ${speedText(pair.second, false, stale)}", color = p.uploadOrange, style = MaterialTheme.typography.labelMedium, maxLines = 1)
                    SparklineCanvas(
                        series = listOf(p.accent to ResourceHistoryStore.get("${key}rx"), p.uploadOrange to ResourceHistoryStore.get("${key}tx")),
                        windowMs = window,
                        nowMs = nowMs,
                        height = 38.dp,
                    )
                }
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            MiniMetric("CPU", cpu?.let { "${it.toInt()}%" } ?: "—", "cpu", p.accent, 100.0, window, nowMs, Modifier.weight(1f))
            MiniMetric("RAM", ram.dbl("usedPercent")?.let { "${it.toInt()}%" } ?: "—", "ram", p.ok, 100.0, window, nowMs, Modifier.weight(1f))
            MiniMetric(
                "本体温度",
                temps.maxOfOrNull { it.value }?.let { String.format("%.1f°C", it) } ?: "—",
                "temperature",
                p.uploadOrange,
                null,
                window,
                nowMs,
                Modifier.weight(1f),
            )
        }
        if (temps.isNotEmpty()) {
            Text(temps.joinToString("  ") { "${it.label} ${String.format("%.0f", it.value)}°" }, color = p.faint, style = MaterialTheme.typography.labelSmall)
        }
    }
}

@Composable
private fun MiniMetric(label: String, value: String, key: String, color: Color, max: Double?, window: Long, now: Long, modifier: Modifier) {
    val p = LocalNccPalette.current
    Column(modifier, verticalArrangement = Arrangement.spacedBy(1.dp)) {
        Text(label, color = p.muted, style = MaterialTheme.typography.labelSmall)
        Text(value, color = p.text, fontSize = 16.sp, fontWeight = FontWeight.Bold, style = MaterialTheme.typography.titleMedium)
        SparklineCanvas(series = listOf(color to ResourceHistoryStore.get(key)), windowMs = window, nowMs = now, fixedMax = max, height = 30.dp, gapMs = 35_000L)
    }
}

// ── Device tile ────────────────────────────────────────────────────────────

@Composable
fun DeviceTile(summary: SystemSummary, onOpen: () -> Unit, modifier: Modifier = Modifier, glass: Boolean = false) {
    val p = LocalNccPalette.current
    val sd = summary.dataObj("sd", allowStale = false)
    val storage = summary.dataObj("internalStorage", allowStale = false)
    val loads = summary.dataString("load")?.trim()?.split(Regex("\\s+"))?.take(3)?.map { it.toDoubleOrNull() }
    NccPanel(
        modifier = modifier,
        title = "本体の状態",
        icon = Icons.Default.Speed,
        glass = glass,
        headerEnd = {
            PanelAction(onClick = onOpen) { Icon(Icons.AutoMirrored.Filled.OpenInNew, contentDescription = "本体の詳細を開く", tint = p.muted, modifier = Modifier.size(18.dp)) }
        },
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            Column(Modifier.weight(1f)) {
                Text("稼働時間", color = p.muted, style = MaterialTheme.typography.labelSmall)
                Text(Formatters.uptime(summary.dataDouble("uptime")), color = p.text, style = MaterialTheme.typography.titleMedium)
            }
            Column(Modifier.weight(1.2f)) {
                Text("負荷 (1 / 5 / 15分)", color = p.muted, style = MaterialTheme.typography.labelSmall)
                Text(
                    loads?.joinToString(" / ") { it?.let { v -> String.format("%.2f", v) } ?: "—" } ?: "— / — / —",
                    color = p.text,
                    style = MaterialTheme.typography.titleMedium,
                    maxLines = 1,
                )
            }
        }
        StorageRow("SDカード", sd, showPercent = true)
        StorageRow("本体ストレージ", storage, showPercent = false)
    }
}

@Composable
private fun StorageRow(label: String, data: JsonObject?, showPercent: Boolean) {
    val p = LocalNccPalette.current
    val pct = data.dbl("usedPercent")?.takeIf { it in 0.0..100.0 }
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(label, color = p.text, style = MaterialTheme.typography.labelLarge, modifier = Modifier.weight(1f))
            Text(
                "空き ${Formatters.bytes(data.str("freeBytes"))} / ${Formatters.bytes(data.str("totalBytes"))}",
                color = p.muted,
                style = MaterialTheme.typography.bodySmall,
            )
        }
        if (pct != null) {
            NccProgressBar((pct / 100).toFloat(), color = if (pct > 90) p.warn else p.accent)
            if (showPercent) Text("${pct.toInt()}% 使用", color = p.faint, style = MaterialTheme.typography.labelSmall)
        }
    }
}
