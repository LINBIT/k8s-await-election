package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/linbit/k8s-await-election/pkg/consts"
)

var Version = "development"

var log = logrus.New()

type AwaitElection struct {
	Name             string
	LockName         string
	LockNamespace    string
	LeaderIdentity   string
	StatusEndpoint   string
	ServiceName      string
	ServiceNamespace string
	PodIP            string
	NodeName         *string
	ServicePorts     []corev1.EndpointPort
	LeaderExec       func(ctx context.Context) error
}

type ConfigError struct {
	missingEnv string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("config: missing required environment variable: '%s'", e.missingEnv)
}

func NewAwaitElectionConfig(exec func(ctx context.Context) error) (*AwaitElection, error) {
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
	podIP := os.Getenv(consts.AwaitElectionPodIP)
	var nodeName *string
	if val, ok := os.LookupEnv(consts.AwaitElectionNodeName); ok {
		nodeName = &val
	}

	serviceName := os.Getenv(consts.AwaitElectionServiceName)
	serviceNamespace := os.Getenv(consts.AwaitElectionServiceNamespace)

	servicePortsJson := os.Getenv(consts.AwaitElectionServicePortsJson)
	var servicePorts []corev1.EndpointPort
	err := json.Unmarshal([]byte(servicePortsJson), &servicePorts)
	if serviceName != "" && err != nil {
		return nil, fmt.Errorf("failed to parse ports from env: %w", err)
	}

	return &AwaitElection{
		Name:             name,
		LockName:         lockName,
		LockNamespace:    lockNamespace,
		LeaderIdentity:   leaderIdentity,
		StatusEndpoint:   statusEndpoint,
		PodIP:            podIP,
		NodeName:         nodeName,
		ServiceName:      serviceName,
		ServiceNamespace: serviceNamespace,
		ServicePorts:     servicePorts,
		LeaderExec:       exec,
	}, nil
}

func (el *AwaitElection) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
				// First we need to register our pod as the service endpoint
				err := el.setServiceEndpoint(ctx, kubeClient)
				if err != nil {
					execResult <- err
					return
				}
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

	leaseDuration, err := strconv.Atoi(os.Getenv(consts.AwaitElectionLeaseDuration))
	if err == nil {
		leaderCfg.LeaseDuration = time.Duration(leaseDuration) * time.Second
	}

	renewDeadline, err := strconv.Atoi(os.Getenv(consts.AwaitElectionRenewDeadline))
	if err == nil {
		leaderCfg.RenewDeadline = time.Duration(renewDeadline) * time.Second
	}

	retryPeriod, err := strconv.Atoi(os.Getenv(consts.AwaitElectionRetryPeriod))
	if err == nil {
		leaderCfg.RetryPeriod = time.Duration(retryPeriod) * time.Second
	}

	elector, err := leaderelection.NewLeaderElector(leaderCfg)
	if err != nil {
		return fmt.Errorf("failed to create leader elector: %w", err)
	}

	statusServerResult := el.startStatusEndpoint(ctx, elector)

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

func (el *AwaitElection) setServiceEndpoint(ctx context.Context, client *kubernetes.Clientset) error {
	if el.ServiceName == "" {
		return nil
	}

	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      el.ServiceName,
			Namespace: el.ServiceNamespace,
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{{IP: el.PodIP, NodeName: el.NodeName}},
				Ports:     el.ServicePorts,
			},
		},
	}
	_, err := client.CoreV1().Endpoints(el.ServiceNamespace).Create(ctx, endpoints, metav1.CreateOptions{})
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return err
		}

		_, err := client.CoreV1().Endpoints(el.ServiceNamespace).Update(ctx, endpoints, metav1.UpdateOptions{})
		return err
	}

	return nil
}

func (el *AwaitElection) startStatusEndpoint(ctx context.Context, elector *leaderelection.LeaderElector) <-chan error {
	statusServerResult := make(chan error)

	if el.StatusEndpoint == "" {
		log.Info("no status endpoint specified, will not be created")
		return statusServerResult
	}

	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		err := elector.Check(2 * time.Second)
		if err != nil {
			log.WithField("err", err).Error("failed to step down gracefully, reporting unhealthy status")
			writer.WriteHeader(500)
			_, err := writer.Write([]byte("{\"status\": \"expired\"}"))
			if err != nil {
				log.WithField("err", err).Error("failed to serve request")
			}
			return
		}

		_, err = writer.Write([]byte("{\"status\": \"ok\"}"))
		if err != nil {
			log.WithField("err", err).Error("failed to serve request")
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

	if os.Getenv(consts.AwaitElectionEnabledKey) == "" {
		path, err := exec.LookPath(os.Args[1])
		if err != nil {
			log.WithError(err).Fatal("Error looking up path")
		}

		err = unix.Exec(path, os.Args[1:], os.Environ())
		log.WithError(err).Fatal("Failed to execve()")
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
