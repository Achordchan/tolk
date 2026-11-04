import { rename, rm } from "node:fs/promises";
import { resolve } from "node:path";
import { trayBuildEnv } from "./mygo-tray-patch";
import config from "../mygo.config";

const args = process.argv.slice(2).filter((a) => a !== "--").flatMap(a => {
 const match = /^(--?platform|--?o)=(.*)$/.exec(a);
 return match ? [match[1]!.replace(/^--/, "-"), match[2]!] : [a.replace(/^--(platform|o)$/, "-$1")];
});
async function run(command: string[], env = process.env) {
  const p = Bun.spawn(command, { stdout: "inherit", stderr: "inherit", env });
  if (await p.exited !== 0) throw new Error(`Build failed: ${command.join(" ")}`);
}
await run(["bun", "x", "mygo", "build", ...args], await trayBuildEnv());
const i = args.indexOf("-platform");
const platforms = i >= 0 ? args[i + 1] : process.platform === "win32" ? `windows/${process.arch === "x64" ? "amd64" : process.arch}` : "";
for (const target of (platforms ?? "").split(",")) {
  if (!target.startsWith("windows/")) continue;
  const arch = target.split("/")[1]!;
  const oi = args.indexOf("-o");
  const dir = resolve(oi >= 0 ? args[oi + 1]! : "build", `windows-${arch}`);
  await rm(`${dir}/Tolk-core.exe`, { force: true });
  await rename(`${dir}/Tolk.exe`, `${dir}/Tolk-core.exe`);
  await run(["go", "build", "-ldflags", `-H windowsgui -X main.version=${config.version}`, "-o", `${dir}/Tolk.exe`, "./cmd/tolk-watch"], { ...process.env, GOOS: "windows", GOARCH: arch, CGO_ENABLED: "0" });
  await rm(`${dir}/Tolk-launcher.exe`, { force: true });
}
