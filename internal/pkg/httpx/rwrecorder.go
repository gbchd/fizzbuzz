package httpx

import (
	"io"
	"net/http"
)

type rwrecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *rwrecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}

	r.ResponseWriter.WriteHeader(status)
}

func (r *rwrecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}

	return r.ResponseWriter.Write(b)
}

func (r *rwrecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// ReadFrom keeps the real writer's fast path reachable, which the wrapper would
// otherwise cost a caller with no visible cause.
func (r *rwrecorder) ReadFrom(src io.Reader) (int64, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}

	if rf, ok := r.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}

	return io.Copy(r.ResponseWriter, src)
}
