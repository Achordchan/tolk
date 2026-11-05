import SwiftUI

struct LanguageSearchPicker: View {
    let title: String
    let allowAuto: Bool
    let options: [Language]
    @Binding var selection: String
    let palette: ScreenshotPalette

    @Environment(\.colorScheme) private var scheme

    @State private var isPresented: Bool = false
    @State private var query: String = ""
    @State private var isHovering: Bool = false
    @State private var hoveringCode: String?

    var body: some View {
        Button {
            query = ""
            isPresented = true
        } label: {
            HStack(spacing: 8) {
                Text(title)
                    .font(.system(size: 13))
                    .foregroundStyle(palette.muted)

                Text(selectedName)
                    .font(.system(size: 14))
                    .foregroundStyle(palette.fg)
                    .lineLimit(1)

                Image(systemName: "chevron.down")
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundStyle(palette.muted)
            }
            .fixedSize(horizontal: true, vertical: false)
            .padding(.horizontal, 11)
            .frame(height: 34)
            .background(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(isHovering ? palette.control.opacity(0.7) : palette.control)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .strokeBorder(palette.controlStroke, lineWidth: 1)
            )
            .contentShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
            .onHover { isHovering = $0 }
        }
        .buttonStyle(.plain)
        .popover(isPresented: $isPresented, arrowEdge: .bottom) {
            VStack(spacing: 10) {
                TextField("搜索语言", text: $query)
                    .textFieldStyle(.roundedBorder)
                    .font(.system(size: 15))
                    .padding(.horizontal, 12)
                    .padding(.top, 12)

                List(filteredOptions, id: \.code) { item in
                    Button {
                        selection = item.code
                        isPresented = false
                    } label: {
                        HStack {
                            Text(item.name)
                                .font(.system(size: 15, weight: .medium))
                                .foregroundStyle(.primary)
                            Spacer()
                            if item.code == selection {
                                Image(systemName: "checkmark")
                                    .font(.system(size: 13, weight: .semibold))
                                    .foregroundStyle(palette.accent)
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 8)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .onHover { hovering in
                        hoveringCode = hovering ? item.code : nil
                    }
                    .listRowSeparator(.hidden)
                    .listRowInsets(EdgeInsets(top: 2, leading: 8, bottom: 2, trailing: 8))
                    .listRowBackground(
                        RoundedRectangle(cornerRadius: 10, style: .continuous)
                            .fill(rowBackgroundColor(code: item.code))
                    )
                }
                .listStyle(.plain)
            }
            .frame(width: 200, height: 400)
        }
    }

    private func rowBackgroundColor(code: String) -> Color {
        if code == selection {
            return palette.accent.opacity(scheme == .dark ? 0.22 : 0.14)
        }
        if hoveringCode == code {
            return DS.cardBackground(scheme).opacity(1.35)
        }
        return Color.clear
    }

    private var selectedName: String {
        options.first(where: { $0.code == selection })?.name ?? selection
    }

    private var filteredOptions: [Language] {
        let list = allowAuto ? options : options.filter { $0.code != LanguagePreset.auto.code }
        let q = query.trimmingCharacters(in: .whitespacesAndNewlines)
        if q.isEmpty { return list }
        return list.filter { $0.name.localizedCaseInsensitiveContains(q) || $0.code.localizedCaseInsensitiveContains(q) }
    }
}
