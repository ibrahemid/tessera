#!/usr/bin/env python3
# Writes a.html, b.html, c.html: three hero/install/app compositions over the same lower page.
from pathlib import Path

HERE = Path(__file__).parent

HEAD = """<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title}</title><link rel="stylesheet" href="wire.css"></head><body><main class="page">"""

HEADER = """<header class="band"><div class="header"><span class="wordmark"><span class="glyph"><span></span><span></span><span></span><span></span></span>Tessera</span>
<nav><a>CLI</a><a>Security</a><a>Changelog</a><a>GitHub</a><a class="btn btn-gold" style="height:2.3rem;padding-inline:.9rem;font-size:.88rem">Get Tessera</a></nav></div></header>"""

COPY = """<h1 class="h-display">Two-factor codes you own.</h1>
<p class="lede measure" style="margin-top:1.5rem">A native Mac app and the <span class="mono" style="color:var(--gold)">tess</span> command line on one documented vault format. Offline, with no account.</p>
<div style="margin-top:2rem;display:flex;flex-wrap:wrap;gap:.75rem;align-items:center"><img class="badge" src="assets/mac-app-store-badge.svg" alt=""><div class="cmd" style="max-width:100%"><code><span class="p">$</span>brew install ibrahemid/tap/tess</code><span>Copy</span></div></div>
<p class="faint" style="margin-top:1.2rem;font-size:.9rem">macOS 14 or later · Apache-2.0 · tess 1.0.2 for macOS and Linux</p>"""

TERM = """<figure class="term"><img src="assets/hero-poster.webp" alt=""></figure>"""

INSTALL_ROWS = """<dl class="rows" style="margin-top:1rem">
<div class="row"><dt>Homebrew</dt><dd><div class="cmd"><code><span class="p">$</span>brew trust ibrahemid/tap</code><span>Copy</span></div><div class="cmd" style="margin-top:.5rem"><code><span class="p">$</span>brew install ibrahemid/tap/tess</code><span>Copy</span></div></dd></div>
<div class="row"><dt>Script</dt><dd><div class="cmd"><code><span class="p">$</span>curl -fsSL https://raw.githubusercontent.com/ibrahemid/tessera/main/install.sh | sh</code><span>Copy</span></div></dd></div>
<div class="row"><dt>Tarballs</dt><dd>macOS Apple silicon · macOS Intel · Linux arm64 · Linux x86_64 <span class="mono faint" style="font-size:.78rem">sha256 under each</span></dd></div>
<div class="row"><dt>From source</dt><dd><div class="cmd"><code><span class="p">$</span>go install github.com/ibrahemid/tessera/go/cmd/tess@latest</code><span>Copy</span></div></dd></div>
</dl>"""

APP_COPY = """<h2 class="h-section">A native Mac app on the same vault.</h2>
<p class="lede measure" style="margin-top:1.2rem">Live codes with countdown rings, one-click copy, on-screen QR scanning and Touch ID unlock. It opens a vault the CLI created, and the CLI keeps working on the same file.</p>
<ul class="soft" style="margin-top:1.6rem;padding-left:1.1rem;display:grid;gap:.4rem"><li>TOTP, HOTP and Steam Guard.</li><li>Imports otpauth links, Google Authenticator exports, and Aegis, 2FAS or Raivo files.</li><li>Folders, pinning, and a short handle per account that both surfaces share.</li><li>Copied codes are marked concealed for clipboard managers, and the window is excluded from screen capture.</li></ul>"""

