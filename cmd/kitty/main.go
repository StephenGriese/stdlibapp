package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/StephenGriese/stdlibapp/dictionary"
	"github.com/StephenGriese/stdlibapp/ngb"
	"github.com/comcast-pulse/kitty/auth/jwt"
	"github.com/comcast-pulse/kitty/auth/jwt/sat"
	"github.com/comcast-pulse/kitty/health"
	kittyhttp "github.com/comcast-pulse/kitty/http"
	kittyclient "github.com/comcast-pulse/kitty/http/client"
	kittyserver "github.com/comcast-pulse/kitty/http/server"
	kittylog "github.com/comcast-pulse/kitty/log"
	kittymetrics "github.com/comcast-pulse/kitty/metrics"
	kittytrace "github.com/comcast-pulse/kitty/tracing"
	plog "github.com/comcast-pulse/log"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
)

var (
	builder   = "Undeclared"
	buildTime = "Undeclared"
	goversion = "Undeclared"
	version   = "Undeclared"
)

// AppConfig defines the configurable attributes for the VBOM. The structure of this type
// should match the structure of the YAML in Consul.
type AppConfig struct {
	AppName      string                `yaml:"appName"`
	Port         int                   `yaml:"port" config:"required"`
	LoggerConfig kittylog.LoggerConfig `yaml:"loggerConfig" config:"required"`
	Health       health.ServiceConfig  `yaml:"health"`
	Tracing      kittytrace.Config     `yaml:"tracing" config:"required"`
	SATClient    sat.ClientConfig      `yaml:"satClient" json:"satClient"`
	JWTConfig    jwt.JWTConfig         `yaml:"jwtConfig"`
	Downstream   DownstreamConfig      `yaml:"downstream"`
}

type DownstreamConfig struct {
	kittyclient.OptionalServiceConfig `yaml:",inline"`
	Name                              string `yaml:"name"`
}

// flags holds the command line options
type flags struct {
	LocalYaml string
}

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Stdout, os.Args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, w io.Writer, args []string) error {

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer func() {
		log.Println("cancelling run's notify context")
		cancel()
	}()

	flags, err := getFlags(args[1:], w)
	if err != nil {
		return fmt.Errorf("error getting flags: %w", err)
	}

	appConfig, err := loadLocalYaml(flags.LocalYaml)
	if err != nil {
		return fmt.Errorf("error in Setup. could not load config: %w", err)
	}

	metricsFactory := kittymetrics.NewFactory(appConfig.AppName)

	logger, err := kittylog.NewLoggerFromConfig(appConfig.LoggerConfig,
		kittylog.WithContexter(kittytrace.TraceIDContexter),
		kittylog.WithContexter(jwt.ClientIDContexter))
	if err != nil {
		panic(err)
	}

	tracer, closer := newTracer(logger, appConfig.Tracing)
	defer func() {
		_ = closer.Close()
	}()

	clientOpts := []kittyclient.Option{kittyclient.WithClientTracer(tracer)}

	satWsTokenService, satWsKeyService, err := newSATService(ctx, logger, appConfig.SATClient)
	if err != nil {
		return fmt.Errorf("error creating SAT service: %w", err)
	}

	client := newClient(logger, appConfig.Downstream.Name, metricsFactory, appConfig.Downstream, satWsTokenService, clientOpts...)
	service := newService(logger, appConfig.Downstream.Name, client, appConfig)

	var wg sync.WaitGroup

	httpServer := newHTTPServer(ctx, logger, metricsFactory, tracer, appConfig, service, satWsKeyService)

	wg.Add(1)
	go func() {
		defer func() {
			wg.Done()
			logger.Info(ctx, "wc.Done() from kitty server goroutine")
		}()
		httpServer.Start(ctx)
		cancel()
	}()

	wg.Wait()
	logger.Info(ctx, "server stopped")

	return nil
}

