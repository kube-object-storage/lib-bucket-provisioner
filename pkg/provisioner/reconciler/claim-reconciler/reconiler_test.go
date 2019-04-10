package reconciler

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/klogr"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testNamespace   = "test-namespace"
	testName        = "test-name"
	provisionerName = "dummyProvisioner"
	className       = "test-class"
)

var objMeta = metav1.ObjectMeta{
	Namespace: testNamespace,
	Name:      testName,
}

var (
	testLogI = klogr.New()
	testLogD = klogr.New().V(1)
)

// test global provisioner fields
type fields struct {
	ctx             context.Context
	internalClient  *internalClient
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
	provisioner:     &fakeProvisioner{},
	retryInterval:   1,
	retryTimeout:    1,
	retryBackoff:    1,
}

func TestNewObjectBucketClaimReconciler(t *testing.T) {

	const (
		retryInt, retryTO = 1, 1
	)

	type args struct {
		c           *internalClient
		name        string
		provisioner api.Provisioner
		options     Options
		scheme      *runtime.Scheme
	}
	tests := []struct {
		name string
		args args
		want *ObjectBucketClaimReconciler
	}{
		{
			name: "should set default options",
			args: args{
				c:           nil,
				name:        provisionerName,
				provisioner: &fakeProvisioner{},
				options: Options{
					RetryInterval: 0,
					RetryTimeout:  0,
				},
			},
			want: &ObjectBucketClaimReconciler{
				provisionerName: strings.ToLower(provisionerName),
				provisioner:     &fakeProvisioner{},
				retryInterval:   defaultRetryBaseInterval,
				retryTimeout:    defaultRetryTimeout,
			},
		},
		{
			name: "should set defined options",
			args: args{
				c:           nil,
				name:        provisionerName,
				provisioner: &fakeProvisioner{},
				options: Options{
					RetryInterval: retryInt,
					RetryTimeout:  retryTO,
				},
			},
			want: &ObjectBucketClaimReconciler{
				provisionerName: strings.ToLower(provisionerName),
				provisioner:     &fakeProvisioner{},
				retryInterval:   retryInt,
				retryTimeout:    retryTO,
			},
		},
	}
	for _, tt := range tests {

		tt.args.c = buildFakeInternalClient(t)

		t.Run(tt.name, func(t *testing.T) {
			got := NewObjectBucketClaimReconciler(tt.args.c.Client, tt.args.c.scheme, tt.args.name, tt.args.provisioner, tt.args.options)
			if n := strings.ToLower(tt.args.name); got.provisionerName != n {
				t.Errorf("ObjectBucketClaimReconciler.NewObjectBucketClaimReconciler() name = %v, want %v", got.provisionerName, tt.want.provisionerName)
			}

			// If the options value does not equal the set value, and the set value was not defaulted to
			// then something has gone wrong.
			if tt.args.options.RetryTimeout != tt.want.retryTimeout && tt.want.retryTimeout != defaultRetryTimeout {
				t.Errorf("ObjectBucketClaimReconciler.NewObjectBucketClaimReconciler() RetryTimeout = %v, want %v", got.retryTimeout, tt.want.retryTimeout)
			}
			if tt.args.options.RetryInterval != tt.want.retryInterval && tt.want.retryInterval != defaultRetryBaseInterval {
				t.Errorf("ObjectBucketClaimReconciler.NewObjectBucketClaimReconciler() RetryInterval = %v, want %v", got.retryInterval, tt.want.retryInterval)
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

		tt.fields.internalClient = buildFakeInternalClient(t)
		if tt.class != nil {
			if err := tt.fields.internalClient.Client.Create(tt.fields.ctx, tt.class); err != nil {
				t.Errorf("error precreating class: %v", err)
			}
		}

		t.Run(tt.name, func(t *testing.T) {
			if got := shouldProvision(tt.args.obc); got != tt.want {
				t.Errorf("ObjectBucketClaimReconciler.shouldProvision() = %v, want %v", got, tt.want)
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

		tt.fields.internalClient = buildFakeInternalClient(t)

		if tt.want != nil {
			if err := tt.fields.internalClient.Client.Create(tt.fields.ctx, tt.want); err != nil {
				t.Errorf("ObjectBucketClaimReconciler.claimForKey() error = %v,", fmt.Sprintf("error precreating object: %v", err))
			}
		}

		t.Run(tt.name, func(t *testing.T) {
			got, err := claimForKey(tt.args.key, tt.fields.internalClient)
			if (err != nil) != tt.wantErr {
				t.Errorf("ObjectBucketClaimReconciler.claimForKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ObjectBucketClaimReconciler.claimForKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
