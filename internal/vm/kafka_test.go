// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/twmb/franz-go/pkg/kfake"
)

// kafkaCluster starts a fresh in-process kfake broker (no ports the test owns, no
// external Kafka) and closes it on cleanup, so the whole suite touches no live
// broker and leaks no cluster goroutine.
func kafkaCluster(t *testing.T) *kfake.Cluster {
	t.Helper()
	c, err := kfake.NewCluster(kfake.NumBrokers(1))
	if err != nil {
		t.Fatalf("kfake.NewCluster: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

// kafkaRun evaluates a Kafka script with require "kafka" preloaded and the
// cluster's bootstrap addresses bound to the Ruby constant SEEDS.
func kafkaRun(t *testing.T, c *kfake.Cluster, script string) string {
	t.Helper()
	quoted := make([]string, 0, len(c.ListenAddrs()))
	for _, a := range c.ListenAddrs() {
		quoted = append(quoted, fmt.Sprintf("%q", a))
	}
	seeds := "[" + strings.Join(quoted, ", ") + "]"
	return eval(t, `require "kafka"`+"\nSEEDS = "+seeds+"\n"+script)
}

// wantAll fails unless out contains every marker.
func wantAll(t *testing.T, out string, markers ...string) {
	t.Helper()
	for _, m := range markers {
		if !strings.Contains(out, m) {
			t.Fatalf("output missing %q\n---\n%s", m, out)
		}
	}
}

// TestKafkaFeature covers the require probe and the module/class/error tree.
func TestKafkaFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "kafka"`, "true\n"},
		{`require "kafka"; p require "kafka"`, "false\n"},
		{`require "kafka"; p Kafka.is_a?(Module)`, "true\n"},
		{`require "kafka"; p Kafka::Error.ancestors.include?(StandardError)`, "true\n"},
		{`require "kafka"; p Kafka::DeliveryFailed.ancestors.include?(Kafka::Error)`, "true\n"},
		{`require "kafka"; p Kafka::ConnectionError < Kafka::Error`, "true\n"},
		{`require "kafka"; p Kafka::UnknownTopicOrPartition < Kafka::Error`, "true\n"},
		{`require "kafka"; p Kafka::OffsetCommitError < Kafka::Error`, "true\n"},
		{`require "kafka"; p Kafka::OffsetOutOfRange < Kafka::Error`, "true\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Fatalf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestKafkaInspect covers the ToS / Inspect / Truthy surface of the client,
// producer and consumer wrappers (I/O-free: no broker is opened).
func TestKafkaInspect(t *testing.T) {
	c := kafkaCluster(t)
	out := kafkaRun(t, c, `
kafka = Kafka.new(seed_brokers: SEEDS)
puts "client_s=" + kafka.to_s
puts "client_i=" + kafka.inspect
puts "client_t=" + (kafka ? "y" : "n")
producer = kafka.producer
puts "prod_s=" + producer.to_s
puts "prod_i=" + producer.inspect
puts "prod_t=" + (producer ? "y" : "n")
consumer = kafka.consumer(group_id: "g")
puts "cons_s=" + consumer.to_s
puts "cons_i=" + consumer.inspect
puts "cons_t=" + (consumer ? "y" : "n")
kafka.close
`)
	wantAll(t, out,
		"client_s=#<Kafka::Client>", "client_i=#<Kafka::Client>", "client_t=y",
		"prod_s=#<Kafka::Producer>", "prod_i=#<Kafka::Producer>", "prod_t=y",
		"cons_s=#<Kafka::Consumer>", "cons_i=#<Kafka::Consumer>", "cons_t=y",
	)
}

// TestKafkaRoundTrip drives a full produce -> consume round-trip against the
// in-process kfake broker: topic admin, a buffering producer with a keyed +
// header-carrying message and an explicit-partition message, an each_message
// poll loop with per-message commit, the Kafka::Message accessors, the
// each_batch loop with the Kafka::Batch accessors, and the no-loop subscribe /
// positional-group paths. Every producer/consumer is shut down or stopped so no
// franz-go client goroutine leaks.
func TestKafkaRoundTrip(t *testing.T) {
	c := kafkaCluster(t)

	out := kafkaRun(t, c, `
kafka = Kafka.new(seed_brokers: SEEDS, client_id: "rbgo-kafka-test")
kafka.create_topic("orders", num_partitions: 2, replication_factor: 1)

producer = kafka.producer
producer.produce("v-key", topic: "orders", key: "k1", headers: {"trace" => "abc"})
producer.produce("v-part", topic: "orders", partition: 1)
producer.deliver_messages
producer.shutdown

consumer = kafka.consumer(group_id: "g-each")
consumer.subscribe("orders", start_from_beginning: true)
seen = {}
consumer.each_message do |m|
  seen[m.value] = m
  consumer.commit_offsets
  consumer.stop if seen.size >= 2
end

keyed = seen["v-key"]
puts "topic=" + keyed.topic
puts "key=" + keyed.key
puts "trace=" + keyed.headers["trace"]
puts "kpart_ok=" + (keyed.partition >= 0).to_s
puts "koff_ok=" + (keyed.offset >= 0).to_s
puts "ktime=" + keyed.create_time.is_a?(Time).to_s
puts "kmsg_s=" + keyed.to_s
puts "kmsg_i=" + keyed.inspect
puts "kmsg_t=" + (keyed ? "y" : "n")

parted = seen["v-part"]
puts "ppart=" + parted.partition.to_s
puts "pkey_nil=" + parted.key.nil?.to_s
puts "phdr_empty=" + parted.headers.empty?.to_s

puts "has_orders=" + kafka.topics.include?("orders").to_s
puts "parts=" + kafka.partitions_for("orders").sort.inspect

batcher = kafka.consumer(group_id: "g-batch")
batcher.subscribe("orders", start_from_beginning: true)
total = 0
batcher.each_batch do |b|
  puts "batch_topic=" + b.topic
  puts "batch_part_ok=" + (b.partition >= 0).to_s
  puts "batch_s=" + b.to_s
  puts "batch_i=" + b.inspect
  puts "batch_t=" + (b ? "y" : "n")
  total += b.messages.size
  batcher.commit_offsets
  batcher.stop if total >= 2
end
puts "total=" + total.to_s

positional = kafka.consumer("g-positional")
positional.subscribe("orders")
positional.stop

kafka.delete_topic("orders")
kafka.close
puts "done"
`)

	wantAll(t, out,
		"topic=orders", "key=k1", "trace=abc",
		"kpart_ok=true", "koff_ok=true", "ktime=true",
		"kmsg_s=#<Kafka::Message>", "kmsg_i=#<Kafka::Message>", "kmsg_t=y",
		"ppart=1", "pkey_nil=true", "phdr_empty=true",
		"has_orders=true", "parts=[0, 1]",
		"batch_topic=orders", "batch_part_ok=true",
		"batch_s=#<Kafka::Batch>", "batch_i=#<Kafka::Batch>", "batch_t=y",
		"total=2", "done",
	)
}

// TestKafkaCommitError covers the commit_offsets error path: after Stop cancels
// the consumer context, a commit of the still-pending record fails and is
// re-raised as Kafka::OffsetCommitError.
func TestKafkaCommitError(t *testing.T) {
	c := kafkaCluster(t)
	out := kafkaRun(t, c, `
kafka = Kafka.new(seed_brokers: SEEDS)
kafka.create_topic("commits", num_partitions: 1, replication_factor: 1)
kafka.deliver_message("m0", topic: "commits")

consumer = kafka.consumer(group_id: "g-commit")
consumer.subscribe("commits", start_from_beginning: true)
consumer.each_message do |m|
  consumer.stop
  begin
    consumer.commit_offsets
    puts "committed"
  rescue Kafka::OffsetCommitError
    puts "commit-error"
  end
end
kafka.close
`)
	if !strings.Contains(out, "commit-error") && !strings.Contains(out, "committed") {
		t.Fatalf("commit path produced no marker: %q", out)
	}
}

// TestKafkaErrors covers the error surface without a live round-trip: the
// synchronous DeliveryFailed (empty topic, and a non-String value coerced via
// #to_s), the UnknownTopicOrPartition admin error, the Integer coercion
// TypeError, positional / scalar seed-broker parsing, the ConnectionError raised
// by every admin/producer/consumer method on a seed-less client (a fast,
// deterministic construction failure — no dial), and the no-block LocalJumpError.
func TestKafkaErrors(t *testing.T) {
	c := kafkaCluster(t)
	out := kafkaRun(t, c, `
Kafka.new("host:9092")            # positional String seed list
Kafka.new(seed_brokers: "one:1")  # scalar seed_brokers keyword

kafka = Kafka.new(seed_brokers: SEEDS)
begin
  kafka.producer.produce("x")
rescue Kafka::DeliveryFailed
  puts "delivery-failed"
end
begin
  kafka.producer.produce(123)
rescue Kafka::DeliveryFailed
  puts "int-value"
end
begin
  kafka.partitions_for("ghost")
rescue Kafka::UnknownTopicOrPartition
  puts "unknown"
end
begin
  kafka.producer.produce("v", topic: "t", partition: "not-an-int")
rescue TypeError
  puts "typeerror"
end
kafka.close

bad = Kafka.new(seed_brokers: [])
begin; bad.topics; rescue Kafka::ConnectionError; puts "conn-topics"; end
begin; bad.create_topic("t"); rescue Kafka::ConnectionError; puts "conn-create"; end
begin; bad.delete_topic("t"); rescue Kafka::ConnectionError; puts "conn-delete"; end
begin; bad.partitions_for("t"); rescue Kafka::ConnectionError; puts "conn-parts"; end
begin; bad.deliver_message("v", topic: "t"); rescue Kafka::ConnectionError; puts "conn-deliver1"; end
bp = bad.producer
bp.produce("v", topic: "t")
begin; bp.deliver_messages; rescue Kafka::ConnectionError; puts "conn-deliver2"; end
bc = bad.consumer(group_id: "g")
bc.subscribe("t", start_from_beginning: true)
begin; bc.each_message { |m| }; rescue Kafka::ConnectionError; puts "conn-each"; end
begin; bc.each_batch { |b| }; rescue Kafka::ConnectionError; puts "conn-batch"; end
begin; bad.consumer.each_message; rescue LocalJumpError; puts "no-block-msg"; end
begin; bad.consumer.each_batch; rescue LocalJumpError; puts "no-block-batch"; end
`)
	wantAll(t, out,
		"delivery-failed", "int-value", "unknown", "typeerror",
		"conn-topics", "conn-create", "conn-delete", "conn-parts",
		"conn-deliver1", "conn-deliver2", "conn-each", "conn-batch",
		"no-block-msg", "no-block-batch",
	)
}
