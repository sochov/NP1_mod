package com.np1control.app.ui.screens

import android.content.Intent
import android.net.Uri
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
import androidx.compose.material.icons.automirrored.filled.OpenInNew
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Save
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.data.SystemSummary
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.KeyValueRow
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.NccTag
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

@Composable
fun TailscaleScreen(
    summary: SystemSummary,
    apiClient: NccApiClient,
    onRefreshSummary: () -> Unit,
    onSaveAsTailscaleHost: (String) -> Unit,
) {
    val p = LocalNccPalette.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    val obs = summary["tailscale"]
    val data = obs?.data as? JsonObject
    val online = (data?.get("online") as? JsonPrimitive)?.booleanOrNull == true
    val backendState = (data?.get("backendState") as? JsonPrimitive)?.contentOrNull ?: ""
    val deviceName = (data?.get("deviceName") as? JsonPrimitive)?.contentOrNull ?: "未取得"
    val hostName = (data?.get("hostName") as? JsonPrimitive)?.contentOrNull ?: ""
    val dnsName = (data?.get("dnsName") as? JsonPrimitive)?.contentOrNull ?: ""
    val account = (data?.get("accountLogin") as? JsonPrimitive)?.contentOrNull
        ?: (data?.get("accountName") as? JsonPrimitive)?.contentOrNull ?: "未取得"
    val ips = (data?.get("ips") as? JsonArray)?.mapNotNull { it.jsonPrimitive.contentOrNull } ?: emptyList()
    val nameWritable = (data?.get("nameWritable") as? JsonPrimitive)?.booleanOrNull == true

    var hostDraft by remember(hostName) { mutableStateOf(hostName) }
    var authUrl by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var notice by remember { mutableStateOf("") }

    LaunchedEffect(hostName) {
        if (hostDraft.isEmpty() && hostName.isNotEmpty()) hostDraft = hostName
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

        NccPanel(
            title = "Tailscale / 遠隔アクセス",
            icon = Icons.Default.Shield,
            headerEnd = {
                NccTag(
                    text = if (online) "接続済み" else "未接続",
                    color = if (online) p.ok else p.muted,
                    background = if (online) p.okSoft else p.bg,
                )
                IconButton(onClick = onRefreshSummary) {
                    Icon(Icons.Default.Refresh, contentDescription = "更新", tint = p.muted)
                }
            },
        ) {
            KeyValueRow("登録機器名", deviceName)
            if (dnsName.isNotBlank()) KeyValueRow("DNS名", dnsName, mono = true)
            KeyValueRow("アカウント", account)
            KeyValueRow(
                "バックエンド状態",
                when (backendState) {
                    "Running" -> "稼働中"
                    "NeedsLogin" -> "認証待ち"
                    "NeedsMachineAuth" -> "端末承認待ち"
                    "Stopped" -> "停止中"
                    else -> backendState.ifBlank { "状態未取得" }
                },
            )
            KeyValueRow("管理アドレス", ips.joinToString("\n").ifBlank { "未割当" }, mono = true)

            val ipv4 = ips.firstOrNull { !it.contains(":") }
            if (ipv4 != null) {
                OutlinedButton(
                    onClick = {
                        onSaveAsTailscaleHost(ipv4)
                        notice = "このアプリのTailscale接続先を $ipv4 に設定しました"
                    },
                ) {
                    Text("このIP ($ipv4) をアプリのTailscale接続先に登録")
                }
            }

            if (backendState in listOf("NeedsLogin", "Stopped") || authUrl.isNotBlank()) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(
                        enabled = !busy,
                        onClick = {
                            scope.launch {
                                busy = true
                                error = ""
                                runCatching { apiClient.requestRaw("/vpn/tailscale/login", "POST", "{}") }
                                    .onSuccess { raw ->
                                        val obj = apiClient.json.parseToJsonElement(raw).jsonObject
                                        val url = (obj["authURL"] as? JsonPrimitive)?.contentOrNull.orEmpty()
                                        if (url.startsWith("https://login.tailscale.com")) {
                                            authUrl = url
                                            notice = "認証リンクを取得しました"
                                        } else {
                                            onRefreshSummary()
                                        }
                                    }
                                    .onFailure { error = it.message ?: "認証開始に失敗しました" }
                                busy = false
                            }
                        },
                    ) {
                        Text(if (busy) "準備中…" else "Tailscaleの認証を開始")
                    }
                    if (authUrl.isNotBlank()) {
                        Button(onClick = {
                            runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(authUrl))) }
                        }) {
                            Icon(Icons.AutoMirrored.Filled.OpenInNew, contentDescription = null)
                            Text(" ログイン画面を開く")
                        }
                    }
                }
            }
        }

        NccPanel(title = "機器名（ホスト名）の設定") {
            OutlinedTextField(
                value = hostDraft,
                onValueChange = { hostDraft = it },
                label = { Text("ホスト名 (英小文字・数字・ハイフン)") },
                singleLine = true,
                enabled = nameWritable && !busy,
                modifier = Modifier.fillMaxWidth(),
            )
            Text("英小文字・数字・ハイフン、1〜63文字。先頭と末尾のハイフンは使えません。", color = p.muted, fontSize = 11.sp)
            val valid = Regex("^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$").matches(hostDraft)
            Button(
                enabled = !busy && nameWritable && valid && hostDraft != hostName,
                onClick = {
                    scope.launch {
                        busy = true
                        error = ""
                        notice = ""
                        runCatching {
                            apiClient.put<Observation>(
                                "/vpn/tailscale/name",
                                """{"expectedHostName":"$hostName","hostName":"$hostDraft"}""",
                            )
                        }.onSuccess {
                            notice = "機器名の変更を確認しました"
                            onRefreshSummary()
                        }.onFailure { error = it.message ?: "機器名の変更に失敗しました" }
                        busy = false
                    }
                },
            ) {
                Icon(Icons.Default.Save, contentDescription = null)
                Text(if (busy) " 確認中…" else " 機器名を変更")
            }
        }
        Spacer(modifier = Modifier.height(12.dp))
    }
}
