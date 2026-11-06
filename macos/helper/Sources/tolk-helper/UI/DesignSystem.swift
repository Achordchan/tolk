import SwiftUI
import TolkHelperCore

enum DS {
    static let cornerRadius: CGFloat = 12
    static let pillCornerRadius: CGFloat = 12

    static func cardBackground(_ scheme: ColorScheme) -> Color {
        switch scheme {
        case .dark:
            return Color.white.opacity(0.06)
        default:
            return Color.black.opacity(0.06)
        }
    }

    static func strokeColor(_ scheme: ColorScheme) -> Color {
        switch scheme {
        case .dark:
            return Color.white.opacity(0.08)
        default:
            return Color.black.opacity(0.06)
        }
    }
}

/// 截图工作区和权限引导的配色，取自主窗口 src/style.css、src/window.css 的浅色、深色、粉色。
/// 这些窗口跟随 Tolk 的主题，不跟随系统强调色。
struct ScreenshotPalette {
    let accent, card, control, controlStroke, stroke, fg, muted, ok, err: Color
    /// 下面几个对应 window.css 里的同名变量：stage 是引导页的着色舞台；
    /// accentFill 用在白字底下，accentText 用作页面上的文字，stageMuted 是舞台上的次要文字。
    let bg, stage, stageMuted, accentFill, accentText, okText: Color
    /// 「已授权」这类标签的底：成功色淡淡地混进卡片色，不透明，铺在粉色舞台上也不发灰。
    let okSoft: Color

    static func make(_ appearance: InputAssistAppearance, _ scheme: ColorScheme) -> ScreenshotPalette {
        if appearance == .pink {
            return ScreenshotPalette(
                accent: Color(hex: 0xFF69B4), card: Color(hex: 0xFFFBFC), control: Color(hex: 0xFFF3F7),
                controlStroke: Color(hex: 0xF0CFD9), stroke: Color(hex: 0xF0CFD9), fg: Color(hex: 0x4A3039),
                muted: Color(hex: 0x8A6672), ok: Color(hex: 0x34C759), err: Color(hex: 0xD72D23),
                bg: Color(hex: 0xFFF4F7), stage: Color(mixing: 0xFF69B4, 7, 0xFFF4F7),
                stageMuted: Color(mixing: 0x4A3039, 72, 0xFFF4F7), accentFill: Color(hex: 0xC2458A),
                accentText: Color(hex: 0xC2458A), okText: Color(mixing: 0x34C759, 50, 0x4A3039),
                okSoft: Color(mixing: 0x34C759, 16, 0xFFFBFC)
            )
        }
        if scheme == .dark {
            return ScreenshotPalette(
                accent: Color(hex: 0x0A84FF), card: Color(hex: 0x232323), control: Color(hex: 0x2C2D31),
                controlStroke: Color(red: 120 / 255, green: 140 / 255, blue: 200 / 255).opacity(0.28),
                stroke: Color.white.opacity(0.08), fg: Color(hex: 0xECECEE),
                muted: Color(hex: 0x9A9DA4), ok: Color(hex: 0x34C759), err: Color(hex: 0xFF6961),
                bg: Color(hex: 0x2B2B2B), stage: Color(mixing: 0x0A84FF, 7, 0x2B2B2B),
                stageMuted: Color(mixing: 0xECECEE, 72, 0x2B2B2B), accentFill: Color(mixing: 0x0A84FF, 84, 0x000000),
                accentText: Color(mixing: 0x0A84FF, 70, 0xFFFFFF), okText: Color(mixing: 0x34C759, 50, 0xECECEE),
                okSoft: Color(mixing: 0x34C759, 16, 0x232323)
            )
        }
        return ScreenshotPalette(
            accent: Color(hex: 0x0A84FF), card: Color(hex: 0xFAFAFA), control: Color(hex: 0xE9EAEE),
            controlStroke: Color(red: 60 / 255, green: 80 / 255, blue: 130 / 255).opacity(0.18),
            stroke: Color.black.opacity(0.09), fg: Color(hex: 0x1D1D1F),
            muted: Color(hex: 0x6B6E75), ok: Color(hex: 0x28A745), err: Color(hex: 0xD72D23),
            bg: Color(hex: 0xECECEC), stage: Color(mixing: 0x0A84FF, 7, 0xECECEC),
            stageMuted: Color(mixing: 0x1D1D1F, 72, 0xECECEC), accentFill: Color(mixing: 0x0A84FF, 84, 0x000000),
            accentText: Color(mixing: 0x0A84FF, 84, 0x000000), okText: Color(mixing: 0x34C759, 50, 0x1D1D1F),
            okSoft: Color(mixing: 0x34C759, 16, 0xFAFAFA)
        )
    }
}

extension Color {
    init(hex: UInt32) {
        self.init(
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255
        )
    }

