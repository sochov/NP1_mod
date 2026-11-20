package com.np1control.app.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject

@Serializable
data class SessionResponse(
    val csrf: String = "",
    val role: String = "admin",
    val expires: String = "",
)

@Serializable
data class ApiErrorBody(
    val status: String = "error",
    val code: String = "",
    val reason: String = "",
)

@Serializable
data class Observation(
    val status: String = "unknown",
    val observedAt: String = "",
    val source: String = "",
    val refreshSeconds: Int = 0,
    val verified: Boolean = false,
    val data: JsonElement? = null,
    val reason: String? = null,
)

@Serializable
data class WebSettings(
    val revision: Int = 1,
    val theme: String = "system",
    val locale: String = "ja",
    val favorites: List<String> = listOf("system", "network", "storage"),
    val cellularLimitBytes: String = "0",
    val mapThemes: Map<String, String> = emptyMap(),
)

@Serializable
data class FileEntry(
    val name: String = "",
    val path: String = "",
    val size: String = "0",
    val modified: String = "",
    val directory: Boolean = false,
    val media: String = "",
)

// ── Plate Recognition ────────────────────────────────────────────────────────

@Serializable
data class PlateReading(
    val region: String = "",
    @SerialName("class") val classNum: String = "",
    val kana: String = "",
    val number: String = "",
    val confidence: Double = 0.0,
)

@Serializable
data class PlateLocation(
    val at: String = "",
    val lat: Double = 0.0,
    val lon: Double = 0.0,
    val accuracyM: Double? = null,
    val estimated: Boolean? = null,
)

@Serializable
data class PlateMatch(
    val ruleId: String = "",
    val ruleName: String = "",
    val saveImage: Boolean = false,
    val speak: Boolean = false,
)

@Serializable
data class PlateRecord(
    val id: String = "",
    val firstSeen: String = "",
    val lastSeen: String = "",
    val sightings: Int = 1,
    val reading: PlateReading = PlateReading(),
    val display: String = "",
    val location: PlateLocation? = null,
    val box: List<Double> = emptyList(),
    val matches: List<PlateMatch> = emptyList(),
    val imageId: String? = null,
    val imageNote: String? = null,
    val speech: String? = null,
)

@Serializable
data class NumberShareResult(
    val status: String = "none",
    val category: String? = null,
    val comment: String? = null,
    val metadataPresent: Boolean? = null,
    val checkedAt: String = "",
)

@Serializable
data class PlateRecordEnvelope(
    val record: PlateRecord,
    val imageAvailable: Boolean = false,
    val numberShare: NumberShareResult? = null,
)

@Serializable
data class PlateRecordsResponse(
    val records: List<PlateRecordEnvelope> = emptyList(),
    val truncated: Boolean = false,
    val reason: String = "",
)

@Serializable
data class PlateLED(
    val mode: String = "off",
    val effect: String = "breathe",
    val rgb: List<Int> = listOf(64, 160, 255),
    val brightness: Int = 180,
)

@Serializable
data class PlateRule(
    val id: String = "",
    val name: String = "",
    val enabled: Boolean = true,
    val region: String = "",
    @SerialName("class") val classNum: String = "",
    val kana: String = "",
    val number: String = "",
    val saveImage: Boolean = true,
    val speak: Boolean = false,
    val message: String = "",
    val messageAudio: String = "",
    val led: PlateLED = PlateLED(),
)

@Serializable
data class PlateVoice(
    val style: Int = 1,
    val volume: Int = 70,
    val phrase: String = "番号を検知しました。",
)

@Serializable
data class PlateSettings(
    val revision: Int = 1,
    val enabled: Boolean = false,
    val rules: List<PlateRule> = emptyList(),
    val retentionDays: Int = 30,
    val imageQuotaMB: Int = 128,
    val mergeSeconds: Int = 30,
    val minConfidence: Double = 0.7,
    val minSightings: Int = 2,
    val instantConfidence: Double = 0.9,
    val voice: PlateVoice = PlateVoice(),
)

@Serializable
data class PlateSensor(
    val state: String = "unavailable",
    val reason: String? = null,
    val instance: String? = null,
    val engine: String? = null,
    val backend: String? = null,
    val fps: Double? = null,
    val lastFrameAt: String? = null,
)

