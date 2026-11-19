//go:build darwin

package clipboard

import (
	"bytes"
	"strings"
)

// sensitiveWriteScript writes stdin as one pasteboard item marked concealed and
// transient, for this Mac only (kept off Universal Clipboard).
const sensitiveWriteScript = `ObjC.import('AppKit');
var input = $.NSFileHandle.fileHandleWithStandardInput.readDataToEndOfFile;
var text = $.NSString.alloc.initWithDataEncoding(input, $.NSUTF8StringEncoding);
if (text.isNil()) { throw new Error('input is not UTF-8'); }
var item = $.NSPasteboardItem.alloc.init;
if (!item.setStringForType(text, 'public.utf8-plain-text') ||
    !item.setStringForType('', 'org.nspasteboard.ConcealedType') ||
    !item.setStringForType('', 'org.nspasteboard.TransientType')) {
  throw new Error('pasteboard item rejected a type');
}
var pb = $.NSPasteboard.generalPasteboard;
pb.prepareForNewContentsWithOptions($.NSPasteboardContentsCurrentHostOnly);
if (!pb.writeObjects($.NSArray.arrayWithObject(item))) { throw new Error('pasteboard write failed'); }
`

const clearScript = `ObjC.import('AppKit');
$.NSPasteboard.generalPasteboard.clearContents;
`

type pasteboard struct {
	osascript string
	pbcopy    string
	pbpaste   string
}

func platformDetect(e env) (Clipboard, error) {
	if _, err := override(e); err != nil {
		return nil, err
	}
	return pasteboard{
		osascript: "/usr/bin/osascript",
		pbcopy:    "/usr/bin/pbcopy",
		pbpaste:   "/usr/bin/pbpaste",
	}, nil
}

func (pasteboard) Name() string { return "pbcopy" }

// Write marks a sensitive copy through NSPasteboard, which pbcopy cannot do,
// and falls back to pbcopy when osascript is missing or fails.
func (p pasteboard) Write(text string, sensitive bool) error {
	if sensitive && p.osascript != "" {
		if p.script(sensitiveWriteScript, text) == nil {
			return nil
		}
	}
	return run(p.Name(), "write", p.pbcopy, nil, strings.NewReader(text), nil)
}

func (p pasteboard) Read() (string, error) {
	var out bytes.Buffer
	if err := run(p.Name(), "read", p.pbpaste, nil, nil, &out); err != nil {
		return "", err
	}
	return out.String(), nil
}

// Clear empties the pasteboard, or leaves it holding "" when osascript fails.
func (p pasteboard) Clear() error {
	if p.osascript != "" && p.script(clearScript, "") == nil {
		return nil
	}
	return run(p.Name(), "clear", p.pbcopy, nil, strings.NewReader(""), nil)
}

func (p pasteboard) script(source, stdin string) error {
	return run(p.Name(), "write", p.osascript, []string{"-l", "JavaScript", "-e", source}, strings.NewReader(stdin), nil)
}
