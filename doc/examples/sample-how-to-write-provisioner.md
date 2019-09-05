# Developer Notes: How to Write a Provisioner

> Some notes on how to write your own provisioner.

- [Overview and Summary](#overview-and-summary)
  * [Key Concepts](#key-concepts)
  * [General Usage Flow](#general-usage-flow)
- [Developer Setup](#developer-setup)
  * [Prerequisites and Key Components](#prerequisites-and-key-components)
  * [Project Layout](#project-layout)  
    + [Create a GitHub Repo](#create-a-github-repo)
    + [Sample Directory Structure](#sample-directory-structure)    
- [Code the Provisioner](#code-the-provisioner)
  * [Import Library and Other Common Packages](#import-library-and-other-common-packages)
  * [Import Provisioner Specific Libraries](#import-provisioner-specific-libraries)
  * [Implement Library Interface Stub](#implement-library-interfacestub)  
  * [Sample Code](#sample-code)  
  * [Dependency Management](#dependency-management)  
- [Testing Provisioner](#testing-provisioner)
  * [Build and Test Local Binary](#build-and-test-local-binary)
  * [Build and Test Docker Image](#build-and-test-docker-image)  
- [Usage Examples](#usage-examples)

<!-- toc -->

## Overview and Summary

This example will walk through the steps on how to create your own provisioner. For this
example we will be revisiting some of the key concepts and steps we did to
produce the AWS-S3-Provisioner. 

For additional information on the design of the library take a look [here](https://github.com/kube-object-storage/lib-bucket-provisioner/blob/master/doc/design/object-bucket-lib.md)

To contribute or view the library code take a look [here](https://github.com/kube-object-storage/lib-bucket-provisioner)

### Key Concepts

- Library uses the [ObjectBucket and ObjectBucketClaim](https://github.com/kube-object-storage/lib-bucket-provisioner/blob/master/deploy/crds) CustomResourceDefinition that is very closely modeled after the existing Kubernetes PV and PVC patterns.

- The Library and Provisoners also use other common Kubernetes Dynamic Provisioning resources, such as StorageClasses, Secrets and ConfigMaps.

### General Usage Flow

*Cluster:*
- CRD is deployed on cluster.
- Provisioner/Operator is deployed on the target cluster.

*Admin:*
- Admin creates StorageClass identifying the provisioner it will serve, the StorageClass will have a free-form parameters 
section that allows each provisioner flexibility in what is required for it to serve the requests.
- Admin might need to create additional Secrets or some kind of access/credentials to the StorageClass to give the provisioner proper permissions to act on Buckets/Endpoints/Requests.
- Admin might also need to create proper service accounts for the Provisioner to run.

*User:*
- User creates an [OBC request](https://github.com/kube-object-storage/lib-bucket-provisioner/blob/master/deploy/example-claim.yaml) (Similar to a PVC) that points to the StorageClass of the provisioner.

*Library/Provisioner:*
- Watches for all OBC's in all Namespaces, If the provisioner exists, it will queue and work on the request.
- Returns OB, Secrets and ConfigMaps to the User/Cluster for consumption.
- Manages all internals of the Kubernetes framework, controller loop logic, etc...

**[Note]** The developer only needs to focus on implementing the main interfaces defined by the library specific to the needs of their backend/provisioner.




## Developer Setup
The following sections will lend some guidance on how a provisioner can be developed, built and tested.

### Prerequisites and Key Components
1. Access to a Kuberenetes Cluster
- Run local with [Minikube](https://kubernetes.io/docs/setup/minikube/).
- Run local with [hack/local-up-cluster.sh](https://github.com/kubernetes/kubernetes/blob/master/hack/local-up-cluster.sh) from [Kubernetes repo](https://github.com/kubernetes/kubernetes)
- Cloud Cluster running on AWS using [Kops](https://kubernetes.io/docs/setup/custom-cloud/kops/)
- etc...

2. Docker
  - Best to install latest Docker, but this should all work with minimum of Docker 1.13.1

3. Access to a public image/application repository
  - Personal Account/Repository on either [docker.io](https://hub.docker.com/) or [quay.io](https://quay.io/repository/)

4. GoLang/IDE
- go1.11.4 or higher should be fine



### Project Layout
A project can be structured in several different ways, this is just one example of a 
project model that was used with the [AWS S3 Provisioner](https://github.com/kube-object-storage/aws-s3-provisioner).

#### Create a GitHub Repo
1. Login to Github and create a personal repository for your project. Our team uses a generic repo where we
typically put dev projects called [kube-object-storage](https://github.com/kube-object-storage).

```
   Good repo should be easy to remmember and find:
   https://github.com/<team repo>/<specific app/project name> i.e. /kube-object-storage/aws-s3-provisioner
   
   You can also use your personal repo for dev if you don't have a formal or team repo to use
   https://github.com/<github user>/<specific app/project name> i.e. /screeley44/aws-s3-provisioner
```


#### Sample Directory Structure

1. Build your repo locally or in github.

From your local $GOPATH build the common *src/github.com* directory structure
```
  # mkdir -p <$GOPATH>/src/github.com/<repo>/<app>
```

2. Create the basic directory structure from the *<Repo Root>* directory. The below example is by no means the
only way to structure a provisioner, the key is to do what makes sense for you and your project.

```
        --> Repo Root dir
            --> cmd (source directory for main package go files)
                --> awss3provisioner.go
                --> util.go
                --> etc...
            --> bin (binary directory for builds)
            --> hack (scripts and utilities directory if needed)
            --> examples or docs (directory for documentation if needed)
            README.md (start up README)
            
```

3. Create a generic .gitignore file.

```
# Logs and archives
*.log
*.tar.*
*.zip
# Binaries for programs and plugins
*.exe
*.exe~
*.dll
*.so
*.dylib
bin/*
# Test binary, build with `go test -c`
*.test

# Output of the go coverage tool, specifically when used with LiteIDE
*.out

# IDE config
.idea*
```
## Code the Provisioner
Now the basic project structure is in place, you can begin building the provisioner. We will add a
*provisioner*.go file in the *Repo Root*/cmd/ directory.
### Import Library and Other Common Packages
```
	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	libbkt "github.com/kube-object-storage/lib-bucket-provisioner/pkg/provisioner"
	apibkt "github.com/kube-object-storage/lib-bucket-provisioner/pkg/provisioner/api"

	storageV1 "k8s.io/api/storage/v1"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
```
### Import Provisioner Specific Libraries
These will vary, project to project, but for example, for our implementation with the AWS S3 Provisioner we needed to import components of the AWS S3 SDK.
```
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	awsuser "github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
```
Maybe for something like Google Cloud Storage (GCS) you might need something like
```
	gcs "cloud.google.com/go/storage"
```
Some other common GoLang packages might be similar to this
```
	"context"
	"flag"
	"fmt"
	_ "net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
```
### Implement Library Interface Stub
The actual implementation of these 4 interfaces is strictly in the hands of the provisioner developer. These interfaces
form a contract with the library, but each implementation could vary based on vendor specific details.
1. Main Library Interfaces
*Provision* - Create a new bucket based on ObjectBucketClaim (OB)
```
   func (p gcsProvisioner) Provision(options *apibkt.BucketOptions) (*v1alpha1.ObjectBucket, error) {}
```
*Delete* - De-provision a bucket that has an existing ObjectBucket (OB) resource attached
```
   func (p gcsProvisioner) Delete(ob *v1alpha1.ObjectBucket) error {}
```
*Grant* - Create Access to an existing Static Bucket based on StorageClass and OBC resource.
```
   func (p gcsProvisioner) Grant(options *apibkt.BucketOptions) (*v1alpha1.ObjectBucket, error) {}
```
*Revoke* - Remove access to an existing static Bucket.
```
    func (p gcsProvisioner) Revoke(ob *v1alpha1.ObjectBucket) error {}
```
2. General ObjectBucket Return object that the library expects on creates.
```
// Return the OB struct with minimal fields filled in.
func (p *gcsProvisioner) rtnObjectBkt(bktName string) *v1alpha1.ObjectBucket {

	host := strings.Replace(s3Hostname, regionInsert, p.region, 1)
	conn := &v1alpha1.Connection{
		Endpoint: &v1alpha1.Endpoint{
			BucketHost: host,
			BucketPort: httpsPort,
			BucketName: bktName,
			Region:     p.region,
		},
		Authentication: &v1alpha1.Authentication{
			AccessKeys: &v1alpha1.AccessKeys{
				AccessKeyID:     p.bktUserAccessId,
				SecretAccessKey: p.bktUserSecretKey,
			},
		},
		AdditionalState: map[string]string{
			obStateARN:  p.bktUserPolicyArn,
			obStateUser: p.bktUserName,
		},
	}

	return &v1alpha1.ObjectBucket{
		Spec: v1alpha1.ObjectBucketSpec{
			Connection: conn,
		},
	}
}
```
### Sample Code
Take a look at some existing provisioners to get an idea of how these interfaces are implemented and you can
most likely use these a template to get started, updating where it is appropriate.
[AWS-S3-Provisioner](https://github.com/kube-object-storage/aws-s3-provisioner)
[Rook-Ceph Provisioner](TBD)
### Dependency Management
The Bucket Library uses `client-go v1.11` and `Kubernete v1.14`. If your project uses different versions
of these packages, there may be dependency resolution issues.
The dependency landscape may be challenging until we (including Kubernetes) use Go modules.

The AWS S3 Provisioner does use Go modules [vgo](https://github.com/golang/vgo) for dependency management and builds easily.
On the other hand, the Rook-Ceph RGW provisioner uses [Dep](https://golang.github.io/dep/docs/installation.html), which is more fragile and restrictive.
In fact, after many tries, we could not build the Rook-Ceph operator using the _runtime-controller_ package originally imported by the library.
This was due to unresolvable deps between _client-go_, _kubernetes_, _controller-runtime_, and Rook's own _Gopkg.toml_ file.
The fix was for the library to not import _controller-runtime_ and, instead, code the needed controller pieces directly into the lib.

The library probably will need to support a branch for each version of _client-go_ needed by provisioners.

Steps:
1. Install vgo
```
 # go get -u golang.org/x/vgo.
```

2. Initialize vgo
```
 # vgo build
 # vgo update
```

3. Create and Manage the vendor directory
```
# vgo mod vendor
```
**[NOTE]** This will pull in all the project dependencies and create the <Repo Root/*vendor* directory
and the *go.mod* and *go.sum* files. If your imports and dependencies change, just rerun the above command.




## Testing Provisioner
As mentioned above, if you don't have a test cluster available and running, now is the time to get one running.
See links above for some guidance on how one might go about doing that.

### Build and Test Local Binary

1. Build the provisioner binary.
```
 # go build -a -o ./bin/<provisioner name>  ./<source dir>/...
 i.e.
 # go build -a -o ./bin/aws-s3-provisioner  ./cmd/...
```

2. Install the [OB/OBC CRDs](https://github.com/kube-object-storage/lib-bucket-provisioner/blob/master/deploy/crds) on your cluster.


3. Push the binary to a remote cluster to test or run it on your local cluster if you have one.
```
# scp /bin/awss3provisioner <user>@<kube-cluster-host>:~
```

Run the provisioner (after the CRD's are created) passing in *master* and *kubeconfig* parameters. (assumes a simple local-up-cluster.sh implemenation)
```
 # ./awss3provisioner -master https://localhost:6443 -kubeconfig /var/run/kubernetes/admin.kubeconfig -alsologtostderr -v=2
I0403 10:30:40.881043   16396 aws-s3-provisioner.go:458] AWS S3 Provisioner - main
I0403 10:30:40.881264   16396 aws-s3-provisioner.go:459] flags: kubeconfig="/var/run/kubernetes/admin.kubeconfig"; masterURL="https://localhost:6443"
I0403 10:30:40.883873   16396 manager.go:75] objectbucket.io "level"=0 "msg"="new provisioner"  "name"="aws-s3.io/bucket"
I0403 10:30:40.884624   16396 manager.go:87] objectbucket.io "level"=2 "msg"="generating controller manager"  
I0403 10:30:40.923627   16396 manager.go:94] objectbucket.io "level"=2 "msg"="adding schemes to manager"  
I0403 10:30:40.923742   16396 reconiler.go:54] objectbucket.io/reconciler/aws-s3.io/bucket "level"=0 "msg"="constructing new reconciler"  "provisioner"="aws-s3.io/bucket"
I0403 10:30:40.923766   16396 reconiler.go:59] objectbucket.io/reconciler/aws-s3.io/bucket "level"=2 "msg"="retry loop setting"  "RetryBaseInterval"=10000000000
I0403 10:30:40.923779   16396 reconiler.go:63] objectbucket.io/reconciler/aws-s3.io/bucket "level"=2 "msg"="retry loop setting"  "RetryTimeout"=360000000000
I0403 10:30:40.923791   16396 manager.go:132] objectbucket.io "level"=0 "msg"="building controller manager"  
I0403 10:30:40.924741   16396 aws-s3-provisioner.go:472] main: running aws-s3.io/bucket provisioner...
I0403 10:30:40.924763   16396 manager.go:150] objectbucket.io "level"=0 "msg"="Starting manager"  "provisioner"="aws-s3.io/bucket"
```

If using a real cluster, like Kops, passing in the *master* and *kubeconfig* parameters.
```
 # kops export kubecfg --name=<kops cluster name from output>
 i.e.
 # kops export kubecfg --name=screeley-s3prov.screeley.sysdeseng.com
 #./awss3provisioner -master https://api.screeley-s3prov.screeley.sysdeseng.com -kubeconfig /home/centos/.kube/config -alsologtostderr -v=2
```


### Build and Test Docker Image
1. Add a simple Dockerfile to the project in the <Repo Root> directory.

```
FROM fedora:29
COPY ./bin/aws-s3-provisioner /usr/bin/
ENTRYPOINT ["/usr/bin/aws-s3-provisioner"]
CMD ["-v=2", "-alsologtostderr"]
```

2. Login to docker and quay.io.
```
 # docker login
 # docker login quay.io
```

3. Build the image and push it to quay.io or docker.io.
```
 # docker build . -t quay.io/<your_quay_account>/aws-s3-provisioner:v1.0.0
 # docker push quay.io/<your_quay_account>/aws-s3-provisioner:v1.0.0
```

i.e.

```
 # docker build . -t quay.io/screeley44/aws-s3-provisioner:v1.0.0
 # docker push quay.io/screeley44/aws-s3-provisioner:v1.0.0
```

4. You can now create a deployment or pod to test your image and provisioner.

## Usage Examples

End-to-End [Examples](https://github.com/kube-object-storage/examples-and-blogs/tree/master/examples)
