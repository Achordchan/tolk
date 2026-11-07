import AppKit
import SwiftUI

struct ScreenshotSelectionWorkspaceView: View {
    @ObservedObject var session: ScreenshotOCRSession

    @Environment(\.colorScheme) private var colorScheme

    let onCancel: () -> Void
    let onSelectionConfirmed: (CGRect) -> Void
    let onTranslateTapped: () -> Void
    let onExtractTapped: () -> Void
    let onPinTapped: () -> Void
    let onFinishTapped: () -> Void

    @State private var dragStart: CGPoint? = nil
    @State private var dragCurrent: CGPoint? = nil

    @State private var showOCRInfo: Bool = false

    private var palette: ScreenshotPalette { .make(session.appearance, colorScheme) }

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .topLeading) {
                if !session.frozenBackgrounds.isEmpty {
                    frozenBackgroundView(size: geo.size)
                }
                maskView(size: geo.size)

                if session.selectionRect.width > 1, session.selectionRect.height > 1 {
                    selectionBorder(size: geo.size)
                    selectionContent(size: geo.size)
                    if session.stage != .selecting {
                        toolbar(size: geo.size)
                        // 框好以后提示回来，但只在盖不到选区和工具条的地方显示。
                        if let center = selectedHintCenter(containerSize: geo.size) {
                            hint(title: "右键可重新框选", detail: "Esc 退出。")
                                .position(x: center.x, y: center.y)
                        }
                    }
                } else if dragStart == nil {
                    // 按下鼠标开始框选就先收起，免得挡住要框的地方；松手后再回来。
                    let frame = displayFrame(containing: nil, containerSize: geo.size)
                    hint(title: "拖动框选要翻译的区域", detail: "Esc 退出，右键清空选区。")
                        .position(x: frame.midX, y: frame.minY + Self.hintCenterOffset)
                }

                if let toast = session.hudToast,
                   session.selectionRect.width > 1,
                   session.selectionRect.height > 1,
                   session.stage != .selecting {
                    let p = hudToastPosition(containerSize: geo.size)
                    hudToastView(toast)
                        .position(x: p.x, y: p.y)
                        .transition(.move(edge: .top).combined(with: .opacity))
                }
            }
            .contentShape(Rectangle())
            .gesture(session.stage == .selecting ? dragGesture(size: geo.size) : nil)
        }
    }

    private func frozenBackgroundView(size: CGSize) -> some View {
        ZStack(alignment: .topLeading) {
            ForEach(session.frozenBackgrounds) { item in
                let rectTopLeft = rectToTopLeft(rectBottomLeft: item.rect, containerSize: size)
                Image(nsImage: item.image)
                    .resizable()
                    .frame(width: rectTopLeft.width, height: rectTopLeft.height)
                    .position(x: rectTopLeft.midX, y: rectTopLeft.midY)
            }
        }
        .ignoresSafeArea()
    }

    private static let hintCenterOffset: CGFloat = 56
    /// 提示卡片大致的大小，判断会不会盖到选区和工具条时用，宁大勿小。
    private static let hintSize = CGSize(width: 260, height: 64)

    /// 框选完成后提示的位置：选区所在显示器顶部居中；会盖到选区或工具条时返回 nil，不显示。
    private func selectedHintCenter(containerSize: CGSize) -> CGPoint? {
        let selection = displayRectTopLeft(
            baseRectTopLeft: rectToTopLeft(rectBottomLeft: session.selectionRect, containerSize: containerSize),
            containerSize: containerSize
        )
        let frame = displayFrame(containing: selection, containerSize: containerSize)
        let center = CGPoint(x: frame.midX, y: frame.minY + Self.hintCenterOffset)
        let card = CGRect(
            x: center.x - Self.hintSize.width / 2,
            y: center.y - Self.hintSize.height / 2,
            width: Self.hintSize.width,
            height: Self.hintSize.height
        ).insetBy(dx: -8, dy: -8)
        let placement = toolbarPlacement(containerSize: containerSize)
        let height = estimatedToolbarHeight(containerSize: containerSize)
        let toolbar = CGRect(
            x: placement.x,
            y: placement.growsUpward ? placement.y - height : placement.y,
            width: toolbarWidth(containerSize: containerSize),
            height: height
        )
        if card.intersects(selection) || card.intersects(toolbar) { return nil }
        return center
    }

    private func hint(title: String, detail: String) -> some View {
        VStack(spacing: 3) {
            Text(title)
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(palette.fg)
            Text(detail)
                .font(.system(size: 13))
                .foregroundStyle(palette.muted)
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 11)
        .screenshotCard(palette, radius: 12)
    }

    private func dragGesture(size: CGSize) -> some Gesture {
        DragGesture(minimumDistance: 0)
            .onChanged { value in
                let start = dragStart ?? value.startLocation
                dragStart = start
                dragCurrent = value.location

                let rectTopLeft = normalizedRectTopLeft(start: start, current: value.location)
                session.selectionRect = rectToBottomLeft(rectTopLeft: rectTopLeft, containerSize: size)
                // @Published 不做相等性检查，拖拽期间每帧重复赋同一个值会多发一次变更通知。
                if session.stage != .selecting {
                    session.stage = .selecting
                }
            }
            .onEnded { _ in
                defer {
                    dragStart = nil
                    dragCurrent = nil
                }

                let rect = session.selectionRect.integral
                if rect.width < 8 || rect.height < 8 {
                    session.selectionRect = .zero
                    session.stage = .selecting
                    return
                }

                session.selectionRect = rect
                session.stage = .selected
                onSelectionConfirmed(rect)
            }
    }

    private func maskView(size: CGSize) -> some View {
        Canvas { context, canvasSize in
            let full = Path(CGRect(origin: .zero, size: canvasSize))
            context.fill(full, with: .color(Color.black.opacity(0.42)))

            if session.selectionRect.width > 1, session.selectionRect.height > 1 {
                let rectTopLeft = rectToTopLeft(rectBottomLeft: session.selectionRect, containerSize: canvasSize)
                var hole = Path()
                hole.addRect(rectTopLeft)
                context.blendMode = .clear
                context.fill(hole, with: .color(.black))
                context.blendMode = .normal
            }
        }
        .ignoresSafeArea()
    }

    private func selectionBorder(size: CGSize) -> some View {
        let rectTopLeft = rectToTopLeft(rectBottomLeft: session.selectionRect, containerSize: size)
        let rect = displayRectTopLeft(baseRectTopLeft: rectTopLeft, containerSize: size)

        // 边框画在选区外面：选区里的像素就是要复制的内容，不能被盖住。
        return Rectangle()
            .strokeBorder(palette.accent, lineWidth: 2)
            .frame(width: rect.width + 4, height: rect.height + 4)
            .position(x: rect.midX, y: rect.midY)
    }

    private func selectionContent(size: CGSize) -> some View {
        let rectTopLeft = rectToTopLeft(rectBottomLeft: session.selectionRect, containerSize: size)
        let rect = displayRectTopLeft(baseRectTopLeft: rectTopLeft, containerSize: size)
        let isCompare = session.showCompare && session.stage == .translated

        return ZStack(alignment: .topLeading) {
            if isCompare {
                compareInlineOverlay
            } else if session.stage == .translated || session.stage == .translating {
                // 回贴：原截图上抹掉原文、按原位置画上译文，不加卡片。翻译进行中翻好一批画一批，
                // 还没画出来的时候什么都不盖，看到的就是原图。
                if let image = session.translatedImage {
                    Image(nsImage: image)
                        .resizable()
                        .interpolation(.high)
                        .frame(width: rect.width, height: rect.height)
                } else if session.overlayUnavailable {
                    overlayText(text: session.ocrBlocks.map { session.translations[$0.id] ?? $0.text }.joined(separator: "\n"))
                }
            } else if session.stage == .ocrReady {
                // 只提取了原文：列出识别到的文字（已经复制到剪贴板），方便核对。
                overlayText(text: session.ocrText)
            }
        }
        .frame(width: rect.width, height: rect.height)
        .position(x: rect.midX, y: rect.midY)
    }

    private func overlayText(text: String) -> some View {
        ScrollView {
            Text(text)
                .font(.system(size: 15))
                .foregroundStyle(palette.fg)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(14)
        }
        .background(palette.card)
    }

    private var compareInlineOverlay: some View {
        VStack(alignment: .leading, spacing: 0) {
            compareSection(title: "原文", text: session.ocrText, color: palette.muted)
            Rectangle().fill(palette.stroke).frame(height: 1)
            compareSection(title: "译文", text: session.translatedText, color: palette.fg)
        }
        .background(palette.card)
    }

    private func compareSection(title: String, text: String, color: Color) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title)
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(palette.muted)
            ScrollView {
                Text(text)
                    .font(.system(size: 14))
                    .foregroundStyle(color)
                    .lineSpacing(3)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    private func toolbar(size: CGSize) -> some View {
        let placement = toolbarPlacement(containerSize: size)
        let width = toolbarWidth(containerSize: size)
        let compact = width < Self.fullToolbarWidth

        return VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 2) {
                LanguageSearchPicker(
                    title: "源语言",
                    allowAuto: true,
                    options: VisionOCRService.selectableSourceLanguages,
                    selection: $session.sourceLanguageCode,
                    palette: palette,
                    showsTitle: !compact
                )

                Image(systemName: "arrow.right")
                    .font(.system(size: 12, weight: .medium))
                    .foregroundStyle(palette.muted)
                    .frame(width: 26)

                LanguageSearchPicker(
                    title: "目标语言",
                    allowAuto: false,
                    options: LanguagePreset.common,
                    selection: $session.targetLanguageCode,
                    palette: palette,
                    showsTitle: !compact
                )

                Spacer(minLength: 10)

                if !compact {
                    statusLabel
                        .padding(.trailing, 8)
                }

                Button(action: onTranslateTapped) {
                    Label(translateButtonTitle, systemImage: "translate")
                }
                .buttonStyle(ScreenshotButtonStyle(kind: session.stage == .translated ? .soft : .primary, palette: palette, tall: true))
                .disabled(!canTranslate)
            }

            Rectangle()
                .fill(palette.stroke)
                .frame(height: 1)
                .padding(.vertical, 8)

            HStack(spacing: 2) {
                HStack(spacing: 7) {
                    Toggle("原文对照", isOn: $session.showCompare)
                        .labelsHidden()
                        .toggleStyle(.switch)
                        .controlSize(.mini)
                        .tint(palette.accent)
                    Text("原文对照")
                        .font(.system(size: 13))
                        .foregroundStyle(palette.fg)
                }
                .padding(.horizontal, 8)
                .disabled(session.stage != .translated)
                .opacity(session.stage == .translated ? 1 : 0.45)

                Button(action: onExtractTapped) {
                    Label("提取原文", systemImage: "text.viewfinder")
                }
                .buttonStyle(ScreenshotButtonStyle(kind: .ghost, palette: palette, iconOnly: compact))
                .help("提取原文")
                .disabled(!canExtract)

                Button(action: onPinTapped) {
                    Label("钉到屏幕", systemImage: "pin")
                }
                .buttonStyle(ScreenshotButtonStyle(kind: .ghost, palette: palette, iconOnly: compact))
                .help("钉到屏幕")
                // plain 样式加了前景色，禁用时不会自己变淡。
                .disabled(!session.canExport)

                Spacer(minLength: 0)

                Button {
                    showOCRInfo.toggle()
                } label: {
                    Label("说明", systemImage: "info.circle")
                }
                .buttonStyle(ScreenshotButtonStyle(kind: .quiet, palette: palette, iconOnly: compact))
                .help("关于截图 OCR")
                .popover(isPresented: $showOCRInfo, arrowEdge: .top) { ocrInfo }

                Button(action: onFinishTapped) {
                    Label("完成", systemImage: "checkmark")
                }
                .buttonStyle(ScreenshotButtonStyle(kind: session.stage == .translated ? .primary : .soft, palette: palette))
                .disabled(!session.canExport)
            }

            if compact, !statusText.isEmpty || session.stage == .ocrRunning || session.stage == .translating {
                statusLabel
                    .padding(.top, 8)
                    .padding(.horizontal, 8)
            }

            if let failure = failureText {
                // 高度是算出来的，放置工具条时用同一个数；太长的错误在里面滚动，不撑出屏幕。
                ScrollView {
                    Text(failure)
                        .font(Font(Self.failureFont))
                        .foregroundStyle(palette.err)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                }
                .frame(height: failureHeight(toolbarWidth: width))
                .padding(.top, 8)
                .padding(.horizontal, 8)
            }
        }
        .padding(10)
        .frame(width: width)
        .screenshotCard(palette, radius: 14)
        // 贴着选区的那条边锚定：错误提示让工具条变高时，不会盖到选区上。
        // 外层固定大小的框把它留在容器内，否则 ZStack 会把工具条算进自己的尺寸，整层跟着移位。
        .padding(.leading, placement.x)
        .frame(
            width: size.width,
            height: placement.growsUpward ? placement.y : max(0, size.height - placement.y),
            alignment: placement.growsUpward ? .bottomLeading : .topLeading
        )
        .offset(y: placement.growsUpward ? 0 : placement.y)
    }

    @ViewBuilder
    private var statusLabel: some View {
        HStack(spacing: 6) {
            if session.stage == .ocrRunning || session.stage == .translating {
                ProgressView().controlSize(.small).scaleEffect(0.75).frame(width: 16, height: 16)
            }
            Text(statusText)
                .font(.system(size: 13))
                .foregroundStyle(palette.muted)
                .lineLimit(1)
        }
    }

    private var statusText: String {
        switch session.stage {
        case .selected: return "已框选"
        case .translated: return "翻译完成"
        // 进行中按钮上已经写着；提取原文有提示条；失败写在工具条最下面。
        case .selecting, .ocrRunning, .ocrReady, .translating, .failed: return ""
        }
    }

    private var ocrInfo: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("关于截图 OCR")
                .font(.system(size: 14, weight: .bold))

            Text("本功能使用 Apple Vision（本地免费 OCR）进行文字识别。")
                .font(.system(size: 13, weight: .medium))

            Text("免费 OCR 的准确度会受截图清晰度、字体、背景、语言等影响，可能出现漏字/错字/断行。")
                .font(.system(size: 13, weight: .medium))
                .foregroundStyle(.secondary)

            Text("更推荐：优先用 Cmd+C+C（复制文字翻译），通常更稳定、更准确。")
                .font(.system(size: 13, weight: .semibold))

            Divider()

            Text("小技巧：右键可清空选区或退出。")
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(.secondary)
        }
        .padding(14)
        .frame(width: 320)
    }

    private func copyToPasteboard(_ text: String) {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty else { return }
        let pb = NSPasteboard.general
        pb.clearContents()
        pb.setString(t, forType: .string)
    }

    private var canTranslate: Bool {
        switch session.stage {
        case .selected:
            return session.capturedImage != nil
        case .ocrReady, .translated:
            return !session.ocrText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        case .translating, .ocrRunning:
            return false
        default:
            return false
        }
    }

    private var canExtract: Bool {
        switch session.stage {
        case .selected:
            return session.capturedImage != nil
        case .ocrReady, .translated:
            return true
        case .translating, .ocrRunning:
            return false
        default:
            return false
        }
    }

    private var translateButtonTitle: String {
        switch session.stage {
        case .ocrRunning:
            return "识别中…"
        case .translating:
            return "正在翻译…"
        case .translated:
            return "重新翻译"
        default:
            return "翻译"
        }
    }

    private static let fullToolbarWidth: CGFloat = 690
    private static let toolbarMargin: CGFloat = 10

    /// 通常是完整宽度；显示器放不下时收窄，语言不写标题，次要操作只留图标。
    private func toolbarWidth(containerSize: CGSize) -> CGFloat {
        let selection = rectToTopLeft(rectBottomLeft: session.selectionRect, containerSize: containerSize)
        let frame = displayFrame(containing: selection, containerSize: containerSize)
        return min(Self.fullToolbarWidth, frame.width - Self.toolbarMargin * 2)
    }

    private struct ToolbarPlacement {
        let x: CGFloat
        /// `growsUpward` 时是工具条底边，否则是顶边。
        let y: CGFloat
        let growsUpward: Bool
    }

    private static let failureFont = NSFont.systemFont(ofSize: 13)

    private var failureText: String? {
        if case .failed(let message) = session.stage {
            return "\(message)。可以重新框选，或调整源语言后再试。"
        }
        return nil
    }

    /// 错误文字占的高度，最多显示五行。
    private func failureHeight(toolbarWidth: CGFloat) -> CGFloat {
        guard let failureText else { return 0 }
        let bounds = (failureText as NSString).boundingRect(
            with: CGSize(width: toolbarWidth - 36, height: .greatestFiniteMagnitude),
            options: [.usesLineFragmentOrigin, .usesFontLeading],
            attributes: [.font: Self.failureFont]
        )
        let lineHeight = ceil(Self.failureFont.ascender - Self.failureFont.descender + Self.failureFont.leading)
        return min(ceil(bounds.height) + 2, lineHeight * 5 + 2)
    }

    private func estimatedToolbarHeight(containerSize: CGSize) -> CGFloat {
        var height: CGFloat = 102
        if failureText != nil { height += 8 + failureHeight(toolbarWidth: toolbarWidth(containerSize: containerSize)) }
        if toolbarWidth(containerSize: containerSize) < Self.fullToolbarWidth { height += 26 }
        return height
    }

    /// 选区下方优先，放不下放上方，再放不下贴着显示器底边。始终留在选区所在的显示器内。
    private func toolbarPlacement(containerSize: CGSize) -> ToolbarPlacement {
        // 对照时选区的显示范围会变大（见 displayRectTopLeft），工具条要让开。
        let selection = displayRectTopLeft(
            baseRectTopLeft: rectToTopLeft(rectBottomLeft: session.selectionRect, containerSize: containerSize),
            containerSize: containerSize
        )
        let frame = displayFrame(containing: selection, containerSize: containerSize)
        let width = toolbarWidth(containerSize: containerSize)
        let height = estimatedToolbarHeight(containerSize: containerSize)
        let gap = Self.toolbarMargin

        let x = clamp(selection.minX, min: frame.minX + gap, max: frame.maxX - width - gap)
        if selection.maxY + gap + height <= frame.maxY - gap {
            return ToolbarPlacement(x: x, y: selection.maxY + gap, growsUpward: false)
        }
        if selection.minY - gap - height >= frame.minY + gap {
            return ToolbarPlacement(x: x, y: selection.minY - gap, growsUpward: true)
        }
        return ToolbarPlacement(x: x, y: frame.maxY - gap, growsUpward: true)
    }

    /// 窗口铺满所有显示器，这里算出一个显示器的可用区域（SwiftUI 坐标）。不传选区时取鼠标所在的显示器。
    private func displayFrame(containing selection: CGRect?, containerSize: CGSize) -> CGRect {
        let whole = CGRect(origin: .zero, size: containerSize)
        let virtual = ScreenshotSelectionWindow.fullVirtualScreenFrame()
        guard abs(virtual.width - containerSize.width) < 1, abs(virtual.height - containerSize.height) < 1 else {
            return whole
        }
        let anchor: CGPoint
        if let selection {
            anchor = CGPoint(x: virtual.minX + selection.midX, y: virtual.maxY - selection.midY)
        } else {
            anchor = NSEvent.mouseLocation
        }
        guard let screen = NSScreen.screens.first(where: { $0.frame.contains(anchor) }) ?? NSScreen.main else {
            return whole
        }
        let visible = screen.visibleFrame
        return CGRect(x: visible.minX - virtual.minX, y: virtual.maxY - visible.maxY, width: visible.width, height: visible.height)
    }

    private func clamp(_ v: CGFloat, min: CGFloat, max: CGFloat) -> CGFloat {
        if v < min { return min }
        if v > max { return max }
        return v
    }

    private func displayRectTopLeft(baseRectTopLeft: CGRect, containerSize: CGSize) -> CGRect {
        // 仅用于显示：对照模式下，把选区显示区域向下扩展一些，方便同时看原文+译文。
        // 注意：不修改 session.selectionRect，避免影响实际截图区域。
        let isCompare = session.showCompare && session.stage == .translated
        guard isCompare else { return baseRectTopLeft }

        let margin: CGFloat = 12
        let desiredMinHeight: CGFloat = 320

        let availableHeight = max(0, containerSize.height - baseRectTopLeft.minY - margin)
        let newHeight = min(max(baseRectTopLeft.height, desiredMinHeight), availableHeight)
        return CGRect(x: baseRectTopLeft.minX, y: baseRectTopLeft.minY, width: baseRectTopLeft.width, height: newHeight)
    }

    /// 工具条外侧优先；那边放不下就放到工具条另一侧，并始终留在显示器内。
    private func hudToastPosition(containerSize: CGSize) -> CGPoint {
        let selection = rectToTopLeft(rectBottomLeft: session.selectionRect, containerSize: containerSize)
        let frame = displayFrame(containing: selection, containerSize: containerSize)
        let placement = toolbarPlacement(containerSize: containerSize)
        let height = estimatedToolbarHeight(containerSize: containerSize)
        let halfHeight: CGFloat = 20
        let margin: CGFloat = 8
        let top = placement.growsUpward ? placement.y - height : placement.y
        let bottom = placement.growsUpward ? placement.y : placement.y + height
        let above = top - margin - halfHeight
        let below = bottom + margin + halfHeight
        let preferred = placement.growsUpward ? above : below
        let fallback = placement.growsUpward ? below : above
        let fits = preferred - halfHeight >= frame.minY && preferred + halfHeight <= frame.maxY
        return CGPoint(
            x: placement.x + toolbarWidth(containerSize: containerSize) / 2,
            y: clamp(fits ? preferred : fallback, min: frame.minY + halfHeight, max: frame.maxY - halfHeight)
        )
    }

    private func normalizedRectTopLeft(start: CGPoint, current: CGPoint) -> CGRect {
        let x1 = min(start.x, current.x)
        let x2 = max(start.x, current.x)
        let y1 = min(start.y, current.y)
        let y2 = max(start.y, current.y)
        return CGRect(x: x1, y: y1, width: x2 - x1, height: y2 - y1)
    }

    // SwiftUI(左上原点) -> AppKit 内容坐标(左下原点)
    private func rectToBottomLeft(rectTopLeft: CGRect, containerSize: CGSize) -> CGRect {
        CGRect(
            x: rectTopLeft.minX,
            y: containerSize.height - rectTopLeft.maxY,
            width: rectTopLeft.width,
            height: rectTopLeft.height
        )
    }

    // AppKit 内容坐标(左下原点) -> SwiftUI(左上原点)
    private func rectToTopLeft(rectBottomLeft: CGRect, containerSize: CGSize) -> CGRect {
        CGRect(
            x: rectBottomLeft.minX,
            y: containerSize.height - rectBottomLeft.maxY,
            width: rectBottomLeft.width,
            height: rectBottomLeft.height
        )
    }

    private func hudToastView(_ toast: ScreenshotOCRSession.HUDToast) -> some View {
        HStack(spacing: 8) {
            Image(systemName: hudIcon(for: toast.style))
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(hudColor(for: toast.style))
            Text(toast.message)
                .font(.system(size: 13))
                .foregroundStyle(palette.fg)
                .lineLimit(2)
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 9)
        .screenshotCard(palette, radius: 10)
        .animation(.spring(response: 0.35, dampingFraction: 0.9), value: toast.id)
    }

    private func hudIcon(for style: ScreenshotOCRSession.HUDToast.Style) -> String {
        switch style {
        case .success: return "checkmark.circle.fill"
        case .info: return "info.circle.fill"
        case .warning: return "exclamationmark.triangle.fill"
        case .error: return "xmark.octagon.fill"
        }
    }

    private func hudColor(for style: ScreenshotOCRSession.HUDToast.Style) -> Color {
        switch style {
        case .success: return palette.ok
        case .info: return palette.accent
        case .warning: return .orange
        case .error: return palette.err
        }
    }
}
