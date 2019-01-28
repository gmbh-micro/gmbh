package gmbh

/*
 * gmbh.go
 * Abe Dick
 * Nov 2018
 */

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/gmbh-micro/cabal"
	"github.com/gmbh-micro/notify"
	yaml "gopkg.in/yaml.v2"
)

const version = "00.07.01"
const debug = true

// HandlerFunc is the publically exposed function to register and use the callback functions
// from within gmbhCore. Its behavior is modeled after the http handler that is baked into go
// by default
type HandlerFunc = func(req Request, resp *Responder)

// Client - the structure between a service and CORE
type Client struct {
	ServiceName         string `yaml:"name"`
	isServer            bool   `yaml:"isserver"`
	isClient            bool   `yaml:"isclient"`
	address             string
	registeredFunctions map[string]HandlerFunc
	msgCounter          int
}

// g - the gmbhCore object that contains the parsed yaml config and other associated data
var g *Client

// NewService should be called only once. It returns the object in which parameters, and
// handler functions can be attached to gmbh Client.
func NewService(configPath string) (*Client, error) {
	if g != nil {
		return g, nil
	}

	var err error
	g, err = parseYamlConfig(configPath)
	if err != nil {
		notify.StdMsgErr(err.Error())
		return nil, errors.New("could not parse config")
	}

	g.registeredFunctions = make(map[string]HandlerFunc)

	return g, nil

}

// Start registers the service with gmbh.
//
// Note that this blocks until receiving the signal to quit. If starting a webserver
// run this in a go thread as to not block content from being delivered the desired
// output.
//
// TODO: Find a better way to start
func (g *Client) Start() {
	addr, err := _ephemeralRegisterService(g.ServiceName, g.isClient, g.isServer)
	if err != nil {
		dlog("gmbh.Start: " + err.Error())
	}

	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		fmt.Println(sig)
		done <- true
	}()
	if addr != "" {
		go rpcConnect(addr)
	}

	dlog("gmbh started")

	<-done
	_makeUnregisterRequest(g.ServiceName)
	os.Exit(0)
	// return nil
}

// Route - Callback functions to be used when handling data
// requests from gmbh or other services
//
// TODO: Add a mechanism to safely add these and check for collisions, etc.
func (g *Client) Route(route string, handler HandlerFunc) {
	g.registeredFunctions[route] = handler
}

// MakeRequest is the default method for making data requests through gmbh
func (g *Client) MakeRequest(target, method, data string) Responder {
	resp, err := _makeDataRequest(target, method, data)
	if err != nil {
		panic(err)
	}
	return resp
}

// Request is the publically exposed requester between services in gmbh
type Request struct {
	// Sender is the name of the service that is sending the message
	Sender string

	// Target is the name or alias of the intended recepient
	Target string

	// Method is the handler to invoke on target entry
	// TODO: Change this as it can be considered confusing with
	// 		 the HTTP methods...
	Method string

	// Data1 is the data to send
	// TODO: remove this and more articulately handle data
	Data1 string
}

// ToProto returns the gproto Request object corresponding to the current
// Request object
func (r *Request) toProto() *cabal.Request {
	return &cabal.Request{
		Sender: r.Sender,
		Target: r.Target,
		Method: r.Method,
		Data1:  r.Data1,
	}
}

// Responder is the publically exposed responder between services in gmbh
type Responder struct {
	// Result is the resulting datat from target
	// TODO: remove this and more articulately handle data
	Result string

	// ErrorString is the corresponding error string if HadError is true
	ErrorString string

	// HadError is true if the request was not completed without error
	HadError bool
}

// ToProto returns the gproto Request object corresponding to the current
// Responder object
func (r *Responder) toProto() *cabal.Responder {
	return &cabal.Responder{
		Result:      r.Result,
		ErrorString: r.ErrorString,
		HadError:    r.HadError,
	}
}

// requestFromProto takes a gproto request and returns the corresponding
// Request object
func requestFromProto(r cabal.Request) Request {
	return Request{
		Sender: r.Sender,
		Target: r.Target,
		Method: r.Method,
		Data1:  r.Data1,
	}
}

// ResponderFromProto takes a gproto Responder and returns the corresponding
// Responder object
func responderFromProto(r cabal.Responder) Responder {
	return Responder{
		Result:      r.Result,
		ErrorString: r.ErrorString,
		HadError:    r.HadError,
	}
}

func handleDataRequest(req cabal.Request) (*cabal.Responder, error) {

	var request Request
	request = requestFromProto(req)
	responder := Responder{}

	handler, ok := g.registeredFunctions[request.Method]
	if !ok {
		responder.HadError = true
		responder.ErrorString = "Could not locate method in registered process map"
	} else {
		handler(request, &responder)
	}

	return responder.toProto(), nil
}

func parseYamlConfig(relativePath string) (*Client, error) {
	path, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatal(err)
	}
	var conf Client
	yamlFile, err := ioutil.ReadFile(path + "/" + relativePath)
	if err != nil {
		notify.StdMsgErr(path + relativePath)
		return nil, errors.New("could not find yaml file")
	}
	err = yaml.Unmarshal(yamlFile, &conf)
	if err != nil {
		return nil, errors.New("could not unmarshal config")
	}
	return &conf, nil
}

func dlog(msg string) {
	if debug {
		notify.StdMsgMagenta(msg)
	}
}
