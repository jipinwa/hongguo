enum VideoEnhancementMode {
  off('关闭'),
  automatic('自动'),
  economy('省电增强'),
  quality('清晰优先');

  const VideoEnhancementMode(this.label);
  final String label;
}

enum VideoEnhancementContent {
  automatic('跟随分类'),
  general('通用／真人'),
  animation('动漫／AI 动漫');

  const VideoEnhancementContent(this.label);
  final String label;
}

class VideoEnhancementPreferences {
  const VideoEnhancementPreferences({
    this.mode = VideoEnhancementMode.off,
    this.content = VideoEnhancementContent.automatic,
  });

  final VideoEnhancementMode mode;
  final VideoEnhancementContent content;

  VideoEnhancementPreferences copyWith({
    VideoEnhancementMode? mode,
    VideoEnhancementContent? content,
  }) => VideoEnhancementPreferences(
    mode: mode ?? this.mode,
    content: content ?? this.content,
  );

  Map<String, dynamic> toJson() => {'mode': mode.name, 'content': content.name};

  factory VideoEnhancementPreferences.fromJson(Object? value) {
    if (value is! Map) return const VideoEnhancementPreferences();
    return VideoEnhancementPreferences(
      mode:
          VideoEnhancementMode.values
              .where((mode) => mode.name == value['mode'])
              .firstOrNull ??
          VideoEnhancementMode.off,
      content:
          VideoEnhancementContent.values
              .where((content) => content.name == value['content'])
              .firstOrNull ??
          VideoEnhancementContent.automatic,
    );
  }

  @override
  bool operator ==(Object other) =>
      other is VideoEnhancementPreferences &&
      other.mode == mode &&
      other.content == content;

  @override
  int get hashCode => Object.hash(mode, content);
}
