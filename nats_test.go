package main

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestBillPostedPublishSubscribe(t *testing.T) {
	// Start an embedded NATS server with temporary storage.
	natsServer, natsConn, err := startEmbeddedNATS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer natsServer.Shutdown()
	defer natsConn.Close()

	// Subscribe to notifications when a company's vendor bill is posted.
	const subject = "company.456.bill.posted"
	subscription, err := natsConn.SubscribeSync(subject)
	if err != nil {
		t.Fatal(err)
	}

	// Make sure the subscription is registered before publishing.
	if err := natsConn.Flush(); err != nil {
		t.Fatal(err)
	}

	// Simulate publishing an event after posting a vendor bill.
	const expected = `{
        "company_id": "456",
        "bill_id": "BILL-1042",
        "journal_id": "JE-9001",
        "amount_cents": 24000
    }`

	if err := natsConn.Publish(subject, []byte(expected)); err != nil {
		t.Fatal(err)
	}

	// Wait for the subscriber to receive the event.
	message, err := subscription.NextMsg(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Verify that the received event matches what was published.
	if got := string(message.Data); got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestBillPostedEventReachesAnotherClient(t *testing.T) {
	// Start the embedded NATS server and accounting client.
	natsServer, accountingConn, err := startEmbeddedNATS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer natsServer.Shutdown()
	defer accountingConn.Close()

	// Create a second client connection for the reporting module.
	reportingConn, err := nats.Connect(
		nats.DefaultURL,
		nats.InProcessServer(natsServer),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer reportingConn.Close()

	// The reporting module subscribes to vendor bill postings.
	const subject = "company.456.bill.posted"

	subscription, err := reportingConn.SubscribeSync(subject)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure the subscription is registered before publishing.
	if err := reportingConn.Flush(); err != nil {
		t.Fatal(err)
	}

	// The accounting module publishes a vendor bill posting event.
	const expected = `{
        "company_id": "456",
        "bill_id": "BILL-1042",
        "journal_id": "JE-9001",
        "amount_cents": 24000
    }`

	if err := accountingConn.Publish(subject, []byte(expected)); err != nil {
		t.Fatal(err)
	}

	// The reporting module should receive the event.
	message, err := subscription.NextMsg(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Verify that the event was delivered correctly.
	if got := string(message.Data); got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestSessionViewStateWatcher(t *testing.T) {
	// Start embedded NATS with isolated test storage.
	natsServer, natsConn, err := startEmbeddedNATS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer natsServer.Shutdown()
	defer natsConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create a JetStream KV bucket.
	js, err := jetstream.New(natsConn)
	if err != nil {
		t.Fatal(err)
	}

	kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: "session_views",
	})
	if err != nil {
		t.Fatal(err)
	}

	const key = "session.accountant123.bill.BILL-1042.tab"
	const initial = "details"
	const updated = "journal_entry"

	// Store the initial UI state.
	if _, err := kv.Put(ctx, key, []byte(initial)); err != nil {
		t.Fatal(err)
	}

	// Watch for changes to this session's selected tab.
	watcher, err := kv.Watch(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Stop()

	// Watchers first deliver the current value.
	select {
	case entry := <-watcher.Updates():
		if entry == nil || string(entry.Value()) != initial {
			t.Fatal("watcher did not receive initial state")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for initial state")
	}

	// A nil entry marks the end of the initial snapshot.
	select {
	case entry := <-watcher.Updates():
		if entry != nil {
			t.Fatal("expected end of initial snapshot")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for snapshot")
	}

	// Simulate switching to the Journal Entry tab.
	if _, err := kv.Put(ctx, key, []byte(updated)); err != nil {
		t.Fatal(err)
	}

	// Verify that the watcher receives the update.
	select {
	case entry := <-watcher.Updates():
		if entry == nil || string(entry.Value()) != updated {
			t.Fatal("watcher did not receive updated state")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for state update")
	}

	// Verify that KV stores the latest value.
	stored, err := kv.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	if got := string(stored.Value()); got != updated {
		t.Fatalf("expected %q, got %q", updated, got)
	}
}
