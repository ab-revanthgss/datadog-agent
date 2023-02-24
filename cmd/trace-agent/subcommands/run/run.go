// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package run

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/DataDog/datadog-agent/cmd/manager"
	remotecfg "github.com/DataDog/datadog-agent/cmd/trace-agent/config/remote"
	"github.com/DataDog/datadog-agent/cmd/trace-agent/subcommands"
	"github.com/DataDog/datadog-agent/comp/trace/config"
	coreconfig "github.com/DataDog/datadog-agent/pkg/config"
	rc "github.com/DataDog/datadog-agent/pkg/config/remote"
	"github.com/DataDog/datadog-agent/pkg/pidfile"
	"github.com/DataDog/datadog-agent/pkg/tagger"
	"github.com/DataDog/datadog-agent/pkg/tagger/local"
	"github.com/DataDog/datadog-agent/pkg/tagger/remote"
	"github.com/DataDog/datadog-agent/pkg/trace/agent"
	"github.com/DataDog/datadog-agent/pkg/trace/api"
	tracecfg "github.com/DataDog/datadog-agent/pkg/trace/config"
	"github.com/DataDog/datadog-agent/pkg/trace/info"
	tracelog "github.com/DataDog/datadog-agent/pkg/trace/log"
	"github.com/DataDog/datadog-agent/pkg/trace/metrics"
	"github.com/DataDog/datadog-agent/pkg/trace/metrics/timing"
	"github.com/DataDog/datadog-agent/pkg/trace/telemetry"
	"github.com/DataDog/datadog-agent/pkg/trace/watchdog"
	"github.com/DataDog/datadog-agent/pkg/util"
	"github.com/DataDog/datadog-agent/pkg/util/log"
	"github.com/DataDog/datadog-agent/pkg/util/profiling"
	"github.com/DataDog/datadog-agent/pkg/version"
	"github.com/DataDog/datadog-agent/pkg/workloadmeta"

	// register all workloadmeta collectors
	_ "github.com/DataDog/datadog-agent/pkg/workloadmeta/collectors"
)

const messageAgentDisabled = `trace-agent not enabled. Set the environment variable
DD_APM_ENABLED=true or add "apm_config.enabled: true" entry
to your datadog.yaml. Exiting...`

type RunParams struct {
	*subcommands.GlobalParams

	// PIDFilePath contains the value of the --pidfile flag.
	PIDFilePath string
	// CPUProfile contains the value for the --cpu-profile flag.
	CPUProfile string
	// MemProfile contains the value for the --mem-profile flag.
	MemProfile string
}

