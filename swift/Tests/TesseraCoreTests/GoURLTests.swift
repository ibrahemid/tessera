import XCTest
import Foundation
@testable import TesseraCore

/// `GoURL` against Go `net/url`: every row's expected parts or error text was
/// produced by `url.Parse`, `EscapedPath`, `QueryEscape` and `PathEscape`.
final class GoURLTests: XCTestCase {

    private struct Parts: Equatable {
        let scheme, opaque, host, path, rawPath, rawQuery, fragment, escapedPath: String
        let forceQuery: Bool

        init(_ scheme: String, _ opaque: String, _ host: String, _ path: String, _ rawPath: String,
             _ rawQuery: String, _ fragment: String, _ escapedPath: String, forceQuery: Bool) {
            self.scheme = scheme; self.opaque = opaque; self.host = host; self.path = path
            self.rawPath = rawPath; self.rawQuery = rawQuery; self.fragment = fragment
            self.escapedPath = escapedPath; self.forceQuery = forceQuery
        }

        init(_ u: GoURL) {
            func s(_ b: [UInt8]) -> String { String(decoding: b, as: UTF8.self) }
            self.init(u.scheme, s(u.opaque), s(u.host), s(u.path), s(u.rawPath), s(u.rawQuery),
                      s(u.fragment), s(u.escapedPath), forceQuery: u.forceQuery)
        }
    }

    func testParseMatchesGo() {
        let cases: [(String, Parts?, String?)] = [
            ("otpauth://totp/ACME:alice?secret=X&issuer=ACME", Parts("otpauth", "", "totp", "/ACME:alice", "", "secret=X&issuer=ACME", "", "/ACME:alice", forceQuery: false), nil),
            ("OTPAUTH://TOTP/a", Parts("otpauth", "", "TOTP", "/a", "", "", "", "/a", forceQuery: false), nil),
            ("otpauth://totp/Ac%ZZme:alice", nil, "invalid URL escape \"%ZZ\""),
            ("otpauth://totp/a%2Fb:c?x=1#frag%20x", Parts("otpauth", "", "totp", "/a/b:c", "/a%2Fb:c", "x=1", "frag x", "/a%2Fb:c", forceQuery: false), nil),
            ("otpauth:totp/opaque?x", Parts("otpauth", "totp/opaque", "", "", "", "x", "", "", forceQuery: false), nil),
            ("otpauth://user:pass@host:80/p", Parts("otpauth", "", "host:80", "/p", "", "", "", "/p", forceQuery: false), nil),
            ("otpauth://[::1]:80/p", Parts("otpauth", "", "[::1]:80", "/p", "", "", "", "/p", forceQuery: false), nil),
            ("otpauth://[fe80::1%25en0]/p", Parts("otpauth", "", "[fe80::1%en0]", "/p", "", "", "", "/p", forceQuery: false), nil),
            ("otpauth://host:port/p", nil, "invalid port \":port\" after host"),
            ("otpauth://ho st/p", nil, "invalid character \" \" in host name"),
            ("otpauth://totp/a b?x=1", Parts("otpauth", "", "totp", "/a b", "/a b", "x=1", "", "/a%20b", forceQuery: false), nil),
            ("otpauth://totp/a\u{7f}", nil, "net/url: invalid control character in URL"),
            ("1otpauth://x", nil, "first path segment in URL cannot contain colon"),
            ("://x", nil, "missing protocol scheme"),
            ("otpauth://totp/a?", Parts("otpauth", "", "totp", "/a", "", "", "", "/a", forceQuery: true), nil),
            ("otpauth://totp/é?x=é", Parts("otpauth", "", "totp", "/é", "/é", "x=é", "", "/%C3%A9", forceQuery: false), nil),
            ("otpauth://totp/%e2%82%ac", Parts("otpauth", "", "totp", "/€", "/%e2%82%ac", "", "", "/%e2%82%ac", forceQuery: false), nil),
            ("otpauth-migration://offline?data=abc%3D", Parts("otpauth-migration", "", "offline", "", "", "data=abc%3D", "", "", forceQuery: false), nil),
            ("otpauth://%41/x", nil, "invalid URL escape \"%41\""),
            ("otpauth://totp/a%", nil, "invalid URL escape \"%\""),
            ("otpauth://totp/a?b=%zz", Parts("otpauth", "", "totp", "/a", "", "b=%zz", "", "/a", forceQuery: false), nil),
            ("otpauth://totp//double", Parts("otpauth", "", "totp", "//double", "", "", "", "//double", forceQuery: false), nil),
            ("otpauth://", Parts("otpauth", "", "", "", "", "", "", "", forceQuery: false), nil),
            ("otpauth:", Parts("otpauth", "", "", "", "", "", "", "", forceQuery: false), nil),
            ("otpauth://totp/a#", Parts("otpauth", "", "totp", "/a", "", "", "", "/a", forceQuery: false), nil),
            ("mailto:x@y", Parts("mailto", "x@y", "", "", "", "", "", "", forceQuery: false), nil),
            ("otpauth://hé/x", Parts("otpauth", "", "hé", "/x", "", "", "", "/x", forceQuery: false), nil),
            ("otpauth://[::1/x", nil, "missing ']' in host"),
            ("otpauth://a@b@c/x", Parts("otpauth", "", "c", "/x", "", "", "", "/x", forceQuery: false), nil),
            ("otpauth://totp/a;b?c", Parts("otpauth", "", "totp", "/a;b", "", "c", "", "/a;b", forceQuery: false), nil),
            ("otpauth://totp/a#b#c", Parts("otpauth", "", "totp", "/a", "", "", "b#c", "/a", forceQuery: false), nil),
            ("otpauth://totp/a%20b%2fc", Parts("otpauth", "", "totp", "/a b/c", "/a%20b%2fc", "", "", "/a%20b%2fc", forceQuery: false), nil),
            ("otpauth://host:/p", Parts("otpauth", "", "host:", "/p", "", "", "", "/p", forceQuery: false), nil),
            ("otpauth://[::ffff:1.2.3.4]/p", Parts("otpauth", "", "[::ffff:1.2.3.4]", "/p", "", "", "", "/p", forceQuery: false), nil),
            ("otpauth://[1::2::3]/p", nil, "invalid host: ParseAddr(\"1::2::3\"): multiple :: in address (at \":3\")"),
            ("otpauth://x%zz@h/p", nil, "invalid URL escape \"%zz\""),
            ("otpauth://totp/%2541", Parts("otpauth", "", "totp", "/%41", "", "", "", "/%2541", forceQuery: false), nil),
            ("Otp-Auth.1+x://h/p", Parts("otp-auth.1+x", "", "h", "/p", "", "", "", "/p", forceQuery: false), nil),
            ("otpauth://h:99999999999999999999/p", Parts("otpauth", "", "h:99999999999999999999", "/p", "", "", "", "/p", forceQuery: false), nil),
            ("otpauth://totp/a?x#y?z", Parts("otpauth", "", "totp", "/a", "", "x", "y?z", "/a", forceQuery: false), nil),
        ]
        for (input, want, wantError) in cases {
            do {
                let got = try GoURL.parse(input)
                if let want {
                    XCTAssertEqual(Parts(got), want, input)
                } else {
                    XCTFail("\(input): parsed, want error \(wantError ?? "")")
                }
            } catch {
                guard let wantError else {
                    XCTFail("\(input): unexpected error \(error)")
                    continue
                }
                XCTAssertTrue(wantError.hasPrefix("\(error)"), "\(input): \(error) is not \(wantError)")
            }
        }
    }

