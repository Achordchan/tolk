import { defineConfig } from "mygo-cli";

// The one place the version is written (version.go has a test that checks it).
const version = "2.0.0";
const [major, minor, patch] = version.split(".").map(Number);
// Sparkle compares this number with the build of the app being replaced, so it
// has to stay above bagayalu's builds (141 at 1.4.1) and only ever grow.
const build = String(major * 10000 + minor * 100 + patch);
const mac = process.platform === "darwin";

export default defineConfig({
  name: "Tolk",
  // macOS keeps bagayalu's bundle id so the Accessibility, Screen Recording and
  // Keychain grants of an upgrading user carry over; elsewhere nothing depends
  // on it.
  identifier: mac ? "achord.dazuofanyiguan" : "app.tolk.desktop",
  version,
  copyright: "Copyright © Achord",
  // `mygo dev` runs devCommand and loads devUrl; `mygo build` runs
  // buildCommand and embeds frontendDist into the app.
  devUrl: "http://localhost:5173",
  devCommand: "bun run dev:web",
  buildCommand: "bun run build:web",
  frontendDist: "dist",
  bindings: "src/mygo.ts",
  out: "build",
  macos: {
    minimumSystemVersion: "13.0",
    infoPlist: {
      CFBundleVersion: build,
      CFBundleDisplayName: "Tolk",
      NSScreenCaptureUsageDescription: "Tolk 需要屏幕录制权限，才能截取你框选的区域并识别其中的文字。",
      NSAppleEventsUsageDescription: "Tolk 需要控制其他应用，才能把译文替换回你选中的文字。",
    },
  },
});
