package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"
	"time"
)

var (
	vagrantfileTplSrc = `
require 'yaml'
Vagrant.configure(2) do |config|
  config.vm.box = "debian/bullseye64"
  config.vm.synced_folder ".", "/vagrant", disabled: true
{{range $h := .Hosts}}
  config.vm.define '{{$h.Name}}' do |m|
    m.vm.hostname = '{{$h.Name}}'
    m.vm.network "private_network", ip: "{{$h.IP}}", libvirt__dhcp_enabled: false
  end
{{end}}
end
`
	vagrantfileTpl = template.Must(template.New("").Parse(vagrantfileTplSrc))

	vmineSSHConfigTplSrc = `
{{$dir := .Dir}}
{{range $h := .Hosts}}
Host {{$h.Name}}
  HostName {{$h.IP}}
  User root
  Port 22
  UserKnownHostsFile /dev/null
  StrictHostKeyChecking no
  PasswordAuthentication no
  IdentityFile {{$dir}}/id_ed25519
  IdentitiesOnly yes
  LogLevel FATAL

{{end}}
`
	vmineSSHConfigTpl = template.Must(template.New("").Parse(vmineSSHConfigTplSrc))
)

type env struct {
	// Temporary directory.
	Dir string

	// Private network for the hosts.
	Network *net.IPNet

	// Host inventory.
	Hosts []host

	// Group memberships.
	Groups map[string][]string
}

type host struct {
	Name string
	IP   string
}

type vmprovider interface {
	sshConfig() string
	ansibleSSHParams() []string

	start(context.Context) error
	stop(context.Context)
}

func withProvider(ctx context.Context, vm vmprovider, f func() error) error {
	log.Printf("bringing up VM environment...")
	err := vm.start(ctx)
	if err != nil {
		return err
	}

	err = f()
	if *keep {
		log.Printf("keeping VMs around for manual inspection...")
	} else {
		// Use a fresh context for stop(), the original one might be completed already.
		log.Printf("stopping VM environment...")
		vm.stop(context.Background())
	}
	return err
}

type vagrant struct {
	env *env
}

func (v *vagrant) sshConfig() string {
	return filepath.Join(v.env.Dir, "ssh-config")
}

func (v *vagrant) ansibleSSHParams() []string {
	return []string{
		"ansible_user=vagrant",
		"ansible_become=true",
		fmt.Sprintf("ansible_ssh_extra_args=\"-F %s %s\"", v.sshConfig(), *extraSSHArgs),
	}
}

func (v *vagrant) start(ctx context.Context) error {
	runCmd(ctx, v.env.Dir, "vagrant", "destroy", "-f")
	if err := runCmd(ctx, v.env.Dir, "vagrant", "up"); err != nil {
		return err
	}
	return runCmd(ctx, v.env.Dir, "sh", "-c", "vagrant ssh-config > ssh-config")
}

func (v *vagrant) stop(ctx context.Context) {
	runCmd(ctx, v.env.Dir, "vagrant", "destroy", "-f")
}

func newVagrant(env *env) (*vagrant, error) {
	vagrantfilePath := filepath.Join(env.Dir, "Vagrantfile")
	if err := createVagrantfile(vagrantfilePath, env); err != nil {
		return nil, err
	}
	return &vagrant{env}, nil
}

func createVagrantfile(path string, env *env) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return vagrantfileTpl.Execute(f, env)
}

const vmineDefaultImage = "bullseye"

type vmineHost struct {
	Name  string `json:"name"`
	IP    string `json:"ip"`
	Image string `json:"image"`
	RAM   int    `json:"ram"`
	CPU   int    `json:"cpu"`
}

type vmineCreateGroupRequest struct {
	NetworkCIDR string      `json:"network"`
	Hosts       []vmineHost `json:"hosts"`
	TTL         float64     `json:"ttl"`
	SSHKey      string      `json:"ssh_key"`
	Name        string      `json:"name"`
}

type vmineCreateGroupResponse struct {
	GroupID string `json:"group_id"`
}

type vmine struct {
	uri    string
	client *http.Client
	env    *env
	sshKey string
	data   vmineCreateGroupResponse
}

