package com.np1control.app.ui.screens

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.OpenInNew
import androidx.compose.material.icons.filled.AddLink
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material3.Button
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import com.np1control.app.data.AppConfig
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.data.SystemSummary
import com.np1control.app.data.bool
import com.np1control.app.data.str
import com.np1control.app.data.strings
import com.np1control.app.ui.components.AdaptiveColumns
import com.np1control.app.ui.components.BannerTone
import com.np1control.app.ui.components.HintText
import com.np1control.app.ui.components.IconBadge
import com.np1control.app.ui.components.KeyValueRow
import com.np1control.app.ui.components.NccBanner
import com.np1control.app.ui.components.NccPanel
import com.np1control.app.ui.components.ScreenColumn
import com.np1control.app.ui.components.StatusPill
import com.np1control.app.ui.components.Tone
import com.np1control.app.ui.components.PanelAction
import com.np1control.app.ui.theme.LocalNccPalette
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put

private val backendLabels = mapOf(
    "Running" to "稼働中",
    "NeedsLogin" to "ログインが必要",
    "NeedsMachineAuth" to "端末の承認待ち",
    "Stopped" to "停止中",
    "Starting" to "起動中",
    "NoState" to "状態未取得",
)

@Composable
fun TailscaleScreen(
    summary: SystemSummary,
    apiClient: NccApiClient,
    onRefreshSummary: () -> Unit,
    onSaveAsTailscaleHost: (String) -> Unit,
    savedTailscaleHost: String = "",
) {
    val p = LocalNccPalette.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    var current by remember { mutableStateOf<Observation?>(summary["tailscale"]) }
    LaunchedEffect(summary["tailscale"]) { summary["tailscale"]?.let { current = it } }
    val data = current?.data as? JsonObject
    var expected by remember { mutableStateOf(data.str("hostName").orEmpty()) }
    var draft by remember { mutableStateOf(data.str("hostName").orEmpty()) }
    var busy by remember { mutableStateOf(false) }
    var loginBusy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var message by remember { mutableStateOf("") }
    var loginUrl by remember { mutableStateOf("") }
    LaunchedEffect(data.str("hostName")) {
        if (draft == expected && !busy) {
            draft = data.str("hostName").orEmpty()
            expected = draft
        }
    }

    val online = data.bool("online")
    val backendState = data.str("backendState").orEmpty()
    val ips = data.strings("ips")
    val ipv4 = ips.firstOrNull { it.contains('.') }
    val needsLogin = backendState == "NeedsLogin" || backendState == "Stopped"
    val nameDiffers = data.str("deviceName") != null && data.str("hostName") != null && data.str("deviceName") != data.str("hostName")
    val dirty = draft != expected
    val validName = Regex("^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$").matches(draft)
    val writable = current?.status == "available" && data.bool("nameWritable") == true && expected.isNotEmpty()
    val changedElsewhere = data.str("hostName") != null && data.str("hostName") != expected
    val registered = ipv4 != null && AppConfig.normalizeBaseUrl(savedTailscaleHost) == AppConfig.normalizeBaseUrl(ipv4)

    ScreenColumn {
        if (error.isNotBlank()) NccBanner(error, BannerTone.ERROR, onDismiss = { error = "" })
        if (message.isNotBlank()) NccBanner(message, BannerTone.SUCCESS, onDismiss = { message = "" })
        AdaptiveColumns(
            left = {
                NccPanel(
                    title = "外出先からの接続",
                    subtitle = "Tailscaleで、NP1のホットスポットにつながっていない時も操作できます。",
                    icon = Icons.Default.Shield,
                    headerEnd = {
                        StatusPill(
                            when (online) { true -> "接続済み"; false -> "未接続"; null -> "状態未取得" },
                            if (online == true) Tone.OK else Tone.NEUTRAL,
                        )
                    },
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        IconBadge(Icons.Default.Shield, tone = if (online == true) Tone.OK else Tone.NEUTRAL, size = 44.dp)
                        Column(Modifier.weight(1f)) {
                            Text("登録機器名", color = p.muted, style = MaterialTheme.typography.labelSmall)
                            Text(data.str("deviceName") ?: "機器名未取得", color = p.text, style = MaterialTheme.typography.titleLarge)
                            data.str("dnsName")?.let { Text(it, color = p.muted, style = MaterialTheme.typography.bodySmall) }
                        }
                    }
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        IconBadge(Icons.Default.Person, tone = Tone.NEUTRAL, size = 44.dp)
                        Column(Modifier.weight(1f)) {
                            Text("アカウント", color = p.muted, style = MaterialTheme.typography.labelSmall)
                            Text(data.str("accountLogin") ?: data.str("accountName") ?: "アカウント未取得", color = p.text, style = MaterialTheme.typography.titleSmall)
                            val name = data.str("accountName")
                            if (name != null && data.str("accountLogin") != null && name != data.str("accountLogin")) {
                                Text(name, color = p.muted, style = MaterialTheme.typography.bodySmall)
                            }
                        }
                    }
                    KeyValueRow("接続状態", backendLabels[backendState] ?: "状態未取得")
                    Text("管理アドレス", color = p.muted, style = MaterialTheme.typography.bodySmall)
                    if (ips.isEmpty()) Text("未割り当て", color = p.text, style = MaterialTheme.typography.bodyMedium)
                    ips.forEach { ip ->
                        Row(
                            Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(p.raised).padding(start = 12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(ip, color = p.text, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f), maxLines = 1)
                            IconButton(onClick = { copy(context, ip); message = "アドレスをコピーしました" }) {
                                Icon(Icons.Default.ContentCopy, contentDescription = "コピー", tint = p.muted, modifier = Modifier.size(18.dp))
                            }
                        }
                    }
                    HintText("別の端末から開くときは、その端末も同じtailnetに接続してください。")
                    if (needsLogin || loginUrl.isNotBlank()) {
                        if (needsLogin) {
                            FilledTonalButton(
                                enabled = !loginBusy && !busy && current?.status == "available",
                                onClick = {
                                    scope.launch {
                                        loginBusy = true
                                        error = ""
                                        runCatching { apiClient.requestRaw("/vpn/tailscale/login", "POST", "{}") }
                                            .onSuccess { raw ->
                                                val url = runCatching {
                                                    (apiClient.json.parseToJsonElement(raw).jsonObject["authURL"] as? JsonPrimitive)?.contentOrNull
                                                }.getOrNull()
                                                if (url.isNullOrBlank()) onRefreshSummary()
                                                else {
                                                    val u = Uri.parse(url)
                                                    if (u.scheme != "https" || u.host != "login.tailscale.com") error = "認証先を確認できません。"
                                                    else loginUrl = url
                                                }
                                            }
                                            .onFailure { error = it.message ?: "ログインを開始できません" }
                                        loginBusy = false
                                    }
                                },
                            ) { Text(if (loginBusy) "接続を準備しています…" else if (backendState == "Stopped") "Tailscaleに接続" else "ログインを始める") }
                        }
                        if (loginUrl.isNotBlank()) {
                            Button(onClick = { runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(loginUrl))) } }) {
                                Text("Tailscaleでログインする ")
                                Icon(Icons.AutoMirrored.Filled.OpenInNew, contentDescription = null, modifier = Modifier.size(16.dp))
                            }
                        }
                    }
                }
                NccPanel(
                    title = "このアプリの接続先",
                    subtitle = "外出先でもこのアプリからNP1を開けるよう、Tailscaleのアドレスを覚えておきます。",
                    icon = Icons.Default.AddLink,
                ) {
                    if (ipv4 == null) {
                        HintText("Tailscaleのアドレスが割り当てられると登録できます。")
                    } else if (registered) {
                        StatusPill("登録済み · $ipv4", Tone.OK)
                        HintText("ホットスポットにつながらないときは、自動でこのアドレスへ接続します（接続先が「自動」のとき）。")
                    } else {
                        Button(onClick = { onSaveAsTailscaleHost(ipv4); message = "アプリの接続先に $ipv4 を登録しました" }) {
                            Text("$ipv4 を登録")
                        }
                        if (savedTailscaleHost.isNotBlank()) HintText("今の登録: $savedTailscaleHost")
                    }
                }
            },
            right = {
                NccPanel(
                    title = "機器名",
                    icon = Icons.Default.Edit,
                    headerEnd = {
                        PanelAction(enabled = !busy, onClick = onRefreshSummary) { Icon(Icons.Default.Refresh, contentDescription = "Tailscaleの状態を更新", tint = p.muted) }
                    },
                ) {
                    if (nameDiffers) HintText("この機器から通知する名前（ホスト名）です。管理コンソールの登録機器名と異なる場合があります。")
                    OutlinedTextField(
                        value = draft,
                        onValueChange = { draft = it.take(63) },
                        label = { Text(if (nameDiffers) "ホスト名" else "機器名") },
                        placeholder = { Text("例: np1-car") },
                        singleLine = true,
                        isError = dirty && !validName,
                        enabled = !busy && writable,
                        supportingText = { Text("英小文字・数字・ハイフン、1〜63文字。先頭と末尾にハイフンは使えません。") },
                        modifier = Modifier.fillMaxWidth(),
                    )
                    if (nameDiffers) HintText("登録機器名は ${data.str("deviceName")}、今のホスト名は ${data.str("hostName")} です。")
                    if (!writable) HintText(current?.reason ?: "今は名前を変更できません。Tailscaleの接続状態を確認してください。", color = p.warn)
                    if (changedElsewhere) HintText("別の操作で名前が変わりました。最新の名前を読み込んでから変更してください。", color = p.warn)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                        Button(
                            enabled = !busy && writable && validName && dirty && !changedElsewhere,
                            onClick = {
                                scope.launch {
                                    busy = true
                                    error = ""
                                    message = ""
                                    val target = draft
                                    val differentRegistration = nameDiffers
                                    runCatching {
                                        apiClient.put<Observation>(
                                            "/vpn/tailscale/name",
                                            buildJsonObject { put("expectedHostName", expected); put("hostName", target) }.toString(),
                                        )
                                    }.onSuccess { v ->
                                        if (v.status != "available" || (v.data as? JsonObject).str("hostName") != target) {
                                            error = "名前の反映をまだ確認できません。状態を更新してください。"
                                        } else {
                                            current = v
                                            expected = target
                                            draft = target
                                            message = if (differentRegistration) "ホスト名の変更を確認しました。" else "機器名の変更を確認しました。"
                                            onRefreshSummary()
                                        }
                                    }.onFailure { error = it.message ?: "変更できませんでした" }
                                    busy = false
                                }
                            },
                        ) { Text(if (busy) "反映を確認中…" else "機器名を変更") }
                        if (dirty || changedElsewhere) {
                            TextButton(enabled = !busy, onClick = {
                                draft = data.str("hostName").orEmpty()
                                expected = draft
                                error = ""
                                message = ""
                            }) { Text(if (changedElsewhere) "最新の名前を読み込む" else "入力を戻す") }
                        }
                    }
                }
            },
        )
    }
}

private fun copy(context: Context, text: String) {
    val cm = context.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager ?: return
    cm.setPrimaryClip(ClipData.newPlainText("Tailscale", text))
}
