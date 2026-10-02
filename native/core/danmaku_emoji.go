package core

import (
	"regexp"
	"strings"
)

var danmakuEmoji = map[string]string{
	"[笑哭]":   "😂",
	"[大笑]":   "😆",
	"[微笑]":   "🙂",
	"[呲牙]":   "😁",
	"[偷笑]":   "🤭",
	"[捂脸]":   "🤦",
	"[害羞]":   "😊",
	"[调皮]":   "😜",
	"[爱慕]":   "😍",
	"[色]":    "😍",
	"[酷]":    "😎",
	"[白眼]":   "🙄",
	"[抠鼻]":   "🤨",
	"[鄙视]":   "😒",
	"[思考]":   "🤔",
	"[什么]":   "🤔",
	"[惊讶]":   "😲",
	"[尴尬]":   "😅",
	"[汗]":    "😅",
	"[闭嘴]":   "🤐",
	"[嘘]":    "🤫",
	"[困]":    "😴",
	"[睡]":    "😴",
	"[哈欠]":   "🥱",
	"[流泪]":   "😢",
	"[大哭]":   "😭",
	"[委屈]":   "🥺",
	"[可怜]":   "🥺",
	"[没看够]":  "🥺",
	"[抓狂]":   "😫",
	"[发怒]":   "😡",
	"[吐]":    "🤮",
	"[打脸]":   "🤦",
	"[灵光一闪]": "💡",
	"[赞]":    "👍",
	"[强]":    "👍",
	"[给力]":   "👍",
	"[弱]":    "👎",
	"[鼓掌]":   "👏",
	"[ok]":   "👌",
	"[胜利]":   "✌️",
	"[耶]":    "✌️",
	"[奋斗]":   "💪",
	"[加油]":   "💪",
	"[威武]":   "💪",
	"[抱抱]":   "🤗",
	"[比心]":   "🫰",
	"[心]":    "❤️",
	"[爱心]":   "❤️",
	"[送花]":   "🌹",
	"[玫瑰]":   "🌹",
	"[鲜花]":   "💐",
	"[钱]":    "💰",
	"[狗头]":   "🐶",
	"[doge]": "🐶",
}

var danmakuShortcode = regexp.MustCompile(`\[[^\[\]\s]{1,10}\]`)

func expandDanmakuEmoji(text string) string {
	if !strings.Contains(text, "[") {
		return text
	}
	return danmakuShortcode.ReplaceAllStringFunc(text, func(token string) string {
		if emoji, found := danmakuEmoji[strings.ToLower(token)]; found {
			return emoji
		}
		return token
	})
}

func trimDanmakuText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	cut := limit
	for cut > 0 {
		retreat := false
		if cut < len(runes) && danmakuEmojiModifier(runes[cut]) {
			cut, retreat = cut-1, true
		}
		if cut > 0 && danmakuEmojiJoiner(runes[cut-1]) {
			cut, retreat = cut-1, true
		}
		if !retreat {
			break
		}
	}
	return string(runes[:cut])
}

func danmakuEmojiModifier(r rune) bool {
	return r == '\ufe0f' || r == '\ufe0e'
}

func danmakuEmojiJoiner(r rune) bool {
	return r == '\u200d'
}
