module github.com/kube-object-storage/lib-bucket-provisioner

go 1.13

require (
	github.com/go-logr/logr v0.2.1
	github.com/google/go-cmp v0.4.0
	github.com/google/uuid v1.1.1
	github.com/hashicorp/golang-lru v0.5.3 // indirect
	golang.org/x/net v0.0.0-20201021035429-f5854403a974 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	k8s.io/api v0.19.3
	k8s.io/apimachinery v0.19.3
	k8s.io/client-go v0.19.3
	k8s.io/klog v1.0.0
)

require k8s.io/code-generator v0.19.3 // indirect
