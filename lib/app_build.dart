import 'dart:io';

const allSourcesEnabled = bool.fromEnvironment('ALL_SOURCES');
const appName = allSourcesEnabled ? '真果鉴' : '红果鉴';
const appSlug = allSourcesEnabled ? 'zhenguojian' : 'hongguojian';

int androidSdkInt = 0;
bool ffmpegSessionStarted = false;

bool ffmpegSupported() => !Platform.isAndroid || androidSdkInt == 0 || androidSdkInt >= 23;
