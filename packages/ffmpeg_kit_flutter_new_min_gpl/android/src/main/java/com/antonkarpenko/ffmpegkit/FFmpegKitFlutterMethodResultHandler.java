package com.antonkarpenko.ffmpegkit;

import static com.antonkarpenko.ffmpegkit.FFmpegKitFlutterPlugin.LIBRARY_NAME;

import android.os.Handler;
import android.os.Looper;
import android.util.Log;

import io.flutter.plugin.common.EventChannel;
import io.flutter.plugin.common.MethodChannel;

public class FFmpegKitFlutterMethodResultHandler {
    private final Handler handler;

    FFmpegKitFlutterMethodResultHandler() {
        handler = new Handler(Looper.getMainLooper());
    }

    public void successAsync(final MethodChannel.Result result, final Object object) {
        handler.post(() -> {
            if (result != null) {
                result.success(object);
            } else {
                Log.w(LIBRARY_NAME, String.format("ResultHandler can not send successful response %s on a null method call result.", object));
            }
        });
    }

    void successAsync(final EventChannel.EventSink eventSink, final Object object) {
        handler.post(() -> {
            if (eventSink != null) {
                eventSink.success(object);
            } else {
                Log.w(LIBRARY_NAME, String.format("ResultHandler can not send event %s on a null event sink.", object));
            }
        });
    }

    void errorAsync(final MethodChannel.Result result, final String errorCode, final String errorMessage) {
        errorAsync(result, errorCode, errorMessage, null);
    }

    void errorAsync(final MethodChannel.Result result, final String errorCode, final String errorMessage, final Object errorDetails) {
        handler.post(() -> {
            if (result != null) {
                result.error(errorCode, errorMessage, errorDetails);
            } else {
                Log.w(LIBRARY_NAME, String.format("ResultHandler can not send failure response %s:%s on a null method call result.", errorCode, errorMessage));
            }
        });
    }

    void notImplementedAsync(final MethodChannel.Result result) {
        handler.post(() -> {
            if (result != null) {
                result.notImplemented();
            } else {
                Log.w(LIBRARY_NAME, "ResultHandler can not send not implemented response on a null method call result.");
            }
        });
    }

}
