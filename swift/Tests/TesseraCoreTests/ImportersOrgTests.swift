import XCTest
import Foundation
@testable import TesseraCore

/// Ports go/internal/importers/org_test.go: same inputs, same results.
final class ImportersOrgTests: XCTestCase {

    private struct Want {
        var folder = ""
        var tags: [String] = []
        var pinned = false
        var note = ""
    }

    private static func onePUXItem(_ extra: String) -> String {
        #"{"details":{"sections":[{"fields":[{"value":{"totp":"JBSWY3DPEHPK3PXP"}}]}]},"# + extra + "}"
    }

    private static let cases: [(String, String, [Want])] = [
        ("aegis groups in file order, legacy group last",
         #"{"version":1,"db":{"version":3,"groups":[{"uuid":"g2","name":"Personal"},{"uuid":"g1","name":"Work"}],"entries":["#
            + #"{"type":"totp","name":"a","info":{"secret":"JBSWY3DPEHPK3PXP"},"groups":["g1","g2","g1"],"group":"Old","favorite":true}]}}"#,
         [Want(folder: "Work", tags: ["Personal", "Old"], pinned: true)]),
        ("aegis wrong-typed organisation members are ignored",
         #"{"version":1,"db":{"version":3,"groups":{"uuid":"g1"},"entries":["#
            + #"{"type":"totp","name":"a","info":{"secret":"JBSWY3DPEHPK3PXP"},"groups":"g1","group":7,"favorite":"true"},"#
            + #"{"type":"totp","name":"b","info":{"secret":"JBSWY3DPEHPK3PXP"},"groups":[1,null,{"uuid":"g1"}],"group":null,"favorite":1}]}}"#,
         [Want(), Want()]),
        ("aegis group list skips malformed rows; a repeated id keeps the later name",
         #"{"version":1,"db":{"version":3,"groups":[5,null,{"uuid":1,"name":"N"},{"uuid":"g1","name":2},{"UUID":"g1","Name":"First"},{"uuid":"g1","name":"Second"}],"entries":["#
            + #"{"type":"totp","name":"a","info":{"secret":"JBSWY3DPEHPK3PXP"},"groups":["g1"]}]}}"#,
         [Want(folder: "Second")]),
        ("2fas group by id; a missing group leaves the folder empty",
         #"{"services":[{"name":"A","secret":"JBSWY3DPEHPK3PXP","otp":{"tokenType":"TOTP"},"groupId":"x"},"#
            + #"{"name":"B","secret":"JBSWY3DPEHPK3PXP","otp":{"tokenType":"TOTP"},"groupId":"nope"},"#
            + #"{"name":"C","secret":"JBSWY3DPEHPK3PXP","otp":{"tokenType":"TOTP"},"groupId":5}],"#
            + #""groups":[{"id":"x","name":" \u202eWork\u0007 "}]}"#,
         [Want(folder: "Work"), Want(), Want()]),
        ("bitwarden folder first, collections after, duplicates dropped",
         #"{"folders":[{"id":"f","name":"Ops"}],"collections":[{"id":"c1","name":"Ops"},{"id":"c2","name":"Shared"},{"id":"c3","name":"  "}],"items":["#
            + #"{"name":"A","notes":"n\u0000ote","favorite":true,"folderId":"f","collectionIds":["c3","c2","c1","c9"],"login":{"totp":"JBSWY3DPEHPK3PXP"}},"#
            + #"{"name":"B","favorite":"yes","folderId":null,"collectionIds":["c2"],"login":{"totp":"JBSWY3DPEHPK3PXP"}}]}"#,
         [Want(folder: "Ops", tags: ["Shared"], pinned: true, note: "note"), Want(folder: "Shared")]),
        ("stratum categories joined through the secret",
         #"{"Authenticators":[{"Type":2,"Issuer":"A","Secret":"JBSWY3DPEHPK3PXP","Digits":6,"Period":30},{"Type":2,"Issuer":"B","Secret":"GEZDGNBVGY3TQOJQ","Digits":6,"Period":30}],"#
            + #""Categories":[{"Id":"k1","Name":"Work"},{"Id":"k2","Name":"Home"}],"#
            + #""AuthenticatorCategories":[{"CategoryId":"k2","AuthenticatorSecret":"JBSWY3DPEHPK3PXP"},{"CategoryId":"k1","AuthenticatorSecret":"JBSWY3DPEHPK3PXP"},5,{"CategoryId":"k1","AuthenticatorSecret":7}]}"#,
         [Want(folder: "Home", tags: ["Work"]), Want()]),
        ("andotp tags keep order, drop blanks and repeats, ignore non-strings",
         #"[{"secret":"JBSWY3DPEHPK3PXP","issuer":"A","label":"a","type":"TOTP","tags":["b","a",""," b ",3,null,"a"]},"#
            + #"{"secret":"JBSWY3DPEHPK3PXP","issuer":"B","label":"b","type":"TOTP","tags":"a"}]"#,
         [Want(tags: ["b", "a"]), Want()]),
        ("raivo pinned as string or bool",
         #"[{"issuer":"A","secret":"JBSWY3DPEHPK3PXP","kind":"TOTP","pinned":"true"},{"issuer":"B","secret":"JBSWY3DPEHPK3PXP","kind":"TOTP","pinned":true},"#
            + #"{"issuer":"C","secret":"JBSWY3DPEHPK3PXP","kind":"TOTP","pinned":"TRUE"},{"issuer":"D","secret":"JBSWY3DPEHPK3PXP","kind":"TOTP","pinned":1}]"#,
         [Want(pinned: true), Want(pinned: true), Want(), Want()]),
        ("1password vault name, tags and favourite index",
         #"{"accounts":[{"vaults":[{"attrs":{"name":" Private "},"items":["#
            + onePUXItem(#""favIndex":1,"overview":{"title":"A","tags":["x","x","y"]}"#) + ","
            + onePUXItem(#""favIndex":0,"overview":{"title":"B"}"#) + ","
            + onePUXItem(#""favIndex":-1,"overview":{"title":"C"}"#) + ","
            + onePUXItem(#""favIndex":0.5,"overview":{"title":"D"}"#) + ","
            + onePUXItem(#""favIndex":0e7,"overview":{"title":"E"}"#) + ","
            + onePUXItem(#""favIndex":"1","overview":{"title":"F","tags":"x"}"#) + "]},"
            + #"{"attrs":"Shared","items":["# + onePUXItem(#""overview":{"title":"G"}"#) + "]}]}]}",
         [Want(folder: "Private", tags: ["x", "y"], pinned: true),
          Want(folder: "Private"),
          Want(folder: "Private"),
          Want(folder: "Private", pinned: true),
          Want(folder: "Private"),
          Want(folder: "Private"),
          Want()]),
        ("1password csv favourite and semicolon tags",
         "Title,Url,Username,Password,OTPAuth,Favorite,Archived,Tags,Notes\n"
            + "A,,,,JBSWY3DPEHPK3PXP, TRUE ,false,;b;a;b; ;,\n"
            + "B,,,,JBSWY3DPEHPK3PXP,yes,false,,\n"
            + "C,,,,JBSWY3DPEHPK3PXP\n",
         [Want(tags: ["b", "a"], pinned: true), Want(), Want()]),
    ]

    func testImportersOrganisation() throws {
        for (name, data, want) in Self.cases {
            let found = try XCTUnwrap(try Importers.parse(Data(data.utf8)), name)
            XCTAssertEqual(found.accounts.count, want.count, name)
            for (i, (a, w)) in zip(found.accounts, want).enumerated() {
                let at = "\(name) account \(i)"
                XCTAssertEqual(Array(a.folder.utf8), Array(w.folder.utf8), "\(at): folder")
                XCTAssertEqual(a.tags.map { Array($0.utf8) }, w.tags.map { Array($0.utf8) }, "\(at): tags")
                XCTAssertEqual(a.pinned, w.pinned, "\(at): pinned")
                XCTAssertEqual(Array(a.note.utf8), Array(w.note.utf8), "\(at): note")
            }
        }
    }

    func testPlaceGroupsKeepsLongNames() {
        let long = String(repeating: "n", count: 10000)
        let placed = Importers.placeGroups(["", "  ", long, long, "b"])
        XCTAssertEqual(placed.folder, long)
        XCTAssertEqual(placed.tags, ["b"])
    }

    func testCleanTagsLargeInputIsLinear() {
        let tags = (0..<200_000).map { "t" + String(repeating: "x", count: $0 % 7) }
        XCTAssertEqual(Importers.cleanTags(tags).count, 7)
    }

    func testOnePUXOrganisationMembersKeepGoTypeErrors() {
        let cases: [(String, String)] = [
            (#"{"accounts":[{"vaults":[1]}]}"#,
             #"1password: json: cannot unmarshal number into onePUXFile.accounts.0.vaults.0 of type struct { Attrs jsontext.Value "json:\"attrs\""; Items []importers.onePUXItem "json:\"items\"" }"#),
            (#"{"accounts":[{"vaults":[]},{"vaults":"x"}]}"#,
             #"1password: json: cannot unmarshal string into Go struct field onePUXFile.accounts.1.vaults of type []struct { Attrs jsontext.Value "json:\"attrs\""; Items []importers.onePUXItem "json:\"items\"" }"#),
            (#"{"accounts":[{"vaults":[{"items":[{"overview":1}]}]}]}"#,
             #"1password: json: cannot unmarshal number into Go struct field onePUXFile.accounts.0.vaults.0.items.0.overview of type struct { Title string "json:\"title\""; Tags jsontext.Value "json:\"tags\"" }"#),
        ]
        for (input, want) in cases {
            XCTAssertThrowsError(try Importers.parse(Data(input.utf8)), input) { error in
                XCTAssertEqual("\(error)", want, input)
            }
        }
    }
}
