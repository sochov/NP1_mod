package com.np1control.app.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.NetworkCheck
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.np1control.app.BuildConfig
import com.np1control.app.data.AppConfig
import com.np1control.app.data.AppThemeMode
import com.np1control.app.data.ConnectionMode
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.PlatePopupFilter
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.FieldLabel
import com.np1control.app.ui.components.HintText
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.components.StatusPill
import com.np1control.app.ui.components.SwitchRow
import com.np1control.app.ui.components.Tone
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.launch

/** Settings that belong to this app (not to the NP1): connection, plate notifications, display. */
@Composable
fun AppSettingsDialog(
    config: AppConfig,
    apiClient: NccApiClient,
    connected: Boolean = false,
    activeLabel: String = "",
    activeUrl: String = "",
    onUpdateConfig: ((AppConfig) -> AppConfig) -> Unit,
    onReconnect: () -> Unit,
    onTestPopup: (Boolean) -> Unit,
    onDismiss: () -> Unit,
) {
    val p = LocalNccPalette.current
    val scope = rememberCoroutineScope()

    var mode by remember(config.connectionMode) { mutableStateOf(config.connectionMode) }
    var hotspotUrl by remember(config.hotspotUrl) { mutableStateOf(config.hotspotUrl) }
    var tailscaleHost by remember(config.tailscaleHost) { mutableStateOf(config.tailscaleHost) }
    var customUrl by remember(config.customUrl) { mutableStateOf(config.customUrl) }
    var result by remember { mutableStateOf<Pair<BannerTone, String>?>(null) }
    var testing by remember { mutableStateOf(false) }
    val draft = config.copy(
        connectionMode = mode,
        hotspotUrl = hotspotUrl.ifBlank { AppConfig.DEFAULT_HOTSPOT_URL },
        tailscaleHost = tailscaleHost.trim(),
        customUrl = customUrl.trim(),
    )
    val dirty = draft.connectionMode != config.connectionMode || draft.hotspotUrl != config.hotspotUrl ||
        draft.tailscaleHost != config.tailscaleHost || draft.customUrl != config.customUrl

    Dialog(onDismissRequest = onDismiss, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Surface(
            shape = RoundedCornerShape(24.dp),
            color = p.paper,
            modifier = Modifier.fillMaxWidth(0.94f).widthIn(max = 620.dp).heightIn(max = 720.dp),
        ) {
            Column(Modifier.fillMaxWidth()) {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(start = 20.dp, end = 8.dp, top = 12.dp, bottom = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("アプリの設定", color = p.text, style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f))
                    IconButton(onClick = onDismiss) { Icon(Icons.Default.Close, contentDescription = "閉じる", tint = p.muted) }
                }
                Column(
                    modifier = Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState()).padding(start = 20.dp, end = 20.dp, bottom = 20.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    // ── Connection ──
                    Row(
                        Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(p.raised).padding(12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(Modifier.weight(1f)) {
                            Text("いまの接続", color = p.muted, style = MaterialTheme.typography.labelSmall)
                            Text(if (connected) activeLabel else "未接続", color = p.text, style = MaterialTheme.typography.titleSmall)
                            Text(activeUrl, color = p.muted, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                        }
                        StatusPill(if (connected) "接続中" else "未接続", if (connected) Tone.OK else Tone.WARN)
                    }
                    FieldLabel("NP1への接続先")
                    SectionTabs(
                        tabs = listOf(
                            ConnectionMode.AUTO.id to "自動",
                            ConnectionMode.HOTSPOT.id to "ホットスポット",
                            ConnectionMode.TAILSCALE.id to "Tailscale",
                            ConnectionMode.CUSTOM.id to "URL指定",
                        ),
                        selectedId = mode.id,
                        onSelect = { mode = ConnectionMode.fromId(it) },
                    )
                    HintText(
                        when (mode) {
                            ConnectionMode.AUTO -> "NP1のホットスポットにつながっていればそちらを使い、つながらなければTailscaleのアドレスへ切り替えます。"
                            ConnectionMode.HOTSPOT -> "NP1のホットスポットにつないだ状態で使います。"
                            ConnectionMode.TAILSCALE -> "外出先からTailscale経由で接続します。この端末にもTailscaleが必要です。"
                            ConnectionMode.CUSTOM -> "指定したURLへ接続します（検証用）。"
                        },
                    )
                    if (mode == ConnectionMode.AUTO || mode == ConnectionMode.HOTSPOT) {
                        OutlinedTextField(
                            value = hotspotUrl,
                            onValueChange = { hotspotUrl = it },
                            label = { Text("ホットスポットのアドレス") },
                            placeholder = { Text(AppConfig.DEFAULT_HOTSPOT_URL) },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                    if (mode == ConnectionMode.AUTO || mode == ConnectionMode.TAILSCALE) {
                        OutlinedTextField(
                            value = tailscaleHost,
                            onValueChange = { tailscaleHost = it },
                            label = { Text("Tailscaleのアドレス") },
                            placeholder = { Text("例: 100.64.0.10") },
                            supportingText = { Text("「Tailscale」画面からワンタップで登録できます。") },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                    if (mode == ConnectionMode.CUSTOM) {
                        OutlinedTextField(
                            value = customUrl,
                            onValueChange = { customUrl = it },
                            label = { Text("URL") },
                            placeholder = { Text("例: http://127.0.0.1:8080") },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                    result?.let { (tone, msg) -> NccBanner(msg, tone, onDismiss = { result = null }) }
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        OutlinedButton(
                            enabled = !testing,
                            onClick = {
                                val targets = when (mode) {
                                    ConnectionMode.HOTSPOT -> listOf(draft.normalizedHotspotUrl())
                                    ConnectionMode.TAILSCALE -> listOfNotNull(draft.normalizedTailscaleUrl())
                                    ConnectionMode.CUSTOM -> listOfNotNull(draft.normalizedCustomUrl())
                                    ConnectionMode.AUTO -> draft.candidateUrls()
                                }
                                if (targets.isEmpty()) {
                                    result = BannerTone.ERROR to "接続先のアドレスを入力してください。"
                                    return@OutlinedButton
                                }
                                scope.launch {
                                    testing = true
                                    result = null
                                    val results = apiClient.testCandidates(targets)
                                    val lines = results.joinToString("\n") { r -> (if (r.ok) "✓ " else "✕ ") + r.url + if (r.ok) "　つながりました" else "　応答なし" }
                                    result = (if (results.any { it.ok }) BannerTone.SUCCESS else BannerTone.ERROR) to lines
                                    testing = false
                                }
                            },
                        ) {
                            Icon(Icons.Default.NetworkCheck, contentDescription = null)
                            Text(if (testing) " 確認中…" else " 接続を確認")
                        }
                        Button(
                            enabled = dirty,
                            onClick = {
                                onUpdateConfig { draft }
                                onReconnect()
                                result = BannerTone.SUCCESS to "接続先を保存し、つなぎ直しています。"
                            },
                        ) { Text("保存してつなぎ直す") }
                    }

                    HorizontalDivider(color = p.line, modifier = Modifier.padding(vertical = 4.dp))

                    // ── Plate notifications ──
                    FieldLabel("ナンバー検知の通知")
                    SectionTabs(
                        tabs = listOf(
                            PlatePopupFilter.MATCHED_ONLY.id to "条件に一致",
                            PlatePopupFilter.ALL.id to "すべて",
                            PlatePopupFilter.OFF.id to "通知しない",
                        ),
                        selectedId = config.popupFilter.id,
                        onSelect = { id -> onUpdateConfig { it.copy(popupFilter = PlatePopupFilter.fromId(id)) } },
                        fill = true,
                    )
                    FieldLabel("確認する間隔", trailing = "短いほど早く表示")
                    SectionTabs(
                        tabs = listOf("500" to "0.5秒", "1000" to "1秒", "2000" to "2秒", "3000" to "3秒"),
                        selectedId = config.popupPollIntervalMs.toString(),
                        onSelect = { ms -> onUpdateConfig { it.copy(popupPollIntervalMs = ms.toLong()) } },
                        fill = true,
                    )
                    FieldLabel("表示する時間")
                    SectionTabs(
                        tabs = listOf("5" to "5秒", "8" to "8秒", "12" to "12秒", "20" to "20秒", "0" to "ずっと"),
                        selectedId = config.popupDisplaySeconds.toString(),
                        onSelect = { sec -> onUpdateConfig { it.copy(popupDisplaySeconds = sec.toInt()) } },
                        fill = true,
                    )
                    SwitchRow("通知音を鳴らす", config.popupSoundEnabled, { v -> onUpdateConfig { it.copy(popupSoundEnabled = v) } })
                    SwitchRow("検知した画像を表示する", config.popupShowImage, { v -> onUpdateConfig { it.copy(popupShowImage = v) } })
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        OutlinedButton(onClick = { onTestPopup(true); onDismiss() }) { Text("試す（条件に一致）") }
                        OutlinedButton(onClick = { onTestPopup(false); onDismiss() }) { Text("試す（通常）") }
                    }

                    HorizontalDivider(color = p.line, modifier = Modifier.padding(vertical = 4.dp))

                    // ── Display ──
                    FieldLabel("表示")
                    SectionTabs(
                        tabs = listOf(AppThemeMode.SYSTEM.id to "端末に合わせる", AppThemeMode.LIGHT.id to "ライト", AppThemeMode.DARK.id to "ダーク"),
                        selectedId = config.themeMode.id,
                        onSelect = { id -> onUpdateConfig { it.copy(themeMode = AppThemeMode.fromId(id)) } },
                        fill = true,
                    )
                    SwitchRow(
                        "画面を消さない",
                        config.keepScreenOn,
                        { v -> onUpdateConfig { it.copy(keepScreenOn = v) } },
                        description = "車載ディスプレイやEcho Show 5で常に表示しておくときに。",
                    )
                    Text("NP1 Control Center for Android ${BuildConfig.VERSION_NAME}", color = p.faint, style = MaterialTheme.typography.labelSmall)
                }
            }
        }
    }
}
