package storetest

import (
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

const fileAllAccess = 0x1f01ff

// AssertOwnerOnly fails t unless path is a regular file whose protected DACL
// grants full control to the current user and SYSTEM and to no one else.
func AssertOwnerOnly(t testing.TB, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Errorf("%s mode = %v, want a regular file", path, info.Mode())
	}
	assertPrivateDACL(t, path, 0)
}

// AssertOwnerOnlyDir fails t unless path is a directory whose protected DACL
// grants full control, inherited by everything created inside it, to the
// current user and SYSTEM and to no one else.
func AssertOwnerOnlyDir(t testing.TB, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Errorf("%s mode = %v, want a directory", path, info.Mode())
	}
	assertPrivateDACL(t, path, windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE)
}

// assertPrivateDACL checks for a protected DACL with exactly two explicit
// full-control ACEs: the current user and SYSTEM.
func assertPrivateDACL(t testing.TB, path string, inherit uint8) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("read security info of %s: %v", path, err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Errorf("%s DACL is not protected", path)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl == nil || dacl.AceCount != 2 {
		t.Fatalf("%s DACL has %v ACEs, want 2", path, dacl)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	var sawUser, sawSystem bool
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			t.Errorf("%s ACE %d type = %d, want access allowed", path, i, ace.Header.AceType)
		}
		if ace.Mask != fileAllAccess {
			t.Errorf("%s ACE %d mask = %#x, want full control", path, i, ace.Mask)
		}
		if ace.Header.AceFlags&(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE) != inherit {
			t.Errorf("%s ACE %d flags = %#x, want inheritance %#x", path, i, ace.Header.AceFlags, inherit)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		switch {
		case sid.Equals(user.User.Sid):
			sawUser = true
		case sid.Equals(system):
			sawSystem = true
		default:
			t.Errorf("%s ACE %d grants access to %s", path, i, sid)
		}
	}
	if !sawUser || !sawSystem {
		t.Errorf("%s DACL user=%v system=%v, want both", path, sawUser, sawSystem)
	}
}
