package com.duanju.duanju_app

import io.flutter.embedding.android.FlutterActivity
import android.app.UiModeManager
import android.app.ActivityManager
import android.app.PictureInPictureParams
import android.os.Build
import android.os.Bundle
import android.os.BatteryManager
import android.os.PowerManager
import android.os.SystemClock
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.ActivityInfo
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.graphics.Rect
import android.net.ConnectivityManager
import android.net.Uri
import android.util.Rational
import android.view.InputDevice
import com.arthenica.mobileffmpeg.Config
import com.mobile.ffmpeg.FFmpeg
import com.mobile.ffmpeg.Statistics
import com.mobile.ffmpeg.StatisticsCallback
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.ConcurrentHashMap

class MainActivity : FlutterActivity() {
    private var headroomReadAt = 0L
    private var thermalHeadroom: Double? = null
    private var deviceChannel: MethodChannel? = null
    private var televisionMode = false

    @Suppress("DEPRECATION")
    private fun isTelevisionDevice(): Boolean {
        val configuration = resources.configuration
        val mode = (getSystemService(Context.UI_MODE_SERVICE) as? UiModeManager)?.currentModeType
            ?: (configuration.uiMode and Configuration.UI_MODE_TYPE_MASK)
        if (mode == Configuration.UI_MODE_TYPE_TELEVISION ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK) ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK_ONLY) ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_TELEVISION) ||
            packageManager.hasSystemFeature("amazon.hardware.fire_tv")) {
            return true
        }
        if (mode == Configuration.UI_MODE_TYPE_CAR ||
            mode == Configuration.UI_MODE_TYPE_WATCH ||
            mode == Configuration.UI_MODE_TYPE_VR_HEADSET ||
            configuration.touchscreen != Configuration.TOUCHSCREEN_NOTOUCH ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_TOUCHSCREEN) ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_TELEPHONY) ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_SENSOR_ACCELEROMETER) ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_AUTOMOTIVE) ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_WATCH) ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_PC)) {
            return false
        }
        val remoteNavigation = configuration.navigation == Configuration.NAVIGATION_DPAD ||
            InputDevice.getDeviceIds().any { id ->
                val device = InputDevice.getDevice(id)
                device != null && !device.isVirtual && device.supportsSource(InputDevice.SOURCE_DPAD)
            }
        return remoteNavigation || packageManager.hasSystemFeature(PackageManager.FEATURE_LIVE_TV)
    }

    override fun setRequestedOrientation(requestedOrientation: Int) {
        super.setRequestedOrientation(
            if (televisionMode) ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE else requestedOrientation
        )
    }

    private fun playbackPower(): Map<String, Any?> {
        val power = getSystemService(Context.POWER_SERVICE) as? PowerManager
        val activity = getSystemService(Context.ACTIVITY_SERVICE) as? ActivityManager
        val battery = registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
        val now = SystemClock.elapsedRealtime()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R &&
            (headroomReadAt == 0L || now - headroomReadAt >= 10000L)) {
            headroomReadAt = now
            thermalHeadroom = runCatching {
                power?.getThermalHeadroom(0)?.toDouble()?.takeIf { it.isFinite() }
            }.getOrNull()
        }
        val thermalStatus = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            power?.currentThermalStatus ?: PowerManager.THERMAL_STATUS_NONE
        } else 0
        return mapOf(
            "batterySaver" to (power?.isPowerSaveMode ?: false),
            "onBattery" to ((battery?.getIntExtra(BatteryManager.EXTRA_PLUGGED, -1) ?: -1) <= 0),
            "thermalStatus" to thermalStatus,
            "headroom" to thermalHeadroom,
            "lowMemory" to (activity?.isLowRamDevice ?: true),
            "gles" to (activity?.deviceConfigurationInfo?.reqGlEsVersion ?: 0)
        )
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        televisionMode = if (savedInstanceState?.containsKey("duanju.televisionMode") == true) {
            savedInstanceState.getBoolean("duanju.televisionMode")
        } else isTelevisionDevice()
        super.onCreate(savedInstanceState)
        if (televisionMode) requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            window.isNavigationBarContrastEnforced = false
            window.isStatusBarContrastEnforced = false
        }
    }

    override fun onResume() {
        super.onResume()
        if (televisionMode) requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
    }

    override fun onSaveInstanceState(outState: Bundle) {
        outState.putBoolean("duanju.televisionMode", televisionMode)
        super.onSaveInstanceState(outState)
    }

    override fun onDestroy() {
        super.onDestroy()
        if (isFinishing && !downloadsServiceRunning()) {
            android.os.Process.killProcess(android.os.Process.myPid())
        }
    }

    private fun downloadsServiceRunning(): Boolean {
        val manager = getSystemService(Context.ACTIVITY_SERVICE) as? ActivityManager ?: return false
        @Suppress("DEPRECATION")
        return runCatching {
            manager.getRunningServices(Int.MAX_VALUE).any {
                it.service.className == "com.pravera.flutter_foreground_task.service.ForegroundService"
            }
        }.getOrDefault(false)
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        deviceChannel = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "duanju/device")
            .also { channel ->
                channel.setMethodCallHandler { call, result ->
                    when (call.method) {
                        "deviceInfo" -> {
                            val version = packageManager.getPackageInfo(packageName, 0).versionName
                            result.success(mapOf(
                                "television" to isTelevisionDevice(),
                                "version" to version,
                                "sdkInt" to Build.VERSION.SDK_INT
                            ))
                        }
                        "setTelevisionMode" -> {
                            val enabled = call.argument<Boolean>("enabled")
                            if (enabled == null) {
                                result.error("invalid_display_mode", "缺少电视模式状态", null)
                            } else {
                                val changed = televisionMode != enabled
                                televisionMode = enabled
                                if (enabled || changed) {
                                    requestedOrientation = if (enabled) {
                                        ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
                                    } else ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
                                }
                                result.success(null)
                            }
                        }
                        "playbackPower" -> result.success(runCatching { playbackPower() }.getOrNull())
                        "systemProxy" -> {
                            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.N) {
                                result.success(mapOf(
                                    "http" to "",
                                    "https" to "",
                                    "bypass" to emptyList<String>(),
                                    "pac" to false
                                ))
                                return@setMethodCallHandler
                            }
                            val connection = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
                            val proxy = connection.defaultProxy
                            val host = proxy?.host.orEmpty()
                            val address = if (host.isNotEmpty() && (proxy?.port ?: 0) > 0) {
                                "http://${if (host.contains(':')) "[$host]" else host}:${proxy!!.port}"
                            } else ""
                            result.success(mapOf(
                                "http" to address,
                                "https" to address,
                                "bypass" to (proxy?.exclusionList?.toList() ?: emptyList<String>()),
                                "pac" to (proxy != null && proxy.pacFileUrl != Uri.EMPTY)
                            ))
                        }
                        "pictureInPictureStatus" -> result.success(pictureInPictureStatus())
                        "enterPictureInPicture" -> {
                            val width = call.argument<Int>("width") ?: 16
                            val height = call.argument<Int>("height") ?: 9
                            result.success(enterPlayerPictureInPicture(
                                width,
                                height,
                                call.argument<Int>("left"),
                                call.argument<Int>("top"),
                                call.argument<Int>("right"),
                                call.argument<Int>("bottom")
                            ))
                        }
                        "ffmpegRun" -> {
                            val id = call.argument<String>("id") ?: ""
                            val arguments = call.argument<List<String>>("arguments") ?: emptyList()
                            if (id.isEmpty() || arguments.isEmpty()) {
                                result.error("invalid_args", "缺少媒体处理参数", null)
                            } else {
                                startFFmpeg(id, arguments, probe = false) { code, cancelled ->
                                    result.success(
                                        mapOf("code" to code, "cancelled" to cancelled)
                                    )
                                }
                            }
                        }
                        "ffmpegProbe" -> {
                            val file = call.argument<String>("file") ?: ""
                            if (file.isEmpty()) {
                                result.error("invalid_args", "缺少媒体路径", null)
                            } else {
                                startFFmpeg(
                                    "probe-${System.nanoTime()}",
                                    listOf("-hide_banner", "-i", file),
                                    probe = true
                                ) { _, _ ->
                                    result.success(lastProbeResult)
                                }
                            }
                        }
                        "ffmpegProgress" -> {
                            val id = call.argument<String>("id") ?: ""
                            result.success(ffmpegJobs[id]?.timeMs ?: 0L)
                        }
                        "ffmpegCancel" -> {
                            val id = call.argument<String>("id") ?: ""
                            val job = ffmpegJobs[id]
                            if (job != null && job.running) {
                                job.cancelled = true
                                FFmpeg.cancel()
                            }
                            result.success(null)
                        }
                        else -> result.notImplemented()
                    }
                }
            }
    }

    private class FFmpegJob(val id: String) {
        val log = StringBuilder()
        @Volatile var timeMs = 0L
        @Volatile var running = true
        @Volatile var cancelled = false
    }

    private val ffmpegJobs = ConcurrentHashMap<String, FFmpegJob>()
    private val ffmpegActive = ThreadLocal<FFmpegJob?>()
    @Volatile private var ffmpegCallbacksReady = false
    @Volatile private var lastProbeResult: Map<String, Any> = emptyMap()

    private fun ensureFFmpegCallbacks() {
        if (ffmpegCallbacksReady) return
        synchronized(ffmpegJobs) {
            if (ffmpegCallbacksReady) return
            Config.enableLogCallback { message ->
                val job = ffmpegActive.get()
                if (job != null) {
                    synchronized(job.log) { job.log.append(message.text).append('\n') }
                }
            }
            Config.enableStatisticsCallback(object : StatisticsCallback {
                override fun apply(statistics: Statistics) {
                    ffmpegActive.get()?.timeMs = statistics.time.toLong()
                }

                override fun onCancel() {}
            })
            ffmpegCallbacksReady = true
        }
    }

    private fun startFFmpeg(
        id: String,
        arguments: List<String>,
        probe: Boolean,
        onDone: (Int, Boolean) -> Unit
    ) {
        val job = FFmpegJob(id)
        ffmpegJobs[id] = job
        Thread {
            val code = runCatching {
                ensureFFmpegCallbacks()
                ffmpegActive.set(job)
                try {
                    FFmpeg.execute(arguments.toTypedArray())
                } finally {
                    ffmpegActive.remove()
                }
            }.getOrElse { -1 }
            job.running = false
            if (probe) {
                val text = synchronized(job.log) { job.log.toString() }
                lastProbeResult = parseFFmpegProbe(text)
            }
            ffmpegJobs.remove(id)
            runOnUiThread { onDone(code, job.cancelled) }
        }.start()
    }

    private fun parseFFmpegProbe(text: String): Map<String, Any> {
        val streams = mutableListOf<Map<String, Any>>()
        var duration = 0.0
        Regex("""Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)""").find(text)?.let { match ->
            duration = match.groupValues[1].toDouble() * 3600 +
                match.groupValues[2].toDouble() * 60 +
                match.groupValues[3].toDouble()
        }
        for (match in Regex("""Stream #\S+[^\n]*""").findAll(text)) {
            val line = match.value
            when {
                line.contains(": Video:") -> {
                    val codec = Regex("""Video:\s*(\w+)""").find(line)
                        ?.groupValues?.get(1).orEmpty()
                    if (codec.isEmpty()) continue
                    val pixel = Regex(
                        """Video:\s*\w+(?:\s*\([^)]*\))*,\s*(\w+)(?:\(([^)]*)\))?"""
                    ).find(line)
                    val size = Regex("""(\d{2,5})x(\d{2,5})""").find(line)
                    val sar = Regex("""SAR\s+([0-9]+:[0-9]+)""").find(line)
                        ?.groupValues?.get(1) ?: "1:1"
                    var colorTransfer = "unknown"
                    var colorPrimaries = "unknown"
                    pixel?.groupValues?.get(2)?.let { colors ->
                        val parts = colors.split(',', '/').map { it.trim() }.filter { it.isNotEmpty() }
                        if (parts.isNotEmpty()) {
                            colorPrimaries = when (parts[0]) {
                                "tv", "pc" -> if (parts.size > 1) parts[1] else "unknown"
                                else -> parts[0]
                            }
                            colorTransfer = when {
                                parts.size >= 3 -> parts[2]
                                parts.size >= 2 && parts[0] != "tv" && parts[0] != "pc" -> parts[1]
                                parts.size >= 2 -> parts[1]
                                else -> "unknown"
                            }
                        }
                    }
                    streams.add(
                        mapOf(
                            "codec_type" to "video",
                            "codec_name" to codec,
                            "width" to (size?.groupValues?.get(1)?.toIntOrNull() ?: 0),
                            "height" to (size?.groupValues?.get(2)?.toIntOrNull() ?: 0),
                            "pix_fmt" to (pixel?.groupValues?.get(1).orEmpty()),
                            "sample_aspect_ratio" to sar,
                            "color_transfer" to colorTransfer,
                            "color_primaries" to colorPrimaries,
                            "extradata_hash" to "unknown"
                        )
                    )
                }
                line.contains(": Audio:") -> {
                    val codec = Regex("""Audio:\s*(\w+)""").find(line)
                        ?.groupValues?.get(1).orEmpty()
                    if (codec.isEmpty()) continue
                    val tags = Regex("""Audio:\s*\w+((?:\s*\([^)]*\))+)""").find(line)
                        ?.groupValues?.get(1).orEmpty()
                    val firstTag = Regex("""\(([^)]*)\)""").find(tags)
                        ?.groupValues?.get(1)
                    val profile = if (firstTag != null && !firstTag.contains('/')) firstTag else "unknown"
                    val sampleRate = Regex("""(\d+)\s*Hz""").find(line)
                        ?.groupValues?.get(1) ?: "0"
                    val layout = Regex("""Hz,\s*([a-zA-Z0-9.()]+?)\s*,""").find(line)
                        ?.groupValues?.get(1) ?: "stereo"
                    val channels = when {
                        layout.startsWith("mono") -> 1
                        layout.startsWith("stereo") -> 2
                        layout.startsWith("1.") -> 2
                        layout.startsWith("2.") -> 3
                        layout.startsWith("5.1") || layout.startsWith("5.0") -> 6
                        layout.startsWith("7.1") -> 8
                        else -> 2
                    }
                    streams.add(
                        mapOf(
                            "codec_type" to "audio",
                            "codec_name" to codec,
                            "profile" to profile,
                            "sample_rate" to sampleRate,
                            "channels" to channels,
                            "channel_layout" to layout,
                            "extradata_hash" to "unknown"
                        )
                    )
                }
            }
        }
        return mapOf(
            "streams" to streams,
            "format" to mapOf("duration" to duration.toString())
        )
    }

    private fun pictureInPictureSupported(): Boolean {
        return Build.VERSION.SDK_INT >= Build.VERSION_CODES.O &&
            packageManager.hasSystemFeature(PackageManager.FEATURE_PICTURE_IN_PICTURE)
    }

    private fun pictureInPictureStatus(): Map<String, Any> {
        return mapOf(
            "supported" to pictureInPictureSupported(),
            "active" to (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N && isInPictureInPictureMode)
        )
    }

    private fun enterPlayerPictureInPicture(
        width: Int,
        height: Int,
        left: Int?,
        top: Int?,
        right: Int?,
        bottom: Int?
    ): Map<String, Any> {
        if (!pictureInPictureSupported()) return pictureInPictureStatus()
        val safeWidth = width.coerceIn(1, 10000)
        val safeHeight = height.coerceIn(1, 10000)
        return runCatching {
            val builder = PictureInPictureParams.Builder()
            builder.setAspectRatio(Rational(safeWidth, safeHeight))
            if (left != null && top != null && right != null && bottom != null &&
                right > left && bottom > top) {
                builder.setSourceRectHint(Rect(left, top, right, bottom))
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                builder.setAutoEnterEnabled(true)
            }
            val entered = enterPictureInPictureMode(builder.build())
            pictureInPictureStatus() + ("requested" to entered)
        }.getOrElse { pictureInPictureStatus() }
    }

    override fun onPictureInPictureModeChanged(
        isInPictureInPictureMode: Boolean,
        newConfig: Configuration
    ) {
        super.onPictureInPictureModeChanged(isInPictureInPictureMode, newConfig)
        deviceChannel?.invokeMethod(
            "pictureInPictureChanged",
            mapOf("active" to isInPictureInPictureMode)
        )
    }

    override fun cleanUpFlutterEngine(flutterEngine: FlutterEngine) {
        deviceChannel?.setMethodCallHandler(null)
        deviceChannel = null
        super.cleanUpFlutterEngine(flutterEngine)
    }
}
