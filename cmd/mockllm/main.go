package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kagent-dev/mockllm"
)

func main() {
	configPath := flag.String("config", "", "Path to mockllm JSON config file")
	listenAddr := flag.String("listen-addr", "", "Optional listen address override (for example 127.0.0.1:8080)")
	checkConfig := flag.Bool("check-config", false, "Validate the config file and exit without starting the server")
	flag.Parse()

	if *configPath == "" {
		flag.Usage()
		os.Exit(2)
	}

	config, err := mockllm.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *listenAddr != "" {
		config.ListenAddr = *listenAddr
	}
	if *checkConfig {
		fmt.Println("config valid")
		return
	}

	server := mockllm.NewServer(config)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	baseURL, err := server.Start(ctx)
	if err != nil {
		log.Fatalf("start server: %v", err)
	}

	fmt.Println(baseURL)

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Stop(shutdownCtx); err != nil {
		log.Printf("stop server: %v", err)
	}
}
