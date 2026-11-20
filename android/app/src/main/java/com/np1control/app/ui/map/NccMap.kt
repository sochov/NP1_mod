package com.np1control.app.ui.map

import android.graphics.PointF
import android.view.Gravity
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.ui.Alignment
import androidx.compose.ui.draw.clip
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.unit.sp
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import com.google.gson.JsonObject
import com.np1control.app.ui.theme.LocalNccPalette
import org.maplibre.android.MapLibre
import org.maplibre.android.camera.CameraPosition
import org.maplibre.android.camera.CameraUpdateFactory
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.geometry.LatLngBounds
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.MapLibreMapOptions
import org.maplibre.android.maps.MapView
import org.maplibre.android.maps.Style
import org.maplibre.android.style.expressions.Expression
import org.maplibre.android.style.layers.CircleLayer
import org.maplibre.android.style.layers.FillLayer
import org.maplibre.android.style.layers.LineLayer
import org.maplibre.android.style.layers.Property
import org.maplibre.android.style.layers.PropertyFactory
import org.maplibre.android.style.sources.GeoJsonSource
import org.maplibre.geojson.Feature
import org.maplibre.geojson.FeatureCollection
import org.maplibre.geojson.LineString
import org.maplibre.geojson.Point
import org.maplibre.geojson.Polygon
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.max
import kotlin.math.sin

data class GeoPoint(val lat: Double, val lon: Double)

/** A coloured dot on the map (plate sightings, radio samples, playback position…). */
data class MapPoint(
    val id: String,
    val lat: Double,
    val lon: Double,
    val color: Color,
    val radius: Float = 5f,
    val stroke: Color = Color.White,
    val strokeWidth: Float = 1.5f,
    val opacity: Float = 0.95f,
    val sortKey: Float = 0f,
)

/** A filled rectangle/polygon such as a radio heat-map cell. Ring is (lat, lon) pairs. */
data class MapPolygon(
    val id: String,
    val ring: List<GeoPoint>,
    val color: Color,
    val opacity: Float = 0.5f,
)

/** The vehicle: live (blue) or last known (grey), with an optional accuracy circle. */
data class MapLocation(
    val lat: Double,
    val lon: Double,
    val live: Boolean,
    val accuracyM: Double? = null,
)

enum class MapFocus {
    /** Keep the vehicle centred at a street-level zoom. */
    LOCATION,

    /** Fit routes, points and the vehicle in view. */
    FIT,

    /** Leave the camera where the user put it. */
    NONE,
}

/**
 * Resolves the basemap style the same way the Web UI does: each view (dashboard, journey,
 * playback, plates) can have its own theme in /settings `mapThemes`; `auto` follows the UI theme.
 */
fun mapStyleAsset(view: String, mapThemes: Map<String, String>, dark: Boolean): String {
    val chosen = mapThemes[view]?.takeIf { it in setOf("mist", "night", "sage", "colorful") }
    val theme = chosen ?: if (dark) "night" else "mist"
    return "asset://map/$theme.json"
}

private const val ROUTES = "ncc-routes"
private const val POLYS = "ncc-polys"
private const val POINTS = "ncc-points"
private const val ACCURACY = "ncc-accuracy"
private const val LOCATION = "ncc-location"

/**
 * MapLibre vector map with NP1 overlays. Rendering happens in a TextureView so the map clips to
 * rounded cards and sits correctly under floating panels.
 */