    /// 等同 CSS 的 `color-mix(in srgb, a percent%, b)`。
    init(mixing a: UInt32, _ percent: Double, _ b: UInt32) {
        let t = percent / 100
        func channel(_ shift: UInt32) -> Double {
            let x = Double((a >> shift) & 0xFF), y = Double((b >> shift) & 0xFF)
            return (x * t + y * (1 - t)) / 255
        }
        self.init(red: channel(16), green: channel(8), blue: channel(0))
    }
}

struct ScreenshotButtonStyle: ButtonStyle {
    enum Kind { case primary, soft, ghost, quiet }

    let kind: Kind
    let palette: ScreenshotPalette
    var iconOnly: Bool = false
    var tall: Bool = false

    func makeBody(configuration: Configuration) -> some View {
        StyledButton(configuration: configuration, kind: kind, palette: palette, iconOnly: iconOnly, tall: tall)
    }

    private struct StyledButton: View {
        let configuration: Configuration
        let kind: Kind
        let palette: ScreenshotPalette
        let iconOnly: Bool
        let tall: Bool

        @Environment(\.isEnabled) private var isEnabled
        @State private var hovering = false

        var body: some View {
            configuration.label
                .labelStyle(ScreenshotLabelStyle(iconColor: iconColor, iconOnly: iconOnly))
                .font(.system(size: 13, weight: kind == .primary ? .medium : .regular))
                .foregroundStyle(textColor)
                .padding(.horizontal, kind == .primary ? 12 : 9)
                .frame(height: tall ? 34 : 30)
                .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(fill))
                .overlay(
                    RoundedRectangle(cornerRadius: 8, style: .continuous)
                        .strokeBorder(kind == .soft ? palette.controlStroke : Color.clear, lineWidth: 1)
                )
                .brightness(kind == .primary && isEnabled ? (configuration.isPressed ? -0.08 : hovering ? 0.04 : 0) : 0)
                .opacity(isEnabled || kind == .primary ? 1 : 0.45)
                .contentShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                .onHover { hovering = $0 }
        }

        private var textColor: Color {
            switch kind {
            case .primary: return isEnabled ? .white : palette.muted
            case .quiet: return palette.muted
            case .soft, .ghost: return palette.fg
            }
        }

        private var iconColor: Color {
            switch kind {
            case .primary: return textColor
            case .soft: return palette.fg
            case .ghost, .quiet: return palette.muted
            }
        }

        private var fill: Color {
            switch kind {
            case .primary: return isEnabled ? palette.accent : palette.control
            case .soft: return palette.control
            case .ghost, .quiet:
                if !isEnabled { return .clear }
                return configuration.isPressed || hovering ? palette.control : .clear
            }
        }
    }
}

private struct ScreenshotLabelStyle: LabelStyle {
    let iconColor: Color
    let iconOnly: Bool

    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 6) {
            configuration.icon
                .font(.system(size: 13))
                .foregroundStyle(iconColor)
            if !iconOnly {
                configuration.title
            }
        }
    }
}

extension View {
    /// 工作区里浮在截图上的卡片：普通描边加一层柔和阴影。
    func screenshotCard(_ palette: ScreenshotPalette, radius: CGFloat) -> some View {
        self
            .background(RoundedRectangle(cornerRadius: radius, style: .continuous).fill(palette.card))
            .overlay(RoundedRectangle(cornerRadius: radius, style: .continuous).strokeBorder(palette.stroke, lineWidth: 1))
            .compositingGroup()
            .shadow(color: .black.opacity(0.18), radius: 9, x: 0, y: 5)
    }
}

struct Card: ViewModifier {
    @Environment(\.colorScheme) private var scheme

    func body(content: Content) -> some View {
        content
            .background(
                RoundedRectangle(cornerRadius: DS.cornerRadius, style: .continuous)
                    .fill(DS.cardBackground(scheme))
            )
            .overlay(
                RoundedRectangle(cornerRadius: DS.cornerRadius, style: .continuous)
                    .strokeBorder(DS.strokeColor(scheme), lineWidth: 1)
            )
    }
}

struct Pill: ViewModifier {
    @Environment(\.colorScheme) private var scheme

    func body(content: Content) -> some View {
        content
            .padding(.horizontal, 12)
            .padding(.vertical, 9)
            .background(
                RoundedRectangle(cornerRadius: DS.pillCornerRadius, style: .continuous)
                    .fill(DS.cardBackground(scheme))
            )
            .overlay(
                RoundedRectangle(cornerRadius: DS.pillCornerRadius, style: .continuous)
                    .strokeBorder(DS.strokeColor(scheme), lineWidth: 1)
            )
            .contentShape(RoundedRectangle(cornerRadius: DS.pillCornerRadius, style: .continuous))
    }
}

struct IconPillButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .frame(width: 34, height: 32)
            .contentShape(Rectangle())
            .opacity(configuration.isPressed ? 0.7 : 1.0)
    }
}

extension View {
    func dsCard() -> some View { modifier(Card()) }
    func dsPill() -> some View { modifier(Pill()) }
}
