/** Engines report the detected language as a code ("en", "zh-Hant", "DE"); show its name instead. */
const NAMES: Record<string, string> = {
  "zh-hans": "中文（简体）", "zh-cn": "中文（简体）", "zh-chs": "中文（简体）", zh: "中文",
  "zh-hant": "中文（繁体）", "zh-tw": "中文（繁体）", "zh-hk": "中文（繁体）", "zh-cht": "中文（繁体）",
  en: "英语", ja: "日语", ko: "韩语", fr: "法语", de: "德语", es: "西班牙语", ru: "俄语", pt: "葡萄牙语",
  it: "意大利语", ar: "阿拉伯语", th: "泰语", vi: "越南语", id: "印尼语", ms: "马来语", tr: "土耳其语",
  nl: "荷兰语", pl: "波兰语", uk: "乌克兰语", hi: "印地语",
  sv: "瑞典语", da: "丹麦语", no: "挪威语", nb: "挪威语", nn: "挪威语", fi: "芬兰语", cs: "捷克语", hu: "匈牙利语",
  ro: "罗马尼亚语", bg: "保加利亚语", el: "希腊语", he: "希伯来语", iw: "希伯来语", fa: "波斯语", bn: "孟加拉语",
  ta: "泰米尔语", fil: "菲律宾语", tl: "菲律宾语", ur: "乌尔都语", sw: "斯瓦希里语",
};

export function langName(code: string): string {
  if (!code) return "";
  if (/[一-鿿]/.test(code)) return code; // already a name
  const c = code.toLowerCase().replace("_", "-");
  return NAMES[c] ?? NAMES[c.split("-")[0]!] ?? "";
}
