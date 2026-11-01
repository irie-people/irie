import Foundation
import Security

// What the app keeps on the phone: each paired machine's address, name and
// pinned authority in UserDefaults, and its token, the one secret, in the
// Keychain, readable only on this phone after it is first unlocked.

/// A paired machine, without its token.
struct Machine: Codable, Identifiable, Equatable {
    var id: String
    var name: String
    var url: URL
    /// The SHA-256 of Irie's certificate authority for it: 64 hex digits.
    var pin: String

    var host: String { Gateway.host(url) }
}

enum Keep {
    private static let machinesKey = "irie.machines"
    private static let service = "io.github.irie-people.irie.token"

    static func loadMachines() -> [Machine] {
        guard let data = UserDefaults.standard.data(forKey: machinesKey),
              let list = try? JSONDecoder().decode([Machine].self, from: data) else { return [] }
        // A machine whose token is gone can't be talked to: pair it again.
        return list.filter { token(for: $0.id) != nil }
    }

    static func save(_ machines: [Machine]) {
        if let data = try? JSONEncoder().encode(machines) {
            UserDefaults.standard.set(data, forKey: machinesKey)
        }
    }

    static func token(for id: String) -> String? {
        let q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: id,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var out: CFTypeRef?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess, let data = out as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }

    @discardableResult
    static func setToken(_ token: String, for id: String) -> Bool {
        removeToken(for: id)
        let q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: id,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
            kSecValueData as String: Data(token.utf8),
        ]
        return SecItemAdd(q as CFDictionary, nil) == errSecSuccess
    }

    static func removeToken(for id: String) {
        let q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: id,
        ]
        SecItemDelete(q as CFDictionary)
    }

    // Small preferences, as the web app keeps them.

    static func value<T: Codable>(_ key: String, default fallback: T) -> T {
        guard let data = UserDefaults.standard.data(forKey: key), let v = try? JSONDecoder().decode(T.self, from: data) else { return fallback }
        return v
    }

    static func set<T: Codable>(_ key: String, _ value: T) {
        if let data = try? JSONEncoder().encode(value) {
            UserDefaults.standard.set(data, forKey: key)
        }
    }
}
