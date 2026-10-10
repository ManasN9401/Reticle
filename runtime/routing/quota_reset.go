package routing

import (
	"regexp"
	"strconv"
	"time"
)

// maxQuotaReset bounds how far ahead a provider-reported reset is believed. A daily
// quota resets within a day; anything much further is a malformed or hostile value
// and must not lock a credential out for good.
const maxQuotaReset = 48 * time.Hour

var quotaResetHeader = regexp.MustCompile(`(?i)x-ratelimit-reset"?\s*[:=]\s*"?(\d{9,13})`)

// QuotaResetAt reads the time a provider says an exhausted quota resets, from the
// X-RateLimit-Reset value that OpenRouter and similar providers include in the error
// body (epoch milliseconds, or seconds). It returns the zero time when the text has
// no usable value, when the value is not in the future, or when it is implausibly far.
func QuotaResetAt(text string, now time.Time) time.Time {
	found := quotaResetHeader.FindStringSubmatch(text)
	if len(found) != 2 {
		return time.Time{}
	}
	value, err := strconv.ParseInt(found[1], 10, 64)
	if err != nil {
		return time.Time{}
	}
	// 13 digits is milliseconds, 9 to 10 digits is seconds.
	var reset time.Time
	if len(found[1]) >= 12 {
		reset = time.UnixMilli(value)
	} else {
		reset = time.Unix(value, 0)
	}
	if !reset.After(now) || reset.Sub(now) > maxQuotaReset {
		return time.Time{}
	}
	return reset
}
