// Package epoch provides utilities for consistent UNIX epoch time representation
// across the medha-api project. It standardizes storing and transmitting time
// as int64 seconds since the Unix epoch instead of standard time.Time or strings.
package epoch

import (
	"time"
)

// Now returns the current UTC time as a UNIX epoch integer (seconds).
func Now() int64 {
	return time.Now().UTC().Unix()
}

// FromTime converts a standard Go time.Time to a UNIX epoch integer (seconds).
func FromTime(t time.Time) int64 {
	return t.Unix()
}

// ToTime converts a UNIX epoch integer (seconds) back to a standard Go time.Time in UTC.
func ToTime(epoch int64) time.Time {
	return time.Unix(epoch, 0).UTC()
}

// Pointer returns a pointer to the provided epoch value, useful for optional fields like deleted_at.
func Pointer(epoch int64) *int64 {
	return &epoch
}

// FromPointer safely dereferences an optional epoch pointer, returning 0 if nil.
func FromPointer(epochPtr *int64) int64 {
	if epochPtr == nil {
		return 0
	}
	return *epochPtr
}