@Serializable
data class PlateCollector(
    val lastPollAt: String? = null,
    val error: String = "",
)

@Serializable
data class PlateSpeechAvail(
    val available: Boolean = false,
    val reason: String = "",
)

@Serializable
data class PlateUsage(
    val recordBytes: Long = 0,
    val recordDays: Int = 0,
    val imageBytes: Long = 0,
    val imageCount: Int = 0,
    val openRecords: Int = 0,
)

@Serializable
data class PlateStatus(
    val enabled: Boolean = false,
    val revision: Int = 1,
    val sensor: PlateSensor = PlateSensor(),
    val collector: PlateCollector = PlateCollector(),
    val speech: PlateSpeechAvail = PlateSpeechAvail(),
    val usage: PlateUsage = PlateUsage(),
    val imageQuotaBytes: Long = 0,
)

@Serializable
data class SpeechStyle(
    val id: Int = 1,
    val speaker: String = "",
    val name: String = "",
)

@Serializable
data class PlateSpeechJob(
    val state: String = "idle",
    val total: Int = 0,
    val ready: Int = 0,
    val error: String? = null,
)

@Serializable
data class PlateSpeechRuleState(
    val ready: Boolean = false,
    val error: String? = null,
)

@Serializable
data class PlateSpeechStatus(
    val keyConfigured: Boolean = false,
    val player: String? = null,
    val job: PlateSpeechJob = PlateSpeechJob(),
    val rules: Map<String, PlateSpeechRuleState> = emptyMap(),
    val style: SpeechStyle? = null,
    val credit: String = "",
)

@Serializable
data class PlateSpeechResponse(
    val available: Boolean = false,
    val reason: String = "",
    val styles: List<SpeechStyle> = emptyList(),
    val status: PlateSpeechStatus = PlateSpeechStatus(),
)

@Serializable
data class VoicevoxConfig(
    val endpoint: String = "",
    val defaultEndpoint: String = "",
    val keyConfigured: Boolean = false,
    val apiKey: String = "",
)

@Serializable
data class VoicevoxCheckResponse(
    val points: Long = 0,
    val resetInHours: Double = 0.0,
)

// ── NumberShare ──────────────────────────────────────────────────────────────

@Serializable
data class NumberSharePlate(
    val serial_number: String = "",
    val region: String? = null,
    val classification_number: String? = null,
    val hiragana: String? = null,
)

@Serializable
data class NumberShareLocation(
    val latitude: Double = 0.0,
    val longitude: Double = 0.0,
    val accuracy_m: Double? = null,
    val observed_at: String? = null,
)

@Serializable
data class NumberSharePayload(
    val event_id: String = "",
    val observed_at: String = "",
    val plate: NumberSharePlate = NumberSharePlate(),
    val location: NumberShareLocation? = null,
)

@Serializable
data class NumberShareQueueItem(
    val eventId: String = "",
    val recordId: String = "",
    val payload: NumberSharePayload = NumberSharePayload(),
    val state: String = "pending",
    val attempts: Int = 0,
    val queuedAt: String = "",
    val lastAttemptAt: String? = null,
    val nextAttemptAt: String? = null,
    val sentAt: String? = null,
    val httpStatus: Int? = null,
    val lastError: String? = null,
    val matchStatus: String? = null,
)

@Serializable
data class NumberShareConfig(
    val revision: Int = 1,
    val enabled: Boolean = false,
    val endpoint: String = "",
    val keyConfigured: Boolean = false,
    val apiKey: String = "",
    val managementUrl: String? = null,
    val adminKeyConfigured: Boolean = false,
    val adminApiKey: String? = null,
    val userHandle: String? = null,
)

@Serializable
data class NumberShareStatus(
    val config: NumberShareConfig = NumberShareConfig(),
    val pending: Int = 0,
    val sent: Int = 0,
    val blocked: Int = 0,
    val items: List<NumberShareQueueItem> = emptyList(),
)

@Serializable
data class NumberShareSighting(
    val id: Long = 0,
    val event_id: String = "",
    val observed_at: String = "",
    val received_at: String = "",
    val plate_id: Long? = null,
    val match_status: String = "none",
    val recognition_revision: Int = 0,
    val plate: NumberSharePlate = NumberSharePlate(),
    val location: NumberShareLocation? = null,
    val mine: Boolean = false,
)

