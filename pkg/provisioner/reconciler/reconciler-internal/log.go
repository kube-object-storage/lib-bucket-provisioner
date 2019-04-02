package reconciler_internal

import (
	"github.com/go-logr/logr"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
	"k8s.io/klog/klogr"
)

var (
	log  logr.Logger
	logD logr.InfoLogger
)

func init() {
	log = klogr.New().WithName(api.Domain + "/internals")
	logD = log.V(1)
}
