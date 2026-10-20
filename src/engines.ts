/** Translation services in display order. Apple's local engine exists on macOS only. */
export function engineList(os: string): [string, string][] {
  const list: [string, string][] = [
    ["google", "Google 翻译"],
    ["microsoft", "微软翻译"],
    ["deepl", "DeepL"],
    ["openai_compat", "AI 翻译"],
    ["achord_builtin", "Achord 内置引擎"],
  ];
  if (os === "darwin") list.unshift(["apple", "Apple 本地翻译"]);
  return list;
}