@Serializable
data class NumberShareSightingsResponse(
    val items: List<NumberShareSighting> = emptyList(),
    val truncated: Boolean = false,
    val syncedAt: String = "",
)

@Serializable
data class NumberShareVehicle(
    val id: Long = 0,
    val serial_number: String = "",
    val region: String = "",
    val classification_number: String = "",
    val hiragana: String = "",
    val category: String = "",
    val comment: String = "",
    val sightings: Int = 0,
    val last_seen: String? = null,
    val last_location: NumberShareLocation? = null,
)

@Serializable
data class NumberShareVehiclesResponse(
    val items: List<NumberShareVehicle> = emptyList(),
    val total: Int = 0,
    val categories: List<String> = emptyList(),
)

@Serializable
data class NumberShareUploadResponse(
    val total: Int = 0,
    val queued: Int = 0,
    val requeued: Int = 0,
    val existing: Int = 0,
    val skipped: Int = 0,
)

// ── Recordings & Trips ───────────────────────────────────────────────────────

@Serializable
data class RecordingClip(
    val id: String = "",
    val name: String = "",
    val camera: String = "front",
    val kind: String = "normal",
    val startAt: String = "",
    val endAt: String = "",
    val durationSeconds: Double = 0.0,
    val sizeBytes: String = "0",
    val videoCodec: String? = null,
    val videoBitrate: Long? = null,
    val audioCodec: String? = null,
    val width: Int? = null,
    val height: Int? = null,
)

@Serializable
data class RecordingDay(
    val date: String = "",
    val count: Int = 0,
    val frontCount: Int = 0,
    val cabinCount: Int = 0,
    val recordedSeconds: Double = 0.0,
)

@Serializable
data class RecordingCatalog(
    val clips: List<RecordingClip> = emptyList(),
    val days: List<RecordingDay> = emptyList(),
    val pendingFiles: Int = 0,
    val unreadableFiles: Int = 0,
    val truncated: Boolean = false,
    val timezone: String = "Asia/Tokyo",
    val indexedAt: String = "",
)

@Serializable
data class TrackPoint(
    val at: String = "",
    val lat: Double = 0.0,
    val lon: Double = 0.0,
    val segment: Int = 0,
    val speedMps: Double? = null,
    val heading: Double? = null,
)

@Serializable
data class TrackData(
    val date: String = "",
    val source: String = "",
    val indexedAt: String = "",
    val sourceTruncated: Boolean = false,
    val reason: String? = null,
    val points: List<TrackPoint> = emptyList(),
)

@Serializable
data class JourneyRange(
    val period: String = "7",
    val from: String = "",
    val to: String = "",
    val fromAt: String? = null,
    val toAt: String? = null,
    val sourceFrom: String? = null,
    val sourceTo: String? = null,
)

@Serializable
data class JourneyPeriod(
    val distanceKm: Double? = null,
    val drivingSeconds: Double? = null,
    val hasData: Boolean = false,
    val validObservedDays: Int = 0,
)

@Serializable
data class JourneyDay(
    val date: String = "",
    val distanceKm: Double? = null,
    val drivingSeconds: Double? = null,
    val observed: Boolean = false,
)

@Serializable
data class JourneyHour(
    val start: String = "",
    val end: String = "",
    val distanceKm: Double? = null,
    val drivingSeconds: Double? = null,
)

@Serializable
data class JourneyTrip(
    val id: String = "",
    val start: String = "",
    val end: String = "",
    val distanceKm: Double? = null,
    val drivingSeconds: Double? = null,
    val observedSeconds: Double? = null,
)

@Serializable
data class JourneyHistory(
    val range: JourneyRange = JourneyRange(),
    val period: JourneyPeriod = JourneyPeriod(),
    val days: List<JourneyDay> = emptyList(),
    val hours: List<JourneyHour> = emptyList(),
    val trips: List<JourneyTrip> = emptyList(),
    val source: String = "",
    val sourceFiles: Int = 0,
    val noFixSentences: Int = 0,
    val reason: String? = null,
    val detailsTruncated: Boolean = false,
    val sourceTruncated: Boolean = false,
)

// ── Network (Cellular / Wi-Fi / AP / Tailscale / Uplink) ─────────────────────