LOWER = """
<section class="band band-sunken" style="padding-block:5.5rem 5rem">
  <div class="measure"><h2 class="h-section">One vault, two implementations.</h2><p class="lede" style="margin-top:1.4rem">The Go CLI and the Swift app are separate codebases that must open each other's files byte for byte. The contract is written down, pinned to test vectors, and checked in CI on every push.</p></div>
  <div class="u-wide pillars" style="margin-top:3.5rem">
    <div class="pillar"><h3 class="h-sub">Canonical JSON</h3><p class="soft" style="margin-top:.8rem">Keys sorted by UTF-8 byte order, Go's string escaping with HTML escaping off, no whitespace, integers only. Both cores produce the same bytes.</p><p class="mono" style="font-size:.8rem;margin-top:1rem;color:var(--gold)">spec/vault-format.md</p></div>
    <div class="pillar"><h3 class="h-sub">Shared vector suite</h3><p class="soft" style="margin-top:.8rem">spec/testvectors.json pins envelopes, wraps, codes and the edge cases. Go tests, the swiftc verifier and the XCTest suite read the same file.</p><p class="mono" style="font-size:.8rem;margin-top:1rem;color:var(--gold)">spec/testvectors.json</p></div>
    <div class="pillar"><h3 class="h-sub">CI cross-decrypt</h3><p class="soft" style="margin-top:.8rem">Every push runs the Go suite, compiles TesseraCore against the vectors, and runs swift test, which decrypts a Go-written vault with real argon2id.</p><p class="mono" style="font-size:.8rem;margin-top:1rem;color:var(--gold)">.github/workflows/ci.yml</p></div>
  </div>
</section>
<section style="padding-block:5.5rem 4.5rem">
  <h2 class="h-section">Security you can read.</h2><p class="lede" style="margin-top:.8rem">Each line links to the file that does it.</p>
  <dl class="rows" style="margin-top:2.5rem">
    <div class="row"><dt>argon2id</dt><dd>A passphrase wraps the vault key through argon2id at 128 MiB, t=3, p=4 with a 16-byte salt.</dd></div>
    <div class="row"><dt>XChaCha20-Poly1305</dt><dd>A random 256-bit key encrypts the account payload with a fresh 24-byte nonce on every write.</dd></div>
    <div class="row"><dt>Secure Enclave wrap</dt><dd>The app wraps the key with a non-extractable Secure Enclave P-256 key. Require Touch ID adds the biometry access control.</dd></div>
    <div class="row"><dt>No network entitlement</dt><dd>The app's entitlements are App Sandbox, user-selected files and app-scoped bookmarks.</dd></div>
    <div class="row"><dt>Sandboxed</dt><dd>The Mac app runs in the App Sandbox and ships through the Mac App Store, where its privacy label reads Data Not Collected.</dd></div>
  </dl>
</section>
<section class="band band-ink" style="padding-block:6rem 5rem">
  <div><h2 class="h-display">All of it, free.</h2><div style="margin-top:2rem;display:flex;gap:.75rem;flex-wrap:wrap"><img class="badge" src="assets/mac-app-store-badge.svg" alt=""><a class="btn" style="height:3.1rem">Get the tess CLI</a></div></div>
  <div class="u-wide" style="border-top:1px solid var(--band-line);margin-top:5rem"></div>
  <div class="footer"><div><span class="wordmark">Tessera</span></div><div><b>Product</b>Mac App Store<br>Install<br>The app<br>Changelog<br>Press kit</div><div><b>CLI</b>tess reference<br>JSON output<br>Shell completions<br>Releases</div><div><b>Source</b>Repository<br>Vault format<br>Security<br>CI<br>Apache-2.0</div><div><b>Help</b>Support<br>New to 2FA<br>vs Ente Auth<br>vs Authy<br>Privacy</div></div>
</section>
</main></body></html>"""

# A: masthead. headline + copy in a row, terminal below at wide.
A = HEAD.format(title="A Masthead") + HEADER + f"""
<section class="band band-ink ref" style="padding-block:3.5rem 4rem"><span class="label">A masthead: copy row, then the terminal at wide</span>
  <div class="u-wide" style="display:grid;grid-template-columns:minmax(0,1.1fr) minmax(0,.9fr);gap:3rem;align-items:end">
    <h1 class="h-display">Two-factor codes you own.</h1>
    <div><p class="lede">A native Mac app and the <span class="mono" style="color:var(--gold)">tess</span> command line on one documented vault format. Offline, with no account.</p>
    <div style="margin-top:1.6rem;display:flex;flex-wrap:wrap;gap:.75rem;align-items:center"><img class="badge" src="assets/mac-app-store-badge.svg" alt=""><div class="cmd"><code><span class="p">$</span>brew install ibrahemid/tap/tess</code><span>Copy</span></div></div>
    <p class="faint" style="margin-top:1rem;font-size:.9rem">macOS 14 or later · Apache-2.0 · tess 1.0.2 for macOS and Linux</p></div>
  </div>
  <div class="u-wide" style="margin-top:2.75rem">{TERM}</div>
</section>
<section class="band" style="padding-block:3.5rem 4rem"><div class="u-wide" style="display:grid;grid-template-columns:minmax(0,.55fr) minmax(0,1.45fr);gap:4rem"><div><h2 class="h-sub">Install</h2><p class="soft" style="margin-top:.8rem;font-size:.95rem">The app and the CLI are separate downloads. They share one vault once you open the CLI's file in the app, or set a recovery passphrase in the app's settings.</p></div><div>{INSTALL_ROWS}</div></div></section>
<section class="band band-sunken" style="padding-block:5rem 0;"><div class="u-bleed-right" style="display:grid;grid-template-columns:minmax(0,.9fr) minmax(0,1.1fr);gap:4rem;align-items:end"><div style="padding-bottom:5rem">{APP_COPY}</div><div style="margin-bottom:-3rem"><img class="shot" src="assets/app-vault-light.png" alt="" style="border-radius:10px 0 0 10px"></div></div></section>
""" + LOWER.replace('class="band band-sunken" style="padding-block:5.5rem 5rem"', 'class="band" style="padding-block:8rem 5rem"')