func newVmine(uri string, env *env) (*vmine, error) {
	sshKey, err := genSSHKey(env.Dir)
	if err != nil {
		return nil, err
	}

	v := &vmine{
		client: new(http.Client),
		env:    env,
		uri:    uri,
		sshKey: sshKey,
	}

	var sshConf bytes.Buffer
	if err := vmineSSHConfigTpl.Execute(&sshConf, env); err != nil {
		return nil, err
	}
	if err := os.WriteFile(v.sshConfig(), sshConf.Bytes(), 0600); err != nil {
		return nil, err
	}

	return v, nil
}

func genSSHKey(dir string) (string, error) {
	keyPath := filepath.Join(dir, "id_ed25519")

	if err := runCmd(context.Background(), "", "ssh-keygen", "-t", "ed25519", "-f", keyPath, "-N", "", "-C", "", "-q"); err != nil {
		return "", err
	}

	data, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (v *vmine) jsonRequest(ctx context.Context, path string, reqObj, respObj interface{}) error {
	payload, err := json.Marshal(reqObj)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", v.uri+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	if respObj != nil {
		return json.NewDecoder(resp.Body).Decode(respObj)
	}
	return nil
}

func hostsToVmine(hosts []host) []vmineHost {
	var out []vmineHost
	for _, host := range hosts {
		out = append(out, vmineHost{
			Name:  host.Name,
			IP:    host.IP,
			Image: vmineDefaultImage,
			RAM:   512,
			CPU:   1,
		})
	}
	return out
}

func (v *vmine) start(ctx context.Context) error {
	return v.jsonRequest(ctx, "/api/create-group", &vmineCreateGroupRequest{
		NetworkCIDR: v.env.Network.String(),
		TTL:         float64(600 * time.Minute),
		SSHKey:      v.sshKey,
		Hosts:       hostsToVmine(v.env.Hosts),
	}, &v.data)
}

func (v *vmine) stop(ctx context.Context) {
	if err := v.jsonRequest(ctx, "/api/stop-group", &v.data, nil); err != nil {
		log.Printf("vmine: error stopping VM group: %v", err)
	}
}

func (v *vmine) sshConfig() string {
	return filepath.Join(v.env.Dir, "ssh-config")
}

func (v *vmine) ansibleSSHParams() []string {
	return []string{
		"ansible_user=root",
		"ansible_become=false",
		fmt.Sprintf("ansible_ssh_extra_args=\"-F %s %s\"", v.sshConfig(), *extraSSHArgs),
	}
}

func createAnsibleInventory(path string, env *env, vm vmprovider) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, host := range env.Hosts {
		fmt.Fprintf(f, "[%s]\n%s test_ip_address=%s\n\n", host.Name, host.Name, host.IP)
	}
	for group, groupHosts := range env.Groups {
		fmt.Fprintf(f, "[%s:children]\n%s\n\n", group, strings.Join(groupHosts, "\n"))
	}
	// Use a different random SSH ControlPath for fast iteration,
	// to avoid Ansible getting stuck trying to talk to old hosts.
	fmt.Fprintf(
		f,
		"[all:vars]\n%s\nansible_control_path=%%(directory)s/%s\n\n",
		strings.Join(vm.ansibleSSHParams(), "\n"),
		randomSmallString(),
	)
	return nil
}

func parseGroupSpec(s string) (map[string][]string, error) {
	g := make(map[string][]string)
	for _, part := range strings.Split(s, ";") {
		tmp := strings.SplitN(part, "=", 2)
		if len(tmp) != 2 {
			return nil, errors.New("invalid group spec")
		}
		g[tmp[0]] = append(g[tmp[0]], strings.Split(tmp[1], ",")...)
	}
	return g, nil
}

func randomNetwork() *net.IPNet {
	_, n, _ := net.ParseCIDR(fmt.Sprintf("10.%d.%d.0/24", rand.Intn(255), rand.Intn(255)))
	return n
}

