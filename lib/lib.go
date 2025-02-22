// Package lib implements core qri business logic. It exports
// canonical methods that a qri instance can perform regardless of
// client interface. API's of any sort must use lib methods
package lib

import (
	"context"
	"encoding/gob"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"strings"
	"sync"

	golog "github.com/ipfs/go-log"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/qri-io/ioes"
	"github.com/qri-io/qfs"
	"github.com/qri-io/qfs/cafs"
	ipfs "github.com/qri-io/qfs/cafs/ipfs"
	ipfs_http "github.com/qri-io/qfs/cafs/ipfs_http"
	"github.com/qri-io/qfs/httpfs"
	"github.com/qri-io/qfs/localfs"
	"github.com/qri-io/qfs/muxfs"
	"github.com/qri-io/qri/config"
	"github.com/qri-io/qri/config/migrate"
	"github.com/qri-io/qri/fsi"
	"github.com/qri-io/qri/p2p"
	"github.com/qri-io/qri/registry/regclient"
	regmock "github.com/qri-io/qri/registry/regserver/mock"
	"github.com/qri-io/qri/remote"
	"github.com/qri-io/qri/repo"
	fsrepo "github.com/qri-io/qri/repo/fs"
	"github.com/qri-io/qri/repo/profile"
	"github.com/qri-io/qri/update"
	"github.com/qri-io/qri/update/cron"
)

var (
	// defaultIPFSLocation is where qri data defaults to looking for / setting up
	// IPFS. The keyword $HOME will be replaced with the current user home
	// directory. only $HOME is replaced (no other $ env vars).
	defaultIPFSLocation = "$HOME/.ipfs"

	log = golog.Logger("lib")
)

// VersionNumber is the current version qri
const VersionNumber = "0.9.0"

func init() {
	// Fields like dataset.Structure.Schema contain data of arbitrary types,
	// registering with the gob package prevents errors when sending them
	// over net/rpc calls.
	gob.Register([]interface{}{})
	gob.Register(map[string]interface{}{})
}

// Receivers returns a slice of CoreRequests that defines the full local
// API of lib methods
func Receivers(inst *Instance) []Methods {
	node := inst.Node()
	r := inst.Repo()

	return []Methods{
		NewDatasetRequestsInstance(inst),
		NewRegistryClientMethods(inst),
		NewLogRequests(node, nil),
		NewExportRequests(node, nil),
		NewPeerRequests(node, nil),
		NewProfileMethods(inst),
		NewConfigMethods(inst),
		NewSearchMethods(inst),
		NewRenderRequests(r, nil),
		NewUpdateMethods(inst),
		NewFSIMethods(inst),
	}
}

// Methods is a related set of library functions
type Methods interface {
	// CoreRequestsName confirms participation in the CoreRequests interface while
	// also giving a human readable string for logging purposes
	// TODO (b5): rename this interface to "MethodsName", or remove entirely
	CoreRequestsName() string
}

// InstanceOptions provides details to NewInstance.
// New will alter InstanceOptions by applying
// any provided Option functions
// to distinguish "Options" from "Config":
// * Options contains state that can only be determined at runtime
// * Config consists only of static values stored in a configuration file
// Options may override config in specific cases to avoid undefined state
type InstanceOptions struct {
	Cfg     *config.Config
	Streams ioes.IOStreams

	node      *p2p.QriNode
	repo      repo.Repo
	store     cafs.Filestore
	qfs       qfs.Filesystem
	regclient *regclient.Client

	// use OptRemoteOptions to set this
	remoteOptsFunc func(*remote.Options)
}

// Option is a function that manipulates config details when fed to New(). Fields on
// the o parameter may be null, functions cannot assume the Config is non-null.
type Option func(o *InstanceOptions) error

// OptConfig supplies a configuration directly
func OptConfig(cfg *config.Config) Option {
	return func(o *InstanceOptions) error {
		o.Cfg = cfg
		return nil
	}
}

