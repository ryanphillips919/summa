package main

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// this code is taken directly from a Synadia video (company behind NATS) here
// https://youtu.be/cdTrl8UfcBo?si=dvRHpllKAdOdeliG
// basically starts our NATS server in embedded mode, but as you can see, we can disable it and start our NATS server anywhere!
func StartEmbeddedServer(inProcess, enableLogging bool) (*nats.Conn, *server.Server, error) {
	serverOpts := &server.Options{
		ServerName: "summa_embedded",
		DontListen: inProcess,
		JetStream:  true,
		StoreDir:   "./data/js",
	}
	ns, err := server.NewServer(serverOpts)
	if err != nil {
		return nil, nil, err
	}
	if enableLogging {
		ns.ConfigureLogger()
	}
	go ns.Start()

	if !ns.ReadyForConnections(5 * time.Second) {
		return nil, nil, errors.New("NATS failed to start")
	}

	clientOpts := []nats.Option{}
	if inProcess {
		clientOpts = append(clientOpts, nats.InProcessServer(ns))
	}

	nc, err := nats.Connect(nats.DefaultURL, clientOpts...)
	if err != nil {
		return nil, nil, err
	}

	return nc, ns, nil
}

// given a NATS connection, retun a jetstream key value store or an error if failed
func StartKV(nc *nats.Conn) (jetstream.KeyValue, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      "demo",
		Description: "key-value store for demo",
		History:     64,
	})
	if err != nil {
		return nil, err
	}
	return kv, nil
}
