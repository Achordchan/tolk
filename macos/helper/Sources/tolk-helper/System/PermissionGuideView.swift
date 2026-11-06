import AppKit
import SwiftUI
import TolkHelperCore

/// 权限引导：和输入助手的试用同一种语言。上面是着色舞台，下面一句话，一次只讲一项权限、只做一件事。
/// 每一项分两步：先打开系统设置，再把图标拖进去。窗口全程一个高度，翻页时按钮不挪位置。
struct PermissionGuideView: View {
    static let windowSize = CGSize(width: 520, height: 496)

    let needsAccessibility: Bool
    let needsScreenRecording: Bool
    let showsScreenRecordingPermission: Bool
    /// 屏幕录制在系统设置里已经打开，但要 Tolk 重新打开一次才生效。
    let screenRecordingNeedsRelaunch: Bool
    let appearance: InputAssistAppearance

    let onOpenAccessibility: () -> Void
    let onOpenScreenRecording: () -> Void
    let onClose: () -> Void
    /// 让 Tolk 重新打开；失败时回一句给用户看的原因。
    let onRelaunch: () async -> String?

    fileprivate enum Page: Hashable {
        case accessibility, screenRecording, done
    }

    private enum Question {
        case withoutPermission, stillNotGranted
    }

    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var page: Page
    /// 已经点过「打开…设置」的项：这之后舞台才开始演示拖动。
    @State private var opened: Set<Page> = []
    @State private var question: Question?
    @State private var movingForward = true
    @State private var appeared = false
    @State private var relaunching = false
    @State private var relaunchError: String?

    init(
        needsAccessibility: Bool,
        needsScreenRecording: Bool,
        showsScreenRecordingPermission: Bool,
        screenRecordingNeedsRelaunch: Bool,
        appearance: InputAssistAppearance,
        onOpenAccessibility: @escaping () -> Void,
        onOpenScreenRecording: @escaping () -> Void,
        onClose: @escaping () -> Void,
        onRelaunch: @escaping () async -> String?
    ) {
        self.needsAccessibility = needsAccessibility
        self.needsScreenRecording = needsScreenRecording
        self.showsScreenRecordingPermission = showsScreenRecordingPermission
        self.screenRecordingNeedsRelaunch = screenRecordingNeedsRelaunch
        self.appearance = appearance
        self.onOpenAccessibility = onOpenAccessibility
        self.onOpenScreenRecording = onOpenScreenRecording
        self.onClose = onClose
        self.onRelaunch = onRelaunch
        let first: Page
        if needsAccessibility {
            first = .accessibility
        } else if showsScreenRecordingPermission && needsScreenRecording {
            first = .screenRecording
        } else {
            first = .done
        }
        _page = State(initialValue: first)
    }

    private var palette: ScreenshotPalette { .make(appearance, colorScheme) }

    private var steps: [Page] {
        showsScreenRecordingPermission ? [.accessibility, .screenRecording] : [.accessibility]
    }

    private func isGranted(_ step: Page) -> Bool {
        switch step {
        case .accessibility: return !needsAccessibility
        case .screenRecording: return !needsScreenRecording
        case .done: return true
        }
    }

    /// 当前这一项之外，下一个还没授权的；没有了就是 nil。
    private var otherMissing: Page? {
        let start = (steps.firstIndex(of: page) ?? -1) + 1
        let ordered = steps[start...] + steps[..<min(start, steps.count)]
        return ordered.first { $0 != page && !isGranted($0) }
    }

    private var waitingForDrag: Bool { opened.contains(page) }

    /// 屏幕录制这一页：已经打开、只差重新打开 Tolk。
    private var needsRelaunch: Bool {
        page == .screenRecording && needsScreenRecording && screenRecordingNeedsRelaunch
    }

    /// 主按钮换成「重新打开 Tolk」：检测到只差重新打开时；或者检测不出来、用户点开了「已经开启，为什么还是未授权？」时。
    private var offersRelaunch: Bool {
        needsRelaunch || (page == .screenRecording && needsScreenRecording && question == .stillNotGranted)
    }

    /// 已授权或只差重新打开时，这一页不再需要常见问题和「跳过」。
    private var pageSettled: Bool { isGranted(page) || needsRelaunch }

    var body: some View {
        VStack(spacing: 0) {
            stage
            copy
            stepDots
            actions
        }
        .frame(width: Self.windowSize.width, height: Self.windowSize.height)
        .background(palette.card)
        .foregroundStyle(palette.fg)
        .ignoresSafeArea()
        .onAppear {
            if reduceMotion {
                appeared = true
            } else {
                withAnimation(.easeOut(duration: 0.24)) { appeared = true }
            }
        }
        .task(id: AdvanceKey(page: page, needsAccessibility: needsAccessibility, needsScreenRecording: needsScreenRecording)) {
            await advanceIfGranted()
        }
    }

