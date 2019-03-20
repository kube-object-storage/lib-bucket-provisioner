package util

import (
	"flag"

	"k8s.io/klog"
)

func InitTestFlags() {
	klog.InitFlags(nil)
	_ = flag.Set("alsologtostderr", "true")
	_ = flag.Set("v", "2")
	if !flag.Parsed() {
		flag.Parse()
	}
}