// Run is the entrypoint of our code, which starts the agent.
func runAgent(ctx context.Context, cliParams *RunParams, cfg config.Component) error {

	tracecfg := cfg.Object()
	// cfg, err := cmdconfig.LoadConfigFile(cliParams.ConfPath)
	// if err != nil {
	// 	fmt.Println(err) // TODO: remove me
	// 	if err == config.ErrMissingAPIKey {
	// 		fmt.Println(config.ErrMissingAPIKey)

	// 		// a sleep is necessary to ensure that supervisor registers this process as "STARTED"
	// 		// If the exit is "too quick", we enter a BACKOFF->FATAL loop even though this is an expected exit
	// 		// http://supervisord.org/subprocess.html#process-states
	// 		time.Sleep(5 * time.Second)

	// 		// osutil.Exitf was used here prior; now that fx handles the process not sure if that makes
	// 		// sense. Return err for now and experiment. this was particularly relevant on windows
	// 		// and how the Windows Service Manager handled service errors and restarts.
	// 		return err
	// 	}
	// 	return err
	// }
	err := info.InitInfo(tracecfg) // for expvar & -info option
	if err != nil {
		return err
	}

	telemetryCollector := telemetry.NewCollector(tracecfg)

	if err := coreconfig.SetupLogger(
		coreconfig.LoggerName("TRACE"),
		coreconfig.Datadog.GetString("log_level"),
		tracecfg.LogFilePath,
		coreconfig.GetSyslogURI(),
		coreconfig.Datadog.GetBool("syslog_rfc"),
		coreconfig.Datadog.GetBool("log_to_console"),
		coreconfig.Datadog.GetBool("log_format_json"),
	); err != nil {
		telemetryCollector.SendStartupError(telemetry.CantCreateLogger, err)
		return fmt.Errorf("Cannot create logger: %v", err)
	}
	tracelog.SetLogger(corelogger{})
	defer log.Flush()

	if !tracecfg.Enabled {
		log.Info(messageAgentDisabled)
		telemetryCollector.SendStartupError(telemetry.TraceAgentNotEnabled, fmt.Errorf(""))

		// a sleep is necessary to ensure that supervisor registers this process as "STARTED"
		// If the exit is "too quick", we enter a BACKOFF->FATAL loop even though this is an expected exit
		// http://supervisord.org/subprocess.html#process-states
		time.Sleep(5 * time.Second)
		return nil
	}

	defer watchdog.LogOnPanic()

	if cliParams.CPUProfile != "" {
		f, err := os.Create(cliParams.CPUProfile)
		if err != nil {
			log.Error(err)
		}
		pprof.StartCPUProfile(f) //nolint:errcheck
		log.Info("CPU profiling started...")
		defer pprof.StopCPUProfile()
	}

	if cliParams.PIDFilePath != "" {
		err := pidfile.WritePID(cliParams.PIDFilePath)
		if err != nil {
			telemetryCollector.SendStartupError(telemetry.CantWritePIDFile, err)
			log.Criticalf("Error writing PID file, exiting: %v", err)
			os.Exit(1)
		}

		log.Infof("PID '%d' written to PID file '%s'", os.Getpid(), cliParams.PIDFilePath)
		defer os.Remove(cliParams.PIDFilePath)
	}

	if err := util.SetupCoreDump(coreconfig.Datadog); err != nil {
		log.Warnf("Can't setup core dumps: %v, core dumps might not be available after a crash", err)
	}

	err = manager.ConfigureAutoExit(ctx, coreconfig.Datadog)
	if err != nil {
		telemetryCollector.SendStartupError(telemetry.CantSetupAutoExit, err)
		return fmt.Errorf("Unable to configure auto-exit, err: %v", err)
	}

	err = metrics.Configure(tracecfg, []string{"version:" + version.AgentVersion})
	if err != nil {
		telemetryCollector.SendStartupError(telemetry.CantConfigureDogstatsd, err)
		return fmt.Errorf("cannot configure dogstatsd: %v", err)
	}
	defer metrics.Flush()
	defer timing.Stop()

	metrics.Count("datadog.trace_agent.started", 1, nil, 1)

	rand.Seed(time.Now().UTC().UnixNano())

	remoteTagger := coreconfig.Datadog.GetBool("apm_config.remote_tagger")
	if remoteTagger {
		options, err := remote.NodeAgentOptions()
		if err != nil {
			log.Errorf("Unable to configure the remote tagger: %s", err)
			remoteTagger = false
		} else {
			tagger.SetDefaultTagger(remote.NewTagger(options))
			if err := tagger.Init(ctx); err != nil {
				log.Infof("Starting remote tagger failed. Falling back to local tagger: %s", err)
				remoteTagger = false
			}
		}
	}

	// starts the local tagger if apm_config says so, or if starting the
	// remote tagger has failed.
	if !remoteTagger {
		store := workloadmeta.CreateGlobalStore(workloadmeta.NodeAgentCatalog)
		store.Start(ctx)

		tagger.SetDefaultTagger(local.NewTagger(store))
		if err := tagger.Init(ctx); err != nil {
			log.Errorf("failed to start the tagger: %s", err)
		}
	}

	defer func() {
		err := tagger.Stop()
		if err != nil {
			log.Error(err)
		}
	}()

	if coreconfig.Datadog.GetBool("remote_configuration.enabled") {
		// Auth tokens are handled by the rcClient
		rcClient, err := rc.NewAgentGRPCConfigFetcher()
		if err != nil {
			telemetryCollector.SendStartupError(telemetry.CantCreateRCCLient, err)
			return fmt.Errorf("could not instantiate the tracer remote config client: %v", err)
		}
		api.AttachEndpoint(api.Endpoint{
			Pattern: "/v0.7/config",
			Handler: func(r *api.HTTPReceiver) http.Handler { return remotecfg.ConfigHandler(r, rcClient, tracecfg) },
		})
	}

	api.AttachEndpoint(api.Endpoint{
		Pattern: "/config/set",
		Handler: func(r *api.HTTPReceiver) http.Handler {
			return cfg.SetHandler()
		},
	})

	agnt := agent.NewAgent(ctx, tracecfg, telemetryCollector)
	log.Infof("Trace agent running on host %s", tracecfg.Hostname)
	if pcfg := profilingConfig(tracecfg); pcfg != nil {
		if err := profiling.Start(*pcfg); err != nil {
			log.Warn(err)
		} else {
			log.Infof("Internal profiling enabled: %s.", pcfg)
		}
		defer profiling.Stop()
	}
	go func() {
		time.Sleep(time.Second * 30)
		telemetryCollector.SendStartupSuccess()
	}()
	agnt.Run()

	// collect memory profile
	if cliParams.CPUProfile != "" {
		f, err := os.Create(cliParams.MemProfile)
		if err != nil {
			log.Error("Could not create memory profile: ", err)
		}

		// get up-to-date statistics
		runtime.GC()
		// Not using WriteHeapProfile but instead calling WriteTo to
		// make sure we pass debug=1 and resolve pointers to names.
		if err := pprof.Lookup("heap").WriteTo(f, 1); err != nil {
			log.Error("Could not write memory profile: ", err)
		}
		f.Close()
	}

	return nil
}