    // MARK: - 翻页

    /// 页面或授权状态一变，上一次的等待就被取消，这里按最新状态重新判断。
    private struct AdvanceKey: Equatable {
        let page: Page
        let needsAccessibility: Bool
        let needsScreenRecording: Bool
    }

    /// 这一项一授权就留一会儿让人看见结果，再自动翻到下一项；用户也可以直接点「继续」。
    /// 完成页上权限又被关掉，就回到缺的那一项。
    private func advanceIfGranted() async {
        if page == .done {
            if let missing = steps.first(where: { !isGranted($0) }) { go(to: missing) }
            return
        }
        guard isGranted(page) else { return }
        try? await Task.sleep(nanoseconds: 1_300_000_000)
        guard !Task.isCancelled else { return }
        go(to: otherMissing ?? .done)
    }

    private func go(to next: Page) {
        let from = steps.firstIndex(of: page) ?? steps.count
        let to = steps.firstIndex(of: next) ?? steps.count
        // 方向要在翻页之前落定，被换下去的那一页才会朝同一边走。
        movingForward = to >= from
        relaunchError = nil
        DispatchQueue.main.async {
            withAnimation(reduceMotion ? .easeOut(duration: 0.01) : .easeOut(duration: 0.22)) {
                question = nil
                page = next
            }
        }
    }

    private var pageTransition: AnyTransition {
        guard !reduceMotion else { return .opacity }
        return .asymmetric(
            insertion: .offset(x: movingForward ? 22 : -22).combined(with: .opacity),
            removal: .offset(x: movingForward ? -22 : 22).combined(with: .opacity)
        )
    }

    private func markOpened() {
        guard page != .done, !opened.contains(page) else { return }
        withAnimation(.easeOut(duration: 0.18)) { _ = opened.insert(page) }
    }

    // MARK: - 舞台

    private var stage: some View {
        ZStack {
            Group {
                switch page {
                case .accessibility:
                    PermissionStage(
                        symbol: "accessibility",
                        name: "辅助功能",
                        isGranted: !needsAccessibility,
                        waitingForDrag: waitingForDrag,
                        palette: palette,
                        onDroppedHere: markOpened
                    )
                case .screenRecording:
                    PermissionStage(
                        symbol: "rectangle.dashed.badge.record",
                        name: "屏幕录制",
                        isGranted: !needsScreenRecording || screenRecordingNeedsRelaunch,
                        grantedLabel: needsScreenRecording ? "待重新打开" : "已授权",
                        waitingForDrag: waitingForDrag,
                        palette: palette,
                        onDroppedHere: markOpened
                    )
                case .done:
                    doneStage
                }
            }
            .id(page)
            .transition(pageTransition)
        }
        .opacity(appeared ? 1 : 0)
        .offset(y: appeared ? 0 : 6)
        .padding(.top, 20) // 让开红绿灯
        .frame(maxWidth: .infinity)
        .frame(height: 256)
        .background(palette.stage)
    }

    private var doneStage: some View {
        ZStack(alignment: .bottomTrailing) {
            Image(nsImage: ParentApp.icon)
                .resizable()
                .scaledToFit()
                .frame(width: 104, height: 104)
                .shadow(color: .black.opacity(0.16), radius: 14, x: 0, y: 8)
            DrawnCheck(palette: palette, animated: !reduceMotion)
                .offset(x: 6, y: 6)
        }
        .accessibilityHidden(true)
    }

    // MARK: - 文字

    private var copy: some View {
        ZStack {
            VStack(spacing: 8) {
                ZStack {
                    VStack(spacing: 8) {
                        Text(heading)
                            .font(.system(size: 23, weight: .semibold))
                            .tracking(-0.2)
                        Text(description)
                            .font(.system(size: 14))
                            .foregroundStyle(relaunchError == nil ? palette.muted : palette.err)
                            .lineSpacing(5)
                            .multilineTextAlignment(.center)
                            .fixedSize(horizontal: false, vertical: true)
                            .frame(minHeight: 48, alignment: .top)
                    }
                    .id(copyKey)
                    .transition(.opacity)
                }
                footnote
            }
            .id(page)
            .transition(pageTransition)
        }
        .padding(.horizontal, 24)
        .padding(.top, 20)
        .frame(maxWidth: .infinity, alignment: .top)
        .frame(height: 148, alignment: .top)
    }