@Composable
fun NccMap(
    modifier: Modifier = Modifier,
    view: String = "dashboard",
    mapThemes: Map<String, String> = emptyMap(),
    routes: List<List<GeoPoint>> = emptyList(),
    routeColor: Color = Color(0xFF2F5BD8),
    routeWidth: Float = 4f,
    points: List<MapPoint> = emptyList(),
    polygons: List<MapPolygon> = emptyList(),
    location: MapLocation? = null,
    focus: MapFocus = MapFocus.FIT,
    focusKey: Any? = null,
    focusZoom: Double = 15.0,
    cameraPadding: PaddingValues = PaddingValues(0.dp),
    interactive: Boolean = true,
    onPointClick: ((String) -> Unit)? = null,
    onUserMove: (() -> Unit)? = null,
    attribution: Boolean = true,
    /** Inside a scrolling page: the map ignores drags until tapped, so the page keeps scrolling. */
    scrollFriendly: Boolean = false,
) {
    val context = LocalContext.current
    val density = LocalDensity.current
    val dark = LocalNccPalette.current.isDark
    val styleUri = mapStyleAsset(view, mapThemes, dark)
    val lifecycle = LocalLifecycleOwner.current.lifecycle

    var map by remember { mutableStateOf<MapLibreMap?>(null) }
    var style by remember { mutableStateOf<Style?>(null) }
    var loadedStyle by remember { mutableStateOf("") }
    val latestPointClick by rememberUpdatedState(onPointClick)
    val latestUserMove by rememberUpdatedState(onUserMove)

    val mapView = remember {
        MapLibre.getInstance(context)
        val options = MapLibreMapOptions.createFromAttributes(context)
            .textureMode(true)
            .logoEnabled(false)
            .attributionEnabled(attribution)
            .attributionGravity(Gravity.BOTTOM or Gravity.END)
            .localIdeographFontFamily("sans-serif")
        MapView(context, options).also { view -> view.onCreate(null) }
    }

    DisposableEffect(lifecycle, mapView) {
        val observer = LifecycleEventObserver { _, event ->
            when (event) {
                Lifecycle.Event.ON_START -> mapView.onStart()
                Lifecycle.Event.ON_RESUME -> mapView.onResume()
                Lifecycle.Event.ON_PAUSE -> mapView.onPause()
                Lifecycle.Event.ON_STOP -> mapView.onStop()
                else -> Unit
            }
        }
        lifecycle.addObserver(observer)
        if (lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) mapView.onStart()
        if (lifecycle.currentState.isAtLeast(Lifecycle.State.RESUMED)) mapView.onResume()
        onDispose {
            lifecycle.removeObserver(observer)
            mapView.onPause()
            mapView.onStop()
            mapView.onDestroy()
        }
    }

    LaunchedEffect(mapView) {
        mapView.getMapAsync { m ->
            m.uiSettings.isRotateGesturesEnabled = false
            m.uiSettings.isTiltGesturesEnabled = false
            m.uiSettings.isCompassEnabled = false
            m.uiSettings.setAttributionMargins(0, 0, (8 * density.density).toInt(), (6 * density.density).toInt())
            m.addOnCameraMoveStartedListener { reason ->
                if (reason == MapLibreMap.OnCameraMoveStartedListener.REASON_API_GESTURE) latestUserMove?.invoke()
            }
            m.addOnMapClickListener { latLng ->
                val handler = latestPointClick ?: return@addOnMapClickListener false
                val screen: PointF = m.projection.toScreenLocation(latLng)
                val r = 14 * density.density
                val hits = m.queryRenderedFeatures(
                    android.graphics.RectF(screen.x - r, screen.y - r, screen.x + r, screen.y + r),
                    "$POINTS-circle",
                )
                val id = hits.firstOrNull()?.getStringProperty("id") ?: return@addOnMapClickListener false
                handler(id)
                true
            }
            map = m
        }
    }

    // Style (and its overlay sources/layers) — reloaded when the theme changes.
    LaunchedEffect(map, styleUri) {
        val m = map ?: return@LaunchedEffect
        if (loadedStyle == styleUri && style != null) return@LaunchedEffect
        style = null
        val firstLoad = loadedStyle.isEmpty()
        m.setStyle(Style.Builder().fromUri(styleUri)) { s ->
            // Until there is something to show, look at Japan rather than the whole world.
            if (firstLoad) m.moveCamera(CameraUpdateFactory.newLatLngZoom(LatLng(36.4, 138.0), 4.2))
            installOverlays(s)
            loadedStyle = styleUri
            style = s
        }
    }

    LaunchedEffect(interactive, map) {
        map?.uiSettings?.setAllGesturesEnabled(interactive)
        map?.uiSettings?.isRotateGesturesEnabled = false
        map?.uiSettings?.isTiltGesturesEnabled = false
    }

    // Overlay data
    LaunchedEffect(style, routes, routeColor, routeWidth) {
        val s = style ?: return@LaunchedEffect
        s.getLayerAs<LineLayer>("$ROUTES-line")?.setProperties(
            PropertyFactory.lineColor(routeColor.toArgb()),
            PropertyFactory.lineWidth(routeWidth),
        )
        s.getSourceAs<GeoJsonSource>(ROUTES)?.setGeoJson(routeCollection(routes))
    }
    LaunchedEffect(style, polygons) {
        val s = style ?: return@LaunchedEffect
        s.getSourceAs<GeoJsonSource>(POLYS)?.setGeoJson(polygonCollection(polygons))
    }
    LaunchedEffect(style, points) {
        val s = style ?: return@LaunchedEffect
        s.getSourceAs<GeoJsonSource>(POINTS)?.setGeoJson(pointCollection(points))
    }
    LaunchedEffect(style, location) {
        val s = style ?: return@LaunchedEffect
        val loc = location
        s.getSourceAs<GeoJsonSource>(LOCATION)?.setGeoJson(
            FeatureCollection.fromFeatures(
                if (loc == null) emptyList() else listOf(
                    Feature.fromGeometry(Point.fromLngLat(loc.lon, loc.lat)).apply { addBooleanProperty("live", loc.live) },
                ),
            ),
        )
        val acc = loc?.accuracyM?.takeIf { loc.live && it > 0 && it < 5000 }
        s.getSourceAs<GeoJsonSource>(ACCURACY)?.setGeoJson(
            FeatureCollection.fromFeatures(
                if (acc == null) emptyList() else listOf(Feature.fromGeometry(circlePolygon(loc.lat, loc.lon, acc))),
            ),
        )
    }

    // Camera
    val layoutDir = LocalLayoutDirection.current
    val padPx = with(density) {
        doubleArrayOf(
            cameraPadding.calculateLeftPadding(layoutDir).toPx().toDouble(),
            cameraPadding.calculateTopPadding().toPx().toDouble(),
            cameraPadding.calculateRightPadding(layoutDir).toPx().toDouble(),
            cameraPadding.calculateBottomPadding().toPx().toDouble(),
        )
    }
    val padKey = padPx.toList()
    var fittedKey by remember { mutableStateOf<Any?>(Unit) }
    LaunchedEffect(map, style, focus, focusKey, location, routes, points, polygons, padKey) {
        val m = map ?: return@LaunchedEffect
        if (style == null) return@LaunchedEffect
        when (focus) {
            MapFocus.NONE -> Unit
            MapFocus.LOCATION -> {
                val loc = location ?: routes.lastOrNull()?.lastOrNull()?.let { MapLocation(it.lat, it.lon, false) } ?: return@LaunchedEffect
                val zoom = if (fittedKey == focusKey) m.cameraPosition.zoom.takeIf { it >= 3 } ?: focusZoom else focusZoom
                val target = CameraPosition.Builder()
                    .target(LatLng(loc.lat, loc.lon))
                    .zoom(zoom)
                    .padding(padPx)
                    .build()
                if (fittedKey == focusKey) {
                    m.animateCamera(CameraUpdateFactory.newCameraPosition(target), 600)
                } else {
                    m.moveCamera(CameraUpdateFactory.newCameraPosition(target))
                }
                fittedKey = focusKey
            }
            MapFocus.FIT -> {
                // Fit once per focusKey; later data updates keep the user's view.
                if (fittedKey == focusKey) return@LaunchedEffect
                val all = buildList {
                    routes.forEach { seg -> seg.forEach { add(LatLng(it.lat, it.lon)) } }
                    points.forEach { add(LatLng(it.lat, it.lon)) }
                    polygons.forEach { pg -> pg.ring.forEach { add(LatLng(it.lat, it.lon)) } }
                    location?.let { add(LatLng(it.lat, it.lon)) }
                }
                if (all.isEmpty()) return@LaunchedEffect
                if (all.size == 1 || spanOf(all) < 0.0005) {
                    m.moveCamera(
                        CameraUpdateFactory.newCameraPosition(
                            CameraPosition.Builder().target(all.first()).zoom(focusZoom).padding(padPx).build(),
                        ),
                    )
                } else {
                    val bounds = LatLngBounds.Builder().includes(all).build()
                    val edge = (36 * density.density).toInt()
                    m.moveCamera(
                        CameraUpdateFactory.newLatLngBounds(
                            bounds,
                            padPx[0].toInt() + edge,
                            padPx[1].toInt() + edge,
                            padPx[2].toInt() + edge,
                            padPx[3].toInt() + edge,
                        ),
                    )
                    if (m.cameraPosition.zoom > 17) m.moveCamera(CameraUpdateFactory.zoomTo(17.0))
                }
                fittedKey = focusKey
            }
        }
    }

    var unlocked by remember { mutableStateOf(false) }
    Box(modifier = modifier) {
        AndroidView(factory = { mapView }, modifier = Modifier.fillMaxSize())
        if (scrollFriendly && interactive) {
            if (!unlocked) {
                Box(
                    Modifier
                        .fillMaxSize()
                        .pointerInput(Unit) { detectTapGestures { unlocked = true } },
                ) {
                    Text(
                        "タップして地図を操作",
                        color = Color.White,
                        fontSize = 11.sp,
                        modifier = Modifier
                            .align(Alignment.TopStart)
                            .padding(8.dp)
                            .clip(RoundedCornerShape(8.dp))
                            .background(Color(0x99000000))
                            .padding(horizontal = 8.dp, vertical = 4.dp),
                    )
                }
            } else {
                Text(
                    "操作を終える",
                    color = Color.White,
                    fontSize = 11.sp,
                    modifier = Modifier
                        .align(Alignment.TopStart)
                        .padding(8.dp)
                        .clip(RoundedCornerShape(8.dp))
                        .background(Color(0xCC2F5BD8))
                        .clickable { unlocked = false }
                        .padding(horizontal = 10.dp, vertical = 5.dp),
                )
            }
        }
    }
}

