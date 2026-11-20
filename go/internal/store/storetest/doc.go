// Package storetest checks that files and directories are readable by the
// current user only, the way the store writes them on each platform: mode
// 0600 or 0700 on Unix, a protected DACL granting only the user and SYSTEM on
// Windows.
package storetest