// OptSetIPFSPath configures the directory to read IPFS from (only if the
// configured store is IPFS). If the given path is the empty string, default
// to the standard IPFS config:
// * IPFS_PATH environment variable if set
// * if none set: "$HOME/.ipfs"
func OptSetIPFSPath(path string) Option {
	return func(o *InstanceOptions) error {
		if o.Cfg == nil {
			return fmt.Errorf("config is nil, can't set IPFS path")
		}
		if o.Cfg.Store == nil {
			return fmt.Errorf("config.Store is nil, can't check type")
		}
		if o.Cfg.Store.Type == "ipfs" && o.Cfg.Store.Path == "" {
			if path == "" {
				path = os.Getenv("IPFS_PATH")
				if path == "" {
					dir, err := homedir.Dir()
					if err != nil {
						return err
					}
					path = strings.Replace(defaultIPFSLocation, "$HOME", dir, 1)
				}
			}
			o.Cfg.Store.Path = path
		}
		return nil
	}
}

// OptIOStreams sets the input IOStreams
func OptIOStreams(streams ioes.IOStreams) Option {
	return func(o *InstanceOptions) error {
		o.Streams = streams
		return nil
	}
}

// OptStdIOStreams sets treams to std, stdout, & stderr
func OptStdIOStreams() Option {
	return func(o *InstanceOptions) error {
		o.Streams = ioes.NewStdIOStreams()
		return nil
	}
}

// OptCheckConfigMigrations checks for any configuration migrations that may need to be run
// running & updating config if so
func OptCheckConfigMigrations(cfgPath string) Option {
	return func(o *InstanceOptions) error {
		if o.Cfg == nil {
			return fmt.Errorf("no config file to check for migrations")
		}

		migrated, err := migrate.RunMigrations(o.Streams, o.Cfg)
		if err != nil {
			return err
		}

		if migrated {
			return o.Cfg.WriteToFile(cfgPath)
		}
		return nil
	}
}

// OptRemoteOptions provides options to the instance remote
// the provided configuration function is called with the Qri configuration-derived
// remote settings applied, allowing partial-overrides.
func OptRemoteOptions(fn func(opt *remote.Options)) Option {
	return func(o *InstanceOptions) error {
		o.remoteOptsFunc = fn
		return nil
	}
}

// OptQriNode configures bring-your-own qri node
func OptQriNode(node *p2p.QriNode) Option {
	return func(o *InstanceOptions) error {
		o.node = node
		if o.node.Repo != nil && o.repo == nil {
			o.repo = o.node.Repo
		}
		if o.node.Repo.Store() != nil {
			o.store = o.node.Repo.Store()
		}
		if o.node.Repo.Filesystem() != nil {
			o.qfs = o.node.Repo.Filesystem()
		}
		return nil
	}
}

// OptRegistryClient overrides any configured registry client
func OptRegistryClient(cli *regclient.Client) Option {
	return func(o *InstanceOptions) error {
		o.regclient = cli
		return nil
	}
}