@Serializable
data class NetworkUsage(
    val interfaceName: String? = null,
    val todayRX: String? = null,
    val todayTX: String? = null,
    val monthRX: String? = null,
    val monthTX: String? = null,
    val rxBytesPerSecond: Double? = null,
    val txBytesPerSecond: Double? = null,
    val sampledAt: String? = null,
    val status: String = "unknown",
    val speedStatus: String = "unknown",
    val active: Boolean = false,
    val stale: Boolean = false,
)

@Serializable
data class UsageNetworks(
    val cellular: NetworkUsage? = null,
    val wifi: NetworkUsage? = null,
)

@Serializable
data class TrafficUsageData(
    val networks: UsageNetworks? = null,
    val sampledAt: String? = null,
    val stale: Boolean = false,
)

@Serializable
data class APNProfile(
    val id: String = "profile1",
    val enabled: Boolean = false,
    val apn: String = "",
    val auth: Int = 0,
    val usernameConfigured: Boolean = false,
    val passwordConfigured: Boolean = false,
)

@Serializable
data class APNSettings(
    val revision: Int = 0,
    val profiles: List<APNProfile> = emptyList(),
    val savedAt: String? = null,
    val appliedRevision: Int? = null,
    val appliedAt: String? = null,
    val applyAvailable: Boolean? = null,
    val applyReason: String? = null,
    val applyInProgress: Boolean? = null,
)

@Serializable
data class UplinkStateDetail(
    val switches: Int = 0,
    val lastAction: String? = null,
    val lastReason: String? = null,
)

@Serializable
data class UplinkStatus(
    val at: String = "",
    val mode: String = "auto",
    val staOn: Boolean? = null,
    val wpaState: String? = null,
    val uplink: String = "none",
    val helperAvailable: Boolean = false,
    val error: String? = null,
    val state: UplinkStateDetail? = null,
)

@Serializable
data class UplinkView(
    val mode: String = "auto",
    val status: UplinkStatus? = null,
)

@Serializable
data class WiFiStatus(
    val state: String = "",
    val ssid: String = "",
    val networkID: Int? = null,
    val ipv4: String = "",
)

@Serializable
data class WiFiNetworkItem(
    val id: Int = 0,
    val ssid: String = "",
    val current: Boolean = false,
    val disabled: Boolean = false,
    val managed: Boolean = false,
)

@Serializable
data class WiFiSavedProfile(
    val id: String = "",
    val ssid: String = "",
    val security: String = "wpa2",
    val passwordConfigured: Boolean = false,
    val password: String = "",
)

@Serializable
data class WiFiTransaction(
    val id: String = "",
    val state: String = "",
    val expiresAt: String = "",
    val targetSSID: String = "",
    val canConfirm: Boolean = false,
    val reason: String? = null,
)

@Serializable
data class WiFiScanResult(
    val ssid: String = "",
    val bssid: String = "",
    val frequency: Int = 0,
    val signalDBm: Int = 0,
    val security: String = "wpa2",
)

@Serializable
data class WiFiScanView(
    val scanRequested: Boolean = false,
    val scannedAt: String = "",
    val results: List<WiFiScanResult> = emptyList(),
)

@Serializable
data class WiFiView(
    val status: WiFiStatus = WiFiStatus(),
    val networks: List<WiFiNetworkItem> = emptyList(),
    val saved: List<WiFiSavedProfile> = emptyList(),
    val transaction: WiFiTransaction? = null,
    val lastScan: WiFiScanView? = null,
)

@Serializable
data class APUpstream(
    val kind: String = "",
    @SerialName("interface") val iface: String = "",
)

@Serializable
data class APSharing(
    val preference: String? = null,
    val enabled: Boolean = false,
    val state: String = "unknown",
    val ready: Boolean = false,
    val dnsReady: Boolean = false,
    val upstream: APUpstream? = null,
    val errorCode: String? = null,
)

@Serializable
data class APSettingsData(
    val revision: Int = 0,
    val enabled: Boolean = false,
    val ssid: String = "",
    val passphraseConfigured: Boolean = false,
    val passphrase: String = "",
    val preference: String? = null,
)

@Serializable
data class APClient(
    val mac: String = "",
    val ipv4: String? = null,
    val hostname: String? = null,
    val authorized: Boolean = false,
    val signalDBm: Int? = null,
    val connectedSeconds: Int? = null,
    val receivedBytes: String? = null,
    val sentBytes: String? = null,
)

