package com.antonkarpenko.ffmpegkit;

import androidx.annotation.NonNull;

import com.antonkarpenko.ffmpegkit.FFmpegKitConfig;

import io.flutter.plugin.common.MethodChannel;

public class FFmpegSessionExecuteTask implements Runnable {
    private final com.antonkarpenko.ffmpegkit.FFmpegSession ffmpegSession;
    private final FFmpegKitFlutterMethodResultHandler resultHandler;
    private final MethodChannel.Result result;

    public FFmpegSessionExecuteTask(@NonNull final com.antonkarpenko.ffmpegkit.FFmpegSession ffmpegSession, @NonNull final FFmpegKitFlutterMethodResultHandler resultHandler, @NonNull final MethodChannel.Result result) {
        this.ffmpegSession = ffmpegSession;
        this.resultHandler = resultHandler;
        this.result = result;
    }

    @Override
    public void run() {
        FFmpegKitConfig.ffmpegExecute(ffmpegSession);
        resultHandler.successAsync(result, null);
    }
}
