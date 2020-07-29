package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/linbit/k8s-await-election/pkg/consts"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

var Version = "development"

var log = logrus.New()

type AwaitElection struct {
	WithElection   bool
	Name           string
	LockName       string
	LockNamespace  string
	LeaderIdentity string
	StatusEndpoint string
	LeaderExec     func(ctx context.Context) error
}

type ConfigError struct {
	missingEnv string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("config: missing required environment variable: '%s'", e.missingEnv)
}

func NewAwaitElectionConfig(exec func(ctx context.Context) error) (*AwaitElection, error) {
	if os.Getenv(consts.AwaitElectionEnabledKey) == "" {
		return &AwaitElection{
			WithElection: false,
			LeaderExec:   exec,
		}, nil
	}

	name := os.Getenv(consts.AwaitElectionNameKey)
	if name == "" {
		return nil, &ConfigError{missingEnv: consts.AwaitElectionNameKey}
	}

	lockName := os.Getenv(consts.AwaitElectionLockNameKey)
	if lockName == "" {
		return nil, &ConfigError{missingEnv: consts.AwaitElectionLockNameKey}
	}

	lockNamespace := os.Getenv(consts.AwaitElectionLockNamespaceKey)
	if lockNamespace == "" {
		return nil, &ConfigError{missingEnv: consts.AwaitElectionLockNamespaceKey}
	}

	leaderIdentity := os.Getenv(consts.AwaitElectionIdentityKey)
	if leaderIdentity == "" {
		return nil, &ConfigError{missingEnv: consts.AwaitElectionIdentityKey}
	}

	// Optional
	statusEndpoint := os.Getenv(consts.AwaitElectionStatusEndpointKey)

	return &AwaitElection{
		WithElection:   true,
		Name:           name,
		LockName:       lockName,
		LockNamespace:  lockNamespace,
		LeaderIdentity: leaderIdentity,
		StatusEndpoint: statusEndpoint,
		LeaderExec:     exec,
	}, nil
}

func (el *AwaitElection) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Not running with leader election/kubernetes context, just run the provided function
	if !el.WithElection {
		log.Info("not running with leader election")
		return el.LeaderExec(ctx)
	}

	// Create kubernetes client
	kubeCfg, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to read cluster config: %w", err)
	}

	kubeClient, err := kubernetes.NewForConfig(kubeCfg)
	if err != nil {
		return fmt.Errorf("failed to create KubeClient for config: %w", err)
	}

	// result of the LeaderExec(ctx) command will be send over this channel
	execResult := make(chan error)

	// Create lock for leader election using provided settings
	lock := resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      el.LockName,
			Namespace: el.LockNamespace,
		},
		Client: kubeClient.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: el.LeaderIdentity,
		},
	}

	leaderCfg := leaderelection.LeaderElectionConfig{
		Lock:            &lock,
		Name:            el.Name,
		ReleaseOnCancel: true,
		// Suggested default values
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				// actual start the command here.
				// Note: this callback is started in a goroutine, so we can block this
				// execution path for as long as we want.
				execResult <- el.LeaderExec(ctx)
			},
			OnNewLeader: func(identity string) {
				log.Infof("long live our new leader: '%s'!", identity)
			},
			OnStoppedLeading: func() {
				log.Info("lost leader status")
			},
		},
	}
	elector, err := leaderelection.NewLeaderElector(leaderCfg)
	if err != nil {
		return fmt.Errorf("failed to create leader elector: %w", err)
	}

	statusServerResult := el.startStatusEndpoint(ctx)

	go elector.Run(ctx)

	// the different end conditions:
	// 1. context was cancelled -> error
	// 2. command executed -> either error or nil, depending on return value
	// 3. status endpoint failed -> could not create status endpoint
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-execResult:
		return r
	case r := <-statusServerResult:
		return r
	}
}

func (el *AwaitElection) startStatusEndpoint(ctx context.Context) <-chan error {
	statusServerResult := make(chan error)

	if el.StatusEndpoint == "" {
		log.Info("no status endpoint specified, will not be created")
		return statusServerResult
	}

	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		_, err := writer.Write([]byte("running"))
		if err != nil {
			log.WithField("err", err).Error("failed to serve status endpoint")
		}
	})
	statusServer := http.Server{
		Addr:    el.StatusEndpoint,
		Handler: serveMux,
		BaseContext: func(listener net.Listener) context.Context {
			return ctx
		},
	}
	go func() {
		statusServerResult <- statusServer.ListenAndServe()
	}()
	return statusServerResult
}

// Run the command specified the this program's arguments to completion.
// Stdout and Stderr are inherited from this process. If the provided context is cancelled,
// the started process is killed.
func Execute(ctx context.Context) error {
	log.Infof("starting command '%s' with arguments: '%v'", os.Args[1], os.Args[2:])
	cmd := exec.CommandContext(ctx, os.Args[1], os.Args[2:]...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

func main() {
	log.WithField("version", Version).Info("running k8s-await-election")

	if len(os.Args) <= 1 {
		log.WithField("args", os.Args).Fatal("Need at least one argument to run")
	}

	awaitElectionConfig, err := NewAwaitElectionConfig(Execute)
	if err != nil {
		log.WithField("err", err).Fatal("failed to create runner")
	}

	err = awaitElectionConfig.Run()
	if err != nil {
		log.WithField("err", err).Fatalf("failed to run")
	}
}
