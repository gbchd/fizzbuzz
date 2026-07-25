package handler

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"

	"github.com/gbchd/fizzbuzz/internal/pkg/httpx"
	"github.com/gbchd/fizzbuzz/internal/pkg/slogx"
)

const (
	// caps what an untrusted caller can push into the request body; sized well
	// above the largest request the document permits.
	maxRequestBodyBytes = 8 << 10

	// probe paths, kept out of the access log: an orchestrator hits them every
	// few seconds and would bury real traffic.
	healthPath = "/healthz"
	readyPath  = "/readyz"
)

//go:embed documentation.gen.html
var documentationHTML string

// isShuttingDown is read by GET /readyz. Package-level rather than a field, so
// the entrypoint can flip it on a signal without holding the assembled handler.
var isShuttingDown atomic.Bool

// BeginShutdown makes GET /readyz report 503, so a load balancer stops routing
// new requests. Call it when the shutdown signal arrives, before draining.
func BeginShutdown() {
	isShuttingDown.Store(true)
}

// FizzBuzzService is declared here rather than in the domain, so the domain
// stays unaware of transport and a test can substitute a stub.
type FizzBuzzService interface {
	FizzBuzz(int1 int, str1 string, int2 int, str2 string, limit int) ([]string, error)
}

// New assembles the whole request path into one http.Handler. Building the
// chain in one place is what keeps its order defined once.
func New(svc FizzBuzzService) (http.Handler, error) {
	spec, err := GetSpec()
	if err != nil {
		return nil, fmt.Errorf("handler: load embedded spec: %w", err)
	}
	definitionJSON, err := GetSpecJSON()
	if err != nil {
		return nil, fmt.Errorf("handler: read embedded spec: %w", err)
	}
	// GET /definition serves the document the validator below enforces, so what
	// is published cannot drift from what is applied.
	var definition map[string]any
	if err := json.Unmarshal(definitionJSON, &definition); err != nil {
		return nil, fmt.Errorf("handler: decode embedded spec: %w", err)
	}

	// Global because kin-openapi offers no per-request switch. If left enabled,
	// validation errors can echo schema details and submitted values back to the client.
	openapi3.SchemaErrorDetailsDisabled = true

	strict := NewStrictHandlerWithOptions(
		&server{svc: svc, definition: definition},
		nil,
		StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  undecodableRequest,
			ResponseErrorHandlerFunc: serverFault,
		},
	)

	validate := nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		ErrorHandlerWithOpts: validationFailed,
	})

	// Outermost first. Nothing is registered outside the generated router, so
	// every path answered is one the document declares.
	return httpx.Chain(
		Handler(strict),
		httpx.LimitRequestBody(maxRequestBodyBytes),
		httpx.RequestID,
		httpx.AccessLog(healthPath, readyPath),
		validate,
	), nil
}

// server implements the generated StrictServerInterface.
type server struct {
	svc        FizzBuzzService
	definition map[string]any
}

func (s *server) PostFizzbuzz(_ context.Context, request PostFizzbuzzRequestObject) (PostFizzbuzzResponseObject, error) {
	body := request.Body

	sequence, err := s.svc.FizzBuzz(body.Int1, body.Str1, body.Int2, body.Str2, body.Limit)
	if err != nil {
		// Validation already guaranteed input the domain accepts, so an error
		// here means document and domain have diverged — our defect, not theirs.
		return nil, fmt.Errorf("generate sequence: %w", err)
	}

	return PostFizzbuzz200JSONResponse(sequence), nil
}

func (s *server) GetHealthz(_ context.Context, _ GetHealthzRequestObject) (GetHealthzResponseObject, error) {
	return GetHealthz200JSONResponse{Status: "ok"}, nil
}

func (s *server) GetReadyz(_ context.Context, _ GetReadyzRequestObject) (GetReadyzResponseObject, error) {
	if isShuttingDown.Load() {
		return GetReadyz503JSONResponse{ServiceUnavailableJSONResponse{Message: "shutting down"}}, nil
	}

	return GetReadyz200JSONResponse{Status: "ok"}, nil
}

func (s *server) GetDefinition(_ context.Context, _ GetDefinitionRequestObject) (GetDefinitionResponseObject, error) {
	return GetDefinition200JSONResponse(s.definition), nil
}

func (s *server) GetDocumentation(_ context.Context, _ GetDocumentationRequestObject) (GetDocumentationResponseObject, error) {
	return GetDocumentation200TexthtmlResponse{
		Body:          strings.NewReader(documentationHTML),
		ContentLength: int64(len(documentationHTML)),
	}, nil
}

// writeError uses the one shape the document declares, so a client needs only
// one error path.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Error{Message: message}) // the client is gone; nothing to report to
}

// validationFailed passes the validator's message through: it already names the
// field and the rule it broke.
func validationFailed(
	ctx context.Context,
	err error,
	w http.ResponseWriter,
	_ *http.Request,
	opts nethttpmiddleware.ErrorHandlerOpts,
) {
	if opts.StatusCode >= http.StatusInternalServerError {
		serverFaultCtx(ctx, w, err)
		return
	}

	status := opts.StatusCode
	// The middleware suggests 404 for anything it could not route, which would
	// tell a caller the path does not exist when only the method is wrong.
	if errors.Is(err, routers.ErrMethodNotAllowed) {
		status = http.StatusMethodNotAllowed
	}

	writeError(w, status, firstLine(err.Error()))
}

// undecodableRequest keeps the error shape uniform for a body the generated
// server cannot unmarshal. Validation rejects these first in practice.
func undecodableRequest(w http.ResponseWriter, r *http.Request, err error) {
	writeError(w, http.StatusBadRequest, firstLine(err.Error()))
	slogx.FromCtx(r.Context()).WarnContext(r.Context(), "undecodable request body", slog.Any("error", err))
}

func serverFault(w http.ResponseWriter, r *http.Request, err error) {
	serverFaultCtx(r.Context(), w, err)
}

// serverFaultCtx tells the client nothing about the cause: reaching here is our
// defect, and there is nothing the caller can act on.
func serverFaultCtx(ctx context.Context, w http.ResponseWriter, err error) {
	slogx.FromCtx(ctx).ErrorContext(ctx, "request failed", slog.Any("error", err))
	writeError(w, http.StatusInternalServerError, "internal server error")
}

// firstLine drops the per-sub-schema detail the validator appends, which
// restates the same failure without adding information.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
