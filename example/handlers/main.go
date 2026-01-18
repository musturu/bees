package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	httpruntime "bees/api/runtime/http"
	"bees/api/runtime/http/endpoint"
)

// HelloInput is the request-scoped input type.
type HelloInput struct {
	Name string `json:"name"`
	Resp string
}

// HelloOutput is created per-request and writes the response using the input.
type HelloOutput struct {
	In *HelloInput
}

func (o *HelloOutput) Write(ctx context.Context, w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]string{"greeting": o.In.Resp})
}

func HandleHello(ctx context.Context, in *HelloInput) (endpoint.Output, error) {
	if in.Name == "" {
		in.Name = "world"
	}
	in.Resp = "hello, " + in.Name
	return &HelloOutput{In: in}, nil
}

func main() {
	// Create POST endpoint (JSON body)
	epPost := endpoint.NewJSONEndpoint("POST /hello", HandleHello)

	// Create GET endpoint with a custom binder that reads query params
	getBinder := func(r *http.Request, in *HelloInput) error {
		in.Name = r.URL.Query().Get("name")
		return nil
	}
	epGet := endpoint.NewEndpoint("GET /hello", HandleHello)
	epGet.Bind = getBinder

	svc, err := endpoint.NewService("/api/",
		endpoint.WithEndpoint(epGet),
		endpoint.WithEndpoint(epPost),
	)
	if err != nil {
		log.Fatal(err)
	}

	rt := httpruntime.NewRuntime(httpruntime.WithAddr(":8080"))
	if err := rt.Register(svc); err != nil {
		log.Fatal(err)
	}
	if err := rt.Run(); err != nil {
		log.Fatal(err)
	}
}