# B: split. copy 5/12, terminal 7/12 aligned to wide-end.
B = HEAD.format(title="B Split") + HEADER + f"""
<section class="band band-ink ref" style="padding-block:4.5rem 4.5rem"><span class="label">B split: copy 5/12, terminal 7/12 ends at the header's edge</span>
  <div class="u-wide" style="display:grid;grid-template-columns:minmax(0,5fr) minmax(0,7fr);gap:3.5rem;align-items:center">
    <div>{COPY}</div>
    <div>{TERM}</div>
  </div>
</section>
<section class="band" style="padding-block:3.5rem 4rem"><div class="u-wide" style="display:grid;grid-template-columns:minmax(0,.55fr) minmax(0,1.45fr);gap:4rem"><div><h2 class="h-sub">Install</h2><p class="soft" style="margin-top:.8rem;font-size:.95rem">The app and the CLI are separate downloads. They share one vault once you open the CLI's file in the app, or set a recovery passphrase in the app's settings.</p></div><div>{INSTALL_ROWS}</div></div></section>
<section class="band band-sunken" style="padding-block:5rem 0;"><div class="u-bleed-right" style="display:grid;grid-template-columns:minmax(0,.9fr) minmax(0,1.1fr);gap:4rem;align-items:end"><div style="padding-bottom:5rem">{APP_COPY}</div><div style="margin-bottom:-3rem"><img class="shot" src="assets/app-vault-light.png" alt="" style="border-radius:10px 0 0 10px"></div></div></section>
""" + LOWER.replace('class="band band-sunken" style="padding-block:5.5rem 5rem"', 'class="band" style="padding-block:8rem 5rem"')

# C: paper hero, the terminal is the only dark object; install is the ink strip.
C = HEAD.format(title="C Paper") + HEADER + f"""
<section class="band ref" style="padding-block:4.5rem 4.5rem"><span class="label">C paper hero: dark terminal on the page, ink install strip below</span>
  <div class="u-wide" style="display:grid;grid-template-columns:minmax(0,5fr) minmax(0,7fr);gap:3.5rem;align-items:center">
    <div>{COPY}</div>
    <div>{TERM}</div>
  </div>
</section>
<section class="band band-ink" style="padding-block:3.5rem 4rem"><div class="u-wide" style="display:grid;grid-template-columns:minmax(0,.55fr) minmax(0,1.45fr);gap:4rem"><div><h2 class="h-sub">Install</h2><p class="soft" style="margin-top:.8rem;font-size:.95rem">The app and the CLI are separate downloads. They share one vault once you open the CLI's file in the app, or set a recovery passphrase in the app's settings.</p></div><div>{INSTALL_ROWS}</div></div></section>
<section class="band" style="padding-block:5rem 0;"><div class="u-bleed-right" style="display:grid;grid-template-columns:minmax(0,.9fr) minmax(0,1.1fr);gap:4rem;align-items:end"><div style="padding-bottom:5rem">{APP_COPY}</div><div style="margin-bottom:-3rem"><img class="shot" src="assets/app-vault-light.png" alt="" style="border-radius:10px 0 0 10px"></div></div></section>
""" + LOWER

MOBILE = """<style>@media (max-width:900px){ [style*="grid-template-columns"]{grid-template-columns:1fr!important} .u-bleed-right img{border-radius:10px!important} [style*="margin-bottom:-3rem"]{margin-bottom:0!important} [style*="padding-bottom:5rem"]{padding-bottom:2rem!important} }</style>"""

for name, html in (("a", A), ("b", B), ("c", C)):
    (HERE / f"{name}.html").write_text(html.replace("</head>", MOBILE + "</head>"))
print("ok")
