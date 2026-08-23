Output stills.gif
Set Shell bash
Set FontFamily "Menlo"
Set FontSize 24
Set Width 1240
Set Height 560
Set Padding 36
Set LineHeight 1.35
Set TypingSpeed 1ms
Set Theme { "name": "tessera", "background": "#0e0f13", "foreground": "#ecedf1", "cursor": "#e3b23c", "black": "#16181f", "red": "#e5643e", "green": "#4cc27a", "yellow": "#e3b23c", "blue": "#6f9bff", "magenta": "#b48cff", "cyan": "#2bb3a3", "white": "#ecedf1", "brightBlack": "#6a6f7c", "brightRed": "#ff7a55", "brightGreen": "#5fd38d", "brightYellow": "#f0c45a", "brightBlue": "#8fb3ff", "brightMagenta": "#c9a8ff", "brightCyan": "#4fd0c0", "brightWhite": "#ffffff" }

Hide
Type "export TESSERA_VAULT=vault.json TESSERA_PASSPHRASE='correct horse battery staple' PS1='$ '"
Enter
Sleep 500ms
Type "tess list | grep -q GitHub || tess add github-2fa.png"
Enter
Sleep 2500ms
Type "clear"
Enter
Sleep 300ms
Show
Type "tess vault status"
Enter
Sleep 1500ms
Screenshot still-vault-status.png
Sleep 300ms
Hide
Type "clear"
Enter
Sleep 300ms
Show
Type "tess code nope"
Enter
Sleep 1500ms
Screenshot still-not-found.png
Sleep 300ms
Hide
Type "clear"
Enter
Sleep 300ms
Show
Type "tess show github"
Enter
Sleep 1500ms
Screenshot still-show.png
Sleep 300ms
Hide
Type "clear"
Enter
Sleep 300ms
Show
Type "tess"
Enter
Sleep 1500ms
Screenshot still-codes.png
Sleep 300ms
