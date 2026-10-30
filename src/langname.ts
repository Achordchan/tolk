/** Engines report the detected language as a code ("en", "zh-Hant", "DE"); show its name instead. */
const NAMES: Record<string, string> = {
  "zh-hans": "中文（简体）", "zh-cn": "中文（简体）", "zh-chs": "中文（简体）", zh: "中文",
  "zh-hant": "中文（繁体）", "zh-tw": "中文（繁体）", "zh-hk": "中文（繁体）", "zh-cht": "中文（繁体）",
  en: "英语", ja: "日语", ko: "韩语", fr: "法语", de: "德语", es: "西班牙语", ru: "俄语", pt: "葡萄牙语",
  it: "意大利语", ar: "阿拉伯语", th: "泰语", vi: "越南语", id: "印尼语", ms: "马来语", tr: "土耳其语",
  nl: "荷兰语", pl: "波兰语", uk: "乌克兰语", hi: "印地语",
};

export function langName(code: string): string {
  if (!code) return "";
  if (/[一-鿿]/.test(code)) return code; // already a name
  const c = code.toLowerCase().replace("_", "-");
  return NAMES[c] ?? NAMES[c.split("-")[0]!] ?? "";
}
