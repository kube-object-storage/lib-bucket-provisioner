/*
Copyright 2019 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ObjectBucketLister helps list ObjectBuckets.
// All objects returned here must be treated as read-only.
type ObjectBucketLister interface {
	// List lists all ObjectBuckets in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.ObjectBucket, err error)
	// Get retrieves the ObjectBucket from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.ObjectBucket, error)
	ObjectBucketListerExpansion
}

// objectBucketLister implements the ObjectBucketLister interface.
type objectBucketLister struct {
	indexer cache.Indexer
}

// NewObjectBucketLister returns a new ObjectBucketLister.
func NewObjectBucketLister(indexer cache.Indexer) ObjectBucketLister {
	return &objectBucketLister{indexer: indexer}
}

// List lists all ObjectBuckets in the indexer.
func (s *objectBucketLister) List(selector labels.Selector) (ret []*v1alpha1.ObjectBucket, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.ObjectBucket))
	})
	return ret, err
}

// Get retrieves the ObjectBucket from the index for a given name.
func (s *objectBucketLister) Get(name string) (*v1alpha1.ObjectBucket, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("objectbucket"), name)
	}
	return obj.(*v1alpha1.ObjectBucket), nil
}
