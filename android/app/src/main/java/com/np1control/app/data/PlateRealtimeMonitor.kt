package com.np1control.app.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import java.time.Instant

data class PlatePopupEvent(
    val uid: String,
    val envelope: PlateRecordEnvelope,
    val detectedAtMs: Long = System.currentTimeMillis(),
    val isTest: Boolean = false,
) {
    val isMatched: Boolean
        get() = envelope.record.matches.isNotEmpty() ||
            (envelope.numberShare?.status == "exact")

    val isDanger: Boolean
        get() = envelope.numberShare?.status == "exact" &&
            envelope.numberShare.category == "危険"
}

/**
 * Monitors /api/v1/plate/records at high frequency (default 1.0s) and immediately surfaces
 * newly detected Japanese license plates according to the user's filter (MATCHED_ONLY / ALL / OFF).
 */
class PlateRealtimeMonitor(
    private val fetchRecords: suspend (from: String, to: String, matchedOnly: Boolean, limit: Int) -> PlateRecordsResponse,
    private val configProvider: () -> AppConfig,
    private val clockMs: () -> Long = { System.currentTimeMillis() },
    private val todayProvider: () -> String = { Formatters.jstDay(0) },
) {
    private val _activePopups = MutableStateFlow<List<PlatePopupEvent>>(emptyList())
    val activePopups: StateFlow<List<PlatePopupEvent>> = _activePopups.asStateFlow()

    private val _recentFeed = MutableStateFlow<List<PlatePopupEvent>>(emptyList())
    val recentFeed: StateFlow<List<PlatePopupEvent>> = _recentFeed.asStateFlow()

    private val _monitorActive = MutableStateFlow(false)
    val monitorActive: StateFlow<Boolean> = _monitorActive.asStateFlow()

    private val _lastError = MutableStateFlow<String?>(null)
    val lastError: StateFlow<String?> = _lastError.asStateFlow()

    private var baselineInitialized = false
    private var baselineDay = ""
    // Maps record.id -> number of matched rules already announced, so a record that gains
    // a rule match after initial ingestion can still trigger a popup.
    private val seenRecords = LinkedHashMap<String, Int>()
    private var pollJob: Job? = null

    fun resetBaseline() {
        baselineInitialized = false
        seenRecords.clear()
    }

    fun start(scope: CoroutineScope) {
        if (pollJob?.isActive == true) return
        pollJob = scope.launch {
            while (isActive) {
                val cfg = configProvider()
                if (cfg.popupFilter == PlatePopupFilter.OFF) {
                    _monitorActive.value = false
                    delay(1000L)
                    continue
                }
                _monitorActive.value = true
                runCatching { pollOnce(cfg) }
                    .onSuccess { _lastError.value = null }
                    .onFailure { _lastError.value = it.message }
                purgeExpired(cfg)
                delay(cfg.popupPollIntervalMs.coerceIn(500L, 10_000L))
            }
        }
    }

    fun stop() {
        pollJob?.cancel()
        pollJob = null
        _monitorActive.value = false
    }

    suspend fun pollOnce(cfg: AppConfig = configProvider()): List<PlatePopupEvent> {
        if (cfg.popupFilter == PlatePopupFilter.OFF) return emptyList()
        val today = todayProvider()
        if (baselineDay != today) {
            baselineDay = today
            baselineInitialized = false
            seenRecords.clear()
        }

        val matchedOnlyQuery = cfg.popupFilter == PlatePopupFilter.MATCHED_ONLY
        val response = fetchRecords(today, today, matchedOnlyQuery, 20)

        if (!baselineInitialized) {
            for (item in response.records) {
                seenRecords[item.record.id] = item.record.matches.size
            }
            baselineInitialized = true
            return emptyList()
        }

        val newlyDetected = mutableListOf<PlatePopupEvent>()
        // Records come newest-first; iterate oldest-first so the newest ends up on top of the popup stack
        for (item in response.records.asReversed()) {
            val rec = item.record
            val prevMatchCount = seenRecords[rec.id]
            val isNew = prevMatchCount == null
            val gainedMatch = prevMatchCount != null && rec.matches.size > prevMatchCount
            if (!isNew && !gainedMatch) continue

            seenRecords[rec.id] = rec.matches.size
            if (seenRecords.size > 500) {
                val oldestKey = seenRecords.keys.firstOrNull()
                if (oldestKey != null) seenRecords.remove(oldestKey)
            }

            val passesFilter = when (cfg.popupFilter) {
                PlatePopupFilter.OFF -> false
                PlatePopupFilter.MATCHED_ONLY -> rec.matches.isNotEmpty() || item.numberShare?.status == "exact"
                PlatePopupFilter.ALL -> true
            }
            if (passesFilter) {
                newlyDetected.add(
                    PlatePopupEvent(
                        uid = "${rec.id}:${rec.matches.size}:${clockMs()}",
                        envelope = item,
                        detectedAtMs = clockMs(),
                    ),
                )
            }
        }

        if (newlyDetected.isNotEmpty()) {
            val combinedActive = (newlyDetected.asReversed() + _activePopups.value).take(3)
            _activePopups.value = combinedActive
            _recentFeed.value = (newlyDetected.asReversed() + _recentFeed.value).take(30)
        }
        return newlyDetected
    }

    fun purgeExpired(cfg: AppConfig = configProvider()) {
        val ttlSec = cfg.popupDisplaySeconds
        if (ttlSec <= 0) return
        val cutoff = clockMs() - ttlSec * 1000L
        val current = _activePopups.value
        val kept = current.filter { it.detectedAtMs >= cutoff }
        if (kept.size != current.size) {
            _activePopups.value = kept
        }
    }

    fun dismiss(uid: String) {
        _activePopups.value = _activePopups.value.filterNot { it.uid == uid }
    }

    fun dismissAll() {
        _activePopups.value = emptyList()
    }

    fun emitTestPopup(matched: Boolean) {
        val nowIso = Instant.ofEpochMilli(clockMs()).toString()
        val record = PlateRecord(
            id = "test-${clockMs()}",
            firstSeen = nowIso,
            lastSeen = nowIso,
            sightings = if (matched) 3 else 1,
            reading = if (matched) {
                PlateReading(region = "品川", classNum = "330", kana = "あ", number = "1234", confidence = 0.96)
            } else {
                PlateReading(region = "横浜", classNum = "500", kana = "わ", number = "810", confidence = 0.89)
            },
            display = if (matched) "品川 330 あ 12-34" else "横浜 500 わ ・8 10",
            location = PlateLocation(at = nowIso, lat = 35.6812, lon = 139.7671, accuracyM = 4.0),
            matches = if (matched) {
                listOf(PlateMatch(ruleId = "demo", ruleName = "登録車両テスト", saveImage = true, speak = true))
            } else {
                emptyList()
            },
            speech = if (matched) "spoken" else null,
        )
        val event = PlatePopupEvent(
            uid = "test-${clockMs()}",
            envelope = PlateRecordEnvelope(record = record, imageAvailable = false),
            detectedAtMs = clockMs(),
            isTest = true,
        )
        _activePopups.value = (listOf(event) + _activePopups.value).take(3)
        _recentFeed.value = (listOf(event) + _recentFeed.value).take(30)
    }
}