    private var copyKey: String {
        "\(question.map { "\($0)" } ?? "-")|\(waitingForDrag)|\(isGranted(page))|\(needsRelaunch)|\(relaunchError ?? "")"
    }

    private var heading: String {
        switch question {
        case .withoutPermission: return "暂不授权还能用吗？"
        case .stillNotGranted: return "已经开启，为什么还是未授权？"
        case nil: break
        }
        if needsRelaunch { return "重新打开 Tolk 就能用了" }
        switch page {
        case .accessibility:
            return needsAccessibility ? "允许 Tolk 使用辅助功能" : "辅助功能已开启"
        case .screenRecording:
            return needsScreenRecording ? "允许 Tolk 录制屏幕" : "屏幕录制已开启"
        case .done:
            return "权限都开好了"
        }
    }

    private var description: String {
        if let relaunchError { return relaunchError }
        switch question {
        case .withoutPermission:
            return page == .screenRecording
                ? "可以。只有截图翻译和截图取字用不了，\n其他功能都不受影响。"
                : "可以。复制文字后在 Tolk 里翻译照常能用，\n只是无法复制的网页或控件翻译不了。"
        case .stillNotGranted:
            return page == .screenRecording
                ? "屏幕录制打开后，要重新打开 Tolk 才会生效，点下面的按钮就行。\n重新打开后还是这样，就在系统设置里删掉旧条目再拖一次。"
                : "通常是旧版本留下了一条对不上的权限记录。\n在系统设置里删掉旧条目，再把左边的图标拖进去并打开开关。"
        case nil:
            break
        }
        let next = "先打开系统设置，下一步把图标拖进去。"
        let drag = "系统设置窗口下方会跟着出现 Tolk 图标，\n把它拖进列表、打开开关，这里会自动变成已授权。"
        switch page {
        case .accessibility:
            if !needsAccessibility { return "选中文字连按两次 ⌘C 就能翻译了。" }
            if waitingForDrag { return drag }
            return showsScreenRecordingPermission
                ? "选中文字连按两次 ⌘C 就能翻译，截图快捷键也要用它。\n\(next)"
                : "在任何软件里选中文字，连按两次 ⌘C 就能翻译。\n\(next)"
        case .screenRecording:
            if !needsScreenRecording { return "截图翻译和截图取字可以用了。" }
            if needsRelaunch { return "屏幕录制已经打开，macOS 要 Tolk 重新打开一次才会生效。\n重新打开后，这里会接着往下走。" }
            if waitingForDrag { return "系统设置窗口下方会跟着出现 Tolk 图标，\n把它拖进列表、打开开关，再按提示重新打开 Tolk。" }
            return "截图翻译和截图取字要用它，Tolk 只读取你框选的区域。\n\(next)"
        case .done:
            return showsScreenRecordingPermission
                ? "全局快捷翻译和截图翻译均可正常使用。"
                : "全局快捷翻译已可正常使用。"
        }
    }

    /// 正文下面的一行：平时是两个常见问题；点开一个后换成「返回」；完成页上是两项的状态。
    @ViewBuilder
    private var footnote: some View {
        if page == .done {
            HStack(spacing: 8) {
                ForEach(steps, id: \.self) { step in
                    StatusChip(text: step.stepName, isGranted: true, palette: palette)
                }
            }
            .padding(.top, 2)
            .accessibilityElement(children: .combine)
        } else if question != nil {
            Button {
                withAnimation(.easeOut(duration: 0.14)) { question = nil }
            } label: {
                Label("回到操作步骤", systemImage: "chevron.left")
            }
            .buttonStyle(GuideButtonStyle(kind: .link, palette: palette))
        } else {
            HStack(spacing: 4) {
                questionButton("暂不授权还能用吗？", .withoutPermission)
                questionButton("已经开启，为什么还是未授权？", .stillNotGranted)
            }
            .opacity(pageSettled ? 0 : 1)
            .allowsHitTesting(!pageSettled)
            .accessibilityHidden(pageSettled)
        }
    }

    private func questionButton(_ text: String, _ topic: Question) -> some View {
        Button(text) {
            withAnimation(.easeOut(duration: 0.14)) { question = topic }
        }
        .buttonStyle(GuideButtonStyle(kind: .link, palette: palette))
    }

    // MARK: - 进度和按钮

