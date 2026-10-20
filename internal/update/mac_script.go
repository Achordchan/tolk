package update

// macApplyScript swaps the app bundle once Tolk has quit. Paths and the pid
// come in through the environment. The old bundle is kept as a backup and put
// back if anything fails, so a bad update never leaves the user without an app.
const macApplyScript = `#!/bin/sh
LOG="$TOLK_UPDATE_LOG"
log() { printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$1" >> "$LOG"; }
result() { printf '{"status":"%s","message":"%s","expected_version":"%s"}\n' "$1" "$2" "$TOLK_UPDATE_VERSION" > "$TOLK_UPDATE_RESULT"; }

log "Update started."
i=0
while kill -0 "$TOLK_UPDATE_PID" 2>/dev/null; do
  i=$((i + 1))
  if [ "$i" -gt 120 ]; then
    log "Timed out waiting for process $TOLK_UPDATE_PID; forcing termination."
    kill -9 "$TOLK_UPDATE_PID" 2>/dev/null
    sleep 1
    break
  fi
  sleep 0.5
done

restore() {
  log "Restoring previous version: $1"
  rm -rf "$TOLK_UPDATE_TARGET"
  if mv "$TOLK_UPDATE_BACKUP" "$TOLK_UPDATE_TARGET"; then
    result failed "$1"
    open "$TOLK_UPDATE_TARGET"
  else
    log "Restore failed; backup stays at $TOLK_UPDATE_BACKUP"
    result failed "$1（旧版本备份保存在 $TOLK_UPDATE_BACKUP）"
  fi
  exit 1
}

rm -rf "$TOLK_UPDATE_BACKUP"
if ! mv "$TOLK_UPDATE_TARGET" "$TOLK_UPDATE_BACKUP"; then
  log "Could not move the old app away; nothing was changed."
  result failed "无法替换当前应用，请确认应用所在文件夹可写"
  open "$TOLK_UPDATE_TARGET"
  exit 1
fi
if ! ditto "$TOLK_UPDATE_NEW" "$TOLK_UPDATE_TARGET"; then
  restore "复制新版本失败"
fi
xattr -dr com.apple.quarantine "$TOLK_UPDATE_TARGET" 2>/dev/null
if ! codesign --verify --deep --strict "$TOLK_UPDATE_TARGET" 2>>"$LOG"; then
  restore "新版本签名校验未通过"
fi
log "Update completed."
result success "Update completed."
open "$TOLK_UPDATE_TARGET"
exit 0
`
