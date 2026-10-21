package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func startEmbeddedNATS(storeDir string, enableLogging bool) (*server.Server, *nats.Conn, error) {
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create NATS storage directory %q: %w", storeDir, err)
	}

	options := &server.Options{
		ServerName: "summa_embedded",
		DontListen: true,
		JetStream:  true,
		StoreDir:   storeDir,
	}
	ns, err := server.NewServer(options)
	if err != nil {
		return nil, nil, fmt.Errorf("create NATS server: %w", err)
	}
	if enableLogging {
		ns.ConfigureLogger()
	}

	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		ns.Shutdown()
		ns.WaitForShutdown()
		return nil, nil, fmt.Errorf("NATS server failed to start")
	}

	nc, err := nats.Connect(nats.DefaultURL, nats.InProcessServer(ns))
	if err != nil {
		ns.Shutdown()
		ns.WaitForShutdown()
		return nil, nil, fmt.Errorf("connect to NATS: %w", err)
	}

	return ns, nc, nil
}

func StartKV(nc *nats.Conn) (jetstream.KeyValue, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("create JetStream client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      "demo",
		Description: "key-value store for demo",
		History:     64,
	})
	if err != nil {
		return nil, fmt.Errorf("create demo KV bucket: %w", err)
	}
	return kv, nil
}

func shutdownEmbeddedNATS(nc *nats.Conn, ns *server.Server) {
	if nc != nil {
		if err := nc.Drain(); err != nil {
			nc.Close()
		}
	}
	if ns != nil {
		ns.Shutdown()
		ns.WaitForShutdown()
	}
}