    private var stepDots: some View {
        HStack(spacing: 7) {
            ForEach(steps, id: \.self) { step in
                Capsule()
                    .fill(dotColor(step))
                    .frame(width: step == page ? 20 : 7, height: 7)
            }
        }
        .padding(.top, 14)
        .padding(.bottom, 6)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: page)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: needsAccessibility)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: needsScreenRecording)
        .accessibilityElement()
        .accessibilityLabel("已完成 \(steps.filter(isGranted).count) 项，共 \(steps.count) 项")
    }

    private func dotColor(_ step: Page) -> Color {
        if isGranted(step) { return palette.ok }
        return step == page ? palette.accent : palette.controlStroke
    }

    private var actions: some View {
        HStack(spacing: 8) {
            if page != .done {
                Button("稍后处理", action: onClose)
                    .buttonStyle(GuideButtonStyle(kind: .quiet, palette: palette))
                    .keyboardShortcut(.cancelAction)
                    .padding(.leading, -8)
            }

            Spacer()

            if page != .done, !pageSettled, let otherMissing {
                Button("跳过这一项") { go(to: otherMissing) }
                    .buttonStyle(GuideButtonStyle(kind: .ghost, palette: palette))
            }

            Button(primaryTitle, action: primaryAction)
                .buttonStyle(GuideButtonStyle(kind: .primary, palette: palette))
                .keyboardShortcut(.defaultAction)
                .disabled(relaunching)
        }
        .padding(.horizontal, 24)
        .padding(.top, 10)
        .padding(.bottom, 20)
    }

    private var primaryTitle: String {
        if offersRelaunch { return relaunching ? "正在重新打开…" : "重新打开 Tolk" }
        switch page {
        case .accessibility where needsAccessibility:
            return "打开辅助功能设置"
        case .screenRecording where needsScreenRecording:
            return "打开屏幕录制设置"
        case .done:
            return "完成"
        default:
            return "继续"
        }
    }

    private func primaryAction() {
        if offersRelaunch {
            relaunch()
            return
        }
        switch page {
        case .accessibility where needsAccessibility:
            onOpenAccessibility()
            markOpened()
        case .screenRecording where needsScreenRecording:
            onOpenScreenRecording()
            markOpened()
        case .done:
            onClose()
        default:
            go(to: otherMissing ?? .done)
        }
    }
}

extension PermissionGuideView {
    private func relaunch() {
        relaunching = true
        relaunchError = nil
        Task { @MainActor in
            // 成功时 Tolk 马上退出，这个窗口随之关闭；只有失败才会回到这里。
            if let error = await onRelaunch() {
                relaunching = false
                withAnimation(.easeOut(duration: 0.14)) { relaunchError = error }
            }
        }
    }
}

private extension PermissionGuideView.Page {
    var stepName: String {
        switch self {
        case .accessibility: return "辅助功能"
        case .screenRecording: return "屏幕录制"
        case .done: return "完成"
        }
    }
}

// MARK: - 舞台：图标拖进系统设置

/// 左边是真能拖的图标，右边画的是一个小小的「系统设置」窗口（有红绿灯、标题和列表下的 + −），
/// 只给人认位置，不接收拖放。点过「打开…设置」之后才开始演示拖动；授权之后，虚线那一行落成真正的一行。
private struct PermissionStage: View {
    let symbol: String
    let name: String
    let isGranted: Bool
    var grantedLabel = "已授权"
    let waitingForDrag: Bool
    let palette: ScreenshotPalette
    let onDroppedHere: () -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var switchOn = false
    @State private var droppedHere = false

    var body: some View {
        Group {
            if waitingForDrag && !isGranted && !reduceMotion {
                TimelineView(.animation(minimumInterval: 1.0 / 30)) { context in
                    content(demo: DragDemo(at: context.date))
                }
            } else {
                content(demo: nil)
            }
        }
        .onAppear { switchOn = isGranted }
        .onChange(of: isGranted) { granted in
            if reduceMotion {
                switchOn = granted
            } else {
                withAnimation(.spring(response: 0.28, dampingFraction: 0.68).delay(0.18)) { switchOn = granted }
            }
        }
    }

    private func content(demo: DragDemo?) -> some View {
        HStack(spacing: 36) {
            iconColumn
                .frame(width: 96)
            settingsWindow(demo: demo)
                .frame(width: 272)
        }
        .overlayPreferenceValue(DemoAnchorKey.self) { anchors in
            GeometryReader { proxy in
                if let demo, demo.ghostOpacity > 0, let from = anchors[.source], let to = anchors[.target] {
                    let a = proxy[from], b = proxy[to]
                    let size = 30 + (b.width - 30) * demo.travel
                    Image(nsImage: ParentApp.icon)
                        .resizable()
                        .scaledToFit()
                        .frame(width: size, height: size)
                        .shadow(color: .black.opacity(0.22), radius: 6, x: 0, y: 3)
                        .opacity(demo.ghostOpacity)
                        .position(
                            x: a.midX + (b.midX - a.midX) * demo.travel,
                            y: a.midY + (b.midY - a.midY) * demo.travel
                        )
                }
            }
            .allowsHitTesting(false)
        }
    }

