package reconciler

import (
	"context"
	"fmt"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/reconciler/util"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
)

const (
	testNamespace   = "test-namespace"
	testName        = "test-name"
	provisionerName = "dummyProvisioner"
)

var objMeta = metav1.ObjectMeta{
	Namespace: testNamespace,
	Name:      testName,
}

// test global provisioner fields
type fields struct {
	ctx             context.Context
	client          client.Client
	provisionerName string
	provisioner     api.Provisioner
	retryInterval   time.Duration
	retryTimeout    time.Duration
	retryBackoff    int
}

var testFields = fields{
	ctx:             context.TODO(),
	client:          nil, // generated per iteration
	provisionerName: provisionerName,
	provisioner:     &dummyProvisioner{},
	retryInterval:   0,
	retryTimeout:    0,
	retryBackoff:    0,
}

type dummyProvisioner struct{}

func (dp *dummyProvisioner) Provision(options *api.BucketOptions) (connection *v1alpha1.Connection, err error) {
	if options == nil || options.ObjectBucketClaim == nil {
		return nil, fmt.Errorf("got nil ptr")
	}
	return &v1alpha1.Connection{
		Endpoint: &v1alpha1.Endpoint{
			BucketHost: "www.test.com",
			BucketPort: 11111,
			BucketName: options.BucketName,
			Region:     "",
			SubRegion:  "",
			SSL:        false,
		},
		Authentication: &v1alpha1.Authentication{},
	}, nil
}

func (dp *dummyProvisioner) Delete(ob *v1alpha1.ObjectBucket) (err error) {
	if ob == nil {
		err = fmt.Errorf("got nil object bucket pointer")
	}
	return err
}

func BuildFakeClient(t *testing.T, initObjs ...runtime.Object) (fakeClient client.Client) {

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
	fakeClient = fake.NewFakeClientWithScheme(scheme, initObjs...)

	return fakeClient
}

