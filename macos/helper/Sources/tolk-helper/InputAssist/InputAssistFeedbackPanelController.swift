import AppKit
import SwiftUI
import TolkHelperCore

private struct InputAssistFeedbackView: View {
    let message: String
    let systemImage: String

    var body: some View {
        Label(message, systemImage: systemImage)
            .font(.system(size: 12, weight: .medium))
            .padding(.horizontal, 12)
            .padding(.vertical, 9)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.regularMaterial)
    }
}

/// 在原选区附近显示短暂反馈，不抢目标 App 焦点。
///
/// helper 没有主窗口，也没有 ToastCenter：老版弹 toast 的每一处，这里都用同一个小浮层显示。
@MainActor
final class InputAssistFeedbackPanelController {
    /// 提示的语气，决定图标。
    enum Style {
        case success
        case info
        case warning
        /// 复制了译文（图标是「复制」）。
        case copied

        var systemImage: String {
            switch self {
            case .success: return "checkmark"
            case .info: return "info.circle"
            case .warning: return "exclamationmark.triangle"
            case .copied: return "doc.on.doc"
            }
        }
    }

    private var panel: NSPanel?
    private var dismissTask: Task<Void, Never>?

    func show(
        message: String,
        style: Style,
        anchorRect: CGRect,
        appearance: InputAssistAppearance?
    ) {
        dismissTask?.cancel()
        panel?.orderOut(nil)

        // 提示文字长短不一，宽度按字数估一下，避免被截断。
        let width = min(420, max(220, CGFloat(message.count) * 13 + 56))
        let size = CGSize(width: width, height: 38)
        let panel = NSPanel(
            contentRect: CGRect(origin: .zero, size: size),
            styleMask: [.borderless, .nonactivatingPanel],
            backing: .buffered,
            defer: false
        )
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = true
        panel.level = .popUpMenu
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .ignoresCycle]
        panel.hidesOnDeactivate = false
        panel.isReleasedWhenClosed = false
        panel.ignoresMouseEvents = true
        panel.appearance = appearance?.nsAppearance
        panel.contentView = NSHostingView(
            rootView: InputAssistFeedbackView(message: message, systemImage: style.systemImage)
                .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
        )

        InputAssistScreen.place(panel, anchorRect: anchorRect, size: size)
        panel.orderFrontRegardless()
        self.panel = panel

        dismissTask = Task { @MainActor [weak self, weak panel] in
            try? await Task.sleep(nanoseconds: 2_400_000_000)
            guard !Task.isCancelled else { return }
            panel?.orderOut(nil)
            if self?.panel === panel {
                self?.panel = nil
            }
        }
    }

    func hide() {
        dismissTask?.cancel()
        dismissTask = nil
        panel?.orderOut(nil)
        panel = nil
    }
}
