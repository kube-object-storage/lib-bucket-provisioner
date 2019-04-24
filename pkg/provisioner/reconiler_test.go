package provisioner

import (
	"fmt"
	"k8s.io/client-go/kubernetes/fake"
	"reflect"
	"testing"
	"time"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	externalFake "github.com/yard-turkey/lib-bucket-provisioner/pkg/client/clientset/versioned/fake"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// test global provisioner fields
type fields struct {
	client          *fake.Clientset
	extClient       *externalFake.Clientset
	provisionerName string
	provisioner     api.Provisioner
	retryInterval   time.Duration
	retryTimeout    time.Duration
	retryBackoff    int
}

var testFields = fields{
	client:          fake.NewSimpleClientset(),
	extClient:       externalFake.NewSimpleClientset(),
	provisionerName: provisionerName,
	provisioner:     &fakeProvisioner{},
	retryInterval:   1,
	retryTimeout:    1,
	retryBackoff:    1,
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
		var err error
		c := tt.fields.client
		if tt.class != nil {
			if tt.class, err = c.StorageV1().StorageClasses().Create(tt.class); err != nil {
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
		key string
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
				key: "",
			},
			want:    nil,
			wantErr: true,
		},
		{
			name:   "object exists for key",
			fields: testFields,
			args: args{
				key: fmt.Sprintf("%s/%s", testNamespace, testName),
			},
			want: &v1alpha1.ObjectBucketClaim{
				ObjectMeta: objMeta,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		ec := tt.fields.extClient

		var err error

		if tt.want != nil {
			if _, err = ec.ObjectbucketV1alpha1().ObjectBucketClaims(tt.want.Namespace).Create(tt.want); err != nil {
				t.Errorf("ObjectBucketClaimReconciler.claimForKey() error = %v,", fmt.Sprintf("error precreating object: %v", err))
			}
		}

		t.Run(tt.name, func(t *testing.T) {
			got, err := claimForKey(tt.args.key, ec)
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
