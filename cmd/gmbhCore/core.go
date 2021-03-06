package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/gmbh-micro/config"
	"github.com/gmbh-micro/fileutil"
	"github.com/gmbh-micro/rpc"
	"github.com/gmbh-micro/rpc/address"
	"github.com/gmbh-micro/rpc/intrigue"
	"github.com/rs/xid"
	"google.golang.org/grpc/metadata"
)

// internal reference to core for use rpc
var core *Core

// Core is the main gmbh controller
type Core struct {
	Version string
	Code    string

	// the filesystem directory to the gmbh project where assumptions can be made about
	// structure accoring to the config file
	ProjectPath string

	// con holds the host connection for the cabal server
	con *rpc.Connection

	// conf is the user configurable parameters as read in from file
	conf *config.SystemCore

	// Router controls all aspects of data requests & handling in Core
	Router *Router

	// env is set in the environment and controls the environment that the core is running
	// in.
	env string

	// parent id is for remote instances of core
	parentID string

	msgCounter int
	startTime  time.Time
	mu         *sync.Mutex
	verbose    bool
}

// NewCore initializes settings of the core and instantiates the core struct which includes the
// service router and handlers
func NewCore(cPath, env, addr string, verbose bool) (*Core, error) {

	// cannot reinit core once it has been created
	// TODO use a "once" function here
	if core != nil {
		return core, nil
	}

	var userConfig *config.SystemCore
	projpath := ""
	var err error
	if cPath == "" {
		userConfig = config.DefaultSystemCore
		userConfig.Address = addr
		projpath = fileutil.Getpwd()
	} else {
		userConfig, err = config.ParseSystemCore(cPath)
		if err != nil {
			print("could not parse config; err=%v", err.Error())
			return nil, err
		}
		projpath = fileutil.GetAbs(cPath)
	}

	if env == "C" {
		userConfig.Address = addr
	}

	core = &Core{
		Version:     config.Version,
		Code:        config.Code,
		ProjectPath: projpath,
		con:         rpc.NewCabalConnection(userConfig.Address, &cabalServer{}),
		conf:        userConfig,
		Router:      NewRouter(),
		msgCounter:  1,
		startTime:   time.Now(),
		// mode:        os.Getenv("SERVICEMODE"),
		env:      env,
		parentID: os.Getenv("REMOTE"),
		mu:       &sync.Mutex{},
		verbose:  verbose,
	}

	if core.ProjectPath == "" {
		print("could not get path to project")
		return nil, errors.New("config path error")
	}

	print("                    _            _              ")
	print("  _  ._ _  |_  |_| /   _  ._ _  | \\  _. _|_  _. ")
	print(" (_| | | | |_) | | \\_ (_) | (/_ |_/ (_|  |_ (_| ")
	print("  _|                                            ")
	print("version=%v; code=%v; env=%s, startTime=%s", core.Version, core.Code, core.env, core.startTime.Format(time.Stamp))
	return core, nil
}

// GetCore retrieves the instance of core. For use with rpc server
func GetCore() (*Core, error) {
	if core != nil {
		return core, nil
	}
	return nil, errors.New("core.GetCore.internalError")
}

// Start the cabal server
func (c *Core) Start() {
	err := c.con.Connect()
	if err != nil {
		print("could not connected; err=%s", err.Error())
		return
	}
	print("connected; address=%s", c.con.Address)

	c.Wait()
}

// Wait holds the main program thread until shutdown signal is received
func (c *Core) Wait() {
	sig := make(chan os.Signal, 1)

	if c.env == "M" {
		print("managed mode; listening for sigusr2; ignoring sigusr1, sigint")
		signal.Notify(sig, syscall.SIGUSR2)
		signal.Ignore(syscall.SIGUSR1, syscall.SIGINT)
	} else {
		signal.Notify(sig, syscall.SIGINT)
	}

	print("main thread waiting")
	_ = <-sig
	// fmt.Println() //dead line to line up output

	c.shutdown(false, "signal")
	return
}

// shutdown begins graceful shutdown procedures
func (c *Core) shutdown(remote bool, source string) {
	// print("shutdown procedure started from " + source)

	if c.env != "M" {
		done := make(chan bool)
		go c.Router.sendShutdownNotices(done)
		<-done
	}

	print("shutdown complete...")
	return
}

/**********************************************************************************
**** Router
**********************************************************************************/

