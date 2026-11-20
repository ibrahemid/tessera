//go:build !unix

package store

import "time"

func removeStaleTemps(string, time.Time) {}
