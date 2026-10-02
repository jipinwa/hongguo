# Keep all FFmpegKit classes (incl. JNI entry points and the Flutter plugin) so
# R8/ProGuard never strips them in release builds. Stripping causes native-load
# failures that surface as a white screen / plugin-registration channel errors.
-keep class com.antonkarpenko.ffmpegkit.** { *; }
-dontwarn com.antonkarpenko.ffmpegkit.**

-keep class com.antonkarpenko.ffmpegkit.FFmpegKitConfig {
    native <methods>;
    void log(long, int, byte[]);
    void statistics(long, int, float, float, long , double, double, double);
    int safOpen(int);
    int safClose(int);
}

-keep class com.antonkarpenko.ffmpegkit.AbiDetect {
    native <methods>;
}
