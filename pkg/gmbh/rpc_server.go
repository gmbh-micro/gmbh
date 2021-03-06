package gmbh

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gmbh-micro/rpc/intrigue"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
)

/**********************************************************************************
** RPC Server
**********************************************************************************/

// _server implements the coms service using gRPC
type _server struct{}

func rpcConnect(address string) {
	list, err := net.Listen("tcp", address)
	if err != nil {
		panic(err)
	}

	s := grpc.NewServer()
	intrigue.RegisterCabalServer(s, &_server{})

	reflection.Register(s)
	if err := s.Serve(list); err != nil {
		panic(err)
	}

}

func (s *_server) RegisterService(ctx context.Context, in *intrigue.NewServiceRequest) (*intrigue.Receipt, error) {
	return &intrigue.Receipt{Message: "operation.invalid"}, nil
}

func (s *_server) UpdateRegistration(ctx context.Context, in *intrigue.ServiceUpdate) (*intrigue.Receipt, error) {

	print(fmt.Sprintf("-> Update Registration; Message=%s", in.String()))

	request := in.GetRequest()
	// target := in.GetMessage()

	if request == "core.shutdown" {
		print("recieved shutdown")

		// either shutdown for real or disconnect and try and reach again if
		// the service wasn't forked from gmbh-core
		if g.env == "M" {
			go g.Shutdown("core")
		} else if !g.closed {
			go func() {

				g.mu.Lock()
				g.reg = nil
				g.mu.Unlock()

				g.disconnect()
				g.connect()
			}()
		}
	}
	return &intrigue.Receipt{Error: "unknown.request"}, nil
}

func (s *_server) Data(ctx context.Context, in *intrigue.DataRequest) (*intrigue.DataResponse, error) {

	mcs := strconv.Itoa(g.msgCounter)
	g.msgCounter++
	if g.env != "C" || os.Getenv("LOGGING") == "1" {
		print("=="+mcs+"==> from=%s; method=%s", in.GetRequest().GetTport().GetSender(), in.GetRequest().GetTport().GetMethod())
	}

	responder, err := handleDataRequest(*in.GetRequest())
	if err != nil {
		panic(err)
	}
	return &intrigue.DataResponse{Responder: responder}, nil
}

func (s *_server) Summary(ctx context.Context, in *intrigue.Action) (*intrigue.SummaryReceipt, error) {

	print(fmt.Sprintf("-> Summary Request; Action=%s", in.String()))

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		print("Could not get metadata from summary request")
		return &intrigue.SummaryReceipt{Error: "unknown.id"}, nil
	}

	fp := strings.Join(md.Get("fingerprint"), "")
	if fp != g.getReg().fingerprint {
		print("Could not match fingerprint from summary request; incoming fp=%s", fp)
		return &intrigue.SummaryReceipt{Error: "unknown.id"}, nil
	}

	response := &intrigue.SummaryReceipt{
		Services: []*intrigue.CoreService{
			&intrigue.CoreService{
				Name:       g.opts.service.Name,
				Address:    g.getReg().address,
				Mode:       g.env,
				PeerGroups: g.opts.service.PeerGroups,
				ParentID:   g.parentID,
				Errors:     []string{},
			},
		},
	}

	return response, nil
}

func (s *_server) WhoIs(ctx context.Context, in *intrigue.WhoIsRequest) (*intrigue.WhoIsResponse, error) {
	return &intrigue.WhoIsResponse{Error: "unsupported in client"}, nil
}
func (s *_server) Alive(ctx context.Context, ping *intrigue.Ping) (*intrigue.Pong, error) {
	return &intrigue.Pong{Time: time.Now().Format(time.Stamp)}, nil
}
