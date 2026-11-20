package com.np1control.app.ui.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
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
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.np1control.app.data.AppConfig
import com.np1control.app.data.AppThemeMode
import com.np1control.app.data.ConnectionMode
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.PlatePopupFilter
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.SectionTabs
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.launch

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun AppSettingsDialog(
    config: AppConfig,
    apiClient: NccApiClient,
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
    var testResult by remember { mutableStateOf<Pair<BannerTone, String>?>(null) }
    var testing by remember { mutableStateOf(false) }

    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(usePlatformDefaultWidth = false),
    ) {
        Surface(
            shape = RoundedCornerShape(16.dp),
            color = p.paper,
            border = BorderStroke(1.dp, p.line),
            modifier = Modifier
                .fillMaxWidth(0.94f)
                .heightIn(max = 640.dp),
        ) {
            Column(modifier = Modifier.fillMaxWidth()) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 16.dp, vertical = 12.dp),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("接続・ポップアップ設定", color = p.text, fontWeight = FontWeight.Bold, fontSize = 16.sp)
                    IconButton(onClick = onDismiss) {
                        Icon(Icons.Default.Close, contentDescription = "閉じる", tint = p.muted)
                    }
                }
                HorizontalDivider(color = p.line)

                Column(
                    modifier = Modifier
                        .weight(1f, fill = false)
                        .verticalScroll(rememberScrollState())
                        .padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    // ── Connection target ─────────────────────────────────────
                    Text("NP1への接続先", color = p.accent, fontSize = 12.sp, fontWeight = FontWeight.Bold)
                    SectionTabs(
                        tabs = listOf(
                            ConnectionMode.AUTO.id to "自動",
                            ConnectionMode.HOTSPOT.id to "ホットスポット",
                            ConnectionMode.TAILSCALE.id to "Tailscale",
                            ConnectionMode.CUSTOM.id to "カスタム",
                        ),
                        selectedId = mode.id,
                        onSelect = { mode = ConnectionMode.fromId(it) },
                    )
                    Text(
                        when (mode) {
                            ConnectionMode.AUTO -> "まずNP1ホットスポット (192.168.226.1) を試し、応答がなければTailscale IPへ自動接続します。"
                            ConnectionMode.HOTSPOT -> "NP1ホットスポットAP配下の固定アドレスへ接続します。"
                            ConnectionMode.TAILSCALE -> "事前に設定したNP1のTailscale IPへ接続します。"
                            ConnectionMode.CUSTOM -> "任意のURL（ポート転送や検証用）へ接続します。"
                        },
                        color = p.muted,
                        fontSize = 11.sp,
                    )

                    OutlinedTextField(
                        value = hotspotUrl,
                        onValueChange = { hotspotUrl = it },
                        label = { Text("NP1 ホットスポット URL") },
                        placeholder = { Text("http://192.168.226.1") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = tailscaleHost,
                        onValueChange = { tailscaleHost = it },
                        label = { Text("Tailscale IP / ホスト名 (例: 100.64.0.10)") },
                        placeholder = { Text("100.x.y.z") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    if (mode == ConnectionMode.CUSTOM) {
                        OutlinedTextField(
                            value = customUrl,
                            onValueChange = { customUrl = it },
                            label = { Text("カスタム URL (例: http://127.0.0.1:8080)") },
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }

                    testResult?.let { (tone, msg) -> NccBanner(msg, tone, onDismiss = { testResult = null }) }

                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Button(
                            onClick = {
                                onUpdateConfig {
                                    it.copy(
                                        connectionMode = mode,
                                        hotspotUrl = hotspotUrl.ifBlank { AppConfig.DEFAULT_HOTSPOT_URL },
                                        tailscaleHost = tailscaleHost.trim(),
                                        customUrl = customUrl.trim(),
                                    )
                                }
                                onReconnect()
                                testResult = BannerTone.SUCCESS to "接続先を保存して再接続を開始しました"
                            },
                        ) { Text("接続先を保存・再接続") }

                        OutlinedButton(
                            enabled = !testing,
                            onClick = {
                                val draftConfig = config.copy(
                                    connectionMode = mode,
                                    hotspotUrl = hotspotUrl.ifBlank { AppConfig.DEFAULT_HOTSPOT_URL },
                                    tailscaleHost = tailscaleHost.trim(),
                                    customUrl = customUrl.trim(),
                                )
                                val targets = when (mode) {
                                    ConnectionMode.HOTSPOT -> listOf(draftConfig.normalizedHotspotUrl())
                                    ConnectionMode.TAILSCALE -> listOfNotNull(draftConfig.normalizedTailscaleUrl())
                                    ConnectionMode.CUSTOM -> listOfNotNull(draftConfig.normalizedCustomUrl())
                                    ConnectionMode.AUTO -> draftConfig.candidateUrls()
                                }
                                if (targets.isEmpty()) {
                                    testResult = BannerTone.ERROR to "接続先URL / Tailscale IP を入力してください"
                                    return@OutlinedButton
                                }
                                scope.launch {
                                    testing = true
                                    testResult = null
                                    val results = apiClient.testCandidates(targets)
                                    val okResults = results.filter { it.ok }
                                    val summaryLines = results.joinToString("\n") { r ->
                                        if (r.ok) "✓ ${r.url} : 接続OK"
                                        else "✕ ${r.url} : 未到達"
                                    }
                                    testResult = if (okResults.isNotEmpty()) {
                                        BannerTone.SUCCESS to "接続OK (${okResults.first().url})\n$summaryLines"
                                    } else {
                                        val detail = results.joinToString("\n") { r ->
                                            "✕ ${r.url}: ${r.error ?: "接続失敗"}"
                                        }
                                        BannerTone.ERROR to "接続失敗\n$detail"
                                    }
                                    testing = false
                                }
                            },
                        ) {
                            Icon(Icons.Default.NetworkCheck, contentDescription = null)
                            Text(if (testing) " 確認中…" else " 接続テスト")
                        }
                    }

                    HorizontalDivider(color = p.line)

                    // ── Realtime Plate Popup Settings ─────────────────────────
                    Text("リアルタイム・ナンバープレート ポップアップ", color = p.accent, fontSize = 12.sp, fontWeight = FontWeight.Bold)
                    SectionTabs(
                        tabs = listOf(
                            PlatePopupFilter.MATCHED_ONLY.id to "条件一致のみ",
                            PlatePopupFilter.ALL.id to "すべて",
                            PlatePopupFilter.OFF.id to "OFF",
                        ),
                        selectedId = config.popupFilter.id,
                        onSelect = { id -> onUpdateConfig { it.copy(popupFilter = PlatePopupFilter.fromId(id)) } },
                    )
                    Text("更新間隔（短いほど即座に表示）", color = p.muted, fontSize = 11.sp)
                    SectionTabs(
                        tabs = listOf("500" to "0.5秒", "1000" to "1秒", "2000" to "2秒", "3000" to "3秒"),
                        selectedId = config.popupPollIntervalMs.toString(),
                        onSelect = { ms -> onUpdateConfig { it.copy(popupPollIntervalMs = ms.toLongOrNull() ?: 1000L) } },
                    )
                    Text("ポップアップ自動消去", color = p.muted, fontSize = 11.sp)
                    SectionTabs(
                        tabs = listOf("5" to "5秒", "8" to "8秒", "12" to "12秒", "20" to "20秒", "0" to "手動"),
                        selectedId = config.popupDisplaySeconds.toString(),
                        onSelect = { sec -> onUpdateConfig { it.copy(popupDisplaySeconds = sec.toIntOrNull() ?: 8) } },
                    )
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text("検知時にチャイム音を鳴らす", color = p.text, fontSize = 13.sp)
                        Switch(
                            checked = config.popupSoundEnabled,
                            onCheckedChange = { v -> onUpdateConfig { it.copy(popupSoundEnabled = v) } },
                        )
                    }
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text("保存画像サムネイルを表示", color = p.text, fontSize = 13.sp)
                        Switch(
                            checked = config.popupShowImage,
                            onCheckedChange = { v -> onUpdateConfig { it.copy(popupShowImage = v) } },
                        )
                    }
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        OutlinedButton(onClick = { onTestPopup(true); onDismiss() }) { Text("テスト表示（条件一致）") }
                        OutlinedButton(onClick = { onTestPopup(false); onDismiss() }) { Text("テスト表示（すべて）") }
                    }

                    HorizontalDivider(color = p.line)

                    // ── Display / Device Settings ─────────────────────────────
                    Text("画面・端末設定", color = p.accent, fontSize = 12.sp, fontWeight = FontWeight.Bold)
                    SectionTabs(
                        tabs = listOf(
                            AppThemeMode.SYSTEM.id to "システム",
                            AppThemeMode.LIGHT.id to "ライト",
                            AppThemeMode.DARK.id to "ダーク",
                        ),
                        selectedId = config.themeMode.id,
                        onSelect = { id -> onUpdateConfig { it.copy(themeMode = AppThemeMode.fromId(id)) } },
                    )
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text("画面を常時点灯 (スリープ防止)", color = p.text, fontSize = 13.sp)
                            Text("Echo Show 5 や車載ディスプレイ常時表示に推奨", color = p.muted, fontSize = 11.sp)
                        }
                        Switch(
                            checked = config.keepScreenOn,
                            onCheckedChange = { v -> onUpdateConfig { it.copy(keepScreenOn = v) } },
                        )
                    }
                }
            }
        }
    }
}
