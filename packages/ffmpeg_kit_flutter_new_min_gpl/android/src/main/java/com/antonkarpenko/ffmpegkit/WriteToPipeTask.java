package com.antonkarpenko.ffmpegkit;

import static com.antonkarpenko.ffmpegkit.FFmpegKitFlutterPlugin.LIBRARY_NAME;

import android.util.Log;

import androidx.annotation.NonNull;

import java.io.IOException;

import io.flutter.plugin.common.MethodChannel;

public class WriteToPipeTask implements Runnable {
    private final String inputPath;
    private final String namedPipePath;
    private final FFmpegKitFlutterMethodResultHandler resultHandler;
    private final MethodChannel.Result result;

    public WriteToPipeTask(@NonNull final String inputPath, @NonNull final String namedPipePath, @NonNull final FFmpegKitFlutterMethodResultHandler resultHandler, @NonNull final MethodChannel.Result result) {
        this.inputPath = inputPath;
        this.namedPipePath = namedPipePath;
        this.resultHandler = resultHandler;
        this.result = result;
    }

    @Override
    public void run() {
        final int rc;

        try {
            final String asyncCommand = "cat " + inputPath + " > " + namedPipePath;
            Log.d(LIBRARY_NAME, String.format("Starting copy %s to pipe %s operation.", inputPath, namedPipePath));

            final long startTime = System.currentTimeMillis();

            final Process process = Runtime.getRuntime().exec(new String[]{"sh", "-c", asyncCommand});
            rc = process.waitFor();

            final long endTime = System.currentTimeMillis();

            Log.d(LIBRARY_NAME, String.format("Copying %s to pipe %s operation completed with rc %d in %d seconds.", inputPath, namedPipePath, rc, (endTime - startTime) / 1000));

            resultHandler.successAsync(result, rc);

        } catch (final IOException | InterruptedException e) {
            Log.e(LIBRARY_NAME, String.format("Copy %s to pipe %s failed with error.", inputPath, namedPipePath), e);
            resultHandler.errorAsync(result, "WRITE_TO_PIPE_FAILED", e.getMessage());
        }
    }

}
