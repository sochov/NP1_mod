package com.np1control.app

import com.np1control.app.data.Formatters
import org.junit.Assert.assertEquals
import org.junit.Test
import java.math.BigInteger

class FormattersTest {

    @Test
    fun plateSerialFormatsJapaneseStyle() {
        assertEquals("12-34", Formatters.plateSerial("1234"))
        assertEquals("・8 10", Formatters.plateSerial("810"))
        assertEquals("・・ 12", Formatters.plateSerial("12"))
        assertEquals("・・ ・5", Formatters.plateSerial("5"))
        assertEquals("", Formatters.plateSerial(""))
        assertEquals("56-78", Formatters.plateSerial("56-78"))
    }

    @Test
    fun bytesAndTrafficVolumeFormatCorrectly() {
        assertEquals("512 B", Formatters.bytes(512L))
        assertEquals("1.5 KiB", Formatters.bytes(1536L))
        assertEquals("320.0 MB", Formatters.trafficVolume(BigInteger.valueOf(320_000_000L)))
        assertEquals("22.6 GB", Formatters.trafficVolume(BigInteger.valueOf(22_600_000_000L)))
    }

    @Test
    fun trafficSpeedFormatsBitsPerSecond() {
        assertEquals("—", Formatters.trafficSpeed(null))
        assertEquals("—", Formatters.trafficSpeed(1000.0, stale = true))
        assertEquals("8.0 kbps", Formatters.trafficSpeed(1000.0))
        assertEquals("11.2 Mbps", Formatters.trafficSpeed(1_400_000.0))
    }

    @Test
    fun uptimeAndDurationFormatInJapanese() {
        assertEquals("1日 1時間 1分", Formatters.uptime(90060.0))
        assertEquals("4時間 56分", Formatters.duration(4 * 3600.0 + 56 * 60.0))
    }
}
