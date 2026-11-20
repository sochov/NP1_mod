package com.np1control.app.data

import java.math.BigInteger
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import kotlin.math.abs
import kotlin.math.floor
import kotlin.math.roundToInt

object Formatters {
    val JST: ZoneId = ZoneId.of("Asia/Tokyo")
    private val dateFormatter = DateTimeFormatter.ofPattern("yyyy-MM-dd").withZone(JST)
    private val shortDateTimeFormatter = DateTimeFormatter.ofPattern("MM/dd HH:mm:ss").withZone(JST)
    private val timeOnlyFormatter = DateTimeFormatter.ofPattern("HH:mm:ss").withZone(JST)
    private val shortTimeFormatter = DateTimeFormatter.ofPattern("HH:mm").withZone(JST)
    private val jpDayFormatter = DateTimeFormatter.ofPattern("yyyy年M月d日(E)", Locale.JAPANESE).withZone(JST)

    fun jstDay(offsetDays: Long = 0): String =
        LocalDate.now(JST).plusDays(offsetDays).format(DateTimeFormatter.ISO_LOCAL_DATE)

    fun formatDateTime(iso: String?): String {
        if (iso.isNullOrBlank()) return "未取得"
        return runCatching { shortDateTimeFormatter.format(Instant.parse(iso)) }.getOrDefault(iso)
    }

    fun formatTime(iso: String?): String {
        if (iso.isNullOrBlank()) return "—"
        return runCatching { timeOnlyFormatter.format(Instant.parse(iso)) }.getOrDefault(iso)
    }

    fun formatShortTime(epochMs: Long): String =
        shortTimeFormatter.format(Instant.ofEpochMilli(epochMs))

    fun formatClock(epochMs: Long): String =
        timeOnlyFormatter.format(Instant.ofEpochMilli(epochMs))

    fun formatJpDate(ymd: String): String =
        runCatching {
            val d = LocalDate.parse(ymd)
            jpDayFormatter.format(d.atStartOfDay(JST))
        }.getOrDefault(ymd)

    fun isoDateJst(iso: String): String =
        runCatching { dateFormatter.format(Instant.parse(iso)) }.getOrDefault(iso.take(10))

    fun bytes(value: String?): String {
        val n = value?.toDoubleOrNull() ?: return "—"
        return bytes(n)
    }

    fun bytes(value: Long): String = bytes(value.toDouble())

    fun bytes(value: Double): String {
        if (!value.isFinite() || value < 0) return "—"
        if (value < 1024.0) return "${value.toLong()} B"
        val units = arrayOf("KiB", "MiB", "GiB", "TiB")
        var x = value / 1024.0
        var idx = 0
        while (x >= 1024.0 && idx < units.lastIndex) {
            x /= 1024.0
            idx++
        }
        val formatted = if (x >= 100.0) String.format(Locale.US, "%.0f", x) else String.format(Locale.US, "%.1f", x)
        return "$formatted ${units[idx]}"
    }

    fun trafficBigInt(value: String?): BigInteger? {
        if (value.isNullOrBlank() || !value.all { it.isDigit() }) return null
        return runCatching { BigInteger(value) }.getOrNull()
    }

    fun trafficVolume(value: BigInteger?): String {
        if (value == null) return "—"
        val units = listOf(
            "TB" to BigInteger.valueOf(1_000_000_000_000L),
            "GB" to BigInteger.valueOf(1_000_000_000L),
            "MB" to BigInteger.valueOf(1_000_000L),
            "KB" to BigInteger.valueOf(1_000L),
        )
        for ((unit, size) in units) {
            if (value >= size) {
                val tenths = (value.multiply(BigInteger.TEN).add(size.divide(BigInteger.TWO))).divide(size)
                val v = tenths.toDouble() / 10.0
                return String.format(Locale.JAPANESE, "%.1f %s", v, unit)
            }
        }
        return "$value B"
    }

    fun trafficSpeed(bytesPerSec: Double?, stale: Boolean = false): String {
        if (stale || bytesPerSec == null || !bytesPerSec.isFinite() || bytesPerSec < 0) return "—"
        val bits = bytesPerSec * 8.0
        val units = listOf("Gbps" to 1e9, "Mbps" to 1e6, "kbps" to 1e3)
        for ((unit, scale) in units) {
            if (bits >= scale) {
                val v = bits / scale
                val rounded = (v * 10.0).roundToInt() / 10.0
                return if (rounded < 10.0 || rounded % 1.0 != 0.0) {
                    String.format(Locale.JAPANESE, "%.1f %s", rounded, unit)
                } else {
                    String.format(Locale.JAPANESE, "%.0f %s", rounded, unit)
                }
            }
        }
        return "${bits.roundToInt()} bps"
    }

    fun uptime(seconds: Double?): String {
        if (seconds == null || !seconds.isFinite() || seconds < 0) return "—"
        val total = seconds.toLong()
        val days = total / 86400
        val hours = (total % 86400) / 3600
        val minutes = (total % 3600) / 60
        return buildString {
            if (days > 0) append("${days}日 ")
            if (days > 0 || hours > 0) append("${hours}時間 ")
            append("${minutes}分")
        }
    }

    fun duration(seconds: Double?): String {
        if (seconds == null || !seconds.isFinite() || seconds < 0) return "—"
        if (seconds < 60) return "${seconds.toLong()}秒"
        val totalMinutes = floor(seconds / 60.0).toLong()
        val hours = totalMinutes / 60
        val mins = totalMinutes % 60
        return if (hours > 0) "${hours}時間 ${mins}分" else "${mins}分"
    }

