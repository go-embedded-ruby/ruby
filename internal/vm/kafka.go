// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	kafka "github.com/go-ruby-kafka/kafka"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerKafka installs the Kafka module (require "kafka"): the client factory
// Kafka.new(seed_brokers:, client_id:), the buffering Kafka::Producer (#produce/
// #deliver_messages/#shutdown), the consumer-group Kafka::Consumer (#subscribe/
// #each_message/#each_batch/#commit_offsets/#stop), the read-only Kafka::Message
// and Kafka::Batch, the topic-admin surface on Kafka::Client (#create_topic/
// #topics/#partitions_for/#delete_topic/#deliver_message) and the gem's error
// tree (Kafka::Error < StandardError, with DeliveryFailed/ConnectionError/
// UnknownTopicOrPartition/OffsetCommitError/OffsetOutOfRange beneath it). The
// ruby-kafka-faithful client core lives in the github.com/go-ruby-kafka/kafka
// library (over twmb/franz-go); this file is the class + method wiring (see
// kafka_bind.go for the wrappers and value conversions).
func (vm *VM) registerKafka() {
	mod := newClass("Kafka", nil)
	mod.isModule = true
	vm.consts["Kafka"] = mod

	vm.registerKafkaErrors(mod)

	cClient := vm.kafkaSub(mod, "Client")
	cProducer := vm.kafkaSub(mod, "Producer")
	cConsumer := vm.kafkaSub(mod, "Consumer")
	cMessage := vm.kafkaSub(mod, "Message")
	cBatch := vm.kafkaSub(mod, "Batch")

	mod.smethods["new"] = &Method{name: "new", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return &KafkaClient{k: kafka.New(kafkaParseOptions(args)), cls: cClient}
		}}

	vm.registerKafkaClient(cClient)
	vm.registerKafkaProducer(cProducer)
	vm.registerKafkaConsumer(cConsumer)
	vm.registerKafkaMessage(cMessage)
	vm.registerKafkaBatch(cBatch)
}

// kafkaSub creates a Kafka::* class under cObject, records it flat (for classOf)
// and nests it under the Kafka module by its simple name.
func (vm *VM) kafkaSub(mod *RClass, simple string) *RClass {
	qualified := "Kafka::" + simple
	c := newClass(qualified, vm.cObject)
	vm.consts[qualified] = c
	mod.consts[simple] = c
	return c
}

// registerKafkaErrors installs the Kafka error tree, mirroring ruby-kafka: the
// root Kafka::Error < StandardError and the failure-specific subclasses beneath
// it. Each class name matches the sentinel raiseKafkaError maps a library error
// to.
func (vm *VM) registerKafkaErrors(mod *RClass) {
	defs := []struct{ qualified, parent string }{
		{"Kafka::Error", "StandardError"},
		{"Kafka::DeliveryFailed", "Kafka::Error"},
		{"Kafka::ConnectionError", "Kafka::Error"},
		{"Kafka::UnknownTopicOrPartition", "Kafka::Error"},
		{"Kafka::OffsetCommitError", "Kafka::Error"},
		{"Kafka::OffsetOutOfRange", "Kafka::Error"},
	}
	for _, d := range defs {
		parent := vm.consts[d.parent].(*RClass)
		cls := newClass(d.qualified, parent)
		vm.consts[d.qualified] = cls
		mod.consts[d.qualified[len("Kafka::"):]] = cls
	}
}

// registerKafkaClient installs the Kafka::Client surface: the producer/consumer
// factories, the one-shot deliver_message, and the topic-admin methods.
func (vm *VM) registerKafkaClient(c *RClass) {
	clientOf := func(self object.Value) *kafka.Kafka { return self.(*KafkaClient).k }

	c.define("producer", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &KafkaProducer{p: clientOf(self).Producer(), cls: vm.kafkaClass("Kafka::Producer")}
	})
	c.define("consumer", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		group := ""
		for _, a := range args {
			switch x := a.(type) {
			case *object.Hash:
				if v, ok := x.Get(object.Symbol("group_id")); ok {
					group = v.ToS()
				}
			case *object.String:
				group = x.Str()
			}
		}
		return &KafkaConsumer{c: clientOf(self).Consumer(group), cls: vm.kafkaClass("Kafka::Consumer")}
	})
	c.define("deliver_message", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		topic := ""
		if kw, ok := lastHash(args); ok {
			if v, ok := kw.Get(object.Symbol("topic")); ok {
				topic = v.ToS()
			}
		}
		if err := clientOf(self).DeliverMessage(valueBytes(args[0]), topic); err != nil {
			raiseKafkaError(err)
		}
		return object.NilV
	})
	c.define("create_topic", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		parts, repl := int32(1), int16(1)
		if kw, ok := lastHash(args); ok {
			if v, ok := kw.Get(object.Symbol("num_partitions")); ok {
				parts = int32(kafkaInt(v))
			}
			if v, ok := kw.Get(object.Symbol("replication_factor")); ok {
				repl = int16(kafkaInt(v))
			}
		}
		if err := clientOf(self).CreateTopic(args[0].ToS(), parts, repl); err != nil {
			raiseKafkaError(err)
		}
		return object.NilV
	})
	c.define("topics", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		names, err := clientOf(self).Topics()
		if err != nil {
			raiseKafkaError(err)
		}
		return stringArrayToRuby(names)
	})
	c.define("partitions_for", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		parts, err := clientOf(self).PartitionsFor(args[0].ToS())
		if err != nil {
			raiseKafkaError(err)
		}
		return int32ArrayToRuby(parts)
	})
	c.define("delete_topic", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if err := clientOf(self).DeleteTopic(args[0].ToS()); err != nil {
			raiseKafkaError(err)
		}
		return object.NilV
	})
	c.define("close", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		clientOf(self).Close()
		return object.NilV
	})
}

