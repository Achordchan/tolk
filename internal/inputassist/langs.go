package inputassist

import "strings"

// Auto is the label and AutoCode the code of "detect the language".
const (
	Auto     = "自动检测"
	AutoCode = "auto"
)

// langCodes maps Tolk's Chinese language labels to the BCP-47 style codes the
// old Mac app used for them (they show in the candidate panel as EN, ZH-CN…).
var langCodes = [][2]string{
	{"简体中文", "zh-CN"}, {"繁体中文", "zh-TW"}, {"英语", "en"}, {"日语", "ja"}, {"韩语", "ko"},
	{"法语", "fr"}, {"德语", "de"}, {"西班牙语", "es"}, {"俄语", "ru"}, {"意大利语", "it"},
	{"葡萄牙语", "pt"}, {"越南语", "vi"}, {"泰语", "th"}, {"阿拉伯语", "ar"}, {"荷兰语", "nl"},
	{"瑞典语", "sv"}, {"丹麦语", "da"}, {"挪威语", "no"}, {"芬兰语", "fi"}, {"波兰语", "pl"},
	{"捷克语", "cs"}, {"匈牙利语", "hu"}, {"罗马尼亚语", "ro"}, {"保加利亚语", "bg"}, {"希腊语", "el"},
	{"乌克兰语", "uk"}, {"土耳其语", "tr"}, {"希伯来语", "he"}, {"波斯语", "fa"}, {"印地语", "hi"},
	{"孟加拉语", "bn"}, {"泰米尔语", "ta"}, {"印尼语", "id"}, {"马来语", "ms"}, {"菲律宾语", "fil"},
	{"乌尔都语", "ur"}, {"斯瓦希里语", "sw"},
}

// CodeOf returns the code of a language label; ok is false for labels Tolk
// does not know. 自动检测 gives AutoCode.
func CodeOf(label string) (string, bool) {
	label = strings.TrimSpace(label)
	if label == Auto {
		return AutoCode, true
	}
	for _, l := range langCodes {
		if l[0] == label {
			return l[1], true
		}
	}
	return "", false
}

// LabelOf returns the label of a code, or "" when it is not one of Tolk's.
func LabelOf(code string) string {
	if code == AutoCode {
		return Auto
	}
	for _, l := range langCodes {
		if l[1] == code {
			return l[0]
		}
	}
	return ""
}

// LabelForRequest is the language label to send to the translator for a
// detected code: codes Tolk does not support fall back to 自动检测, which lets
// the engine work the language out itself.
func LabelForRequest(code string) string {
	if l := LabelOf(code); l != "" {
		return l
	}
	return Auto
}

// DisplayCode is the compact tag a candidate row shows: EN, ZH-CN.
func DisplayCode(code string) string { return strings.ToUpper(code) }
