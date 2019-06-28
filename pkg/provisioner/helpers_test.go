package provisioner

import (
	"fmt"
	"reflect"
	"regexp"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/fake"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	externalFake "github.com/kube-object-storage/lib-bucket-provisioner/pkg/client/clientset/versioned/fake"
	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/provisioner/api"
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

func TestStorageClassForClaim(t *testing.T) {

	const (
		storageClassName = "testStorageClass"
	)

	testObjectMeta := metav1.ObjectMeta{
		Name:      "testname",
		Namespace: "testnamespace",
		Finalizers: []string{
			finalizer,
		},
	}

	type args struct {
		obc       *v1alpha1.ObjectBucketClaim
		client    *fake.Clientset
		extClient *externalFake.Clientset
	}

	tests := []struct {
		name    string
		args    args
		want    *storagev1.StorageClass
		wantErr bool
	}{
		{
			name: "nil OBC ptr",
			args: args{
				obc:       nil,
				client:    fake.NewSimpleClientset(),
				extClient: externalFake.NewSimpleClientset(),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "nil storage class name",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: testObjectMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						StorageClassName: "",
					},
				},
				client:    fake.NewSimpleClientset(),
				extClient: externalFake.NewSimpleClientset(),
			},
			want:    nil,
			wantErr: true,
		}, {
			name: "non nil storage class name",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: testObjectMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						StorageClassName: storageClassName,
					},
				},
				client:    fake.NewSimpleClientset(),
				extClient: externalFake.NewSimpleClientset(),
			},
			want: &storagev1.StorageClass{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name: storageClassName,
				},
			},
			wantErr: false,
		},
		{
			name: "storage class does not exist",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: testObjectMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						StorageClassName: storageClassName,
					},
				},
				client:    fake.NewSimpleClientset(),
				extClient: externalFake.NewSimpleClientset(),
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			obc := tt.args.obc

			if obc != nil {
				if obc, err = tt.args.extClient.ObjectbucketV1alpha1().ObjectBucketClaims(obc.Namespace).Create(obc); err != nil {
					t.Errorf("error pre-creating OBC: %v", err)
				}
			}
			class := tt.want
			if class != nil {
				if class, err = tt.args.client.StorageV1().StorageClasses().Create(class); err != nil {
					t.Errorf("error pre-creating StorageClass: %v", err)
				}
			}

			got, err := storageClassForClaim(tt.args.client, tt.args.obc)
			if (err != nil) != tt.wantErr {
				t.Errorf("StorageClassForClaim() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("StorageClassForClaim() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateBucketName(t *testing.T) {
	type args struct {
		prefix string
	}
	tests := []struct {
		name    string
		args    args
		wanterr bool
	}{
		{
			name: "empty name",
			args: args{
				prefix: "",
			},
			wanterr: false,
		},
		{
			name: "below max name",
			args: args{
				prefix: "foobar",
			},
			wanterr: false,
		},
		{
			name: "over max name length name",
			args: args{
				prefix: rand.String(maxNameLen * 2),
			},
			wanterr: false,
		},
	}

	const pattern = `.*-[a-z0-9]{8}(-[a-z0-9]{4}){3}-[a-z0-9]{12}`

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateBucketName(tt.args.prefix)
			if len(got) > maxNameLen {
				t.Errorf("GenerateName() wanted len <= %d, got len %d", maxNameLen, len(got))
			}
			if match, err := regexp.MatchString(pattern, got); err != nil {
				t.Errorf("error matching pattern: %v", err)
			} else if !match {
				t.Errorf("GenerateName() want match: %v, got %v", pattern, got)
			}
		})
	}
}
