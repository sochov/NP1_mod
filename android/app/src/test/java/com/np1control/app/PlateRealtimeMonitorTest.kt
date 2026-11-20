package com.np1control.app

import com.np1control.app.data.AppConfig
import com.np1control.app.data.PlateMatch
import com.np1control.app.data.PlatePopupFilter
import com.np1control.app.data.PlateReading
import com.np1control.app.data.PlateRealtimeMonitor
import com.np1control.app.data.PlateRecord
import com.np1control.app.data.PlateRecordEnvelope
import com.np1control.app.data.PlateRecordsResponse
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class PlateRealtimeMonitorTest {

    private fun envelope(id: String, number: String, matched: Boolean): PlateRecordEnvelope =
        PlateRecordEnvelope(
            record = PlateRecord(
                id = id,
                firstSeen = "2026-11-20T09:00:00Z",
                lastSeen = "2026-11-20T09:00:00Z",
                reading = PlateReading(region = "品川", classNum = "300", kana = "あ", number = number, confidence = 0.95),
                matches = if (matched) listOf(PlateMatch(ruleId = "r1", ruleName = "家族")) else emptyList(),
            ),
            imageAvailable = matched,
        )

    @Test
    fun baselineDoesNotPopupOldRecordsThenEmitsNewDetectionsInAllMode() = runTest {
        var now = 1_000_000L
        var serverRecords = listOf(envelope("old-1", "1111", false))
        var cfg = AppConfig(popupFilter = PlatePopupFilter.ALL, popupDisplaySeconds = 8)

        val monitor = PlateRealtimeMonitor(
            fetchRecords = { _, _, _, _ -> PlateRecordsResponse(records = serverRecords) },
            configProvider = { cfg },
            clockMs = { now },
            todayProvider = { "2026-11-20" },
        )

        // First poll establishes baseline -> no popups
        val first = monitor.pollOnce(cfg)
        assertTrue(first.isEmpty())
        assertTrue(monitor.activePopups.value.isEmpty())

        // New unmatched + matched plates arrive
        now += 1000L
        serverRecords = listOf(
            envelope("new-2", "2222", true),
            envelope("new-1", "3333", false),
            envelope("old-1", "1111", false),
        )
        val second = monitor.pollOnce(cfg)
        assertEquals(2, second.size)
        // Newest plate sits on top of activePopups
        assertEquals("new-2", monitor.activePopups.value.first().envelope.record.id)

        // Polling again without new records emits nothing
        assertTrue(monitor.pollOnce(cfg).isEmpty())

        // Expiry removes popups after TTL
        now += 10_000L
        monitor.purgeExpired(cfg)
        assertTrue(monitor.activePopups.value.isEmpty())
    }

    @Test
    fun matchedOnlyFilterIgnoresUnmatchedAndTriggersWhenExistingRecordGainsMatch() = runTest {
        var now = 2_000_000L
        var serverRecords = listOf(envelope("rec-1", "1234", false))
        val cfg = AppConfig(popupFilter = PlatePopupFilter.MATCHED_ONLY, popupDisplaySeconds = 8)

        val monitor = PlateRealtimeMonitor(
            fetchRecords = { _, _, _, _ -> PlateRecordsResponse(records = serverRecords) },
            configProvider = { cfg },
            clockMs = { now },
            todayProvider = { "2026-11-20" },
        )

        monitor.pollOnce(cfg) // baseline

        // Unmatched new plate is ignored in MATCHED_ONLY mode
        now += 1000L
        serverRecords = listOf(envelope("rec-2", "5678", false), envelope("rec-1", "1234", false))
        assertTrue(monitor.pollOnce(cfg).isEmpty())

        // Existing rec-2 later matches a rule -> triggers popup immediately
        now += 1000L
        serverRecords = listOf(envelope("rec-2", "5678", true), envelope("rec-1", "1234", false))
        val emitted = monitor.pollOnce(cfg)
        assertEquals(1, emitted.size)
        assertEquals("rec-2", emitted.first().envelope.record.id)
    }
}
