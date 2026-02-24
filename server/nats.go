package server

import (
	"fmt"
	"log"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// StartEmbeddedNATS starts an in-process NATS server and returns a client connection.
func StartEmbeddedNATS() (*natsserver.Server, *nats.Conn, error) {
	opts := &natsserver.Options{
		DontListen: true, // in-process only, no TCP listener
	}

	ns, err := natsserver.NewServer(opts)
	if err != nil {
		return nil, nil, fmt.Errorf("creating nats server: %w", err)
	}

	ns.Start()

	if !ns.ReadyForConnections(5 * time.Second) {
		return nil, nil, fmt.Errorf("nats server not ready")
	}

	nc, err := nats.Connect("", nats.InProcessServer(ns))
	if err != nil {
		ns.Shutdown()
		return nil, nil, fmt.Errorf("connecting to embedded nats: %w", err)
	}

	log.Printf("Embedded NATS server started (in-process)")
	return ns, nc, nil
}
