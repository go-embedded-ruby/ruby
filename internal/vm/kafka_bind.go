// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"sort"
	stdtime "time"

	kafka "github.com/go-ruby-kafka/kafka"

	gotime "github.com/go-composites/time/src"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent github.com/go-ruby-kafka/kafka library, a
// pure-Go (CGO=0) reimplementation of the ruby-kafka gem's client surface over
// twmb/franz-go. The library owns the whole client model — the Kafka.new factory,
// the buffering producer, the consumer-group poll loop and the topic-admin
// surface — while the Kafka wire protocol is franz-go's and, in the tests, the
// in-process kfake broker's. rbgo wraps each library object as a Ruby object
// reporting the matching Kafka::* class (see kafka.go for the class + method
// registration) and converts values across the boundary here.

// The wrapper types. Each holds a pointer into the library and carries the
// matching Kafka::* class (see classOf); the methods registered in kafka.go
// operate on the held value.

// KafkaClient wraps a *kafka.Kafka, the object returned by Kafka.new
// (Kafka::Client): the factory for producers/consumers and the admin surface.
type KafkaClient struct {
	k   *kafka.Kafka
	cls *RClass
}

// KafkaProducer wraps a *kafka.Producer (Kafka::Producer), the buffering
// producer returned by client.producer.
type KafkaProducer struct {
	p   *kafka.Producer
	cls *RClass
}

// KafkaConsumer wraps a *kafka.Consumer (Kafka::Consumer), the consumer-group
// member returned by client.consumer(group_id:).
type KafkaConsumer struct {
	c   *kafka.Consumer
	cls *RClass
}

// KafkaMessage wraps a consumed *kafka.Message (Kafka::Message).
type KafkaMessage struct {
	m   *kafka.Message
	cls *RClass
}

// KafkaBatch wraps a per-partition *kafka.Batch (Kafka::Batch).
type KafkaBatch struct {
	b   *kafka.Batch
	cls *RClass
}

func (v *KafkaClient) ToS() string       { return "#<Kafka::Client>" }
func (v *KafkaClient) Inspect() string   { return "#<Kafka::Client>" }
func (v *KafkaClient) Truthy() bool      { return true }
func (v *KafkaProducer) ToS() string     { return "#<Kafka::Producer>" }
func (v *KafkaProducer) Inspect() string { return "#<Kafka::Producer>" }
func (v *KafkaProducer) Truthy() bool    { return true }
func (v *KafkaConsumer) ToS() string     { return "#<Kafka::Consumer>" }
func (v *KafkaConsumer) Inspect() string { return "#<Kafka::Consumer>" }
func (v *KafkaConsumer) Truthy() bool    { return true }
func (v *KafkaMessage) ToS() string      { return "#<Kafka::Message>" }
func (v *KafkaMessage) Inspect() string  { return "#<Kafka::Message>" }
func (v *KafkaMessage) Truthy() bool     { return true }
func (v *KafkaBatch) ToS() string        { return "#<Kafka::Batch>" }
func (v *KafkaBatch) Inspect() string    { return "#<Kafka::Batch>" }
func (v *KafkaBatch) Truthy() bool       { return true }

// kafkaClass returns a registered Kafka::* class by its qualified name.
func (vm *VM) kafkaClass(name string) *RClass { return vm.consts[name].(*RClass) }

// kafkaMessage wraps a library *kafka.Message as a Ruby Kafka::Message.
func (vm *VM) kafkaMessage(m *kafka.Message) object.Value {
	return &KafkaMessage{m: m, cls: vm.kafkaClass("Kafka::Message")}
}

// --- argument parsing -------------------------------------------------------

// kafkaStrings coerces a broker/topic-list argument to a []string: an Array maps
// each element through #to_s; any scalar becomes a single-element slice. This
// lets seed_brokers accept both an Array (["h:9092", ...]) and a bare String.
func kafkaStrings(v object.Value) []string {
	if a, ok := v.(*object.Array); ok {
		out := make([]string, len(a.Elems))
		for i, e := range a.Elems {
			out[i] = e.ToS()
		}
		return out
	}
	return []string{v.ToS()}
}

