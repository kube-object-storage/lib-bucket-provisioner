package reconciler

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/klog/klogr"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/reconciler/util"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
	internal "github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/reconciler/reconciler-internal"
)

const (
	testNamespace   = "test-namespace"
	testName        = "test-name"
	provisionerName = "dummyProvisioner"
	className       = "test-class"
	bucketName      = "test-bucket"
)

var objMeta = metav1.ObjectMeta{
	Namespace: testNamespace,
	Name:      testName,
}

var (
	testLogI = klogr.New()
	testLogD = klogr.New().V(util.DebugLogLvl)
)

// test global provisioner fields
type fields struct {
	ctx             context.Context
	internalClient  *internal.InternalClient
	provisionerName string
	provisioner     api.Provisioner
	retryInterval   time.Duration
	retryTimeout    time.Duration
	retryBackoff    int
}

var testFields = fields{
	ctx:             context.TODO(),
	internalClient:  nil, // generated per iteration
	provisionerName: provisionerName,
	provisioner:     &util.FakeProvisioner{},
	retryInterval:   1,
	retryTimeout:    1,
	retryBackoff:    1,
}

func BuildFakeInternalClient(t *testing.T, initObjs ...runtime.Object) (fakeClient *internal.InternalClient) {

	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Errorf("error adding core/v1 scheme: %v", err)
	}
	if err := storagev1.AddToScheme(scheme); err != nil {
		t.Errorf("error adding storage/v1 scheme: %v", err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Errorf("error adding storage/v1 scheme: %v", err)
	}
	fakeClient = &internal.InternalClient{
		Ctx:    context.Background(),
		Client: fake.NewFakeClientWithScheme(scheme, initObjs...),
		Scheme: scheme,
	}
	return fakeClient
}

func TestNewObjectBucketClaimReconciler(t *testing.T) {

	const (
		retryInt, retryTO = 1, 1
	)

	type args struct {
		c           *internal.InternalClient
		name        string
		provisioner api.Provisioner
		options     Options
		scheme      *runtime.Scheme
	}
	tests := []struct {
		name string
		args args
		want *objectBucketClaimReconciler
	}{
		{
			name: "should set default options",
			args: args{
				c:           nil,
				name:        provisionerName,
				provisioner: &util.FakeProvisioner{},
				options: Options{
					RetryInterval: 0,
					RetryTimeout:  0,
				},
			},
			want: &objectBucketClaimReconciler{
				provisionerName: strings.ToLower(provisionerName),
				provisioner:     &util.FakeProvisioner{},
				retryInterval:   util.DefaultRetryBaseInterval,
				retryTimeout:    util.DefaultRetryTimeout,
			},
		},
		{
			name: "should set defined options",
			args: args{
				c:           nil,
				name:        provisionerName,
				provisioner: &util.FakeProvisioner{},
				options: Options{
					RetryInterval: retryInt,
					RetryTimeout:  retryTO,
				},
			},
			want: &objectBucketClaimReconciler{
				provisionerName: strings.ToLower(provisionerName),
				provisioner:     &util.FakeProvisioner{},
				retryInterval:   retryInt,
				retryTimeout:    retryTO,
			},
		},
	}
	for _, tt := range tests {

		tt.args.c = BuildFakeInternalClient(t)

		t.Run(tt.name, func(t *testing.T) {
			got := NewObjectBucketClaimReconciler(tt.args.c, tt.args.name, tt.args.provisioner, tt.args.options)
			if n := strings.ToLower(tt.args.name); got.provisionerName != n {
				t.Errorf("objectBucketClaimReconciler.NewObjectBucketClaimReconciler() name = %v, want %v", got.provisionerName, tt.want.provisionerName)
			}

			// If the options value does not equal the set value, and the set value was not defaulted to
			// then something has gone wrong.
			if tt.args.options.RetryTimeout != tt.want.retryTimeout && tt.want.retryTimeout != util.DefaultRetryTimeout {
				t.Errorf("objectBucketClaimReconciler.NewObjectBucketClaimReconciler() RetryTimeout = %v, want %v", got.retryTimeout, tt.want.retryTimeout)
			}
			if tt.args.options.RetryInterval != tt.want.retryInterval && tt.want.retryInterval != util.DefaultRetryBaseInterval {
				t.Errorf("objectBucketClaimReconciler.NewObjectBucketClaimReconciler() RetryInterval = %v, want %v", got.retryInterval, tt.want.retryInterval)
			}
		})
	}
}

func Test_objectBucketClaimReconciler_shouldProvision(t *testing.T) {

	type args struct {
		obc *v1alpha1.ObjectBucketClaim
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
		class  *storagev1.StorageClass
	}{
		{
			name:   "should succeed if storage class exists",
			fields: testFields,
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: objMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						StorageClassName: className,
					},
				},
			},
			class: &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: className,
				},
				Provisioner: provisionerName,
			},
			want: true,
		},
	}
	for _, tt := range tests {

		tt.fields.internalClient = BuildFakeInternalClient(t)
		if tt.class != nil {
			if err := tt.fields.internalClient.Client.Create(tt.fields.ctx, tt.class); err != nil {
				t.Errorf("error precreating class: %v", err)
			}
		}

		t.Run(tt.name, func(t *testing.T) {
			r := &objectBucketClaimReconciler{
				InternalClient:  tt.fields.internalClient,
				provisionerName: tt.fields.provisionerName,
				provisioner:     tt.fields.provisioner,
				retryInterval:   tt.fields.retryInterval,
				retryTimeout:    tt.fields.retryTimeout,
				logD:            testLogD,
				logI:            testLogI,
			}
			if got := r.shouldProvision(tt.args.obc); got != tt.want {
				t.Errorf("objectBucketClaimReconciler.shouldProvision() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_objectBucketClaimReconciler_claimFromKey(t *testing.T) {

	type args struct {
		key client.ObjectKey
	}

	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *v1alpha1.ObjectBucketClaim
		wantErr bool
	}{
		{
			name:   "empty key values",
			fields: testFields,
			args: args{
				key: client.ObjectKey{
					Namespace: "",
					Name:      "",
				},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name:   "object exists for key",
			fields: testFields,
			args: args{
				key: client.ObjectKey{
					Namespace: testNamespace,
					Name:      testName,
				},
			},
			want: &v1alpha1.ObjectBucketClaim{
				ObjectMeta: objMeta,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {

		tt.fields.internalClient = BuildFakeInternalClient(t)

		if tt.want != nil {
			if err := tt.fields.internalClient.Client.Create(tt.fields.ctx, tt.want); err != nil {
				t.Errorf("objectBucketClaimReconciler.claimForKey() error = %v,", fmt.Sprintf("error precreating object: %v", err))
			}
		}

		t.Run(tt.name, func(t *testing.T) {
			r := &objectBucketClaimReconciler{
				InternalClient:  tt.fields.internalClient,
				provisionerName: tt.fields.provisionerName,
				provisioner:     tt.fields.provisioner,
				retryInterval:   tt.fields.retryInterval,
				retryTimeout:    tt.fields.retryTimeout,
				logD:            nil,
				logI:            nil,
			}
			got, err := r.claimForKey(tt.args.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("objectBucketClaimReconciler.claimForKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("objectBucketClaimReconciler.claimForKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
