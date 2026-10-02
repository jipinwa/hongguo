package core

import (
	"strings"
	"testing"
)

func TestDanmakuEmojiExpandsVerifiedShortcodes(t *testing.T) {
	cases := map[string]string{
		"这不是韩立老魔？？？？？[笑哭]": "这不是韩立老魔？？？？？😂",
		"[笑哭]":           "😂",
		"韩立[偷笑][偷笑][偷笑]": "韩立🤭🤭🤭",
		"非常有看，大家冲[爱慕][爱慕][爱慕]":         "非常有看，大家冲😍😍😍",
		"修仙老祖：从一个黑罐开始，根据原著改编。[鼓掌]":     "修仙老祖：从一个黑罐开始，根据原著改编。👏",
		"与凤行的配乐[鼓掌]":                   "与凤行的配乐👏",
		"完了我才第一次看还有4季爽[捂脸]":            "完了我才第一次看还有4季爽🤦",
		"二刷起立[赞]":                      "二刷起立👍",
		"😯好看[送花]":                      "😯好看🌹",
		"这么高质量的AI剧可不多见[没看够][没看够][没看够]": "这么高质量的AI剧可不多见🥺🥺🥺",
	}
	for input, want := range cases {
		if got := expandDanmakuEmoji(input); got != want {
			t.Fatalf("expand %q => %q, want %q", input, got, want)
		}
	}
}

func TestDanmakuEmojiKeepsUnknownTokensAndPlainText(t *testing.T) {
	for _, input := range []string{
		"普通弹幕没有表情",
		"[这不是表情",
		"[未收录的表情]",
		"数组写法 arr[0] 不是表情",
		"",
	} {
		if got := expandDanmakuEmoji(input); got != input {
			t.Fatalf("expand %q => %q, should be unchanged", input, got)
		}
	}
}

func TestDanmakuEmojiIsCaseInsensitiveForLatinTokens(t *testing.T) {
	if got := expandDanmakuEmoji("[OK]"); got != "👌" {
		t.Fatalf("[OK] => %q", got)
	}
	if got := expandDanmakuEmoji("[Doge]"); got != "🐶" {
		t.Fatalf("[Doge] => %q", got)
	}
}

func TestDanmakuEmojiTableHasNoEmptyValues(t *testing.T) {
	for token, emoji := range danmakuEmoji {
		if emoji == "" {
			t.Fatalf("%s maps to an empty string", token)
		}
		if !strings.HasPrefix(token, "[") || !strings.HasSuffix(token, "]") {
			t.Fatalf("%s is not a bracketed token", token)
		}
		if strings.ToLower(token) != token {
			t.Fatalf("%s should be stored lowercase for lookup", token)
		}
	}
}

func TestDanmakuTruncationKeepsEmojiIntact(t *testing.T) {
	joined := strings.Repeat("字", 4) + "❤️"
	if got := trimDanmakuText(joined, 5); got != strings.Repeat("字", 4) {
		t.Fatalf("truncation split a variation selector: %q", got)
	}
	plain := strings.Repeat("字", 10)
	if got := trimDanmakuText(plain, 5); got != strings.Repeat("字", 5) {
		t.Fatalf("plain truncation changed: %q", got)
	}
	if got := trimDanmakuText("短", 5); got != "短" {
		t.Fatalf("short text should pass through: %q", got)
	}
	zwj := strings.Repeat("字", 3) + "\u200d👨"
	if got := trimDanmakuText(zwj, 4); got != strings.Repeat("字", 3) {
		t.Fatalf("truncation split a zero-width joiner sequence: %q", got)
	}
}