// NewInstance creates a new Qri Instance, if no Option funcs are provided,
// New uses a default set of Option funcs. Any Option functions passed to this
// function must check whether their fields are nil or not.
func NewInstance(ctx context.Context, repoPath string, opts ...Option) (qri *Instance, err error) {
	if repoPath == "" {
		return nil, fmt.Errorf("repo path is required")
	}

	o := &InstanceOptions{}

	// attempt to load a base configuration from repoPath
	if o.Cfg, err = loadRepoConfig(repoPath); err != nil {
		log.Error("loading config: %s", err)
		return
	}

	if len(opts) == 0 {
		// default to a standard composition of Option funcs
		opts = []Option{
			OptStdIOStreams(),
			OptSetIPFSPath(""),
			OptCheckConfigMigrations(""),
		}
	}
	for _, opt := range opts {
		if err = opt(o); err != nil {
			return
		}
	}

	cfg := o.Cfg
	if cfg == nil {
		// If at this point we don't have a configuration pointer one couldn't be
		// loaded from repoPath, and a configuration wasn't provided through Options,
		// so qri needs to be set up
		err = fmt.Errorf("no qri repo found, please run `qri setup`")
		return
	} else if err = cfg.Validate(); err != nil {
		return
	}

	ctx, teardown := context.WithCancel(ctx)
	inst := &Instance{
		ctx:      ctx,
		teardown: teardown,
		repoPath: repoPath,
		cfg:      cfg,

		node:     o.node,
		streams:  o.Streams,
		registry: o.regclient,
	}
	qri = inst

	// configure logging straight away
	if cfg != nil && cfg.Logging != nil {
		for name, level := range cfg.Logging.Levels {
			golog.SetLogLevel(name, level)
		}
	}

	if inst.cron, err = newCron(cfg, inst.repoPath); err != nil {
		log.Error("initializing cron:", err.Error())
		return nil, fmt.Errorf("newCron: %s", err)
	}

	// check if we're operating over RPC
	if cfg.RPC.Enabled {
		addr := fmt.Sprintf(":%d", cfg.RPC.Port)
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			// we have a connection
			log.Debugf("using RPC address %s", addr)
			inst.rpc = rpc.NewClient(conn)
			return qri, err
		}
	}

	if o.store != nil {
		inst.store = o.store
	} else if inst.store == nil {
		if inst.store, err = newStore(ctx, cfg); err != nil {
			log.Error("intializing store:", err.Error())
			return nil, fmt.Errorf("newStore: %s", err)
		}
	}

	if o.qfs != nil {
		inst.qfs = o.qfs
	} else if inst.qfs == nil {
		if inst.qfs, err = newFilesystem(cfg, inst.store); err != nil {
			log.Error("intializing filesystem:", err.Error())
			return nil, fmt.Errorf("newFilesystem: %s", err)
		}
	}

	if inst.registry == nil {
		inst.registry = newRegClient(ctx, cfg)
	}

	if o.repo != nil {
		inst.repo = o.repo
	} else if inst.repo == nil {
		if inst.repo, err = newRepo(inst.repoPath, cfg, inst.store); err != nil {
			log.Error("intializing repo:", err.Error())
			return nil, fmt.Errorf("newRepo: %s", err)
		}
	}

	if inst.repo != nil {
		inst.fsi = fsi.NewFSI(inst.repo)

		if qfssetter, ok := inst.repo.(repo.QFSSetter); ok {
			qfssetter.SetFilesystem(inst.qfs)
		}
	}

	if inst.node == nil {
		if inst.node, err = p2p.NewQriNode(inst.repo, cfg.P2P); err != nil {
			log.Error("intializing p2p:", err.Error())
			return
		}
	}

	if inst.node != nil {
		inst.node.LocalStreams = o.Streams

		if _, e := inst.node.IPFSCoreAPI(); e == nil {
			if inst.remoteClient, err = remote.NewClient(inst.node); err != nil {
				log.Error("initializing remote client:", err.Error())
				return
			}
		}

		if cfg.Remote != nil && cfg.Remote.Enabled {
			if o.remoteOptsFunc == nil {
				o.remoteOptsFunc = func(*remote.Options) {}
			}

			if inst.remote, err = remote.NewRemote(inst.node, cfg.Remote, o.remoteOptsFunc); err != nil {
				log.Error("intializing remote:", err.Error())
				return
			}
		}
	}

	return
}

// TODO (b5): this is a repo layout assertion, move to repo package?
func loadRepoConfig(repoPath string) (*config.Config, error) {
	path := filepath.Join(repoPath, "config.yaml")

	if _, e := os.Stat(path); os.IsNotExist(e) {
		return nil, nil
	}

	return config.ReadFromFile(path)
}

var (
	pluginLoadLock  sync.Once
	pluginLoadError error
)

func loadIPFSPluginsOnce(path string) error {
	body := func() {
		pluginLoadError = ipfs.LoadPlugins(path)
	}
	pluginLoadLock.Do(body)
	return pluginLoadError
}