func TestNewObjectBucketClaimReconciler(t *testing.T) {
	type args struct {
		c           client.Client
		name        string
		provisioner api.Provisioner
		options     Options
	}
	tests := []struct {
		name string
		args args
		want *objectBucketClaimReconciler
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewObjectBucketClaimReconciler(tt.args.c, tt.args.name, tt.args.provisioner, tt.args.options); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewObjectBucketClaimReconciler() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_objectBucketClaimReconciler_Reconcile(t *testing.T) {
	type fields struct {
		ctx             context.Context
		client          client.Client
		provisionerName string
		provisioner     api.Provisioner
		retryInterval   time.Duration
		retryTimeout    time.Duration
		retryBackoff    int
	}

	testFields := fields{
		ctx:             context.TODO(),
		client:          BuildFakeClient(t),
		provisionerName: "test-provisioner",
		provisioner:     &dummyProvisioner{},
	}

	type args struct {
		request reconcile.Request
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    reconcile.Result
		wantErr bool
	}{
		{
			name:   "nil request",
			fields: testFields,
			args: args{
				request: reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: "",
						Name:      "",
					},
				},
			},
			want:    reconcile.Result{},
			wantErr: true,
		},
		{
			name:   "stale request",
			fields: testFields,
			args: args{
				request: reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: testNamespace,
						Name:      testName,
					},
				},
			},
			want:    reconcile.Result{},
			wantErr: true,
		},
		{
			name:   "processable request",
			fields: testFields,
			args: args{
				request: reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: testNamespace,
						Name:      testName,
					},
				},
			},
			want:    reconcile.Result{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &objectBucketClaimReconciler{
				ctx:             tt.fields.ctx,
				client:          tt.fields.client,
				provisionerName: tt.fields.provisionerName,
				provisioner:     tt.fields.provisioner,
				retryInterval:   tt.fields.retryInterval,
				retryTimeout:    tt.fields.retryTimeout,
				retryBackoff:    tt.fields.retryBackoff,
			}

			className := "test-class"

			class := &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: className,
				},
				Provisioner: tt.fields.provisionerName,
			}

			claim := &v1alpha1.ObjectBucketClaim{
				ObjectMeta: objMeta,
				Spec: v1alpha1.ObjectBucketClaimSpec{
					StorageClassName: className,
				},
			}

			if ! tt.wantErr && tt.args.request != (reconcile.Request{}) {
				if err := tt.fields.client.Create(tt.fields.ctx, class); err != nil {
					t.Errorf("error precreating storage class: %v", err)
				}
				if err := tt.fields.client.Create(tt.fields.ctx, claim); err != nil {
					t.Errorf("error precreating object bucket claim: %v", err)
				}
			}

			got, err := r.Reconcile(tt.args.request)
			if (err != nil) != tt.wantErr {
				t.Errorf("objectBucketClaimReconciler.Reconcile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("objectBucketClaimReconciler.Reconcile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_objectBucketClaimReconciler_handelReconcile(t *testing.T) {

	const (
		obname     = "test-ob"
		policy     = "retain"
		bucketName = "test-bucket"
		className  = "test-class"
	)

	type args struct {
		options *api.BucketOptions
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name:   "nil options ptr",
			fields: testFields,
			args: args{
				options: nil,
			},
			wantErr: true,
		},
		{
			name:   "provisioner method success",
			fields: testFields,
			args: args{
				options: &api.BucketOptions{
					ReclaimPolicy:    policy,
					ObjectBucketName: obname,
					BucketName:       bucketName,
					ObjectBucketClaim: &v1alpha1.ObjectBucketClaim{
						ObjectMeta: objMeta,
						Spec: v1alpha1.ObjectBucketClaimSpec{
							StorageClassName: className,
							BucketName:       bucketName,
						},
					},
					Parameters: map[string]string{},
				},
			},
			wantErr: false,
		},
		{
			name:   "cleanup on failure",
			fields: testFields,
			args: args{
				options: &api.BucketOptions{
					ReclaimPolicy:    "delete",
					ObjectBucketName: obname,
					BucketName:       "",
					ObjectBucketClaim: &v1alpha1.ObjectBucketClaim{
						ObjectMeta: objMeta,
					},
					Parameters: map[string]string{},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {

		class := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: className,
			},
			Provisioner: tt.fields.provisionerName,
		}
		tt.fields.client = BuildFakeClient(t, class)
		if tt.args.options != nil && tt.args.options.ObjectBucketClaim != nil {
			if err := tt.fields.client.Create(tt.fields.ctx, tt.args.options.ObjectBucketClaim); err != nil {
				t.Errorf("Error creating test obc: %v", err)
			}
		}

		t.Run(tt.name, func(t *testing.T) {
			r := &objectBucketClaimReconciler{
				ctx:             tt.fields.ctx,
				client:          tt.fields.client,
				provisionerName: tt.fields.provisionerName,
				provisioner:     tt.fields.provisioner,
				retryInterval:   tt.fields.retryInterval,
				retryTimeout:    tt.fields.retryTimeout,
				retryBackoff:    tt.fields.retryBackoff,
			}

			reconcileErr := r.handelReconcile(tt.args.options)

			// Excluding expected nil ptr err, check if resources were generated or cleaned up depending on
			// expectations
			if tt.args.options != nil && tt.args.options.ObjectBucketClaim != nil {
				if (reconcileErr != nil) != tt.wantErr {
					// Got an unexpected error
					t.Errorf("objectBucketClaimReconciler.handelReconcile() error = %v, wantErr %v", reconcileErr, tt.wantErr)
				}

				// From here down, either there is no error or we got an expected error

				obcKey, err := client.ObjectKeyFromObject(tt.args.options.ObjectBucketClaim)
				if err != nil {
					t.Errorf("error forming object key: %v", err)
				}
				obKey := client.ObjectKey{
					Name: fmt.Sprintf(util.ObjectBucketFormat, obcKey.Namespace, obcKey.Name),
				}

				var errList []error

				// Check for generated resources
				if err = tt.fields.client.Get(tt.fields.ctx, obKey, &v1alpha1.ObjectBucket{}); err != nil {
					errList = append(errList, err)
				}
				if err = tt.fields.client.Get(tt.fields.ctx, obcKey, &corev1.ConfigMap{}); err != nil {
					errList = append(errList, err)
				}
				if err = tt.fields.client.Get(tt.fields.ctx, obcKey, &corev1.Secret{}); err != nil {
					errList = append(errList, err)
				}

				if reconcileErr != nil {
					if len(errList) > 0 {
						// Reconciler errored, generated resources were cleaned up
						return
					} else {
						t.Error("reconciler errored, expected generated resources to be deleted")
					}
				} else {
					if len(errList) > 0 {
						t.Error("reconciler did not error, expected generated resources to exist")
					}
				}
			}
		})
	}
}

func Test_objectBucketClaimReconciler_shouldProvision(t *testing.T) {

	const (
		className = "test-class"
	)

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
			name:   "storage class exists",
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
		{
			name:   "storage class does not exist (expect error)",
			fields: testFields,
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: objMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						StorageClassName: className,
					},
				},
			},
			class: nil,
			want:  false,
		},
		{
			name: "provisioner name mismatch",
			fields: fields{
				ctx:             context.TODO(),
				client:          nil,
				provisionerName: "bad-provisioner",
				provisioner:     &dummyProvisioner{},
				retryInterval:   0,
				retryTimeout:    0,
				retryBackoff:    0,
			},
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
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
			want: false,
		},
	}
	for _, tt := range tests {

		tt.fields.client = BuildFakeClient(t)
		if tt.class != nil {
			if err := tt.fields.client.Create(tt.fields.ctx, tt.class); err != nil {
				t.Errorf("error precreating class: %v", err)
			}
		}

		t.Run(tt.name, func(t *testing.T) {
			r := &objectBucketClaimReconciler{
				ctx:             tt.fields.ctx,
				client:          tt.fields.client,
				provisionerName: tt.fields.provisionerName,
				provisioner:     tt.fields.provisioner,
				retryInterval:   tt.fields.retryInterval,
				retryTimeout:    tt.fields.retryTimeout,
				retryBackoff:    tt.fields.retryBackoff,
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

		tt.fields.client = BuildFakeClient(t)

		if tt.want != nil {
			if err := tt.fields.client.Create(tt.fields.ctx, tt.want); err != nil {
				t.Errorf("objectBucketClaimReconciler.claimFromKey() error = %v,", fmt.Sprintf("error precreating object: %v", err))
			}
		}

		t.Run(tt.name, func(t *testing.T) {
			r := &objectBucketClaimReconciler{
				ctx:             tt.fields.ctx,
				client:          tt.fields.client,
				provisionerName: tt.fields.provisionerName,
				provisioner:     tt.fields.provisioner,
				retryInterval:   tt.fields.retryInterval,
				retryTimeout:    tt.fields.retryTimeout,
				retryBackoff:    tt.fields.retryBackoff,
			}
			got, err := r.claimFromKey(tt.args.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("objectBucketClaimReconciler.claimFromKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("objectBucketClaimReconciler.claimFromKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMain(m *testing.M) {
	util.InitTestFlags()
	m.Run()
}
