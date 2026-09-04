//go:build !unix

package main

import "syscall"

func detachedProcAttr() *syscall.SysProcAttr { return nil }