private fun spanOf(points: List<LatLng>): Double {
    val lats = points.map { it.latitude }
    val lons = points.map { it.longitude }
    return max((lats.max() - lats.min()), (lons.max() - lons.min()))
}

private fun installOverlays(style: Style) {
    style.addSource(GeoJsonSource(ACCURACY, FeatureCollection.fromFeatures(emptyList())))
    style.addSource(GeoJsonSource(POLYS, FeatureCollection.fromFeatures(emptyList())))
    style.addSource(GeoJsonSource(ROUTES, FeatureCollection.fromFeatures(emptyList())))
    style.addSource(GeoJsonSource(POINTS, FeatureCollection.fromFeatures(emptyList())))
    style.addSource(GeoJsonSource(LOCATION, FeatureCollection.fromFeatures(emptyList())))

    style.addLayer(
        FillLayer("$ACCURACY-fill", ACCURACY).withProperties(
            PropertyFactory.fillColor(Color(0xFF397FD7).toArgb()),
            PropertyFactory.fillOpacity(0.12f),
            PropertyFactory.fillOutlineColor(Color(0x66397FD7).toArgb()),
        ),
    )
    style.addLayer(
        FillLayer("$POLYS-fill", POLYS).withProperties(
            PropertyFactory.fillColor(Expression.toColor(Expression.get("color"))),
            PropertyFactory.fillOpacity(Expression.get("opacity")),
        ),
    )
    style.addLayer(
        LineLayer("$POLYS-edge", POLYS).withProperties(
            PropertyFactory.lineColor(Expression.toColor(Expression.get("color"))),
            PropertyFactory.lineWidth(1f),
            PropertyFactory.lineOpacity(0.7f),
        ),
    )
    style.addLayer(
        LineLayer("$ROUTES-casing", ROUTES).withProperties(
            PropertyFactory.lineColor(Color.White.toArgb()),
            PropertyFactory.lineWidth(7f),
            PropertyFactory.lineOpacity(0.85f),
            PropertyFactory.lineCap(Property.LINE_CAP_ROUND),
            PropertyFactory.lineJoin(Property.LINE_JOIN_ROUND),
        ),
    )
    style.addLayer(
        LineLayer("$ROUTES-line", ROUTES).withProperties(
            PropertyFactory.lineColor(Color(0xFF2F5BD8).toArgb()),
            PropertyFactory.lineWidth(4f),
            PropertyFactory.lineCap(Property.LINE_CAP_ROUND),
            PropertyFactory.lineJoin(Property.LINE_JOIN_ROUND),
        ),
    )
    style.addLayer(
        CircleLayer("$POINTS-circle", POINTS).withProperties(
            PropertyFactory.circleColor(Expression.toColor(Expression.get("color"))),
            PropertyFactory.circleRadius(Expression.get("radius")),
            PropertyFactory.circleOpacity(Expression.get("opacity")),
            PropertyFactory.circleStrokeColor(Expression.toColor(Expression.get("stroke"))),
            PropertyFactory.circleStrokeWidth(Expression.get("strokeWidth")),
            PropertyFactory.circleSortKey(Expression.get("sort")),
        ),
    )
    val live = Expression.get("live")
    style.addLayer(
        CircleLayer("$LOCATION-halo", LOCATION).withProperties(
            PropertyFactory.circleRadius(15f),
            PropertyFactory.circleColor(
                Expression.switchCase(live, Expression.color(Color(0xFF397FD7).toArgb()), Expression.color(Color(0xFF8792A2).toArgb())),
            ),
            PropertyFactory.circleOpacity(0.22f),
        ),
    )
    style.addLayer(
        CircleLayer("$LOCATION-dot", LOCATION).withProperties(
            PropertyFactory.circleRadius(7.5f),
            PropertyFactory.circleColor(
                Expression.switchCase(live, Expression.color(Color(0xFF397FD7).toArgb()), Expression.color(Color(0xFF8792A2).toArgb())),
            ),
            PropertyFactory.circleStrokeColor(Color.White.toArgb()),
            PropertyFactory.circleStrokeWidth(3f),
        ),
    )
}

