package com.np1control.app

import com.np1control.app.data.AppConfig
import com.np1control.app.data.ConnectionMode
import com.np1control.app.data.JourneyDay
import com.np1control.app.data.JourneyHistory
import com.np1control.app.data.NccApiClient
import com.np1control.app.data.Observation
import com.np1control.app.data.RadioHistoryResponse
import com.np1control.app.data.TrackData
import com.np1control.app.data.TrackPoint
import com.np1control.app.data.TrafficUsageData
import com.np1control.app.ui.components.JourneyGrouping
import com.np1control.app.ui.components.calendarDays
import com.np1control.app.ui.components.groupJourney
import com.np1control.app.ui.components.niceMaximum
import com.np1control.app.ui.components.rollingJourney
import com.np1control.app.ui.screens.splitTrail
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.time.Instant

/** Regression tests for the problems found in the November 2026 screen review. */
class ReviewFixesTest {
    private lateinit var server: MockWebServer
    private val json = NccApiClient { AppConfig() }.json

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    @Test
    fun rollingWindowHoursUseFromAndTo() {
        // /trips?period=24h returns hours with from/to/observed (the app used to expect start/end).
        val body = """{"range":{"period":"24h","from":"2026-11-19","to":"2026-11-20"},
            "period":{"distanceKm":17.8,"drivingSeconds":2666,"hasData":true,"validObservedDays":1},
            "days":[],"trips":[],"hours":[
              {"from":"2026-11-20T00:00:00Z","to":"2026-11-20T01:00:00Z","distanceKm":null,"drivingSeconds":null,"observed":false},
              {"from":"2026-11-20T01:00:00Z","to":"2026-11-20T02:00:00Z","distanceKm":5.5,"drivingSeconds":900,"observed":true}]}"""
        val h = json.decodeFromString(JourneyHistory.serializer(), body)
        assertEquals("2026-11-20T01:00:00Z", h.hours[1].from)
        val buckets = rollingJourney(h.hours)
        assertNull(buckets[0].distance)
        assertEquals(5.5, buckets[1].distance!!, 1e-9)
        assertEquals(0.25, buckets[1].hours!!, 1e-9)
        assertEquals(5.5, buckets[1].cumulativeDistance!!, 1e-9)
    }

    @Test
    fun groupingFillsMissingDaysAndSumsWeeksAndMonths() {
        val days = calendarDays(
            "2026-11-14",
            "2026-11-20",
            listOf(
                JourneyDay("2026-11-14", 57.6, 5335.0, true),
                JourneyDay("2026-11-17", 33.0, 3901.0, true),
                JourneyDay("2026-11-20", 17.9, 2678.0, true),
            ),
        )
        assertEquals(7, days.size)
        val daily = groupJourney(days, JourneyGrouping.DAY)
        assertNull(daily[1].distance) // 11/15 not observed → shown as "×", not 0
        assertEquals(108.5, daily.last().cumulativeDistance!!, 1e-9)
        val weekly = groupJourney(days, JourneyGrouping.WEEK) // 11/14 Sat–11/15 Sun, then Mon 11/16–
        assertEquals(2, weekly.size)
        assertEquals(57.6, weekly[0].distance!!, 1e-9)
        assertEquals(50.9, weekly[1].distance!!, 1e-9)
        assertEquals(1, groupJourney(days, JourneyGrouping.MONTH).size)
        assertEquals(60.0, niceMaximum(listOf(57.6, null)), 1e-9)
    }

