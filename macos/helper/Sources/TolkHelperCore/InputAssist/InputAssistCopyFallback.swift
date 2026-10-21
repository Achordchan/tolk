import Foundation

// 「复制取词」兜底的纯判定逻辑。
// AX 读不到选区的应用（微信等自绘界面）改用模拟 ⌘C / ⌘V；这里只放可以脱离 AppKit 单测的规则。

public enum InputAssistCopyFallbackLogic {
    /// 模拟 ⌘C 之后对剪贴板的判定结果。
    public enum CopyResult: Equatable {
        /// 剪贴板确实被这次复制改写了，且读到了可用文字。
        case copied(String)
        /// changeCount 没变：目标应用没有写剪贴板（没有选区、或不响应 ⌘C）。
        /// 此时剪贴板里的是用户原有的内容，**绝不能**当成选区。
        case clipboardUnchanged
        /// 检测到变化之后剪贴板又变了：有别的写入者（剪贴板管理器、通用剪贴板等）在同一窗口里动过它，
        /// 没法确定里面的文字是不是用户选中的。不翻译、也不去覆盖它。
        case contended
        /// 剪贴板变了，但读不到文字（比如复制出来的是图片）或只有空白。
        case noText
    }

    /// 判断「复制是否成功」。成功与否只看 changeCount 有没有变化，
    /// 不看内容是否和旧剪贴板相同：用户原本就复制过同一段文字时，内容一样但 changeCount 仍会变。
    public static func judgeCopy(
        changeCountBefore: Int,
        changeCountNow: Int,
        pasteboardText: String?
    ) -> CopyResult {
        guard changeCountNow != changeCountBefore else { return .clipboardUnchanged }
        guard let text = pasteboardText,
              !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        else {
            return .noText
        }
        return .copied(text)
    }

    /// 带稳定性检查的判定：检测到 changeCount 变化后再等一小段时间，期间如果又变了，
    /// 说明不只一个写入者，当作争用处理，宁可不取词。
    public static func judgeCopy(
        changeCountBefore: Int,
        changeCountAtDetection: Int,
        changeCountAfterSettle: Int,
        pasteboardText: String?
    ) -> CopyResult {
        guard changeCountAtDetection != changeCountBefore else { return .clipboardUnchanged }
        guard changeCountAfterSettle == changeCountAtDetection else { return .contended }
        return judgeCopy(
            changeCountBefore: changeCountBefore,
            changeCountNow: changeCountAtDetection,
            pasteboardText: pasteboardText
        )
    }

    /// 兜底来源的选区没有 AX 元素，做不了「全文没变」的校验，守卫只剩前台进程和安全输入两条。
    public enum TargetVerdict: Equatable {
        case allowed
        case secureInputActive
        case applicationChanged
    }

    /// 复制取词之后、以及粘贴之前，都用它确认目标还是取词时批准的那个进程。
    /// 任何一边拿不到进程号都当作变了（与 `InputAssistReplacementSafetyGuard.isSameProcess` 一致）。
    public static func validateTarget(
        expectedProcessIdentifier: Int32?,
        currentProcessIdentifier: Int32?,
        isSecureEventInputEnabled: Bool
    ) -> TargetVerdict {
        guard !isSecureEventInputEnabled else { return .secureInputActive }
        guard let expected = expectedProcessIdentifier,
              let current = currentProcessIdentifier,
              expected == current
        else {
            return .applicationChanged
        }
        return .allowed
    }
}