    private var iconColumn: some View {
        VStack(spacing: 10) {
            ZStack(alignment: .bottomTrailing) {
                Image(nsImage: ParentApp.icon)
                    .resizable()
                    .scaledToFit()
                    .frame(width: 76, height: 76)
                    .shadow(color: .black.opacity(0.16), radius: 12, x: 0, y: 7)
                    .anchorPreference(key: DemoAnchorKey.self, value: .bounds) { [.source: $0] }

                if isGranted || waitingForDrag {
                    Image(systemName: isGranted ? "checkmark" : "hand.draw.fill")
                        .font(.system(size: 11, weight: .bold))
                        .foregroundStyle(.white)
                        .frame(width: 24, height: 24)
                        .background(Circle().fill(isGranted ? palette.ok : palette.accentFill))
                        .overlay(Circle().strokeBorder(palette.card, lineWidth: 2))
                        .offset(x: 5, y: 5)
                        .transition(.scale(scale: 0.5).combined(with: .opacity))
                }
            }

            Text(caption)
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(droppedHere || (waitingForDrag && !isGranted) ? palette.accentText : palette.stageMuted)
                .lineLimit(1)
                .fixedSize()
                .frame(height: 16)
        }
        // `.help` 留给 VoiceOver；盖在上面的拖拽源会挡住鼠标悬停，悬停提示由它自己的 toolTip 出。
        .help(PermissionGuideDrag.help)
        .overlay {
            if !isGranted {
                PermissionGuideApplicationDragSource(
                    applicationURL: ParentApp.bundleURL,
                    toolTip: PermissionGuideDrag.help,
                    onDropInsideWindow: droppedInsideWindow
                )
            }
        }
    }

    private var caption: String {
        if isGranted { return "Tolk" }
        if droppedHere { return "要拖到系统设置的窗口里" }
        return waitingForDrag ? "拖到系统设置" : "Tolk"
    }

    /// 拖完松手还落在这个窗口里（多半是拖到了右边的示意图上）：提醒一句，并直接进入拖动那一步。
    private func droppedInsideWindow() {
        onDroppedHere()
        withAnimation(.easeOut(duration: 0.15)) { droppedHere = true }
        Task { @MainActor in
            try? await Task.sleep(nanoseconds: 3_000_000_000)
            withAnimation(.easeOut(duration: 0.2)) { droppedHere = false }
        }
    }

    private func settingsWindow(demo: DragDemo?) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 5) {
                ForEach(0..<3, id: \.self) { _ in
                    Circle().fill(palette.controlStroke).frame(width: 7, height: 7)
                }
                Spacer(minLength: 0)
                Text("系统设置")
                    .font(.system(size: 11, weight: .medium))
                    .foregroundStyle(palette.muted)
                Spacer(minLength: 0)
                Color.clear.frame(width: 31, height: 1)
            }
            .frame(height: 12)

            HStack(spacing: 8) {
                Image(systemName: symbol)
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(palette.accentText)
                    .frame(width: 18)
                Text(name)
                    .font(.system(size: 13, weight: .semibold))
                Spacer(minLength: 0)
                StatusChip(text: isGranted ? grantedLabel : "未授权", isGranted: isGranted, palette: palette)
            }
            .frame(height: 24)

            VStack(spacing: 6) {
                PlaceholderRow(palette: palette)
                AppRow(
                    fill: isGranted ? 1 : demo?.fill ?? 0,
                    isOn: isGranted ? switchOn : demo?.switchOn ?? false,
                    palette: palette
                )
                .animation(reduceMotion || demo != nil ? nil : .spring(response: 0.32, dampingFraction: 0.72), value: isGranted)
            }

            HStack(spacing: 0) {
                Text("+").frame(width: 20)
                Rectangle().fill(palette.stroke).frame(width: 1, height: 10)
                Text("−").frame(width: 20)
            }
            .font(.system(size: 12, weight: .medium))
            .foregroundStyle(palette.muted)
            .frame(height: 14)
        }
        .padding(12)
        .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(palette.card))
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(palette.stroke, lineWidth: 1))
        .compositingGroup()
        .shadow(color: .black.opacity(0.10), radius: 15, x: 0, y: 8)
        .accessibilityHidden(true)
    }
}

