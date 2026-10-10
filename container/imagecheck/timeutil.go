package main

import "time"

// unixTime converts seconds since the epoch to a UTC time.
func unixTime(sec int64) time.Time { return time.Unix(sec, 0).UTC() }
