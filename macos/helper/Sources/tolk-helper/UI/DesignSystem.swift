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

/// 截图工作区的配色，取自主窗口 src/style.css 的浅色、深色、粉色。
/// 选区层跟随 Tolk 的主题，不跟随系统强调色。
struct ScreenshotPalette {
    let accent, card, control, controlStroke, stroke, fg, muted, ok, err: Color

    static func make(_ appearance: InputAssistAppearance, _ scheme: ColorScheme) -> ScreenshotPalette {
        if appearance == .pink {
            return ScreenshotPalette(
                accent: Color(hex: 0xFF69B4), card: Color(hex: 0xFFFBFC), control: Color(hex: 0xFFF3F7),
                controlStroke: Color(hex: 0xF0CFD9), stroke: Color(hex: 0xF0CFD9), fg: Color(hex: 0x4A3039),
                muted: Color(hex: 0x8A6672), ok: Color(hex: 0x34C759), err: Color(hex: 0xD72D23)
            )
        }
        if scheme == .dark {
            return ScreenshotPalette(
                accent: Color(hex: 0x0A84FF), card: Color(hex: 0x232323), control: Color(hex: 0x2C2D31),
                controlStroke: Color(red: 120 / 255, green: 140 / 255, blue: 200 / 255).opacity(0.28),
                stroke: Color.white.opacity(0.08), fg: Color(hex: 0xECECEE),
                muted: Color(hex: 0x9A9DA4), ok: Color(hex: 0x34C759), err: Color(hex: 0xFF6961)
            )
        }
        return ScreenshotPalette(
            accent: Color(hex: 0x0A84FF), card: Color(hex: 0xFAFAFA), control: Color(hex: 0xE9EAEE),
            controlStroke: Color(red: 60 / 255, green: 80 / 255, blue: 130 / 255).opacity(0.18),
            stroke: Color.black.opacity(0.09), fg: Color(hex: 0x1D1D1F),
            muted: Color(hex: 0x6B6E75), ok: Color(hex: 0x28A745), err: Color(hex: 0xD72D23)
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
}

struct ScreenshotButtonStyle: ButtonStyle {
    enum Kind { case primary, soft, ghost, quiet }

    let kind: Kind
    let palette: ScreenshotPalette

    func makeBody(configuration: Configuration) -> some View {
        StyledButton(configuration: configuration, kind: kind, palette: palette)
    }

    private struct StyledButton: View {
        let configuration: Configuration
        let kind: Kind
        let palette: ScreenshotPalette

        @Environment(\.isEnabled) private var isEnabled
        @State private var hovering = false

        var body: some View {
            configuration.label
                .labelStyle(ScreenshotLabelStyle(iconColor: iconColor))
                .font(.system(size: kind == .primary ? 14 : 13, weight: kind == .primary ? .medium : .regular))
                .foregroundStyle(kind == .primary ? Color.white : kind == .quiet ? palette.muted : palette.fg)
                .padding(.horizontal, kind == .primary ? 13 : 9)
                .frame(height: kind == .primary ? 34 : 30)
                .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(fill))
                .overlay(
                    RoundedRectangle(cornerRadius: 8, style: .continuous)
                        .strokeBorder(kind == .soft ? palette.controlStroke : Color.clear, lineWidth: 1)
                )
                .brightness(kind == .primary && isEnabled ? (configuration.isPressed ? -0.08 : hovering ? 0.04 : 0) : 0)
                .opacity(isEnabled ? 1 : 0.45)
                .contentShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                .onHover { hovering = $0 }
        }

        private var iconColor: Color {
            switch kind {
            case .primary: return .white
            case .soft: return palette.fg
            case .ghost, .quiet: return palette.muted
            }
        }

        private var fill: Color {
            switch kind {
            case .primary: return palette.accent
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

    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 6) {
            configuration.icon
                .font(.system(size: 13))
                .foregroundStyle(iconColor)
            configuration.title
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
            .shadow(color: .black.opacity(0.22), radius: 16, x: 0, y: 8)
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
