package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/StephenGriese/stdlibapp/hello"
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
	kittyclient.OptionalServiceConfig
	Name string `yaml:"name"`
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

	helloClient := newHelloClient(logger, metricsFactory, appConfig.Downstream, satWsTokenService, clientOpts...)
	helloService := newHelloService(logger, helloClient, appConfig)

	var wg sync.WaitGroup

	httpServer := newHTTPServer(ctx, logger, metricsFactory, tracer, appConfig, helloService, satWsKeyService)

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
	appConfig AppConfig, helloService hello.Service, keyServices ...jwt.KeyService) kittyserver.Server {
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
		kittyserver.WithAdminScope(hello.AdminScope),
		kittyserver.WithRequestHandlers(hello.NewRequestHandlers(logger, helloService)),
		kittyserver.WithConfig(appConfig),
	}

	s, err := kittyserver.NewServer(ctx, srvrOpts...)

	if err != nil {
		panic(errors.Wrap(err, "error creating server"))
	}

	return s
}

func newHelloClient(logger plog.Logger, metricsFactory kittymetrics.Factory, config DownstreamConfig, tokenService jwt.TokenService, clientOpts ...kittyclient.Option) hello.Client {
	if !config.Enabled {
		logger.Info(context.Background(), "Client is disabled.")
		return nil
	}
	clientOpts = append(clientOpts, kittyclient.WithClientConfig(config.Client))
	clientOpts = kittyclient.AppendRetryConfigOpts(config.Retry, clientOpts...)

	logger.Info(context.Background(), "Using Client", "url", config.URL, "clientOpts", clientOpts)

	client := hello.NewHTTPClient(logger, config.URL, tokenService, clientOpts...)
	client = hello.WithLoggingClient(logger, client)
	client = hello.WithInstrumentingClient(metricsFactory, client)

	return client
}

func newHelloService(logger plog.Logger, helloClient hello.Client, config AppConfig) hello.Service {

	svcOpts := []hello.ServiceOption{
		hello.WithID(config.AppName),
		hello.WithLogger(logger),
	}
	if helloClient != nil {
		svcOpts = append(svcOpts, hello.WithHelloClient(helloClient))
	}
	service := hello.NewService(svcOpts...)
	service = hello.WithLoggingService(logger, service)

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
