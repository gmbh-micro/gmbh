package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gmbh-micro/config"
	"github.com/gmbh-micro/fileutil"
	"github.com/gmbh-micro/notify"
)

const (
	deployDir = "gmbh-deploy"
	doNotEdit = "# DO NOT EDIT\r\n# File automatically generated by gmbh\r\n"
	dockerCmd = `docker build -t gmbh-img-node_%d -f ./node_%d.Dockerfile --build-arg CACHEBUST=$(date +%s)  ../`
	service   = `

[[service]]
id = "%s"
args = %s
env = %s
language = "%s"
bin_path = "%s"
src_path = "%s"
interpreter = "%s"
entry_point = "%s"`

	core = `[[service]]
id = "CoreData"
args = %s
bin_path = "%s"`
)

type compNode struct {
	name string

	image   string
	env     []string
	envFile []string
	ports   []string
	ldriver string
}

// genDeploy generates the gmbh-deploy folder containing everything needed to build a docker cluster
// of the gmbh project.
func builddeploy(cfile string, verbose bool) {
	fmt.Println("Generating gmbh deployment")
	fmt.Println(". . .")

	conf, err := config.ParseSystemConfig(cfile)
	if err != nil {
		notify.LnRedF("could not parse config")
		return
	}

	fmt.Println("creating deployment directory")
	fileutil.MkDir("gmbh-deploy")

	fmt.Println("generating core node file")
	err = genCoreConf(filepath.Join(deployDir, "core.toml"), "./configFile.toml", conf)
	if err != nil {
		notify.LnRedF("could not create core node config")
		return
	}

	// why must you be like this, go?
	numNodes := math.Ceil(float64(len(conf.Service)) / float64(conf.MaxPerNode))
	fmt.Println("generating node files")
	fmt.Printf("generating %d services in %d nodes\n", len(conf.Service), int(numNodes))

	nodes := []string{}
	ports := make(map[int][]string)

	for i := 0; i < int(numNodes); i++ {
		start := i * conf.MaxPerNode
		end := start + conf.MaxPerNode
		if end > len(conf.Service) {
			end = len(conf.Service)
		}

		nodes = append(nodes, fmt.Sprintf(dockerCmd+"\n", i+1, i+1, "%s"))
		genNodeConf(i+1, conf.Service[start:end])
		genDockerfile(i+1, conf.Service[start:end])

		// gather used ports and put them in a map
		usedPorts := genPortGather(conf.Service[start:end])
		if len(usedPorts) != 0 {
			ports[i+1] = usedPorts
		}
	}

	fmt.Println("generating dockerfiles")
	fmt.Println("...for core")
	f, err := fileutil.CreateFile(filepath.Join(deployDir, "core.Dockerfile"))
	if err != nil {
		return
	}
	f.WriteString(fmt.Sprintf(config.CoreDkr, cfile))
	f.Close()

	fmt.Println("...for procm")
	g, err := fileutil.CreateFile(filepath.Join(deployDir, "procm.Dockerfile"))
	if err != nil {
		return
	}
	g.WriteString(config.ProcMDkr)
	g.Close()

	if conf.Dashboard {
		fmt.Println("...for dashboard")
		p, err := fileutil.CreateFile(filepath.Join(deployDir, "dashboard.Dockerfile"))
		if err != nil {
			return
		}
		p.WriteString(fmt.Sprintf(config.Dashboard))
		p.Close()
	}

	fmt.Println("generating build file")
	genBuildScript(nodes, conf.Dashboard)

	fmt.Println("generating docker compose file")
	m, err := fileutil.CreateFile(filepath.Join(deployDir, "docker-compose.yml"))
	if err != nil {
		return
	}
	m.WriteString(config.Compose)

	if conf.Dashboard {
		m.WriteString(config.ComposeDashboard)
	}

	for i := 1; i < int(numNodes)+1; i++ {
		portStr := genPortStr(i, ports)
		m.WriteString(fmt.Sprintf(config.ComposeNode, i, i, portStr, i))
	}
	m.Close()

	fmt.Println("generating env file")
	n, err := fileutil.CreateFile(filepath.Join(deployDir, "gmbh.env"))
	if err != nil {
		return
	}
	n.WriteString(fmt.Sprintf(config.EnvFile, "FINGERPRINT="+fingerprint))
	if verbose {
		n.WriteString(config.EnvLog + "\n")
	}
	n.Close()
}

// genCoreConf generates the core config for the core service.
func genCoreConf(path, fname string, config *config.SystemConfig) error {
	f, err := fileutil.CreateFile(path)
	if err != nil {
		return err
	}

	args, _ := json.Marshal([]string{"--verbose", "--config=" + fname})
	w := f.WriteString
	w(doNotEdit)
	w("\n\n")
	w("fingerprint = \"" + fingerprint + "\"\n")
	w("\n")
	w(fmt.Sprintf(
		core,
		args,
		"gmbhCore"),
	)
	f.Close()
	return nil
}

