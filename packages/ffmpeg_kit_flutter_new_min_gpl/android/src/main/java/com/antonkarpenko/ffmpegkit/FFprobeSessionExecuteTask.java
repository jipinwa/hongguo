package com.antonkarpenko.ffmpegkit;

import androidx.annotation.NonNull;

import com.antonkarpenko.ffmpegkit.FFmpegKitConfig;
import com.antonkarpenko.ffmpegkit.FFprobeSession;

import io.flutter.plugin.common.MethodChannel;

public class FFprobeSessionExecuteTask implements Runnable {
    private final FFprobeSession ffprobeSession;
    private final FFmpegKitFlutterMethodResultHandler resultHandler;
    private final MethodChannel.Result result;

    public FFprobeSessionExecuteTask(@NonNull final FFprobeSession ffprobeSession, @NonNull final FFmpegKitFlutterMethodResultHandler resultHandler, @NonNull final MethodChannel.Result result) {
        this.ffprobeSession = ffprobeSession;
        this.resultHandler = resultHandler;
        this.result = result;
    }

    @Override
    public void run() {
        FFmpegKitConfig.ffprobeExecute(ffprobeSession);
        resultHandler.successAsync(result, null);
    }
}