    func testEscapeMatchesGo() {
        let cases: [(String, String, String)] = [
            ("a b+c/d:e@f?g=h&i%j#k~l", "a+b%2Bc%2Fd%3Ae%40f%3Fg%3Dh%26i%25j%23k~l", "a%20b+c%2Fd:e@f%3Fg=h&i%25j%23k~l"),
            ("é€", "%C3%A9%E2%82%AC", "%C3%A9%E2%82%AC"),
            ("\u{0}\u{7f}!*'();$,[]", "%00%7F%21%2A%27%28%29%3B%24%2C%5B%5D", "%00%7F%21%2A%27%28%29%3B$%2C%5B%5D"),
        ]
        for (input, query, path) in cases {
            XCTAssertEqual(GoURL.escape(input, .queryComponent), query, input)
            XCTAssertEqual(GoURL.escape(input, .pathSegment), path, input)
        }
    }

    func testUnescapeRejectsBadEscapes() throws {
        for bad in ["%", "%4", "%ZZ", "a%g0"] {
            XCTAssertThrowsError(try GoURL.unescape(Array(bad.utf8), .queryComponent), bad)
        }
        XCTAssertEqual(try GoURL.unescape(Array("a+b%2Bc".utf8), .queryComponent), Array("a b+c".utf8))
        XCTAssertEqual(try GoURL.unescape(Array("a+b%2Bc".utf8), .pathSegment), Array("a+b+c".utf8))
    }
}