func newStore(ctx context.Context, cfg *config.Config) (store cafs.Filestore, err error) {
	switch cfg.Store.Type {
	case "ipfs":
		path := cfg.Store.Path
		if path == "" && os.Getenv("IPFS_PATH") != "" {
			path = os.Getenv("IPFS_PATH")
		} else if path == "" {
			home, err := homedir.Dir()
			if err != nil {
				return nil, fmt.Errorf("creating IPFS store: %s", err)
			}
			path = filepath.Join(home, ".ipfs")
		}

		if err := loadIPFSPluginsOnce(path); err != nil {
			return nil, err
		}

		fsOpts := []ipfs.Option{
			func(c *ipfs.StoreCfg) {
				c.Ctx = ctx
				c.FsRepoPath = path
			},
			ipfs.OptsFromMap(cfg.Store.Options),
		}
		return ipfs.NewFilestore(fsOpts...)
	case "ipfs_http":
		urli, ok := cfg.Store.Options["url"]
		if !ok {
			return nil, fmt.Errorf("ipfs_http store requires 'url' option")
		}
		urlStr, ok := urli.(string)
		if !ok {
			return nil, fmt.Errorf("ipfs_http 'url' option must be a string")
		}
		return ipfs_http.New(urlStr)
	case "map":
		return cafs.NewMapstore(), nil
	default:
		return nil, fmt.Errorf("unknown store type: %s", cfg.Store.Type)
	}
}

func newRegClient(ctx context.Context, cfg *config.Config) (rc *regclient.Client) {
	if cfg.Registry != nil {
		switch cfg.Registry.Location {
		case "mock":
			cli, server := regmock.NewMockServerRegistry(regmock.NewMemRegistry())
			log.Infof("mock registry serving at: '%s'", server.URL)
			go func() {
				<-ctx.Done()
				server.Close()
			}()
			return cli
		case "":
			return rc
		default:
			return regclient.NewClient(&regclient.Config{
				Location: cfg.Registry.Location,
			})
		}
	}

	return nil
}

func newRepo(path string, cfg *config.Config, store cafs.Filestore) (r repo.Repo, err error) {
	var pro *profile.Profile
	if pro, err = profile.NewProfile(cfg.Profile); err != nil {
		return
	}

	switch cfg.Repo.Type {
	case "fs":
		return fsrepo.NewRepo(store, nil, pro, path)
	case "mem":
		return repo.NewMemRepo(pro, store, nil, profile.NewMemStore())
	default:
		return nil, fmt.Errorf("unknown repo type: %s", cfg.Repo.Type)
	}
}

func newFilesystem(cfg *config.Config, store cafs.Filestore) (qfs.Filesystem, error) {
	mux := map[string]qfs.PathResolver{
		"local": localfs.NewFS(),
		"http":  httpfs.NewFS(),
		"cafs":  store,
	}

	if ipfss, ok := store.(*ipfs.Filestore); ok {
		mux["ipfs"] = ipfss
	}

	fsys := muxfs.NewMux(mux)
	return fsys, nil
}

func newCron(cfg *config.Config, repoPath string) (cron.Scheduler, error) {
	updateCfg := cfg.Update
	if updateCfg == nil {
		updateCfg = config.DefaultUpdate()
	}

	cli := cron.HTTPClient{Addr: updateCfg.Address}
	if err := cli.Ping(); err == nil {
		return cli, nil
	}

	path, err := update.Path(repoPath)
	if err != nil {
		return nil, err
	}

	var jobStore, logStore cron.JobStore
	switch updateCfg.Type {
	case "fs":
		jobStore = cron.NewFlatbufferJobStore(filepath.Join(path, "jobs.qfb"))
		logStore = cron.NewFlatbufferJobStore(filepath.Join(path, "logs.qfb"))
	case "mem":
		jobStore = &cron.MemJobStore{}
		logStore = &cron.MemJobStore{}
	default:
		return nil, fmt.Errorf("unknown cron type: %s", updateCfg.Type)
	}

	svc := cron.NewCron(jobStore, logStore, update.Factory)
	return svc, nil
}