// registerKafkaProducer installs the Kafka::Producer surface: the buffering
// produce, the deliver_messages flush and shutdown.
func (vm *VM) registerKafkaProducer(c *RClass) {
	producerOf := func(self object.Value) *kafka.Producer { return self.(*KafkaProducer).p }

	c.define("produce", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		kw, _ := lastHash(args)
		if err := producerOf(self).Produce(valueBytes(args[0]), kafkaProduceOptions(kw)); err != nil {
			raiseKafkaError(err)
		}
		return object.NilV
	})
	c.define("deliver_messages", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := producerOf(self).DeliverMessages(); err != nil {
			raiseKafkaError(err)
		}
		return object.NilV
	})
	c.define("shutdown", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		producerOf(self).Shutdown()
		return object.NilV
	})
}

// registerKafkaConsumer installs the Kafka::Consumer surface: subscribe, the
// each_message / each_batch poll loops, commit_offsets and stop.
func (vm *VM) registerKafkaConsumer(c *RClass) {
	consumerOf := func(self object.Value) *kafka.Consumer { return self.(*KafkaConsumer).c }

	c.define("subscribe", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		fromBeginning := false
		if kw, ok := lastHash(args); ok {
			if v, ok := kw.Get(object.Symbol("start_from_beginning")); ok {
				fromBeginning = v.Truthy()
			}
		}
		consumerOf(self).Subscribe(args[0].ToS(), fromBeginning)
		return object.NilV
	})
	c.define("each_message", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		err := consumerOf(self).EachMessage(func(m *kafka.Message) error {
			vm.callBlock(blk, []object.Value{vm.kafkaMessage(m)})
			return nil
		})
		if err != nil {
			raiseKafkaError(err)
		}
		return object.NilV
	})
	c.define("each_batch", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		err := consumerOf(self).EachBatch(func(b *kafka.Batch) error {
			vm.callBlock(blk, []object.Value{&KafkaBatch{b: b, cls: vm.kafkaClass("Kafka::Batch")}})
			return nil
		})
		if err != nil {
			raiseKafkaError(err)
		}
		return object.NilV
	})
	c.define("commit_offsets", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := consumerOf(self).CommitOffsets(); err != nil {
			raiseKafkaError(err)
		}
		return object.NilV
	})
	c.define("stop", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		consumerOf(self).Stop()
		return object.NilV
	})
}

// registerKafkaMessage installs the read-only Kafka::Message accessors.
func (vm *VM) registerKafkaMessage(c *RClass) {
	msgOf := func(self object.Value) *kafka.Message { return self.(*KafkaMessage).m }

	c.define("topic", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(msgOf(self).Topic)
	})
	c.define("partition", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(msgOf(self).Partition))
	})
	c.define("offset", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(msgOf(self).Offset)
	})
	c.define("key", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if k := msgOf(self).Key; k != nil {
			return object.NewStringBytes(k)
		}
		return object.NilV
	})
	c.define("value", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewStringBytes(msgOf(self).Value)
	})
	c.define("headers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return byteMapToHash(msgOf(self).Headers)
	})
	c.define("create_time", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return kafkaTime(msgOf(self).CreateTime)
	})
}

// registerKafkaBatch installs the read-only Kafka::Batch accessors.
func (vm *VM) registerKafkaBatch(c *RClass) {
	batchOf := func(self object.Value) *kafka.Batch { return self.(*KafkaBatch).b }

	c.define("topic", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(batchOf(self).Topic)
	})
	c.define("partition", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(batchOf(self).Partition))
	})
	c.define("messages", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		msgs := batchOf(self).Messages
		elems := make([]object.Value, len(msgs))
		for i, m := range msgs {
			elems[i] = vm.kafkaMessage(m)
		}
		return object.NewArrayFromSlice(elems)
	})
}