    fun durationWithSeconds(seconds: Double): String {
        val total = seconds.toLong().coerceAtLeast(0)
        val hours = total / 3600
        val mins = (total % 3600) / 60
        val secs = total % 60
        return when {
            hours > 0 -> "${hours}時間${mins}分"
            mins > 0 -> "${mins}分${secs}秒"
            else -> "${secs}秒"
        }
    }

    /**
     * Mirrors plateSerial() in frontend/src/services/plate.ts:
     * Four positions; unused leading positions shown as "・", hyphen only when all 4 digits exist.
     */
    fun plateSerial(number: String?): String {
        val digits = (number ?: "").filter { it.isDigit() }.takeLast(4)
        if (digits.isEmpty()) return ""
        val padded = digits.padStart(4, '・')
        return if (digits.length == 4) {
            "${padded.substring(0, 2)}-${padded.substring(2)}"
        } else {
            "${padded.substring(0, 2)} ${padded.substring(2)}"
        }
    }

    fun plateDisplay(record: PlateRecord): String {
        if (record.display.isNotBlank()) return record.display
        return listOf(
            record.reading.region,
            record.reading.classNum,
            record.reading.kana,
            record.reading.number,
        ).filter { it.isNotBlank() }.joinToString(" ")
    }

    fun plateLocationLabel(record: PlateRecord): String = when {
        record.location == null -> "位置なし"
        record.location.estimated == true -> "位置推定"
        else -> "位置あり"
    }

    fun sensorLabel(state: String): String = when (state) {
        "running" -> "認識中"
        "starting" -> "起動中"
        "idle" -> "待機中（記録停止）"
        "model-missing" -> "認識モデル未導入"
        "engine-missing" -> "認識エンジン未導入"
        "error" -> "認識エラー"
        "unavailable" -> "認識プロセス未起動"
        else -> "状態不明"
    }

    fun speechLabel(state: String?): String = when (state) {
        "spoken" -> "読み上げ済み"
        "requested" -> "読み上げ中"
        "failed" -> "読み上げ失敗"
        "unsupported" -> "読み上げ未設定"
        else -> state ?: ""
    }

    fun carrierName(carrier: String?, plmn: String?): String {
        if (!carrier.isNullOrBlank()) return carrier
        return when (plmn) {
            "44010" -> "NTT docomo"
            "44020", "44021" -> "SoftBank"
            "44050", "44051", "44052", "44053", "44054" -> "KDDI"
            "44011" -> "Rakuten Mobile"
            "44003" -> "IIJ"
            null, "" -> "回線情報を確認中"
            else -> "PLMN $plmn"
        }
    }

    /** 「12秒前」「3分前」「2時間前」 */
    fun ago(epochMs: Long?, now: Long = System.currentTimeMillis()): String {
        if (epochMs == null) return "—"
        val sec = ((now - epochMs) / 1000).coerceAtLeast(0)
        return when {
            sec < 60 -> "${sec}秒前"
            sec < 3600 -> "${sec / 60}分前"
            sec < 86400 -> "${sec / 3600}時間前"
            else -> "${sec / 86400}日前"
        }
    }

    /** Short JST date like 「11/20」. */
    fun shortDate(ymd: String): String = runCatching {
        val d = LocalDate.parse(ymd)
        "${d.monthValue}/${d.dayOfMonth}"
    }.getOrDefault(ymd)

    fun formatHourMinute(iso: String?): String {
        if (iso.isNullOrBlank()) return "—"
        return runCatching { shortTimeFormatter.format(Instant.parse(iso)) }.getOrDefault("—")
    }

    fun formatDateTimeShort(iso: String?): String {
        if (iso.isNullOrBlank()) return "—"
        return runCatching { DateTimeFormatter.ofPattern("M/d HH:mm").withZone(JST).format(Instant.parse(iso)) }.getOrDefault("—")
    }

    /** Signal bars (1–5) and label from RSRP, the same thresholds as the Web UI. */
    fun signalLevel(rsrp: Double?): Int? = when {
        rsrp == null -> null
        rsrp >= -80 -> 5
        rsrp >= -90 -> 4
        rsrp >= -100 -> 3
        rsrp >= -110 -> 2
        else -> 1
    }

    fun signalLabel(rsrp: Double?): String = when {
        rsrp == null -> "未取得"
        rsrp >= -90 -> "良好"
        rsrp >= -105 -> "標準"
        else -> "弱い"
    }

    fun bandLabel(band: String?): String = when {
        band.isNullOrBlank() -> "—"
        band.matches(Regex("^B?[0-9]+$")) ->"LTE Band ${band.removePrefix("B")}"
        else -> band
    }

    fun decimalGB(bytes: BigInteger, digits: Int = 1): String {
        val v = bytes.toBigDecimal().divide(java.math.BigDecimal("1000000000"))
        return String.format(Locale.JAPANESE, "%,.${digits}f", v)
    }

    fun distanceText(km: Double?): String {
        if (km == null || !km.isFinite()) return "—"
        return if (abs(km) >= 100.0) {
            String.format(Locale.JAPANESE, "%.0f", km)
        } else {
            String.format(Locale.JAPANESE, "%.1f", km)
        }
    }
}