private fun routeCollection(routes: List<List<GeoPoint>>): FeatureCollection =
    FeatureCollection.fromFeatures(
        routes.filter { it.size >= 2 }.map { seg ->
            Feature.fromGeometry(LineString.fromLngLats(seg.map { Point.fromLngLat(it.lon, it.lat) }))
        },
    )

private fun hex(c: Color): String {
    val argb = c.toArgb()
    return String.format("#%06X", argb and 0xFFFFFF)
}

private fun pointCollection(points: List<MapPoint>): FeatureCollection =
    FeatureCollection.fromFeatures(
        points.map { pt ->
            val props = JsonObject().apply {
                addProperty("id", pt.id)
                addProperty("color", hex(pt.color))
                addProperty("stroke", hex(pt.stroke))
                addProperty("radius", pt.radius)
                addProperty("strokeWidth", pt.strokeWidth)
                addProperty("opacity", pt.opacity)
                addProperty("sort", pt.sortKey)
            }
            Feature.fromGeometry(Point.fromLngLat(pt.lon, pt.lat), props)
        },
    )

private fun polygonCollection(polys: List<MapPolygon>): FeatureCollection =
    FeatureCollection.fromFeatures(
        polys.filter { it.ring.size >= 3 }.map { pg ->
            val ring = (pg.ring + pg.ring.first()).map { Point.fromLngLat(it.lon, it.lat) }
            val props = JsonObject().apply {
                addProperty("id", pg.id)
                addProperty("color", hex(pg.color))
                addProperty("opacity", pg.opacity)
            }
            Feature.fromGeometry(Polygon.fromLngLats(listOf(ring)), props)
        },
    )

private fun circlePolygon(lat: Double, lon: Double, metres: Double): Polygon {
    val ring = (0..48).map { i ->
        val a = i / 48.0 * PI * 2
        val dLon = cos(a) * metres / (111_320.0 * max(0.01, cos(lat * PI / 180)))
        val dLat = sin(a) * metres / 111_320.0
        Point.fromLngLat(lon + dLon, lat + dLat)
    }
    return Polygon.fromLngLats(listOf(ring))
}