func newHTTPServer(ctx context.Context, logger plog.Logger, metricsFactory kittymetrics.Factory, tracer opentracing.Tracer,
	appConfig AppConfig, dictionaryService ngb.Service, keyServices ...jwt.KeyService) kittyserver.Server {
	srvrOpts := []kittyserver.ServerOption{
		kittyserver.WithPort(appConfig.Port),
		kittyserver.WithLogger(logger),
		kittyserver.WithRequestLoggerConfig(logger, appConfig.LoggerConfig.Request),
		kittyserver.WithName(appConfig.AppName),
		kittyserver.WithBuildInfo(version, builder, buildTime, goversion),
		kittyserver.WithCodebigServiceName(appConfig.AppName),
		kittyserver.WithMetricsFactory(metricsFactory),
		kittyserver.WithServerTracer(tracer),
		kittyserver.WithWebsec(appConfig.JWTConfig, jwt.NewKeyStore(keyServices...)),
		kittyserver.WithErrorEncoder(kittyhttp.EncodeCodedErrorsResponse),
		kittyserver.WithAdminScope(ngb.AdminScope),
		kittyserver.WithRequestHandlers(ngb.NewRequestHandlers(logger, appConfig.AppName, "lookup handler", dictionaryService)),
		kittyserver.WithConfig(appConfig),
	}

	s, err := kittyserver.NewServer(ctx, srvrOpts...)

	if err != nil {
		panic(errors.Wrap(err, "error creating server"))
	}

	return s
}

func newClient(logger plog.Logger, clientComponentName string, metricsFactory kittymetrics.Factory, downstreamConfig DownstreamConfig, tokenService jwt.TokenService, clientOpts ...kittyclient.Option) dictionary.Client {
	if !downstreamConfig.Enabled {
		logger.Info(context.Background(), "Client is disabled.")
		return nil
	}
	clientOpts = append(clientOpts, kittyclient.WithClientConfig(downstreamConfig.Client))
	clientOpts = kittyclient.AppendRetryConfigOpts(downstreamConfig.Retry, clientOpts...)

	logger.Info(context.Background(), "Using Client", "url", downstreamConfig.URL, "clientOpts", clientOpts)

	client := ngb.NewHTTPClient(logger, downstreamConfig.Name, downstreamConfig.Name, downstreamConfig.URL, tokenService, clientOpts...)
	client = ngb.WithLoggingClient(logger, clientComponentName, client)
	client = ngb.WithInstrumentingClient(metricsFactory, downstreamConfig.Name, client)

	return client
}

func newService(logger plog.Logger, serviceComponentName string, client dictionary.Client, config AppConfig) ngb.Service {

	svcOpts := []ngb.ServiceOption{
		ngb.WithAppName(config.AppName),
		ngb.WithLogger(logger),
	}
	if client != nil {
		svcOpts = append(svcOpts, ngb.WithClient(client))
	}
	service := ngb.NewService(svcOpts...)
	service = ngb.WithLoggingService(logger, serviceComponentName, service)

	return service
}

func newTracer(logger plog.Logger, config kittytrace.Config) (opentracing.Tracer, io.Closer) {
	tracer, closer, err := kittytrace.NewTracer(logger, config)
	if err != nil {
		panic(errors.Wrap(err, "error creating tracer"))
	}

	return tracer, closer
}

func newSATService(ctx context.Context, logger plog.Logger, clientConfig sat.ClientConfig) (jwt.TokenService, jwt.KeyService, error) {
	logger.Info(ctx, "Using SAT", "url", clientConfig.URL, "clientId", clientConfig.ClientID, "scope", clientConfig.Scopes)

	client, err := sat.NewConfiguredHTTPClient(logger, clientConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating SAT client: %w", err)
	}

	var (
		tokenService jwt.TokenService
		keyService   jwt.KeyService
	)

	if tokenService, err = jwt.NewCache(logger, client); err != nil {
		return nil, nil, fmt.Errorf("error creating JWT cache: %w", err)
	}

	if keyService, err = jwt.NewKeyService(logger, clientConfig.JWTConfig, client); err != nil {
		return nil, nil, fmt.Errorf("error creating JWT key service: %w", err)
	}

	return tokenService, keyService, nil
}

func loadLocalYaml(yamlFiles string) (AppConfig, error) {
	var cfg AppConfig

	fsl := strings.Split(yamlFiles, ",")
	for _, f := range fsl {
		if err := loadYamlFile(&cfg, f); err != nil {
			return AppConfig{}, fmt.Errorf("error reading yaml from file %s: %w", f, err)
		}
	}

	return cfg, nil
}

func loadYamlFile(rawConfig any, name string) error {
	fileBytes, err := os.ReadFile(name)
	if err != nil {
		return err
	}

	if err = yaml.Unmarshal(fileBytes, rawConfig); err != nil {
		return err
	}

	return nil
}

func getFlags(args []string, w io.Writer) (flags, error) {
	f := flags{}
	fs := flag.NewFlagSet("", flag.ExitOnError)
	fs.SetOutput(w)
	fs.StringVar(&(f.LocalYaml), "local-yaml", "", "Comma separated list of yaml files to load instead of normal configuration read")
	if err := fs.Parse(args); err != nil {
		return flags{}, err
	}

	return f, nil
}