// Router handles all of the addressing and mapping of services that are attached to gmbhCore
type Router struct {

	// services (Name|Alias)->Service
	// map contains all registered services
	services map[string]*GmbhService

	// serviceNames is a list of the names of all services attached. This is useful because if the
	// map is walked using a range it will return a value for every alias and thus have duplicates
	serviceNames []string

	// idCounter keeps track of the current runnig id
	idCounter int

	// addressHandler is in charge of assigning addresses and making sure that there are no collisions
	addressing *address.Handler

	verbose bool
	mu      *sync.Mutex
}

// NewRouter instantiates and returns a new Router structure
func NewRouter() *Router {
	r := &Router{
		services:     make(map[string]*GmbhService),
		serviceNames: make([]string, 0),
		idCounter:    100,
		addressing:   address.NewHandler(config.Localhost, config.ServicePort, config.ServicePort+1000),
		mu:           &sync.Mutex{},
		verbose:      true,
	}
	return r
}

// LookupService looks through the services map and returns the service if it exists
func (r *Router) LookupService(name string) (*GmbhService, error) {
	// r.v("looking up %s", name)
	retrievedService := r.services[name]
	if retrievedService == nil {
		// print("%s not found in router", name)
		return nil, errors.New("router.LookupService.NotFound")
	}
	// r.v("found")
	return retrievedService, nil
}

// AddService attaches a service to gmbH
func (r *Router) AddService(name string, aliases []string, peerGroups []string, env, addr string) (*GmbhService, error) {

	newAddr := addr
	if env != "C" {
		var err error
		newAddr, err = r.addressing.NextAddress()
		if err != nil {
			return nil, err
		}
	}

	newService := NewService(
		r.assignNextID(),
		name,
		aliases,
		newAddr,
		peerGroups,
	)

	// check to see if it exists in map already
	s, err := r.LookupService(name)
	if err == nil {
		// r.v("found new service already in map")
		if s.State == Shutdown {
			print("correct params reported for this service to assume role of one found")
			s.UpdateState(Running)
			return s, nil
		}
		alive := r.CheckIsAlive(s.Address)
		if !alive {
			print("could not get a response from service on file, treating new service as one found")
			s.UpdateState(Running)
			return s, nil
		}
		print("service in map reporting still alive; naming err; not adding new service")
		return nil, fmt.Errorf("duplicate service")
	}

	err = r.addToMap(newService)
	if err != nil {
		print(newService.String())
		print("could not add service to map; err=%s", err.Error())
		return nil, err
	}

	print("added service=%s", newService.String())
	return newService, nil
}

// Verify a ping
func (r *Router) Verify(name, fp string) error {
	s := r.services[name]
	if s == nil {
		return errors.New("verify.notFound")
	}
	if r.services[name].Fingerprint != fp {
		return errors.New("verify.fingerprintMismatch")
	}
	if s.State == Shutdown {
		return errors.New("verify.reportedShutdown")
	}
	s.LastPing = time.Now()
	return nil
}