// genNodeConf generates the node config file for this node. This node is composed of each of the services
// passed in the services array
func genNodeConf(node int, services []*config.ServiceConfig) error {
	f, err := fileutil.CreateFile(filepath.Join(deployDir, "node_"+strconv.Itoa(node)+".toml"))
	if err != nil {
		return err
	}
	w := f.WriteString

	w(doNotEdit)
	w("#\n# Services in this file (by directory) \n")
	for _, s := range services {
		w(fmt.Sprintf("# - %s\n", fileutil.GetAbsFpath(s.BinPath)))
	}
	w("#\n# Fingerprint - the id that refers to this cluster\n")
	w(fmt.Sprintf("fingerprint = \"%s\"", fingerprint))

	base := 49500

	for i, s := range services {

		args, _ := json.Marshal(s.Args)
		env, _ := json.Marshal(append(s.Env, "ADDR="+"node_"+strconv.Itoa(node)+":"+strconv.Itoa(base+((i+1)*20))))
		w(fmt.Sprintf(
			service,
			s.ID,
			args,
			env,
			s.Language,
			"/services/"+s.ID+"/"+filepath.Base(s.BinPath),
			s.SrcPath,
			s.Interpreter,
			"/services/"+s.ID+"/"+filepath.Base(s.EntryPoint),
		))
	}

	f.Close()
	return nil
}

// genDockerfile generates the dockerfile for this node. This node is composed of each of the services
// passed in the services array.
func genDockerfile(node int, services []*config.ServiceConfig) error {

	f, err := fileutil.CreateFile(filepath.Join(deployDir, "node_"+strconv.Itoa(node)+".Dockerfile"))
	if err != nil {
		return err
	}

	addLocs := []string{}
	makeInstrs := []string{}
	for _, s := range services {
		addLocs = append(addLocs, s.SrcPath+" ./"+s.ID)
		makeStr := "# Build instructions for " + s.ID + "\nRUN "
		if s.Language == "go" {
			makeStr += "cd ./" + s.ID + "; go get ./...; go build ."
		} else if s.Language == "node" {
			makeStr += "cd ./" + s.ID + "; rm -rf node_modules; npm install"
		}
		makeInstrs = append(makeInstrs, makeStr)
	}

	addStr := ""
	for _, v := range addLocs {
		addStr += "ADD " + v + "\n"
	}

	makeStr := ""
	for _, v := range makeInstrs {
		makeStr += v + "\n"
	}

	nodeConf := "node_" + strconv.Itoa(node)
	f.WriteString(fmt.Sprintf(config.ServiceDkr, addStr, makeStr, nodeConf, nodeConf))

	f.Close()
	return nil
}

func genBuildScript(nodes []string, dashboard bool) error {
	h, err := fileutil.CreateFile(filepath.Join(deployDir, "build.sh"))
	if err != nil {
		return err
	}
	h.WriteString(config.Bash)
	h.WriteString("docker build -t gmbh-img-core -f ./core.Dockerfile --build-arg CACHEBUST=$(date +%s) ../\n")
	h.WriteString(config.CheckBash)
	h.WriteString("docker build -t gmbh-img-procm -f ./procm.Dockerfile --build-arg CACHEBUST=$(date +%s)  ../\n")
	h.WriteString(config.CheckBash)
	h.WriteString("docker build -t gmbh-dashboard-image -f ./dashboard.Dockerfile ../\n")
	h.WriteString(config.CheckBash)
	for _, v := range nodes {
		h.WriteString(v)
		h.WriteString(config.CheckBash)
	}
	h.Close()

	err = os.Chmod(filepath.Join(deployDir, "build.sh"), 0755)
	if err != nil {
		fmt.Println(err.Error())
		return err
	}
	return nil
}

func genPortGather(services []*config.ServiceConfig) []string {
	ret := []string{}
	for _, v := range services {
		for _, p := range v.Ports {
			if p != "" {
				ret = append(ret, p)
			}
		}
	}
	return ret
}

func genPortStr(i int, ports map[int][]string) string {
	portStrArr := []string{}
	if ports[i] != nil {
		if len(ports[i]) != 0 {
			for _, p := range ports[i] {
				portStrArr = append(portStrArr, "      - \""+p+":"+p+"\"")
			}
		}
	}
	portStr := strings.Join(portStrArr, "\n")
	if portStr != "" {
		portStr = "\n    ports:\n" + portStr
	}
	return portStr
}
