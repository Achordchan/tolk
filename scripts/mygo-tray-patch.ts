import { cp, chmod, mkdtemp, mkdir, readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

/** A build-local module copy keeps the pinned upstream cache immutable and the patch reproducible. */
export async function trayBuildEnv() {
  const p = Bun.spawn(["go", "list", "-m", "-f", "{{.Dir}}", "github.com/egoist/mygo"], { stdout: "pipe" });
  const root = (await new Response(p.stdout).text()).trim();
  if (await p.exited !== 0) throw new Error("Cannot locate MyGo module");
  const source = resolve(root, "internal/windows/tray.go");
  let text = await readFile(source, "utf8");
  function replace(old: string, next: string) {
    if (!text.includes(old)) throw new Error("MyGo tray patch no longer matches; review upstream before building");
    text = text.replace(old, next);
  }
  replace('"image/png"', '"image/png"\n "log"');
  replace('\tt.add()\n\treturn t, nil', '\t// Register only after SetImage supplies a valid icon.\n\treturn t, nil');
  replace(`func (t *tray) add() {
\td := t.data(0)
\tprocShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&d)))
\tt.added = true
}`, `func (t *tray) add() error {
 d := t.data(0)
 ok, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&d)))
 t.added = ok != 0
 if !t.added {
  e := fmt.Errorf("Shell_NotifyIconW(NIM_ADD) failed: %v", err)
  log.Printf("tray registration failed: %v", e)
  return e
 }
 log.Printf("tray registration succeeded: id=%d", t.id)
 return nil
}`);
  replace(`func (t *tray) update() {
\td := t.data(0)
\tprocShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&d)))
}`, `func (t *tray) update() error {
 if !t.added { return t.add() }
 d := t.data(0)
 ok, _, err := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&d)))
 if ok == 0 {
  log.Printf("tray update failed: %v; registering again", err)
  t.added = false
  return t.add()
 }
 return nil
}`);
  replace('\tt.update()\n\treturn nil', '\tif err := t.update(); err != nil { t.Destroy(); return err }; return nil');
  const dir = resolve(".mygo/tray-patch");
  await mkdir(dir, { recursive: true });
  const localModule = await mkdtemp(resolve(dir, "mygo-build-"));
  await cp(root, localModule, { recursive: true, force: true });
  const patched = resolve(localModule, "internal/windows/tray.go");
  await chmod(patched, 0o644);
  await writeFile(patched, text);
  const modfile = resolve(dir, "build.mod");
  await writeFile(modfile, (await readFile("go.mod", "utf8")) + `\nreplace github.com/egoist/mygo => ${JSON.stringify(localModule)}\n`);
  await writeFile(resolve(dir, "build.sum"), await readFile("go.sum", "utf8"));
  return { ...process.env, GOFLAGS: `${process.env.GOFLAGS ?? ""} -modfile=${modfile}`.trim() };
}