// Doesn't have to be very random.
func randomSmallString() string {
	var s string
	for i := 0; i < 4; i++ {
		s += fmt.Sprintf("%08x", rand.Int63())
	}
	return s
}

func hostInNet(i int, network *net.IPNet) net.IP {
	ip := network.IP.Mask(network.Mask)
	ip[len(ip)-1] = byte(i + 100)
	return ip
}

func generateHosts(n int, network *net.IPNet) []host {
	var hosts []host
	for i := 0; i < n; i++ {
		hosts = append(hosts, host{
			Name: fmt.Sprintf("host%d", i+1),
			IP:   hostInNet(i, network).String(),
		})
	}
	return hosts
}

func runCmd(ctx context.Context, dir, cmd string, args ...string) error {
	c := exec.CommandContext(ctx, cmd, args...)
	c.Dir = dir
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

var (
	keep         = flag.Bool("keep", false, "keep the VMs around after exiting")
	inspectHost  = flag.String("inspect", "", "inspect this `host` manually")
	testNetwork  = flag.String("network", "", "test network")
	numHosts     = flag.Int("hosts", 2, "number of desired hosts")
	groupSpec    = flag.String("groups", "gateway=host1;client=host2", "group specs (name=host1,host2;...)")
	provider     = flag.String("provider", "vagrant", "choose vm provider (vagrant/vmine:URL)")
	extraSSHArgs = flag.String("ssh-args", "", "additional ssh args for connecting to hosts")
)

func createEnv() (*env, error) {
	var e env
	var err error

	e.Dir, err = os.MkdirTemp("", "vagrant-")
	if err != nil {
		return nil, err
	}

	e.Network = randomNetwork()
	if *testNetwork != "" {
		_, e.Network, err = net.ParseCIDR(*testNetwork)
		if err != nil {
			return nil, err
		}
	}

	e.Hosts = generateHosts(*numHosts, e.Network)

	e.Groups, err = parseGroupSpec(*groupSpec)
	if err != nil {
		return nil, fmt.Errorf("error parsing --groups: %w", err)
	}

	return &e, nil
}

func (e *env) Close() {
	if *keep {
		log.Printf("VM directory: %s", e.Dir)
	} else {
		os.RemoveAll(e.Dir)
	}
}

func run(ctx context.Context) error {
	env, err := createEnv()
	if err != nil {
		return err
	}
	defer env.Close()

	var vm vmprovider
	switch {
	case *provider == "vagrant":
		vm, err = newVagrant(env)
	case *provider == "vmine":
		vm, err = newVmine("http://localhost:4949", env)
	case strings.HasPrefix(*provider, "vmine:"):
		uri := (*provider)[6:]
		vm, err = newVmine(uri, env)
	default:
		err = errors.New("unknown VM provider")
	}
	if err != nil {
		return err
	}

	inventoryFile := filepath.Join(env.Dir, "hosts.ini")
	if err := createAnsibleInventory(inventoryFile, env, vm); err != nil {
		return err
	}

	// Create a controlling Context that can be stopped with a signal.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	go func() {
		<-sigCh
		cancel()
	}()
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	return withProvider(ctx, vm, func() error {
		log.Printf("env: %+v", env)
		for _, playbook := range flag.Args() {
			playbookAbs, _ := filepath.Abs(playbook)
			log.Printf("running ansible on %s...", playbook)

			err := runCmd(ctx, env.Dir, "ansible-playbook", "-v", "-i", inventoryFile, playbookAbs)
			if err != nil {
				if *inspectHost != "" {
					log.Printf("connecting to %s for manual inspection...", *inspectHost)
					runCmd(ctx, env.Dir, "ssh", "-F", vm.sshConfig(), *inspectHost)
				} else {
					log.Printf("dumping journal from host1...")
					runCmd(ctx, env.Dir, "ssh", "-F", vm.sshConfig(), "host1",
						"sudo journalctl -n 200 | grep -v pam_unix | grep -v 'sudo.*vagrant'")
				}
				return err
			}
		}
		return nil
	})
}

func main() {
	log.SetFlags(0)
	flag.Parse()
	rand.Seed(time.Now().UnixNano())

	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
