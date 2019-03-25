package errors

import (
	"fmt"
)

type BucketExistsErr struct {
	errString string
}

func (e BucketExistsErr) Error() string {
	return fmt.Sprintf("%v", e.errString)
}

func NewBucketExistsError(msg string) *BucketExistsErr {
	return &BucketExistsErr{
		errString: msg,
	}
}

func IsBucketExists(e error) (is bool) {
	_, is = e.(BucketExistsErr)
	return is
}
