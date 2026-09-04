import Foundation

extension Importers {

    static func parseFreeOTPPlus(_ probe: [String: Any]) throws -> [Account] {
        let tokens = probe["tokens"] as? [[String: Any]] ?? []
        var out: [Account] = []
        out.reserveCapacity(tokens.count)
        for tk in tokens {
            let issuerExt = str(tk["issuerExt"])
            let issuerInt = str(tk["issuerInt"])
            let issuerAlt = str(tk["issuerAlt"])
            var issuer = issuerExt.isEmpty ? issuerInt : issuerExt
            if !issuerAlt.isEmpty { issuer = issuerAlt }
            let labelAlt = str(tk["labelAlt"])
            let label = labelAlt.isEmpty ? str(tk["label"]) : labelAlt
            let storedType = str(tk["type"])
            var type = storedType
            // FreeOTP+ has no Steam token type: a Steam account is stored as a
            // TOTP token whose issuer is "Steam" and whose codes come out of a
            // different alphabet.
            if issuer.caseInsensitiveCompare("Steam") == .orderedSame,
               storedType.caseInsensitiveCompare("TOTP") == .orderedSame {
                type = "steam"
            }
            let secret: Data
            do { secret = try javaSignedBytes(tk["secret"] as? [NSNumber] ?? []) }
            catch { throw ImporterError.malformed("freeotp+ token \"\(issuer)\": \(message(error))") }
            var counter = int64Val(tk["counter"])
            if storedType.caseInsensitiveCompare("HOTP") == .orderedSame {
                // FreeOTP+ persists counter-1 and adds one back when it rebuilds
                // the URI, so the stored value is one step behind the real counter.
                counter += 1
            }
            do {
                out.append(try buildAccountRaw(
                    type: type,
                    issuer: issuer,
                    acct: label,
                    secret: secret,
                    algo: str(tk["algo"]),
                    digits: intVal(tk["digits"]),
                    period: intVal(tk["period"]),
                    counter: counter))
            } catch {
                throw ImporterError.malformed("freeotp+ token \"\(issuer)\": \(message(error))")
            }
        }
        return out
    }

    /// Converts Gson's byte[] rendering — a JSON array of Java signed bytes,
    /// -128..127 — into raw key bytes.
    private static func javaSignedBytes(_ vals: [NSNumber]) throws -> Data {
        if vals.isEmpty {
            throw ImporterError.malformed("missing secret")
        }
        for v in vals where v.intValue < -128 || v.intValue > 255 {
            throw ImporterError.malformed("secret byte out of range")
        }
        var out = [UInt8]()
        out.reserveCapacity(vals.count)
        for v in vals { out.append(UInt8(truncatingIfNeeded: v.intValue)) }
        return Data(out)
    }
}