type corelogger struct{}

// Trace implements Logger.
func (corelogger) Trace(v ...interface{}) { log.Trace(v...) }

// Tracef implements Logger.
func (corelogger) Tracef(format string, params ...interface{}) { log.Tracef(format, params...) }

// Debug implements Logger.
func (corelogger) Debug(v ...interface{}) { log.Debug(v...) }

// Debugf implements Logger.
func (corelogger) Debugf(format string, params ...interface{}) { log.Debugf(format, params...) }

// Info implements Logger.
func (corelogger) Info(v ...interface{}) { log.Info(v...) }

// Infof implements Logger.
func (corelogger) Infof(format string, params ...interface{}) { log.Infof(format, params...) }

// Warn implements Logger.
func (corelogger) Warn(v ...interface{}) error { return log.Warn(v...) }

// Warnf implements Logger.
func (corelogger) Warnf(format string, params ...interface{}) error {
	return log.Warnf(format, params...)
}

// Error implements Logger.
func (corelogger) Error(v ...interface{}) error { return log.Error(v...) }

// Errorf implements Logger.
func (corelogger) Errorf(format string, params ...interface{}) error {
	return log.Errorf(format, params...)
}

// Critical implements Logger.
func (corelogger) Critical(v ...interface{}) error { return log.Critical(v...) }

// Criticalf implements Logger.
func (corelogger) Criticalf(format string, params ...interface{}) error {
	return log.Criticalf(format, params...)
}

// Flush implements Logger.
func (corelogger) Flush() { log.Flush() }

func profilingConfig(tracecfg *tracecfg.AgentConfig) *profiling.Settings {
	if !coreconfig.Datadog.GetBool("apm_config.internal_profiling.enabled") {
		return nil
	}
	endpoint := coreconfig.Datadog.GetString("internal_profiling.profile_dd_url")
	if endpoint == "" {
		endpoint = fmt.Sprintf(profiling.ProfilingURLTemplate, tracecfg.Site)
	}
	return &profiling.Settings{
		ProfilingURL: endpoint,

		// remaining configuration parameters use the top-level `internal_profiling` config
		Period:               coreconfig.Datadog.GetDuration("internal_profiling.period"),
		CPUDuration:          coreconfig.Datadog.GetDuration("internal_profiling.cpu_duration"),
		MutexProfileFraction: coreconfig.Datadog.GetInt("internal_profiling.mutex_profile_fraction"),
		BlockProfileRate:     coreconfig.Datadog.GetInt("internal_profiling.block_profile_rate"),
		WithGoroutineProfile: coreconfig.Datadog.GetBool("internal_profiling.enable_goroutine_stacktraces"),
		Tags:                 []string{fmt.Sprintf("version:%s", version.AgentVersion)},
	}
}