private enum DemoAnchor { case source, target }

private struct DemoAnchorKey: PreferenceKey {
    static var defaultValue: [DemoAnchor: Anchor<CGRect>] = [:]
    static func reduce(value: inout [DemoAnchor: Anchor<CGRect>], nextValue: () -> [DemoAnchor: Anchor<CGRect>]) {
        value.merge(nextValue()) { $1 }
    }
}

/// 一轮演示：小图标从左边飞过去、缩成列表里的大小落进那一行，行亮起来、开关拨开，停一下再复原。
private struct DragDemo {
    static let period: Double = 3.6

    let travel: Double
    let ghostOpacity: Double
    let fill: Double
    let switchOn: Bool

    init(at date: Date) {
        let t = date.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: Self.period) / Self.period
        func clamp(_ x: Double) -> Double { min(1, max(0, x)) }
        let x = clamp(t / 0.42)
        travel = x * x * (3 - 2 * x)
        // 边走边浮现，不在原图标上叠一个重影；落进去的那一刻交给行里的图标。
        ghostOpacity = t < 0.43 ? 0.95 * clamp((travel - 0.15) / 0.2) : 0
        if t < 0.42 {
            fill = 0
        } else if t < 0.86 {
            fill = clamp((t - 0.42) / 0.04)
        } else {
            fill = 1 - clamp((t - 0.86) / 0.1)
        }
        switchOn = t >= 0.52 && t < 0.86
    }
}

/// 系统设置列表里别的应用，只用来认位置，压得很淡。
private struct PlaceholderRow: View {
    let palette: ScreenshotPalette

    var body: some View {
        HStack(spacing: 10) {
            RoundedRectangle(cornerRadius: 5, style: .continuous)
                .fill(palette.controlStroke)
                .frame(width: 22, height: 22)
            RoundedRectangle(cornerRadius: 3, style: .continuous)
                .fill(palette.controlStroke)
                .frame(width: 64, height: 7)
            Spacer(minLength: 0)
            MiniSwitch(isOn: false, knob: palette.card, palette: palette)
        }
        .padding(.horizontal, 10)
        .frame(height: 38)
        .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(palette.control.opacity(0.55)))
        .opacity(0.6)
    }
}

/// Tolk 那一行：fill 为 0 是虚线的空位，为 1 是已经加进列表的真实一行。
private struct AppRow: View {
    let fill: Double
    let isOn: Bool
    let palette: ScreenshotPalette

    var body: some View {
        HStack(spacing: 10) {
            Image(nsImage: ParentApp.icon)
                .resizable()
                .scaledToFit()
                .frame(width: 22, height: 22)
                .saturation(fill)
                .opacity(0.35 + 0.65 * fill)
                .anchorPreference(key: DemoAnchorKey.self, value: .bounds) { [.target: $0] }
            Text("Tolk")
                .font(.system(size: 13, weight: .medium))
                .foregroundStyle(palette.fg.opacity(0.45 + 0.55 * fill))
            Spacer(minLength: 0)
            MiniSwitch(isOn: isOn, knob: .white, palette: palette)
                .opacity(0.5 + 0.5 * fill)
        }
        .padding(.horizontal, 10)
        .frame(height: 38)
        .background(
            ZStack {
                RoundedRectangle(cornerRadius: 8, style: .continuous).fill(palette.accent.opacity(0.06 * (1 - fill)))
                RoundedRectangle(cornerRadius: 8, style: .continuous).fill(palette.control.opacity(fill))
            }
        )
        .overlay(
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .strokeBorder(palette.accent.opacity(0.55 * (1 - fill)), style: StrokeStyle(lineWidth: 1, dash: [4, 3]))
        )
        .scaleEffect(1 - 0.02 * sin(fill * .pi)) // 落进去时轻轻按一下
    }
}

private struct MiniSwitch: View {
    let isOn: Bool
    let knob: Color
    let palette: ScreenshotPalette

    var body: some View {
        Capsule()
            .fill(isOn ? palette.accent : palette.controlStroke)
            .frame(width: 26, height: 15)
            .overlay(alignment: isOn ? .trailing : .leading) {
                Circle()
                    .fill(knob)
                    .frame(width: 13, height: 13)
                    .padding(1)
                    .shadow(color: .black.opacity(0.2), radius: 1, x: 0, y: 0.5)
            }
            .animation(.spring(response: 0.25, dampingFraction: 0.7), value: isOn)
    }
}

/// 和主窗口「系统权限」里的 .badge 同一种状态标签。
private struct StatusChip: View {
    let text: String
    let isGranted: Bool
    let palette: ScreenshotPalette

