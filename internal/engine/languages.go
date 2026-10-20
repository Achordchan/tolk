package engine

import "fmt"

// lang is one row of the language table: the Chinese label stored in the
// config and the code each engine expects. An empty code means the engine
// does not offer that language.
type lang struct {
	name   string
	google string
	ms     string
	dlSrc  string
	dlTgt  string
}

// langTable is the union of the Windows app's 14 languages and the Mac app's
// 38, in the Mac app's picker order with the Windows ones kept first.
var langTable = []lang{
	{"简体中文", "zh-CN", "zh-Hans", "ZH", "ZH-HANS"},
	{"繁体中文", "zh-TW", "zh-Hant", "ZH", "ZH-HANT"},
	{"英语", "en", "en", "EN", "EN-US"},
	{"日语", "ja", "ja", "JA", "JA"},
	{"韩语", "ko", "ko", "KO", "KO"},
	{"法语", "fr", "fr", "FR", "FR"},
	{"德语", "de", "de", "DE", "DE"},
	{"西班牙语", "es", "es", "ES", "ES"},
	{"俄语", "ru", "ru", "RU", "RU"},
	{"意大利语", "it", "it", "IT", "IT"},
	{"葡萄牙语", "pt", "pt", "PT", "PT-PT"},
	{"越南语", "vi", "vi", "VI", "VI"},
	{"泰语", "th", "th", "TH", "TH"},
	{"阿拉伯语", "ar", "ar", "AR", "AR"},
	{"荷兰语", "nl", "nl", "NL", "NL"},
	{"瑞典语", "sv", "sv", "SV", "SV"},
	{"丹麦语", "da", "da", "DA", "DA"},
	{"挪威语", "no", "nb", "NB", "NB"},
	{"芬兰语", "fi", "fi", "FI", "FI"},
	{"波兰语", "pl", "pl", "PL", "PL"},
	{"捷克语", "cs", "cs", "CS", "CS"},
	{"匈牙利语", "hu", "hu", "HU", "HU"},
	{"罗马尼亚语", "ro", "ro", "RO", "RO"},
	{"保加利亚语", "bg", "bg", "BG", "BG"},
	{"希腊语", "el", "el", "EL", "EL"},
	{"乌克兰语", "uk", "uk", "UK", "UK"},
	{"土耳其语", "tr", "tr", "TR", "TR"},
	{"希伯来语", "iw", "he", "HE", "HE"},
	{"波斯语", "fa", "fa", "", ""},
	{"印地语", "hi", "hi", "", ""},
	{"孟加拉语", "bn", "bn", "", ""},
	{"泰米尔语", "ta", "ta", "", ""},
	{"印尼语", "id", "id", "ID", "ID"},
	{"马来语", "ms", "ms", "", ""},
	{"菲律宾语", "tl", "fil", "", ""},
	{"乌尔都语", "ur", "ur", "", ""},
	{"斯瓦希里语", "sw", "sw", "", ""},
}

// Languages lists the language labels in display order.
var Languages = func() []string {
	out := make([]string, len(langTable))
	for i, l := range langTable {
		out[i] = l.name
	}
	return out
}()

// codes builds a label→code map for one engine, skipping unsupported rows.
func codes(pick func(lang) string) map[string]string {
	m := make(map[string]string, len(langTable)+1)
	for _, l := range langTable {
		if c := pick(l); c != "" {
			m[l.name] = c
		}
	}
	return m
}

// unsupported says which engine cannot handle a language, instead of letting
// it quietly translate into something else.
func unsupported(engine, role, name string) error {
	return fmt.Errorf("%s暂不支持%s语言：%s，请换一个翻译引擎", engine, role, name)
}
