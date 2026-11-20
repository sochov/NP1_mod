package com.np1control.app.data

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.longOrNull
import java.time.Instant

// Small, null-safe readers for the loosely typed `/system/summary` observations.

fun JsonObject?.str(key: String): String? = (this?.get(key) as? JsonPrimitive)?.takeIf { it.isString || it.contentOrNull != null }?.contentOrNull
fun JsonObject?.dbl(key: String): Double? = (this?.get(key) as? JsonPrimitive)?.doubleOrNull?.takeIf { it.isFinite() }
fun JsonObject?.int(key: String): Int? = (this?.get(key) as? JsonPrimitive)?.intOrNull
fun JsonObject?.long(key: String): Long? = (this?.get(key) as? JsonPrimitive)?.longOrNull
fun JsonObject?.bool(key: String): Boolean? = (this?.get(key) as? JsonPrimitive)?.booleanOrNull
fun JsonObject?.obj(key: String): JsonObject? = this?.get(key) as? JsonObject
fun JsonObject?.arr(key: String): JsonArray? = this?.get(key) as? JsonArray
fun JsonObject?.strings(key: String): List<String> =
    this.arr(key)?.mapNotNull { (it as? JsonPrimitive)?.contentOrNull }.orEmpty()

fun Observation?.isUsable(allowStale: Boolean = true): Boolean =
    this != null && (status == "available" || (allowStale && status == "stale"))

/** Observation data as an object when the observation is usable. */
fun SystemSummary.dataObj(key: String, allowStale: Boolean = true): JsonObject? =
    this[key]?.takeIf { it.isUsable(allowStale) }?.data as? JsonObject

fun SystemSummary.dataElement(key: String, allowStale: Boolean = true): JsonElement? =
    this[key]?.takeIf { it.isUsable(allowStale) }?.data

fun SystemSummary.dataDouble(key: String): Double? = (dataElement(key) as? JsonPrimitive)?.doubleOrNull
fun SystemSummary.dataString(key: String): String? = (dataElement(key) as? JsonPrimitive)?.contentOrNull

fun parseInstantMs(iso: String?): Long? = iso?.takeIf { it.isNotBlank() }?.let { runCatching { Instant.parse(it).toEpochMilli() }.getOrNull() }

/** IPv4 addresses on the given interface (and its VLAN children) from summary `interfaces`. */
fun SystemSummary.interfaceIPv4(base: String?): List<String> {
    if (base.isNullOrBlank()) return emptyList()
    val list = dataElement("interfaces") as? JsonArray ?: return emptyList()
    return list.mapNotNull { it as? JsonObject }
        .filter { (it.str("ifname") == base || it.str("ifname")?.startsWith("$base.") == true) && it.strings("flags").contains("UP") }
        .flatMap { iface -> iface.arr("addr_info")?.mapNotNull { it as? JsonObject }.orEmpty() }
        .filter { it.str("family") == "inet" && it.str("scope") == "global" }
        .mapNotNull { it.str("local") }
        .distinct()
}
