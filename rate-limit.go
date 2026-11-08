package main

import (
	"hash/fnv"
	"net/http"
	"time"

	"github.com/coalaura/lock"
)

type RateLimitType int

const (
	RateLimitLogin RateLimitType = iota
)

var RateLimits = lock.NewSingleFlightMap[uint64, time.Time]()

func GetRateLimitFromType(typ RateLimitType) (string, time.Duration) {
	switch typ {
	case RateLimitLogin:
		return "login", 30 * time.Second
	}

	panic("unknown RateLimitType")
}

func CheckRateLimit(r *http.Request, typ RateLimitType) *time.Duration {
	ip := GetClientIP(r)

	name, limit := GetRateLimitFromType(typ)

	key := RateLimitKey(ip, name)

	lt, created, _ := RateLimits.GetOrFillWithTTL(key, limit, func() (time.Time, error) {
		return time.Now(), nil
	})

	if !created {
		now := time.Now()
		if now.Sub(lt) < limit {
			tryAgain := limit - now.Sub(lt)

			return &tryAgain
		}
	}

	return nil
}

func ClearRateLimit(r *http.Request, typ RateLimitType) {
	ip := GetClientIP(r)

	name, _ := GetRateLimitFromType(typ)

	key := RateLimitKey(ip, name)

	RateLimits.Delete(key)
}

func RateLimitKey(ip, path string) uint64 {
	h := fnv.New64a()

	h.Write([]byte(ip))
	h.Write([]byte{0})
	h.Write([]byte(path))

	return h.Sum64()
}
