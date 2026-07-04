// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"fmt"
	"testing"

	kafka "github.com/go-ruby-kafka/kafka"
)

// TestRaiseKafkaErrorMapping white-box-covers raiseKafkaError's classification of
// every library sentinel to its Kafka::* class, including the OffsetCommit and
// OffsetOutOfRange branches that are impractical to trigger deterministically
// through a live round-trip, plus the Kafka::Error default for an unrecognised
// error.
func TestRaiseKafkaErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("wrap: %w", kafka.ErrDeliveryFailed), "Kafka::DeliveryFailed"},
		{fmt.Errorf("wrap: %w", kafka.ErrConnection), "Kafka::ConnectionError"},
		{fmt.Errorf("wrap: %w", kafka.ErrUnknownTopicOrPartition), "Kafka::UnknownTopicOrPartition"},
		{fmt.Errorf("wrap: %w", kafka.ErrOffsetCommit), "Kafka::OffsetCommitError"},
		{fmt.Errorf("wrap: %w", kafka.ErrOffsetOutOfRange), "Kafka::OffsetOutOfRange"},
		{errors.New("something else"), "Kafka::Error"},
	}
	for _, tc := range cases {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("raiseKafkaError(%v) did not panic", tc.err)
				}
				re, ok := r.(RubyError)
				if !ok {
					t.Fatalf("panic is %T, want RubyError", r)
				}
				if re.Class != tc.want {
					t.Fatalf("raiseKafkaError(%v) = %s, want %s", tc.err, re.Class, tc.want)
				}
			}()
			raiseKafkaError(tc.err)
		}()
	}
}
