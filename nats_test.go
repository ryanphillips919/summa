package main

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
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