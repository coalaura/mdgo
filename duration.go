package main

import (
	"fmt"
	"math"
)

func ParseDuration(str string) (int64, error) {
	if str == "" {
		return 0, fmt.Errorf("duration is empty")
	}

	var total int64

	for i := 0; i < len(str); {
		if str[i] < '0' || str[i] > '9' {
			return 0, fmt.Errorf("invalid duration %q: expected number at byte %d", str, i)
		}

		var value int64

		for i < len(str) && str[i] >= '0' && str[i] <= '9' {
			digit := int64(str[i] - '0')
			if value > (math.MaxInt64-digit)/10 {
				return 0, fmt.Errorf("duration %q overflows int64", str)
			}

			value = value*10 + digit

			i++
		}

		if i == len(str) {
			return 0, fmt.Errorf("invalid duration %q: missing unit", str)
		}

		var multiplier int64

		switch str[i] {
		case 'd':
			multiplier = 24 * 60 * 60
		case 'h':
			multiplier = 60 * 60
		case 'm':
			multiplier = 60
		case 's':
			multiplier = 1
		default:
			return 0, fmt.Errorf("invalid duration %q: unknown unit %q at byte %d", str, str[i], i)
		}

		i++

		if value > (math.MaxInt64-total)/multiplier {
			return 0, fmt.Errorf("duration %q overflows int64", str)
		}

		total += value * multiplier
	}

	return total, nil
}
