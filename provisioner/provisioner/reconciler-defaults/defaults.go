package reconciler_defaults

import "time"

const (
	DefaultRetryInterval     = time.Second * 10
	DefaultRetryTimeout      = time.Second * 360
	DefaultConditionRetryMax = 5
)
