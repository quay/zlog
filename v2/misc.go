package zlog

import (
	"errors"
	"io"
	"os"
	"sync"
)

// NoCopy is a trick for ensuring a handler isn't copied around.
type noCopy struct{}

// Lock implements [sync.Locker].
func (noCopy) Lock() {}

// Unlock implements [sync.Locker].
func (noCopy) Unlock() {}

// SyncWriter ensures calls to its Write method are serialized for the inner
// Writer.
type syncWriter struct {
	sync.Mutex
	io.Writer
}

// Write implements io.Writer.
func (w *syncWriter) Write(b []byte) (int, error) {
	w.Lock()
	defer w.Unlock()
	return w.Writer.Write(b)
}

// InK8s reports whether this process is (probably) being run inside a
// kubernetes pod. This relies on some default behavior which is trivially
// changed in a PodSpec.
var inK8s = sync.OnceValue(func() bool {
	_, haveEnv := os.LookupEnv(`KUBERNETES_SERVICE_HOST`)
	_, statErr := os.Stat(`/var/run/secrets/kubernetes.io`)
	return haveEnv || !errors.Is(statErr, os.ErrNotExist)
})
