package com.np1control.app

import androidx.compose.ui.unit.dp
import com.np1control.app.data.PlateMatch
import com.np1control.app.data.PlateRecord
import com.np1control.app.data.PlateRecordEnvelope
import com.np1control.app.ui.screens.landscapePaneWidth
import com.np1control.app.ui.screens.plateArrivals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class LandscapeDashboardTest {
    private fun rec(id: String, matches: Int = 0) = PlateRecordEnvelope(
        record = PlateRecord(id = id, matches = List(matches) { PlateMatch(ruleId = "r$it", ruleName = "条件$it") }),
    )

    @Test
    fun firstPollDoesNotHighlightExistingRecords() {
        val seen = HashMap<String, Int>()
        val arrivals = plateArrivals(seen, listOf(rec("a"), rec("b")), emptyMap(), baseline = true, now = 1_000)
        assertTrue(arrivals.isEmpty())
        assertEquals(setOf("a", "b"), seen.keys)
    }

    @Test
    fun newRecordsAndNewRuleMatchesAreHighlighted() {
        val seen = HashMap<String, Int>()
        plateArrivals(seen, listOf(rec("a"), rec("b")), emptyMap(), baseline = true, now = 1_000)
        // "c" is new; "a" gained a rule match after it was first listed; "b" is unchanged.
        val arrivals = plateArrivals(seen, listOf(rec("c"), rec("a", matches = 1), rec("b")), emptyMap(), baseline = false, now = 2_000)
        assertEquals(mapOf("c" to 2_000L, "a" to 2_000L), arrivals)
        // Highlights older than a minute are forgotten; recent ones are kept.
        val later = plateArrivals(seen, listOf(rec("d"), rec("c"), rec("a", 1), rec("b")), arrivals, baseline = false, now = 62_500)
        assertEquals(mapOf("d" to 62_500L), later)
        val soon = plateArrivals(seen, listOf(rec("d"), rec("c")), later, baseline = false, now = 63_000)
        assertEquals(mapOf("d" to 62_500L), soon)
    }

    @Test
    fun rightColumnWidthStaysReadable() {
        assertEquals(300.dp, landscapePaneWidth(800.dp)) // phone in landscape
        assertEquals(345.6.dp.value, landscapePaneWidth(960.dp).value, 0.01f) // Echo Show 5
        assertEquals(460.dp, landscapePaneWidth(1600.dp)) // tablet
    }
}
