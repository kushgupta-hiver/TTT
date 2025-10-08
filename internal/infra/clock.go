package infra

import "time"

type Clock interface {
	Now() time.Time
}
