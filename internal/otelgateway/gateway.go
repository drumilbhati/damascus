package otelgateway

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bufbuild/protocompile"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

//go:embed demo.proto
var demoProto string

const maxRequestBytes = 1 << 20

type Gateway struct {
	file        protoreflect.FileDescriptor
	grpcTargets map[string]string
	httpTargets map[string]httpTarget
	client      *http.Client

	mu      sync.Mutex
	clients map[string]*grpc.ClientConn
}

type httpTarget struct {
	baseURL string
	path    string
	mutates bool
}

// New builds a gateway for the RPC and HTTP services in the pinned OTel Demo.
func New() (*Gateway, error) {
	resolver := protocompile.ResolverFunc(func(path string) (protocompile.SearchResult, error) {
		if path != "demo.proto" {
			return protocompile.SearchResult{}, fmt.Errorf("unknown protobuf file %q", path)
		}
		return protocompile.SearchResult{Source: strings.NewReader(demoProto)}, nil
	})
	files, err := (&protocompile.Compiler{Resolver: resolver}).Compile(context.Background(), "demo.proto")
	if err != nil {
		return nil, fmt.Errorf("compile OTel Demo protobuf schema: %w", err)
	}
	return &Gateway{
		file: files[0],
		grpcTargets: map[string]string{
			"CartService":           "cartservice:8080",
			"RecommendationService": "recommendationservice:8080",
			"ProductCatalogService": "productcatalogservice:3550",
			"ShippingService":       "shippingservice:50050",
			"CurrencyService":       "currencyservice:7001",
			"PaymentService":        "paymentservice:50051",
			"CheckoutService":       "checkoutservice:5050",
			"AdService":             "adservice:9555",
		},
		httpTargets: map[string]httpTarget{
			"quoteservice/getquote":                {baseURL: "http://quoteservice:8090", path: "/getquote"},
			"emailservice/send_order_confirmation": {baseURL: "http://emailservice:6060", path: "/send_order_confirmation", mutates: true},
		},
		client:  &http.Client{Timeout: 10 * time.Second},
		clients: make(map[string]*grpc.ClientConn),
	}, nil
}

func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/v1/", g.handle)
	return mux
}

func (g *Gateway) handle(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, "path must be /v1/{service}/{operation}", http.StatusNotFound)
		return
	}
	service, operation := parts[0], parts[1]
	input, status, err := requestMessage(r)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}

	if target, ok := g.httpTargets[strings.ToLower(service)+"/"+strings.ToLower(operation)]; ok {
		if target.mutates && r.Method != http.MethodPost {
			http.Error(w, "this operation changes state and requires POST", http.StatusMethodNotAllowed)
			return
		}
		g.proxyHTTP(w, r, target, input)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "only GET and POST are supported", http.StatusMethodNotAllowed)
		return
	}
	serviceDesc := g.file.Services().ByName(protoreflect.Name(service))
	if serviceDesc == nil {
		http.Error(w, "unknown OTel Demo service", http.StatusNotFound)
		return
	}
	target, ok := g.grpcTargets[string(serviceDesc.Name())]
	if !ok {
		http.Error(w, "service has no configured gRPC backend", http.StatusNotFound)
		return
	}
	method := serviceDesc.Methods().ByName(protoreflect.Name(operation))
	if method == nil {
		http.Error(w, "unknown operation for service", http.StatusNotFound)
		return
	}
	if mutatingRPC(serviceDesc.Name(), method.Name()) && r.Method != http.MethodPost {
		http.Error(w, "this operation changes state and requires POST", http.StatusMethodNotAllowed)
		return
	}

	in := dynamicpb.NewMessage(method.Input())
	if len(input) > 0 {
		if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(input, in); err != nil {
			http.Error(w, fmt.Sprintf("invalid protobuf JSON request: %v", err), http.StatusBadRequest)
			return
		}
	}
	out := dynamicpb.NewMessage(method.Output())
	conn, err := g.connection(target)
	if err != nil {
		http.Error(w, fmt.Sprintf("connect to OTel Demo service: %v", err), http.StatusBadGateway)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	rpcName := "/" + string(serviceDesc.FullName()) + "/" + string(method.Name())
	if err := conn.Invoke(ctx, rpcName, in, out); err != nil {
		http.Error(w, fmt.Sprintf("OTel Demo RPC failed: %v", err), http.StatusBadGateway)
		return
	}
	response, err := (protojson.MarshalOptions{UseProtoNames: false}).Marshal(out)
	if err != nil {
		http.Error(w, "could not encode RPC response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(response)
}

func requestMessage(r *http.Request) ([]byte, int, error) {
	if r.Method == http.MethodGet {
		if raw := r.URL.Query().Get("request"); raw != "" {
			if !json.Valid([]byte(raw)) {
				return nil, http.StatusBadRequest, fmt.Errorf("request query parameter must contain JSON")
			}
			return []byte(raw), 0, nil
		}
		return nil, 0, nil
	}
	if r.Method != http.MethodPost {
		return nil, http.StatusMethodNotAllowed, fmt.Errorf("only GET and POST are supported")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil {
		return nil, http.StatusBadRequest, fmt.Errorf("read request body: %w", err)
	}
	if len(body) > maxRequestBytes {
		return nil, http.StatusRequestEntityTooLarge, fmt.Errorf("request body exceeds %d bytes", maxRequestBytes)
	}
	if len(bytes.TrimSpace(body)) > 0 && !json.Valid(body) {
		return nil, http.StatusBadRequest, fmt.Errorf("request body must be valid JSON")
	}
	return body, 0, nil
}

func mutatingRPC(service, operation protoreflect.Name) bool {
	switch string(service) + "." + string(operation) {
	case "CartService.AddItem", "CartService.EmptyCart", "ShippingService.ShipOrder",
		"PaymentService.Charge", "CheckoutService.PlaceOrder", "EmailService.SendOrderConfirmation",
		"FeatureFlagService.CreateFlag", "FeatureFlagService.UpdateFlag", "FeatureFlagService.DeleteFlag":
		return true
	default:
		return false
	}
}

func (g *Gateway) connection(target string) (*grpc.ClientConn, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if conn := g.clients[target]; conn != nil {
		return conn, nil
	}
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	g.clients[target] = conn
	return conn, nil
}

func (g *Gateway) proxyHTTP(w http.ResponseWriter, r *http.Request, target httpTarget, body []byte) {
	requestURL, err := url.JoinPath(target.baseURL, target.path)
	if err != nil {
		http.Error(w, "invalid configured HTTP service URL", http.StatusInternalServerError)
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "build upstream request", http.StatusInternalServerError)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := g.client.Do(request)
	if err != nil {
		http.Error(w, fmt.Sprintf("HTTP service request failed: %v", err), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	for name, values := range response.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}
