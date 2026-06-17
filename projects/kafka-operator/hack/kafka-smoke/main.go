// Command kafka-smoke is a standalone connectivity check for 담당자 1.
// It connects to a Kafka broker with franz-go and prints cluster metadata,
// proving the library + local docker-compose broker work before client.go.
//
//	docker compose -f hack/local/docker-compose.yaml up -d
//	go run ./hack/kafka-smoke            # defaults to localhost:9092
//	go run ./hack/kafka-smoke -brokers host:9092,host2:9092
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func main() {
	brokers := flag.String("brokers", "localhost:9092", "comma-separated bootstrap servers")
	flag.Parse()

	if err := run(strings.Split(*brokers, ",")); err != nil {
		fmt.Fprintln(os.Stderr, "smoke test FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("smoke test OK")
}

func run(brokers []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	defer cl.Close()

	// Ping forces an actual connection so unreachable brokers fail fast.
	if err := cl.Ping(ctx); err != nil {
		return fmt.Errorf("ping %v: %w", brokers, err)
	}

	adm := kadm.NewClient(cl)

	meta, err := adm.BrokerMetadata(ctx)
	if err != nil {
		return fmt.Errorf("broker metadata: %w", err)
	}
	fmt.Printf("connected to cluster %q (controller=%d)\n", meta.Cluster, meta.Controller)
	for _, b := range meta.Brokers {
		fmt.Printf("  broker %d at %s:%d\n", b.NodeID, b.Host, b.Port)
	}

	topics, err := adm.ListTopics(ctx)
	if err != nil {
		return fmt.Errorf("list topics: %w", err)
	}
	fmt.Printf("existing topics: %d\n", len(topics))
	for _, t := range topics.Sorted() {
		fmt.Printf("  - %s (%d partitions)\n", t.Topic, len(t.Partitions))
	}
	return nil
}
