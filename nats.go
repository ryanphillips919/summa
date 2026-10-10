package main

import (
	"fmt"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func startEmbeddedNATS() (*server.Server, *nats.Conn, error) {
	// Configure the embedded NATS server.
	options := &server.Options{
		DontListen: true,        // Only allow in-process connections.
		JetStream:  true,        // Enable persistent messaging and KV storage.
		StoreDir:   ".data/nats", // Store JetStream data locally.
	}

	// Create and start the server.
	natsServer, err := server.NewServer(options)
	if err != nil {
		return nil, nil, fmt.Errorf("create NATS server: %w", err)
	}

	go natsServer.Start()

	// Wait until the server is ready to accept connections.
	if !natsServer.ReadyForConnections(5 * time.Second) {
		natsServer.Shutdown()
		return nil, nil, fmt.Errorf("NATS server failed to start")
	}

	// Connect directly to the embedded server without TCP.
	natsConn, err := nats.Connect(
		nats.DefaultURL,
		nats.InProcessServer(natsServer),
	)
	if err != nil {
		natsServer.Shutdown()
		return nil, nil, fmt.Errorf("connect to NATS: %w", err)
	}

	return natsServer, natsConn, nil
}