// kafkaParseOptions reads Kafka.new's arguments: an optional positional seed
// broker list (Array or String) and a trailing keyword Hash (seed_brokers:,
// client_id:), mirroring the gem's Kafka.new(seed_brokers:, client_id:).
func kafkaParseOptions(args []object.Value) kafka.Options {
	var o kafka.Options
	for _, a := range args {
		if h, ok := a.(*object.Hash); ok {
			if v, ok := h.Get(object.Symbol("seed_brokers")); ok {
				o.SeedBrokers = append(o.SeedBrokers, kafkaStrings(v)...)
			}
			if v, ok := h.Get(object.Symbol("client_id")); ok {
				o.ClientID = v.ToS()
			}
			continue
		}
		o.SeedBrokers = append(o.SeedBrokers, kafkaStrings(a)...)
	}
	return o
}

// kafkaInt marshals a Ruby Integer to an int64, raising TypeError for anything
// else (partition / num_partitions / replication_factor arguments).
func kafkaInt(v object.Value) int64 {
	if n, ok := v.(object.Integer); ok {
		return int64(n)
	}
	raise("TypeError", "no implicit conversion of %s into Integer", v.Inspect())
	return 0
}

// valueBytes reads a message value/key argument as bytes: a String yields its
// raw contents, and any other value yields its #to_s bytes.
func valueBytes(v object.Value) []byte {
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	return []byte(v.ToS())
}

// kafkaProduceOptions reads producer.produce's keyword arguments
// (topic:, key:, partition:, headers:) into the library's ProduceOptions.
func kafkaProduceOptions(kw *object.Hash) kafka.ProduceOptions {
	var o kafka.ProduceOptions
	if kw == nil {
		return o
	}
	if v, ok := kw.Get(object.Symbol("topic")); ok {
		o.Topic = v.ToS()
	}
	if v, ok := kw.Get(object.Symbol("key")); ok && !object.IsNil(v) {
		o.Key = valueBytes(v)
	}
	if v, ok := kw.Get(object.Symbol("partition")); ok && !object.IsNil(v) {
		p := int32(kafkaInt(v))
		o.Partition = &p
	}
	if v, ok := kw.Get(object.Symbol("headers")); ok {
		if h, ok := v.(*object.Hash); ok {
			o.Headers = byteMapFromHash(h)
		}
	}
	return o
}

// byteMapFromHash converts a Ruby Hash of String→String into the library's
// record-header map, keying and valuing by each entry's bytes.
func byteMapFromHash(h *object.Hash) map[string][]byte {
	m := make(map[string][]byte, h.Len())
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		m[k.ToS()] = valueBytes(v)
	}
	return m
}

// --- Go → Ruby conversion ---------------------------------------------------

// byteMapToHash renders a record-header map as a Ruby Hash of String→String,
// ordered by key so the result is deterministic.
func byteMapToHash(m map[string][]byte) object.Value {
	h := object.NewHash()
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Set(object.NewString(k), object.NewStringBytes(m[k]))
	}
	return h
}

// stringArrayToRuby renders a []string (topic names) as a Ruby Array of String.
func stringArrayToRuby(ss []string) object.Value {
	elems := make([]object.Value, len(ss))
	for i, s := range ss {
		elems[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(elems)
}

// int32ArrayToRuby renders a []int32 (partition ids) as a Ruby Array of Integer.
func int32ArrayToRuby(ns []int32) object.Value {
	elems := make([]object.Value, len(ns))
	for i, n := range ns {
		elems[i] = object.IntValue(int64(n))
	}
	return object.NewArrayFromSlice(elems)
}

// kafkaTime renders a message CreateTime as a Ruby Time, whole-second resolution
// (matching the resolution the Time binding exposes through go-composites/time).
func kafkaTime(t stdtime.Time) object.Value {
	return &Time{t: gotime.FromUnix(t.Unix())}
}

// raiseKafkaError re-raises a library error as its matching Kafka::* exception.
// Every error the library returns wraps one of its sentinels, classified here by
// errors.Is; an unrecognised error raises the Kafka::Error base. It never returns
// (raise panics).
func raiseKafkaError(err error) {
	switch {
	case errors.Is(err, kafka.ErrDeliveryFailed):
		raise("Kafka::DeliveryFailed", "%s", err.Error())
	case errors.Is(err, kafka.ErrConnection):
		raise("Kafka::ConnectionError", "%s", err.Error())
	case errors.Is(err, kafka.ErrUnknownTopicOrPartition):
		raise("Kafka::UnknownTopicOrPartition", "%s", err.Error())
	case errors.Is(err, kafka.ErrOffsetCommit):
		raise("Kafka::OffsetCommitError", "%s", err.Error())
	case errors.Is(err, kafka.ErrOffsetOutOfRange):
		raise("Kafka::OffsetOutOfRange", "%s", err.Error())
	default:
		raise("Kafka::Error", "%s", err.Error())
	}
}