// addToMap returns an error if there is a name or alias conflict with an existing
// service in the service map, otherwise the service's name and alias are added to
// the map
func (r *Router) addToMap(newService *GmbhService) error {

	if _, ok := r.services[newService.Name]; ok {
		print("could not add to map, duplicate name")
		return errors.New("router.addToMap: duplicate service with same name found")
	}

	for _, alias := range newService.Aliases {
		if _, ok := r.services[alias]; ok {
			print("could not add to map, duplicate alias=" + alias)
			return errors.New("router.addToMap: duplicate service with same alias found")
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.services[newService.Name] = newService
	r.serviceNames = append(r.serviceNames, newService.Name)
	for _, alias := range newService.Aliases {
		if alias != "" {
			r.services[alias] = newService
		}
	}

	// r.v("added %s to map", newService.Name)

	return nil
}

// sendShutdownNotices sends a notice to all clients that core is shutting down
func (r *Router) sendShutdownNotices(done chan bool) {
	var wg sync.WaitGroup
	for _, name := range r.serviceNames {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			service := r.services[n]
			// print("sending shutdown to %s at %s", service.Name, service.Address)
			client, ctx, can, err := rpc.GetCabalRequest(service.Address, time.Millisecond*500)
			if err != nil {
				// print("could not create client")
				can()
				return
			}
			req := &intrigue.ServiceUpdate{
				Request: "core.shutdown",
				Message: service.Name,
			}
			_, err = client.UpdateRegistration(ctx, req)
			if err != nil {
				if service.State != Shutdown {
					// print("error contacting service; id=%s; err=%s", service.ID, err.Error())
				}
			}
		}(name)
	}
	wg.Wait()
	done <- true
}

// CheckIsAlive checks a connected service for aliveness (via a ping request)
// returns true if the service could be contacted, else false
func (r *Router) CheckIsAlive(addr string) bool {
	client, ctx, can, err := rpc.GetCabalRequest(addr, time.Second*15)
	if err != nil {
		return false
	}
	defer can()
	_, err = client.Alive(ctx, &intrigue.Ping{Time: time.Now().Format(time.Stamp)})
	if err != nil {
		return false
	}
	return true
}

// GetCoreServiceData queries each attached client to respond with their information using their
// fingerprint for validation
func (r *Router) GetCoreServiceData(core *intrigue.CoreService) []*intrigue.CoreService {
	ret := []*intrigue.CoreService{core}
	for _, n := range r.serviceNames {
		service := r.services[n]
		// print("sending summary request to %s at %s", service.Name, service.Address)
		client, ctx, can, err := rpc.GetCabalRequest(service.Address, time.Second*1)
		if err != nil {
			// print("could not create client")
			can()
			continue
		}
		ctx = metadata.AppendToOutgoingContext(
			ctx,
			"sender", "gmbhCore",
			"target", n,
			"fingerprint", service.Fingerprint,
		)
		req := &intrigue.Action{
			Request: "request.info.all",
		}
		resp, err := client.Summary(ctx, req)
		if err != nil {
			print("error contacting service; id=%s; err=%s", service.ID, err.Error())
			continue
		}
		if resp.GetServices() == nil {
			ret = append(ret, &intrigue.CoreService{
				Name:   n,
				Errors: []string{"could not contact, err=" + resp.GetError()},
			})
			continue
		}
		ret = append(ret, resp.Services...)
	}
	return ret
}

// GrantPermissions checks the peer groups of from and to; If they have a common element,
// then permission for them to speek is granted, else error
func (r *Router) GrantPermissions(from, to string) (string, error) {
	fromserv, err := r.LookupService(from)
	if err != nil {
		return "", err
	}

	serv, err := r.LookupService(to)
	if err != nil {
		return "", err
	}

	for k := range fromserv.PeerGroups {
		if serv.PeerGroups[k] {
			return serv.Address, nil
		}
	}
	return "", fmt.Errorf("denied")
}

func (r *Router) assignNextID() string {
	mu := &sync.Mutex{}
	mu.Lock()
	defer mu.Unlock()
	r.idCounter++
	return strconv.Itoa(r.idCounter)
}

/**********************************************************************************
**** Service
**********************************************************************************/

// GmbhService is the data representation of a connected service
type GmbhService struct {
	// The id assigned by the router
	ID string

	// Aliases of the service
	Aliases []string

	// the name of the service
	Name string

	// the address to the service
	Address string

	// the peer group is the service defined group id
	PeerGroups map[string]bool

	// The time that the service was added to the router
	Added time.Time

	// The last known state of the service
	State State

	// The last time a ping was received
	LastPing time.Time

	// assigned by the server, the fingerprint is sent with each ping to verify id
	Fingerprint string

	mu *sync.Mutex
}

func (g *GmbhService) String() string {
	return fmt.Sprintf("name=%s; id=%s; address=%s;", g.Name, g.ID, g.Address)
}

// NewService returns a gmbhService object with data filled in
func NewService(id string, name string, aliases []string, address string, peerGroups []string) *GmbhService {
	serv := &GmbhService{
		ID:          id,
		Name:        name,
		Aliases:     aliases,
		Address:     address,
		PeerGroups:  make(map[string]bool),
		Added:       time.Now(),
		State:       Running,
		LastPing:    time.Now().Add(time.Hour),
		Fingerprint: xid.New().String(),
		mu:          &sync.Mutex{},
	}
	serv.setPeerGroups(peerGroups)
	return serv
}

func (g *GmbhService) setPeerGroups(pg []string) {
	for _, v := range pg {
		g.PeerGroups[v] = true
	}
}

// UpdateState of the current state of the service
func (g *GmbhService) UpdateState(s State) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if s != g.State {
		print("marking %s(%s) as %s", g.Name, g.ID, s.String())
		g.State = s
	}
}

// State controls the state of a remote server
type State int

const (
	// Running as normal
	Running State = 1 + iota

	// Shutdown notice received from remote
	Shutdown

	// Failed to return a pong
	Failed
)

var states = [...]string{
	"Running",
	"Shutdown",
	"Failed",
}

func (s State) String() string {
	if Running <= s && s <= Failed {
		return states[s-1]
	}
	return "%!State()"
}
