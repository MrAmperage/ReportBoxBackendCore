// rpc-demo uses typed ConnectRPC handlers/clients with protobuf well-known types.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"connectrpc.com/connect"
	backendcore "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/rpc"
	"go.uber.org/fx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const servicePath = "/reportbox.example.v1.EchoService/"
const echoProcedure = servicePath + "Echo"

func handler() rpc.Handler {
	return rpc.Handler{AllowUnauthenticated: true, Build: func(options ...connect.HandlerOption) (string, http.Handler) {
		mux := http.NewServeMux()
		mux.Handle(echoProcedure, connect.NewUnaryHandler(echoProcedure, func(ctx context.Context, req *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
			return connect.NewResponse(wrapperspb.String(req.Msg.Value)), nil
		}, options...))
		return servicePath, mux
	}}
}

func main() {
	mode := flag.String("mode", "server", "server or client")
	endpoint := flag.String("endpoint", "http://127.0.0.1:9090", "RPC origin")
	message := flag.String("message", "Hello ReportBox", "echo input")
	flag.Parse()
	switch *mode {
	case "server":
		backendcore.Run(app.Info{Name: "rpc-demo-server"}, rpc.ServerModule(), rpc.ProvideHandler(handler))
	case "client":
		if err := runClient(*endpoint, *message); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatal("unknown mode")
	}
}

func runClient(endpoint, message string) (err error) {
	var owner *rpc.Client
	a := backendcore.New(app.Info{Name: "rpc-demo-client"}, rpc.ClientModule(), fx.Populate(&owner))
	if err = a.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = a.Start(ctx); err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if stopErr := a.Stop(ctx); err == nil {
			err = stopErr
		}
	}()
	target, err := owner.Target(endpoint, rpc.GRPC)
	if err != nil {
		return err
	}
	client := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](target.HTTPClient, target.BaseURL+echoProcedure, target.Options...)
	ctxCall, cancelCall := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCall()
	response, err := client.CallUnary(ctxCall, connect.NewRequest(wrapperspb.String(message)))
	if err != nil {
		return err
	}
	fmt.Println(response.Msg.Value)
	return nil
}