    var body: some View {
        HStack(spacing: 4) {
            if isGranted {
                Image(systemName: "checkmark")
                    .font(.system(size: 9, weight: .bold))
            }
            Text(text)
                .font(.system(size: 12, weight: .medium))
        }
        .foregroundStyle(isGranted ? palette.okText : palette.muted)
        .padding(.horizontal, 9)
        .frame(height: 24)
        .background(Capsule().fill(isGranted ? palette.okSoft : palette.control))
    }
}

/// 全部完成时画出来的勾。
private struct DrawnCheck: View {
    let palette: ScreenshotPalette
    let animated: Bool

    @State private var drawn: CGFloat = 0

    var body: some View {
        ZStack {
            Circle().fill(palette.ok)
            CheckShape()
                .trim(from: 0, to: drawn)
                .stroke(.white, style: StrokeStyle(lineWidth: 3, lineCap: .round, lineJoin: .round))
                .padding(10)
        }
        .overlay(Circle().strokeBorder(palette.card, lineWidth: 2.5))
        .frame(width: 36, height: 36)
        .onAppear {
            if animated {
                withAnimation(.easeOut(duration: 0.32).delay(0.12)) { drawn = 1 }
            } else {
                drawn = 1
            }
        }
    }

    private struct CheckShape: Shape {
        func path(in rect: CGRect) -> Path {
            var path = Path()
            path.move(to: CGPoint(x: rect.minX, y: rect.midY + rect.height * 0.04))
            path.addLine(to: CGPoint(x: rect.minX + rect.width * 0.36, y: rect.maxY - rect.height * 0.12))
            path.addLine(to: CGPoint(x: rect.maxX, y: rect.minY + rect.height * 0.14))
            return path
        }
    }
}

// MARK: - 按钮

/// 主窗口 window.css 里 primary / ghost / quiet 三种按钮，外加一种行内链接（同 window.css 的 a）。
private struct GuideButtonStyle: ButtonStyle {
    enum Kind: Equatable {
        case primary, ghost, quiet, link
    }

    let kind: Kind
    let palette: ScreenshotPalette

    func makeBody(configuration: Configuration) -> some View {
        StyledButton(configuration: configuration, kind: kind, palette: palette)
    }

    private struct StyledButton: View {
        let configuration: Configuration
        let kind: Kind
        let palette: ScreenshotPalette

        @State private var hovering = false

        var body: some View {
            configuration.label
                .labelStyle(LinkLabelStyle())
                .font(.system(size: kind == .link ? 13 : 14))
                .foregroundStyle(textColor)
                .lineLimit(1)
                .padding(.horizontal, kind == .primary || kind == .ghost ? 18 : 8)
                .frame(height: kind == .link ? 28 : 36)
                .background(RoundedRectangle(cornerRadius: kind == .link ? 7 : 10, style: .continuous).fill(fill))
                .brightness(kind == .primary && hovering && !configuration.isPressed ? 0.05 : 0)
                .scaleEffect(configuration.isPressed ? 0.97 : 1)
                .animation(.easeOut(duration: 0.08), value: configuration.isPressed)
                .contentShape(RoundedRectangle(cornerRadius: kind == .link ? 7 : 10, style: .continuous))
                .onHover { hovering = $0 }
        }

        private var textColor: Color {
            switch kind {
            case .primary: return .white
            case .ghost: return palette.fg
            case .quiet: return hovering ? palette.fg : palette.muted
            case .link: return palette.accentText
            }
        }

        private var fill: Color {
            switch kind {
            case .primary:
                return palette.accentFill
            case .ghost:
                return palette.fg.opacity(hovering ? 0.11 : 0.06)
            case .quiet:
                return hovering ? palette.fg.opacity(0.07) : .clear
            case .link:
                return hovering ? palette.accent.opacity(0.10) : .clear
            }
        }
    }

    private struct LinkLabelStyle: LabelStyle {
        func makeBody(configuration: Configuration) -> some View {
            HStack(spacing: 4) {
                configuration.icon.font(.system(size: 11, weight: .semibold))
                configuration.title
            }
        }
    }
}

private enum PermissionGuideDrag {
    static let help = "将 Tolk 拖到系统设置的权限列表"
}