// NewInstanceFromConfigAndNode is a temporary solution to create an instance from an
// already-allocated QriNode & configuration
// don't write new code that relies on this, instead create a configuration
// and options that can be fed to NewInstance
func NewInstanceFromConfigAndNode(cfg *config.Config, node *p2p.QriNode) *Instance {
	ctx, teardown := context.WithCancel(context.Background())
	inst := &Instance{
		ctx:      ctx,
		teardown: teardown,
		cfg:      cfg,
		node:     node,
	}

	if node != nil && node.Repo != nil {
		inst.repo = node.Repo
		inst.store = node.Repo.Store()
		inst.qfs = node.Repo.Filesystem()
		inst.fsi = fsi.NewFSI(inst.repo)
	}

	return inst
}

// Instance bundles the foundational values qri relies on, including a qri
// configuration, p2p node, and base context.
// An instance wraps required state for for "Method" constructors, which
// contain qri business logic. Think of instance as the "core" of the qri
// ecosystem. Create an Instance pointer with NewInstance
type Instance struct {
	ctx      context.Context
	teardown context.CancelFunc

	repoPath string
	cfg      *config.Config

	streams ioes.IOStreams
	repo    repo.Repo
	store   cafs.Filestore
	node    *p2p.QriNode

	qfs          qfs.Filesystem
	cron         cron.Scheduler
	fsi          *fsi.FSI
	remote       *remote.Remote
	remoteClient *remote.Client
	registry     *regclient.Client

	rpc *rpc.Client
}

// Connect takes an instance online
func (inst *Instance) Connect(ctx context.Context) (err error) {
	if err = inst.node.GoOnline(); err != nil {
		log.Debugf("taking node online: %s", err.Error())
		return
	}

	// for now if we have an IPFS node instance, node.GoOnline has to make a new
	// instance to connect properly. If remoteClient retains the reference to the
	// old instance, we run into issues where the online instance can't "see"
	// the additions. We fix that by re-initializing the client with the new
	// instance
	if inst.remoteClient, err = remote.NewClient(inst.node); err != nil {
		log.Debugf("initializing remote client: %s", err.Error())
		return
	}

	// chriswhong/usgs_earthquakes
	return nil
}

// Context returns the base context for this instance
func (inst *Instance) Context() context.Context {
	return inst.ctx
}

// Config provides methods for manipulating Qri configuration
func (inst *Instance) Config() *config.Config {
	return inst.cfg
}

// ChangeConfig implements the ConfigSetter interface
func (inst *Instance) ChangeConfig(cfg *config.Config) (err error) {
	cfg = cfg.WithPrivateValues(inst.cfg)

	if path := inst.cfg.Path(); path != "" {
		if err = cfg.WriteToFile(path); err != nil {
			return
		}
	}

	inst.cfg = cfg
	return nil
}

// Node accesses the instance qri node if one exists
func (inst *Instance) Node() *p2p.QriNode {
	if inst == nil {
		return nil
	}
	return inst.node
}

// Repo accesses the instance Repo if one exists
func (inst *Instance) Repo() repo.Repo {
	if inst == nil {
		return nil
	}
	if inst.repo != nil {
		return inst.repo
	} else if inst.node != nil {
		return inst.node.Repo
	}
	return nil
}

// RepoPath returns the path to the directory qri is operating from
func (inst *Instance) RepoPath() string {
	if inst == nil {
		return ""
	}
	return inst.repoPath
}

// RPC accesses the instance RPC client if one exists
func (inst *Instance) RPC() *rpc.Client {
	if inst == nil {
		return nil
	}
	return inst.rpc
}

// Remote accesses the remote subsystem if one exists
func (inst *Instance) Remote() *remote.Remote {
	if inst == nil {
		return nil
	}
	return inst.remote
}

// RemoteClient exposes the instance client for making requests to remotes
func (inst *Instance) RemoteClient() *remote.Client {
	if inst == nil {
		return nil
	}
	return inst.remoteClient
}

// Teardown destroys the instance, releasing reserved resources
func (inst *Instance) Teardown() {
	inst.teardown()
}
