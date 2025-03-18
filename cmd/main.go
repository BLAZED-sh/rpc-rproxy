package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"

	"github.com/BLAZED-sh/rpc-rproxy/pkg/proxy"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// CLI flag definitions
	// Basic options
	listenSocket := flag.String("listen", "/tmp/rpc-proxy.sock", "Unix socket path to listen on")
	upstreamSocket := flag.String("upstream", "", "Unix socket path for upstream connection")
	socketPerms := flag.String("socket-perms", "0666", "Unix socket permissions in octal (e.g. 0666)")

	// Feature options
	asyncCallbacks := flag.Bool("async", false, "Enable asynchronous callbacks")
	multiplexing := flag.Bool("multiplex", false, "Enable message multiplexing for the upstream")
	
	// Debug options
	debugSignal := flag.Int("debug-signal", int(syscall.SIGUSR1), "Signal number to use for dumping debug info (default: SIGUSR1)")

	// Performance options
	bufferSize := flag.Int("buffer", 16384, "Buffer size for JSON stream lexer")
	maxRead := flag.Int("max-read", 4096, "Maximum read size per operation")

	// Logging options
	logLevel := flag.String("log-level", "info", "Log level (trace, debug, info, warn, error, fatal)")
	prettyLogs := flag.Bool("pretty", false, "Enable pretty logging output")

	// Other options
	showVersion := flag.Bool("version", false, "Show version and exit")

	flag.Parse()

	// Version info
	const version = "0.1.0"

	// Handle version flag
	if *showVersion {
		fmt.Printf("rpc-rproxy version %s\n", version)
		os.Exit(0)
	}

	// Validate required flags
	if *upstreamSocket == "" {
		fmt.Println("Error: --upstream flag is required")
		flag.Usage()
		os.Exit(1)
	}

	// Configure zerolog
	setupLogging(*logLevel, *prettyLogs)

	// Create proxy
	rpcProxy := proxy.NewUnixUpstreamJsonRpcProxy(*upstreamSocket, *asyncCallbacks, *multiplexing, *bufferSize, *maxRead)

	// Remove socket file if it exists
	if _, err := os.Stat(*listenSocket); err == nil {
		if err := os.Remove(*listenSocket); err != nil {
			log.Fatal().Err(err).Str("socket", *listenSocket).Msg("Failed to remove existing socket file")
		}
		log.Debug().Str("socket", *listenSocket).Msg("Removed existing socket file")
	}

	// Add listener
	err := rpcProxy.AddUnixSocketListener(context.Background(), *listenSocket)
	if err != nil {
		log.Fatal().Err(err).Str("socket", *listenSocket).Msg("Failed to add Unix socket listener")
	}

	// Set socket permissions
	socketMode, err := strconv.ParseUint(*socketPerms, 8, 32)
	if err != nil {
		log.Warn().Err(err).Str("perms", *socketPerms).Msg("Invalid socket permissions format, using default 0666")
		socketMode = 0666
	}

	if err := os.Chmod(*listenSocket, os.FileMode(socketMode)); err != nil {
		log.Warn().Err(err).Str("socket", *listenSocket).Uint64("mode", socketMode).Msg("Failed to set socket permissions")
	}

	// Start listening
	rpcProxy.Listen()
	log.Info().
		Str("listen", *listenSocket).
		Str("upstream", *upstreamSocket).
		Bool("async_callbacks", *asyncCallbacks).
		Bool("multiplexing", *multiplexing).
		Int("buffer_size", *bufferSize).
		Int("max_read", *maxRead).
		Str("version", version).
		Msg("JSON-RPC proxy started")

	// Setup signal handlers
	sigChan := make(chan os.Signal, 1)
	debugSigChan := make(chan os.Signal, 1)
	
	// Register for debug signal 
	debugSig := syscall.Signal(*debugSignal)
	signal.Notify(debugSigChan, debugSig)
	log.Info().Int("signal", *debugSignal).Msg("Debug signal registered - send this signal to dump debug info")
	
	// Register for termination signals
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	// Handle signals
	go func() {
		for {
			select {
			case <-debugSigChan:
				log.Info().Int("signal", *debugSignal).Msg("Received debug signal - dumping debug information")
				rpcProxy.DumpDebugInfo()
				
				// Additional debug: print goroutine stacks to stderr
				buf := make([]byte, 1<<20) // 1MB buffer
				stackLen := runtime.Stack(buf, true)
				log.Info().Msgf("=== GOROUTINE DUMP ===\n%s", buf[:stackLen])
			}
		}
	}()
	
	// Wait for termination signal
	<-sigChan
	log.Info().Msg("Shutting down...")

	// Shutdown proxy
	rpcProxy.Shutdown()

	// Remove the socket file
	if err := os.Remove(*listenSocket); err != nil {
		log.Warn().Err(err).Str("socket", *listenSocket).Msg("Failed to remove socket file on shutdown")
	} else {
		log.Debug().Str("socket", *listenSocket).Msg("Removed socket file")
	}
}

func setupLogging(level string, pretty bool) {
	// Set log level
	var logLevel zerolog.Level
	switch level {
	case "trace":
		logLevel = zerolog.TraceLevel
	case "debug":
		logLevel = zerolog.DebugLevel
	case "info":
		logLevel = zerolog.InfoLevel
	case "warn":
		logLevel = zerolog.WarnLevel
	case "error":
		logLevel = zerolog.ErrorLevel
	case "fatal":
		logLevel = zerolog.FatalLevel
	default:
		logLevel = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(logLevel)

	// Configure output format
	if pretty {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	} else {
		log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	}
}