/// 权限引导里「拖到系统设置」的拖拽源：拖拽剪贴板里只放 .app 的文件引用。
///
/// 不用 SwiftUI 的 `.onDrag { NSItemProvider(object: url as NSURL) }`：实测别的进程一读拖拽里的
/// `public.file-url`（系统设置在松手时读，有的接收方悬停时就读），SwiftUI 就把整个 .app 拷进
/// `~/Library/Caches/com.apple.SwiftUI.Drag-<UUID>/`，交出去的是拷贝的路径，拷贝从不清理。
/// 这份拷贝随后以同一 bundle ID 登记进 LaunchServices，「退出并重新打开」就可能挑中它。
struct PermissionGuideApplicationDragSource: NSViewRepresentable {
    let applicationURL: URL
    let toolTip: String
    /// 松手时还在引导窗口里、谁也没接收：多半是拖到了窗口里那张示意图上。
    var onDropInsideWindow: () -> Void = {}

    func makeNSView(context: Context) -> DragSourceView {
        DragSourceView(applicationURL: applicationURL)
    }

    func updateNSView(_ nsView: DragSourceView, context: Context) {
        nsView.applicationURL = applicationURL
        nsView.toolTip = toolTip
        nsView.onDropInsideWindow = onDropInsideWindow
    }

    final class DragSourceView: NSView, NSDraggingSource {
        var applicationURL: URL
        var onDropInsideWindow: () -> Void = {}
        private var mouseDownEvent: NSEvent?

        init(applicationURL: URL) {
            self.applicationURL = applicationURL
            super.init(frame: .zero)
        }

        @available(*, unavailable)
        required init?(coder: NSCoder) {
            fatalError("init(coder:) has not been implemented")
        }

        override func resetCursorRects() {
            addCursorRect(bounds, cursor: .openHand)
        }

        // 用户通常先打开系统设置、再从后面的引导窗口拖：第一下按住就能拖，
        // 拖动时引导窗口也不跳到系统设置前面挡住列表。
        override func acceptsFirstMouse(for event: NSEvent?) -> Bool {
            true
        }

        override func shouldDelayWindowOrdering(for event: NSEvent) -> Bool {
            true
        }

        override func mouseDown(with event: NSEvent) {
            mouseDownEvent = event
        }

        override func mouseUp(with event: NSEvent) {
            mouseDownEvent = nil
        }

        override func mouseDragged(with event: NSEvent) {
            guard let mouseDownEvent else { return }
            self.mouseDownEvent = nil

            NSApp.preventWindowOrdering()
            let location = convert(mouseDownEvent.locationInWindow, from: nil)
            beginDraggingSession(
                with: [
                    PermissionGuideApplicationDrag.draggingItem(
                        applicationURL: applicationURL,
                        centeredAt: location,
                        image: ParentApp.icon
                    )
                ],
                event: mouseDownEvent,
                source: self
            )
        }

        func draggingSession(
            _ session: NSDraggingSession,
            sourceOperationMaskFor context: NSDraggingContext
        ) -> NSDragOperation {
            PermissionGuideApplicationDrag.operationMask(for: context)
        }

        func draggingSession(
            _ session: NSDraggingSession,
            endedAt screenPoint: NSPoint,
            operation: NSDragOperation
        ) {
            guard operation.isEmpty, let window, window.frame.contains(screenPoint) else { return }
            onDropInsideWindow()
        }

        // 按住 ⌘ / ⌃ 时 AppKit 会把可选操作收窄成 generic / link，和只给的 copy 一交就空了，拖不进去。
        func ignoreModifierKeys(for session: NSDraggingSession) -> Bool {
            true
        }
    }
}

enum PermissionGuideApplicationDrag {
    static let iconSize: CGFloat = 64

    /// `NSURL` 作为 `NSPasteboardWriting` 只写文件引用（`public.file-url` 及其旧式别名），
    /// 不经过 `NSItemProvider`，也就没有 SwiftUI 替接收方「备一份文件」的那一步。
    static func draggingItem(
        applicationURL: URL,
        centeredAt location: NSPoint,
        image: NSImage
    ) -> NSDraggingItem {
        let item = NSDraggingItem(pasteboardWriter: applicationURL as NSURL)
        item.setDraggingFrame(
            NSRect(
                x: location.x - iconSize / 2,
                y: location.y - iconSize / 2,
                width: iconSize,
                height: iconSize
            ),
            contents: image
        )
        return item
    }

    /// 只给 copy：旧版 SwiftUI 拖拽给的也只有 copy，系统设置照收。
    /// 不给 move / delete，免得拖进废纸篓或 Finder 时把正式安装的 .app 挪走；本应用里没有接收方。
    static func operationMask(for context: NSDraggingContext) -> NSDragOperation {
        switch context {
        case .outsideApplication:
            return .copy
        case .withinApplication:
            return []
        @unknown default:
            return []
        }
    }
}
