package reconciler

import (
	"context"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// internalClient wraps related reconciler fields for convenient passing to helpers
type internalClient struct {
	Ctx    context.Context
	Client client.Client
	Scheme *runtime.Scheme
}
