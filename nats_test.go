package main

import (
	"testing"
	"time"
)

func TestNATSPublishSubscribe(t *testing.T) {
	// Give this test its own JetStream storage so it does not touch app data.
	natsServer, natsConn, err := startEmbeddedNATS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer natsServer.Shutdown()
	defer natsConn.Close()

	// Subscribe before publishing so this Core NATS subscriber receives it.
	const subject = "expenses.updated"
	subscription, err := natsConn.SubscribeSync(subject)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure NATS has registered the subscription before sending the message.
	if err := natsConn.Flush(); err != nil {
		t.Fatal(err)
	}

	const expected = "Expense 42 updated"
	if err := natsConn.Publish(subject, []byte(expected)); err != nil {
		t.Fatal(err)
	}

	message, err := subscription.NextMsg(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if got := string(message.Data); got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}