@Serializable
data class APData(
    val sharing: APSharing? = null,
    val settings: APSettingsData? = null,
    val enabled: Boolean? = null,
    val dhcpReady: Boolean? = null,
    val address: String = "",
    val channel: Int? = null,
    val clientCount: Int? = null,
    val clients: List<APClient>? = null,
)

// ── Camera ───────────────────────────────────────────────────────────────────

@Serializable
data class CameraSessionResponse(
    val id: String = "",
    val idleTimeout: Int = 30,
    val fps: Int = 1,
    val width: Int = 640,
    val height: Int = 360,
)

// ── System / Stock Services / LED / NAS / OTA / Native ──────────────────────

@Serializable
data class StockService(
    val id: String = "",
    val name: String = "",
    val label: String = "",
    val description: String = "",
    val impact: String = "",
    val state: String = "",
    val subState: String = "",
    val mainPID: Int = 0,
    val invocationID: String = "",
    val loadState: String = "",
    val unitFileState: String = "",
    val canStart: Boolean = false,
    val canStop: Boolean = false,
    val canRestart: Boolean = false,
    val controlReason: String? = null,
)

@Serializable
data class LEDSettingsData(
    val revision: Int = 1,
    val mode: String = "off",
    val brightness: Int = 180,
    val speed: Int = 240,
    val fps: Int = 15,
    val rgb: List<Int> = listOf(255, 255, 255),
    val autostart: Boolean = false,
)

@Serializable
data class LEDLastCommand(
    val mode: String = "",
)

@Serializable
data class LEDObservationData(
    val settings: LEDSettingsData? = null,
    val serviceState: String? = null,
    val lastCommand: LEDLastCommand? = null,
)

@Serializable
data class NativeSettingOption(
    val value: JsonElement,
    val label: String = "",
)

@Serializable
data class NativeSettingItem(
    val id: String = "",
    val label: String = "",
    val description: String? = null,
    val kind: String = "info",
    val value: JsonElement? = null,
    val valueLabel: String? = null,
    val options: List<NativeSettingOption>? = null,
    val min: Int? = null,
    val max: Int? = null,
    val step: Int? = null,
    val writable: Boolean = false,
    val reason: String? = null,
    val status: String? = null,
)

@Serializable
data class NativeSettingsData(
    val items: List<NativeSettingItem> = emptyList(),
    val revision: Int? = null,
    val scope: String? = null,
)

@Serializable
data class AlexaWakeWord(
    val value: Int = 0,
    val enabled: Boolean = false,
    val fixed: Boolean = false,
)

@Serializable
data class AlexaData(
    val wakeWord: AlexaWakeWord = AlexaWakeWord(),
    val writable: Boolean = false,
    val reason: String? = null,
)

@Serializable
data class NativeOpOption(
    val value: Int = 0,
    val label: String = "",
)

@Serializable
data class NativeOpItem(
    val id: String = "",
    val label: String = "",
    val field: String = "",
    val writable: Boolean = false,
    val options: List<NativeOpOption>? = null,
)

@Serializable
data class NativeOpGroup(
    val id: String = "",
    val label: String = "",
    val items: List<NativeOpItem> = emptyList(),
)

@Serializable
data class NativeActionMeta(
    val id: String = "",
    val label: String = "",
    val description: String = "",
    val category: String = "",
    val method: String = "POST",
    val confirmation: String? = null,
    val available: Boolean = false,
    val reason: String? = null,
)

@Serializable
data class NativeOperationsCatalog(
    val groups: List<NativeOpGroup> = emptyList(),
    val actions: List<NativeActionMeta> = emptyList(),
    val available: Boolean = false,
)

@Serializable
data class NativeOpState(
    val values: Map<String, Long> = emptyMap(),
    val observedAt: String = "",
    val readback: Boolean? = null,
)

@Serializable
data class RecorderAdvancedConfig(
    val frontBitrateKbps: Int = 8000,
    val cabinBitrateKbps: Int = 4000,
    val frontRateControl: String = "vbr",
    val cabinRateControl: String = "vbr",
    val frontGopMilliseconds: Int = 1000,
    val cabinGopMilliseconds: Int = 1000,
    val cabinRecording: Boolean = true,
    val segmentDurationSeconds: Int = 180,
)

