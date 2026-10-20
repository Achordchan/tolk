import Foundation
import Security
import TolkHelperCore

/// `legacy.read`：只读旧版 bagayalu 的设置和 Keychain。不写、不删任何东西，也不写日志（API Key 是机密）。
enum LegacyReader {
    static func read() -> [String: JSONValue] {
        let domain = UserDefaults.standard.persistentDomain(forName: LegacyData.defaultsDomain) ?? [:]
        var keys: [String: JSONValue] = [:]
        for account in LegacyData.keychainAccounts {
            if let value = readKey(account: account) {
                keys[account] = .string(value)
            }
        }
        var languages: [String: JSONValue] = [:]
        for (code, label) in TolkLanguages.codeToLabel { languages[code] = .string(label) }
        return [
            "defaults": .object(LegacyData.filterDefaults(domain)),
            "keys": .object(keys),
            "languages": .object(languages)
        ]
    }

    /// 先找固定 service 的条目，没有再找旧版没写 service 的历史条目（只读，不迁移）。
    /// 读不到（不存在、用户在钥匙串弹窗里点了拒绝）一律返回 nil。
    private static func readKey(account: String) -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: LegacyData.keychainService,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne
        ]
        var item: CFTypeRef?
        if SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess,
           let data = item as? Data,
           let value = String(data: data, encoding: .utf8) {
            return value
        }
        return readLegacyServiceLessKey(account: account)
    }

    private static func readLegacyServiceLessKey(account: String) -> String? {
        let listQuery: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrAccount as String: account,
            kSecReturnAttributes as String: true,
            kSecReturnPersistentRef as String: true,
            kSecMatchLimit as String: kSecMatchLimitAll
        ]
        var listed: CFTypeRef?
        guard SecItemCopyMatching(listQuery as CFDictionary, &listed) == errSecSuccess else { return nil }
        let rows: [[String: Any]]
        if let many = listed as? [[String: Any]] {
            rows = many
        } else if let one = listed as? [String: Any] {
            rows = [one]
        } else {
            return nil
        }
        for row in rows {
            let service = row[kSecAttrService as String] as? String
            guard service == nil || service!.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  let ref = row[kSecValuePersistentRef as String] else { continue }
            let dataQuery: [String: Any] = [
                kSecClass as String: kSecClassGenericPassword,
                kSecValuePersistentRef as String: ref,
                kSecReturnData as String: true,
                kSecMatchLimit as String: kSecMatchLimitOne
            ]
            var item: CFTypeRef?
            if SecItemCopyMatching(dataQuery as CFDictionary, &item) == errSecSuccess,
               let data = item as? Data,
               let value = String(data: data, encoding: .utf8) {
                return value
            }
        }
        return nil
    }
}
