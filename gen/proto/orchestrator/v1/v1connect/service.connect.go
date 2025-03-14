// Code generated by protoc-gen-connect-go. DO NOT EDIT.
//
// Source: proto/orchestrator/v1/service.proto

package v1connect

import (
	connect "connectrpc.com/connect"
	context "context"
	errors "errors"
	v1 "github.com/dynoinc/skyvault/gen/proto/orchestrator/v1"
	http "net/http"
	strings "strings"
)

// This is a compile-time assertion to ensure that this generated file and the connect package are
// compatible. If you get a compiler error that this constant is not defined, this code was
// generated with a version of connect newer than the one compiled into your binary. You can fix the
// problem by either regenerating this code with an older version of connect or updating the connect
// version compiled into your binary.
const _ = connect.IsAtLeastVersion1_13_0

const (
	// OrchestratorServiceName is the fully-qualified name of the OrchestratorService service.
	OrchestratorServiceName = "orchestrator.v1.OrchestratorService"
)

// These constants are the fully-qualified names of the RPCs defined in this package. They're
// exposed at runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// OrchestratorServiceListWriteAheadLogsProcedure is the fully-qualified name of the
	// OrchestratorService's ListWriteAheadLogs RPC.
	OrchestratorServiceListWriteAheadLogsProcedure = "/orchestrator.v1.OrchestratorService/ListWriteAheadLogs"
)

// OrchestratorServiceClient is a client for the orchestrator.v1.OrchestratorService service.
type OrchestratorServiceClient interface {
	ListWriteAheadLogs(context.Context, *connect.Request[v1.ListWriteAheadLogsRequest]) (*connect.Response[v1.ListWriteAheadLogsResponse], error)
}

// NewOrchestratorServiceClient constructs a client for the orchestrator.v1.OrchestratorService
// service. By default, it uses the Connect protocol with the binary Protobuf Codec, asks for
// gzipped responses, and sends uncompressed requests. To use the gRPC or gRPC-Web protocols, supply
// the connect.WithGRPC() or connect.WithGRPCWeb() options.
//
// The URL supplied here should be the base URL for the Connect or gRPC server (for example,
// http://api.acme.com or https://acme.com/grpc).
func NewOrchestratorServiceClient(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) OrchestratorServiceClient {
	baseURL = strings.TrimRight(baseURL, "/")
	orchestratorServiceMethods := v1.File_proto_orchestrator_v1_service_proto.Services().ByName("OrchestratorService").Methods()
	return &orchestratorServiceClient{
		listWriteAheadLogs: connect.NewClient[v1.ListWriteAheadLogsRequest, v1.ListWriteAheadLogsResponse](
			httpClient,
			baseURL+OrchestratorServiceListWriteAheadLogsProcedure,
			connect.WithSchema(orchestratorServiceMethods.ByName("ListWriteAheadLogs")),
			connect.WithClientOptions(opts...),
		),
	}
}

// orchestratorServiceClient implements OrchestratorServiceClient.
type orchestratorServiceClient struct {
	listWriteAheadLogs *connect.Client[v1.ListWriteAheadLogsRequest, v1.ListWriteAheadLogsResponse]
}

// ListWriteAheadLogs calls orchestrator.v1.OrchestratorService.ListWriteAheadLogs.
func (c *orchestratorServiceClient) ListWriteAheadLogs(ctx context.Context, req *connect.Request[v1.ListWriteAheadLogsRequest]) (*connect.Response[v1.ListWriteAheadLogsResponse], error) {
	return c.listWriteAheadLogs.CallUnary(ctx, req)
}

// OrchestratorServiceHandler is an implementation of the orchestrator.v1.OrchestratorService
// service.
type OrchestratorServiceHandler interface {
	ListWriteAheadLogs(context.Context, *connect.Request[v1.ListWriteAheadLogsRequest]) (*connect.Response[v1.ListWriteAheadLogsResponse], error)
}

// NewOrchestratorServiceHandler builds an HTTP handler from the service implementation. It returns
// the path on which to mount the handler and the handler itself.
//
// By default, handlers support the Connect, gRPC, and gRPC-Web protocols with the binary Protobuf
// and JSON codecs. They also support gzip compression.
func NewOrchestratorServiceHandler(svc OrchestratorServiceHandler, opts ...connect.HandlerOption) (string, http.Handler) {
	orchestratorServiceMethods := v1.File_proto_orchestrator_v1_service_proto.Services().ByName("OrchestratorService").Methods()
	orchestratorServiceListWriteAheadLogsHandler := connect.NewUnaryHandler(
		OrchestratorServiceListWriteAheadLogsProcedure,
		svc.ListWriteAheadLogs,
		connect.WithSchema(orchestratorServiceMethods.ByName("ListWriteAheadLogs")),
		connect.WithHandlerOptions(opts...),
	)
	return "/orchestrator.v1.OrchestratorService/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case OrchestratorServiceListWriteAheadLogsProcedure:
			orchestratorServiceListWriteAheadLogsHandler.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// UnimplementedOrchestratorServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedOrchestratorServiceHandler struct{}

func (UnimplementedOrchestratorServiceHandler) ListWriteAheadLogs(context.Context, *connect.Request[v1.ListWriteAheadLogsRequest]) (*connect.Response[v1.ListWriteAheadLogsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("orchestrator.v1.OrchestratorService.ListWriteAheadLogs is not implemented"))
}
