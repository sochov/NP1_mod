package com.np1control.app.ui.components

import androidx.annotation.OptIn
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.PlaybackParameters
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.ui.PlayerView
import com.np1control.app.data.NccApiClient
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive

/**
 * Plays an original recording (H.264/AAC MP4) straight from the NP1. A new [streamUrl] loads the
 * clip at [startOffsetMs]; changing [seekRequest] seeks within the loaded clip without reloading.
 */
@OptIn(UnstableApi::class)
@Composable
fun VideoPlayerBox(
    streamUrl: String,
    apiClient: NccApiClient,
    startOffsetMs: Long,
    seekRequest: Pair<Int, Long>? = null,
    playbackSpeed: Float = 1f,
    muted: Boolean = false,
    onPositionChanged: ((Long) -> Unit)? = null,
    onEnded: (() -> Unit)? = null,
    onError: ((String) -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val latestPosition by rememberUpdatedState(onPositionChanged)
    val latestEnded by rememberUpdatedState(onEnded)
    val latestError by rememberUpdatedState(onError)
    val player = remember {
        ExoPlayer.Builder(context)
            .setMediaSourceFactory(DefaultMediaSourceFactory(OkHttpDataSource.Factory(apiClient.okHttpClient)))
            .build()
    }
    var loadedUrl by remember { mutableStateOf("") }

    LaunchedEffect(streamUrl) {
        if (loadedUrl != streamUrl) {
            player.setMediaItem(MediaItem.fromUri(streamUrl), startOffsetMs.coerceAtLeast(0))
            player.prepare()
            player.playWhenReady = true
            loadedUrl = streamUrl
        }
    }
    LaunchedEffect(seekRequest) {
        val req = seekRequest ?: return@LaunchedEffect
        if (loadedUrl == streamUrl) {
            player.seekTo(req.second.coerceAtLeast(0))
            player.playWhenReady = true
        }
    }
    LaunchedEffect(playbackSpeed) { player.playbackParameters = PlaybackParameters(playbackSpeed) }
    LaunchedEffect(muted) { player.volume = if (muted) 0f else 1f }

    DisposableEffect(player) {
        val listener = object : Player.Listener {
            override fun onPlaybackStateChanged(playbackState: Int) {
                if (playbackState == Player.STATE_ENDED) latestEnded?.invoke()
            }

            override fun onPlayerError(error: PlaybackException) {
                latestError?.invoke("この録画を再生できません。原本をダウンロードして確認してください。")
            }
        }
        player.addListener(listener)
        onDispose {
            player.removeListener(listener)
            player.release()
        }
    }
    LaunchedEffect(player) {
        while (isActive) {
            if (player.isPlaying) latestPosition?.invoke(player.currentPosition)
            delay(400L)
        }
    }

    Box(
        modifier = modifier
            .fillMaxWidth()
            .aspectRatio(16f / 9f)
            .clip(RoundedCornerShape(14.dp))
            .background(Color(0xFF0B0E13)),
    ) {
        AndroidView(
            factory = { ctx ->
                PlayerView(ctx).apply {
                    this.player = player
                    useController = true
                    controllerShowTimeoutMs = 2500
                    setShowNextButton(false)
                    setShowPreviousButton(false)
                }
            },
            modifier = Modifier.fillMaxSize(),
        )
    }
}