    @Test
    fun updateSettingsKeepsFieldsTheAppDoesNotKnow() = runTest {
        val base = server.url("").toString().trimEnd('/')
        val client = NccApiClient { AppConfig(connectionMode = ConnectionMode.CUSTOM, customUrl = base) }
        server.enqueue(MockResponse().setBody("""{"csrf":"tok","role":"admin","expires":"2026-11-21T00:00:00Z"}"""))
        server.enqueue(
            MockResponse().setBody(
                """{"revision":7,"theme":"dark","locale":"ja","favorites":["system"],"cellularLimitBytes":"0",
                   "mapThemes":{"dashboard":"night"},"futureField":{"keep":true}}""",
            ),
        )
        server.enqueue(MockResponse().setBody("""{"revision":8,"cellularLimitBytes":"30000000000"}"""))

        client.updateSettings(mapOf("cellularLimitBytes" to JsonPrimitive("30000000000")))

        server.takeRequest() // session
        server.takeRequest() // GET /settings
        val put = server.takeRequest()
        assertEquals("PUT", put.method)
        val sent = json.parseToJsonElement(put.body.readUtf8()).jsonObject
        assertEquals(7, sent["expectedRevision"]!!.jsonPrimitive.content.toInt())
        val settings = sent["settings"] as JsonObject
        assertEquals("30000000000", settings["cellularLimitBytes"]!!.jsonPrimitive.content)
        assertEquals("night", (settings["mapThemes"] as JsonObject)["dashboard"]!!.jsonPrimitive.content)
        assertTrue(settings.containsKey("futureField"))
        assertEquals("dark", settings["theme"]!!.jsonPrimitive.content)
    }

    @Test
    fun serverReasonIsShownWithoutStatusCodePrefix() = runTest {
        val base = server.url("").toString().trimEnd('/')
        val client = NccApiClient { AppConfig(connectionMode = ConnectionMode.CUSTOM, customUrl = base) }
        server.enqueue(MockResponse().setBody("""{"csrf":"tok","role":"admin","expires":"2026-11-21T00:00:00Z"}"""))
        server.enqueue(MockResponse().setResponseCode(409).setBody("""{"status":"error","code":"REVISION_CONFLICT","reason":"設定が変更されています"}"""))
        val e = runCatching { client.requestRaw("/network/cellular/apn", "PUT", "{}") }.exceptionOrNull()
        assertEquals("設定が変更されています", e?.message)
    }

    @Test
    fun trailSplitsOnSegmentChangeAndGaps() {
        val t0 = Instant.parse("2026-11-20T09:00:00Z")
        fun pt(s: Long, seg: Int) = TrackPoint(at = t0.plusSeconds(s).toString(), lat = 35.0 + s / 1e4, lon = 139.0, segment = seg)
        val track = TrackData(points = listOf(pt(0, 0), pt(1, 0), pt(2, 0), pt(30, 0), pt(31, 0), pt(32, 1)))
        val segs = splitTrail(track, t0.toEpochMilli() - 1, t0.plusSeconds(60).toEpochMilli())
        assertEquals(listOf(3, 2, 1), segs.map { it.size })
    }

    @Test
    fun radioHistoryUsesNestedGpsAndCells() {
        val body = """{"unit":"dBm","total":3,"points":[{"at":"2026-11-20T01:00:00Z","gps":{"at":"2026-11-20T01:00:00Z","lat":35.1,"lon":139.2},
            "value":-97,"state":"ok","color":"#b89d52","accuracyUnknown":false}],
            "cells":[{"bounds":[[35.0,139.0],[35.001,139.001]],"color":"#247976","median":-80,"sample_count":4,"low_confidence":false}]}"""
        val r = json.decodeFromString(RadioHistoryResponse.serializer(), body)
        assertEquals(35.1, r.points[0].gps.lat, 1e-9)
        assertEquals(4, r.cells[0].sampleCount)
    }

    @Test
    fun trafficUsageKeepsInterfaceName() {
        val obs = json.decodeFromString(
            Observation.serializer(),
            """{"status":"available","data":{"networks":{"cellular":{"interface":"usb0","status":"available","monthRX":"10","monthTX":"5"}}}}""",
        )
        val usage = json.decodeFromJsonElement(TrafficUsageData.serializer(), obs.data!!)
        assertEquals("usb0", usage.networks?.cellular?.interfaceName)
    }
}
