package main

import (
	"fmt"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func startEmbeddedNATS() (*server.Server, *nats.Conn, error) {
    opts := &server.Options{
        DontListen: true,
        JetStream:  true,
        StoreDir:   ".data/nats",
    }

    ns, err := server.NewServer(opts)
    if err != nil {
        return nil, nil, err
    }

    go ns.Start()

    if !ns.ReadyForConnections(5 * time.Second) {
        ns.Shutdown()
        return nil, nil, fmt.Errorf("NATS failed to start")
    }

    nc, err := nats.Connect(
        nats.DefaultURL,
        nats.InProcessServer(ns),
    )
    if err != nil {
        ns.Shutdown()
        return nil, nil, err
    }

    return ns, nc, nil
}