@Serializable
data class RecorderAdvancedState(
    val available: Boolean = false,
    val configured: Boolean = false,
    val hookLoaded: Boolean = false,
    val restartRequired: Boolean = false,
    val generation: Int = 0,
    val settings: RecorderAdvancedConfig = RecorderAdvancedConfig(),
    val applied: Boolean? = null,
    val message: String? = null,
)

@Serializable
data class NativeAudioMute(
    val value: Int = 1,
    val enabled: Boolean = false,
)

@Serializable
data class NativeAudioCorrection(
    val value: Int = 0,
    val label: String = "",
)

@Serializable
data class NativeAudioData(
    val masterVolume: Int = 0,
    val mute: NativeAudioMute = NativeAudioMute(),
    val correction: NativeAudioCorrection = NativeAudioCorrection(),
    val writable: Boolean = false,
    val reason: String? = null,
)

@Serializable
data class OtaGuardFlag(
    val present: Boolean = false,
    val managed: Boolean = false,
    val protected: Boolean = false,
    val heals: Int = 0,
    val lastHeal: String? = null,
)

@Serializable
data class OtaGuardNetwork(
    val wanted: Boolean = false,
    val active: Boolean = false,
    val rejectedPackets: Long = 0,
)

@Serializable
data class OtaGuardStaging(
    val workFiles: Int = 0,
    val offeredItems: Int = 0,
    val listReadable: Boolean = false,
    val sdTrigger: Boolean = false,
    val firmwareKnown: Boolean = true,
)

@Serializable
data class OtaGuard(
    val checkedAt: String = "",
    val flag: OtaGuardFlag = OtaGuardFlag(),
    val network: OtaGuardNetwork = OtaGuardNetwork(),
    val staging: OtaGuardStaging = OtaGuardStaging(),
    val alerts: List<String> = emptyList(),
)

@Serializable
data class OtaPolicyData(
    val disabled: Boolean = true,
    val managed: Boolean = false,
    val writable: Boolean = false,
    val token: String = "",
    val scope: String = "",
    val protected: Boolean? = null,
    val heals: Int? = null,
    val lastHeal: String? = null,
    val guard: OtaGuard? = null,
)

@Serializable
data class NasTarget(
    val protocol: String = "smb",
    val host: String = "",
    val port: Int = 0,
    val share: String = "",
    val directory: String = "",
    val scheme: String? = "https",
    val hostKeySHA256: String? = "",
    val configured: Boolean = false,
    val usernameConfigured: Boolean = false,
    val passwordConfigured: Boolean = false,
)

@Serializable
data class NasPolicy(
    val enabled: Boolean = false,
    val route: String = "wifi",
    val timing: String = "realtime",
    val ssid: String = "",
    val targets: Map<String, NasTarget> = emptyMap(),
)

@Serializable
data class NasSettings(
    val revision: Int = 0,
    val policies: Map<String, NasPolicy> = emptyMap(),
)

@Serializable
data class NasJobProgress(
    val stage: String = "",
    val bytes: String = "0",
    val totalBytes: String = "0",
    val resumedBytes: String = "0",
)

@Serializable
data class NasJob(
    val id: String = "",
    val recordingID: String = "",
    val kind: String = "video",
    val route: String = "wifi",
    val originalName: String = "",
    val status: String = "",
    val active: Boolean = false,
    val canCancel: Boolean = false,
    val reason: String? = null,
    val progress: NasJobProgress = NasJobProgress(),
)

@Serializable
data class NasAutomaticState(
    val state: String = "",
    val reason: String? = null,
    val paused: Boolean = false,
)

@Serializable
data class NasStateData(
    val settings: NasSettings = NasSettings(),
    val automatic: Map<String, NasAutomaticState> = emptyMap(),
    val job: NasJob? = null,
)

@Serializable
data class RadioPoint(
    val at: String = "",
    val lat: Double = 0.0,
    val lon: Double = 0.0,
    val rsrp: Double? = null,
    val sinr: Double? = null,
    val rsrq: Double? = null,
)

@Serializable
data class RadioHistoryResponse(
    val points: List<RadioPoint> = emptyList(),
)

typealias SystemSummary = Map<String, Observation>
