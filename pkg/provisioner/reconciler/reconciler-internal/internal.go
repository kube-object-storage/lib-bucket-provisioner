package reconciler_internal

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type InternalClient struct {
	Ctx    context.Context
	Client client.Client
	Scheme *runtime.Scheme
}
